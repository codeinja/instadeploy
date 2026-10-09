package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// gitResolve returns the commit a branch (or tag) points to, using Git's
// smart HTTP protocol (what `git ls-remote` does), so the backend doesn't
// need git installed or a clone. token, if set, is a GitHub installation
// token for private repositories.
func gitResolve(ctx context.Context, repoURL, ref, token string) (string, error) {
	if err := validateGitURL(repoURL); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	url := strings.TrimSuffix(repoURL, "/") + "/info/refs?service=git-upload-pack"
	header := http.Header{"Git-Protocol": {"version=0"}}
	if token != "" {
		header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token)))
	}
	resp, err := safeGet(ctx, url, header)
	if err != nil {
		return "", fmt.Errorf("could not reach the repository: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		if token != "" {
			return "", errors.New("repository not found. Check the URL, and that your GitHub App is installed on this repository")
		}
		if githubRepoPath(repoURL) != "" {
			return "", errors.New("repository not found. Check the URL. If it's private, connect a GitHub App in Settings > GitHub and install it on this repository")
		}
		return "", errors.New("repository not found. Check the URL; private repositories are supported on GitHub only (through a GitHub App)")
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Type"), "git-upload-pack") {
		return "", fmt.Errorf("%s doesn't look like a Git repository (HTTP %d)", repoURL, resp.StatusCode)
	}

	want := map[string]bool{"refs/heads/" + ref: true, "refs/tags/" + ref + "^{}": true, "refs/tags/" + ref: true}
	found := ""
	r := bufio.NewReader(io.LimitReader(resp.Body, 16<<20))
	for {
		line, err := readPktLine(r)
		if err != nil {
			break
		}
		// "<sha> <ref>\x00capabilities" or "<sha> <ref>"
		line, _, _ = strings.Cut(strings.TrimSuffix(line, "\n"), "\x00")
		sha, name, ok := strings.Cut(line, " ")
		if !ok || len(sha) != 40 {
			continue
		}
		if _, err := hex.DecodeString(sha); err != nil {
			continue
		}
		if want[name] {
			found = sha
			if strings.HasPrefix(name, "refs/heads/") || strings.HasSuffix(name, "^{}") {
				return sha, nil
			}
		}
	}
	if found != "" {
		return found, nil
	}
	return "", fmt.Errorf("branch %q not found in the repository", ref)
}

// readPktLine reads one pkt-line: 4 hex digits of length, then data.
// "0000" (flush) lines return "".
func readPktLine(r *bufio.Reader) (string, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return "", err
	}
	var n int
	if _, err := fmt.Sscanf(string(hdr[:]), "%04x", &n); err != nil {
		return "", err
	}
	if n <= 4 {
		return "", nil
	}
	buf := make([]byte, n-4)
	_, err := io.ReadFull(r, buf)
	return string(buf), err
}

// pollGitDeployments redeploys deployments with auto deploy on when their
// branch moves to a new commit. It catches everything webhooks miss: apps
// without a webhook, deliveries that failed, pushes during a build.
func (s *Server) pollGitDeployments() {
	for range time.Tick(s.cfg.GitPollEvery) {
		list, err := s.autoDeployments(context.Background(), "", "")
		if err != nil {
			log.Printf("git poll: %v", err)
			continue
		}
		for _, d := range list {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			latest, err := s.resolveGitCommit(ctx, d.userID, d.Spec.Source.GitURL, d.Spec.Source.GitRef)
			if err == nil {
				_, err = s.queueAutoDeploy(ctx, d, latest, "poll")
			}
			if err != nil {
				log.Printf("git poll %s: %v", d.ID, err)
			}
			cancel()
		}
	}
}

// autoDeployments lists Git deployments with auto deploy on that aren't
// already deploying; optionally only one user's, on one branch.
func (s *Server) autoDeployments(ctx context.Context, uid, branch string) ([]*Deployment, error) {
	query := deploymentSelect + ` WHERE d.auto_deploy AND d.spec->'source'->>'kind' = 'git' AND d.status NOT IN ('DELETING', 'STOPPED')
		AND NOT EXISTS (SELECT 1 FROM agent_tasks t WHERE t.deployment_id = d.id AND t.type = 'DEPLOY' AND t.status IN ('PENDING', 'DISPATCHED'))`
	var args []any
	if uid != "" {
		query += ` AND p.user_id = $1 AND d.spec->'source'->>'git_branch' = $2`
		args = append(args, uid, branch)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*Deployment
	for rows.Next() {
		d, err := scanDeployment(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, d)
	}
	return list, rows.Err()
}

// queueAutoDeploy redeploys d at commit, unless that commit is already the
// current one or a deploy is already waiting (the next poll catches up once
// it's done). via is "poll" or "push". Reports whether it queued one.
func (s *Server) queueAutoDeploy(ctx context.Context, d *Deployment, commit, via string) (bool, error) {
	s.autoDeployMu.Lock()
	defer s.autoDeployMu.Unlock()
	var current string
	var busy bool
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(r.git_commit, ''),
		EXISTS (SELECT 1 FROM agent_tasks t WHERE t.deployment_id = d.id AND t.type = 'DEPLOY' AND t.status IN ('PENDING', 'DISPATCHED'))
		FROM deployments d LEFT JOIN deployment_revisions r ON r.id = d.current_revision_id WHERE d.id = $1`, d.ID).Scan(&current, &busy)
	if err != nil || busy || current == commit {
		return false, err
	}
	if _, err := s.deploy(ctx, d, d.Spec, "auto", "", commit, nil); err != nil {
		return false, err
	}
	how := "detected"
	if via == "push" {
		how = "pushed"
	}
	s.event(ctx, d.userID, "info", fmt.Sprintf("New commit %s %s on %s: redeploying %s", commit[:7], how, d.Spec.Source.GitRef, d.Name), withDeployment(d))
	return true, nil
}
