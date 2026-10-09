package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Builds, Compose and Git go through their CLIs: Docker has no Compose
// API, and BuildKit's API output isn't human-readable. Each command is a
// fixed binary with an argument list (never a shell string), runs with a
// minimal environment, and streams its output line by line.

type command struct {
	name string
	args []string
	dir  string
	// Environment for the command. The agent's own environment (including
	// its token) is never passed on, so a Compose file can't read it with
	// ${INSTA_DEPLOY_TOKEN}.
	env []string
	// Called for every output line (stdout and stderr).
	onLine func(string)
}

// run executes the command and returns its combined output's last lines
// in the error if it fails.
func (c command) run(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, c.name, c.args...)
	cmd.Dir = c.dir
	cmd.Env = append([]string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=" + os.TempDir(),
		"DOCKER_HOST=unix://" + dockerSocket,
		"GIT_TERMINAL_PROMPT=0",
	}, c.env...)

	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s is not available on this machine: %w", c.name, err)
	}

	var tail []string
	var mu sync.Mutex
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			mu.Lock()
			tail = append(tail, line)
			if len(tail) > 20 {
				tail = tail[1:]
			}
			mu.Unlock()
			if c.onLine != nil {
				c.onLine(line)
			}
		}
		io.Copy(io.Discard, pr)
	}()
	err := cmd.Wait()
	pw.Close()
	<-done
	if err != nil {
		mu.Lock()
		defer mu.Unlock()
		return fmt.Errorf("%w\n%s", err, strings.Join(tail, "\n"))
	}
	return nil
}

// output runs the command and returns stdout (stderr is discarded unless
// it fails).
func (c command) output(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.name, c.args...)
	cmd.Dir = c.dir
	cmd.Env = append([]string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=" + os.TempDir(),
		"DOCKER_HOST=unix://" + dockerSocket,
	}, c.env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s", strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// dockerConfigDir writes a temporary Docker CLI config with registry
// credentials, so builds and Compose pulls can use private images.
func dockerConfigDir(auths []RegistryAuth) (string, func(), error) {
	dir, err := os.MkdirTemp("", "insta-docker-config-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	entries := map[string]any{}
	for _, a := range auths {
		server := a.Server
		if server == "docker.io" {
			server = "https://index.docker.io/v1/"
		}
		entries[server] = map[string]string{"auth": base64.StdEncoding.EncodeToString([]byte(a.Username + ":" + a.Password))}
	}
	b, _ := json.Marshal(map[string]any{"auths": entries})
	if err := os.WriteFile(filepath.Join(dir, "config.json"), b, 0o600); err != nil {
		cleanup()
		return "", nil, err
	}
	return dir, cleanup, nil
}

// buildImage runs `docker build` (BuildKit) with plain progress output.
func buildImage(ctx context.Context, contextDir, dockerfile, tag string, auths []RegistryAuth, onLine func(string)) error {
	cfg, cleanup, err := dockerConfigDir(auths)
	if err != nil {
		return err
	}
	defer cleanup()
	args := []string{"build", "--progress=plain", "-t", tag}
	if dockerfile != "" {
		args = append(args, "-f", filepath.Join(contextDir, dockerfile))
	}
	args = append(args, "--", contextDir)
	return command{name: "docker", args: args, env: []string{"DOCKER_CONFIG=" + cfg, "DOCKER_BUILDKIT=1"}, onLine: onLine}.run(ctx)
}

// gitClone makes a shallow clone of one branch, then checks out the exact
// commit the backend resolved (if given). token, if set, is a short-lived
// GitHub installation token for a private repository. It's passed through
// the environment (not the URL or arguments), so it never shows up in the
// process list, git's output or the clone's .git/config.
func gitClone(ctx context.Context, url, branch, commit, token, dest string, onLine func(string)) (string, error) {
	var env []string
	if token != "" {
		auth := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
		env = []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.extraHeader", "GIT_CONFIG_VALUE_0=Authorization: Basic " + auth}
	}
	err := command{name: "git", args: []string{"clone", "--depth", "1", "--branch", branch, "--single-branch", "--", url, dest}, env: env, onLine: onLine}.run(ctx)
	if err != nil {
		return "", fmt.Errorf("git clone failed: %w", err)
	}
	head, err := command{name: "git", args: []string{"rev-parse", "HEAD"}, dir: dest}.output(ctx)
	if err != nil {
		return "", err
	}
	got := strings.TrimSpace(string(head))
	if commit != "" && got != commit {
		// The branch moved since the backend looked; fetch the exact commit.
		if err := (command{name: "git", args: []string{"fetch", "--depth", "1", "origin", commit}, dir: dest, env: env, onLine: onLine}).run(ctx); err != nil {
			return "", fmt.Errorf("could not fetch commit %s: %w", commit, err)
		}
		if err := (command{name: "git", args: []string{"checkout", "--detach", commit}, dir: dest}).run(ctx); err != nil {
			return "", err
		}
		got = commit
	}
	os.RemoveAll(filepath.Join(dest, ".git"))
	return got, nil
}
