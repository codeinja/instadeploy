package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Compose deployments:
//
//  1. `docker compose config --format json` normalizes the user's file
//     (interpolation, defaults, relative paths made absolute).
//  2. validateCompose rejects settings that would give containers access to
//     the host (privileged, host network, host paths, the Docker socket...).
//  3. transformCompose drops published host ports, joins only the public
//     services to the tunnel network, and adds variables and labels.
//  4. The result is written next to the project and run with
//     `docker compose up -d`.

type composeProject map[string]any

// normalizeCompose runs `docker compose config` on the project's file.
// env is used for ${VAR} interpolation (the user's variables, never the
// agent's own environment).
func normalizeCompose(ctx context.Context, projectDir, file, projectName string, env map[string]string) (composeProject, error) {
	var envList []string
	for k, v := range env {
		envList = append(envList, k+"="+v)
	}
	config := func(extra ...string) (composeProject, error) {
		args := append([]string{"compose", "--project-directory", projectDir, "-f", filepath.Join(projectDir, file), "-p", projectName,
			"config", "--format", "json"}, extra...)
		out, err := command{name: "docker", args: args, dir: projectDir, env: envList}.output(ctx)
		if err != nil {
			return nil, fmt.Errorf("the Compose file is not valid: %v", err)
		}
		var p composeProject
		if err := json.Unmarshal(out, &p); err != nil {
			return nil, fmt.Errorf("could not read the normalized Compose file: %w", err)
		}
		return p, nil
	}

	// Compose reads env_file (and label_file) contents while normalizing,
	// so check where every referenced file points before running it.
	if err := precheckComposeFiles(projectDir, filepath.Join(projectDir, file), 0); err != nil {
		return nil, err
	}
	return config()
}

// precheckComposeFiles reads a Compose file as plain YAML and makes sure
// every file it pulls in (env_file, label_file, include, extends) is inside
// the project. Otherwise a file like /proc/1/environ could be copied into a
// container's environment.
func precheckComposeFiles(projectDir, path string, depth int) error {
	if depth > 10 {
		return fmt.Errorf("Compose files include each other too deeply")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", filepath.Base(path), err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return fmt.Errorf("the Compose file is not valid YAML: %v", err)
	}
	dir := filepath.Dir(path)
	check := func(what, ref string) (string, error) {
		if strings.Contains(ref, "$") {
			return "", fmt.Errorf("%s %q can't use variables", what, ref)
		}
		p := ref
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		if !isWithin(projectDir, p) {
			return "", fmt.Errorf("%s %s must be inside the project", what, ref)
		}
		return p, nil
	}

	services, _ := doc["services"].(map[string]any)
	for name, v := range services {
		svc, _ := v.(map[string]any)
		for _, key := range []string{"env_file", "label_file"} {
			for _, f := range envFiles(svc[key]) {
				if _, err := check(name+": "+key, f); err != nil {
					return err
				}
			}
		}
		if ext, ok := svc["extends"].(map[string]any); ok {
			if f, ok := ext["file"].(string); ok {
				p, err := check(name+": extends file", f)
				if err != nil {
					return err
				}
				if err := precheckComposeFiles(projectDir, p, depth+1); err != nil {
					return err
				}
			}
		}
	}
	includes, _ := doc["include"].([]any)
	for _, inc := range includes {
		var paths, envs []string
		switch x := inc.(type) {
		case string:
			paths = []string{x}
		case map[string]any:
			paths = envFiles(x["path"])
			envs = envFiles(x["env_file"])
		}
		for _, f := range envs {
			if _, err := check("include env_file", f); err != nil {
				return err
			}
		}
		for _, f := range paths {
			p, err := check("include", f)
			if err != nil {
				return err
			}
			if err := precheckComposeFiles(projectDir, p, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

// envFiles lists file references written as "x.env", ["a.env"] or
// [{path: "a.env"}].
func envFiles(v any) []string {
	var out []string
	switch e := v.(type) {
	case string:
		out = append(out, e)
	case []any:
		for _, x := range e {
			switch y := x.(type) {
			case string:
				out = append(out, y)
			case map[string]any:
				if p, ok := y["path"].(string); ok {
					out = append(out, p)
				}
			}
		}
	}
	return out
}

func (p composeProject) services() map[string]map[string]any {
	out := map[string]map[string]any{}
	if svcs, ok := p["services"].(map[string]any); ok {
		for name, v := range svcs {
			if m, ok := v.(map[string]any); ok {
				out[name] = m
			}
		}
	}
	return out
}

func (p composeProject) serviceNames() []string {
	var names []string
	for name := range p.services() {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type composePolicy struct {
	projectDir       string
	allowedHostPaths []string
}

// validateCompose returns every problem found, so the user can fix them
// all at once.
func validateCompose(p composeProject, pol composePolicy) []string {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	for _, name := range p.serviceNames() {
		svc := p.services()[name]
		if v, _ := svc["privileged"].(bool); v {
			add("%s: privileged containers are not allowed", name)
		}
		for _, key := range []string{"network_mode", "pid", "ipc", "uts", "userns_mode", "cgroup"} {
			v, _ := svc[key].(string)
			if v == "host" || strings.HasPrefix(v, "container:") {
				add("%s: %s: %s is not allowed", name, key, v)
			}
		}
		for _, key := range []string{"cap_add", "devices", "device_cgroup_rules", "cgroup_parent"} {
			if svc[key] != nil {
				add("%s: %s is not allowed", name, key)
			}
		}
		for _, opt := range asStrings(svc["security_opt"]) {
			if !strings.HasPrefix(opt, "no-new-privileges") {
				add("%s: security_opt %q is not allowed", name, opt)
			}
		}
		for _, vf := range asStrings(svc["volumes_from"]) {
			if strings.HasPrefix(vf, "container:") {
				add("%s: volumes_from a container outside this project is not allowed", name)
			}
		}
		for _, v := range asMaps(svc["volumes"]) {
			typ, _ := v["type"].(string)
			src, _ := v["source"].(string)
			if typ == "bind" {
				if msg := checkHostPath(src, pol); msg != "" {
					add("%s: %s", name, msg)
				}
			}
		}
		if b, ok := svc["build"].(map[string]any); ok {
			bctx, _ := b["context"].(string)
			if !filepath.IsAbs(bctx) || !isWithin(pol.projectDir, bctx) {
				add("%s: the build context must be a folder inside the project", name)
			}
		}
	}
	// File-based secrets and configs must come from the project.
	for _, section := range []string{"secrets", "configs"} {
		entries, _ := p[section].(map[string]any)
		for key, v := range entries {
			if m, ok := v.(map[string]any); ok {
				if f, _ := m["file"].(string); f != "" {
					if msg := checkHostPath(f, pol); msg != "" {
						add("%s %s: %s", section, key, msg)
					}
				}
			}
		}
	}
	return problems
}

// checkHostPath allows paths inside the project folder, or inside a folder
// the machine's owner allowed with INSTA_DEPLOY_ALLOWED_HOST_PATHS.
func checkHostPath(src string, pol composePolicy) string {
	if strings.Contains(src, "docker.sock") {
		return "mounting the Docker socket is not allowed"
	}
	if isWithin(pol.projectDir, src) {
		return ""
	}
	for _, allowed := range pol.allowedHostPaths {
		if isWithin(allowed, src) {
			return ""
		}
	}
	return fmt.Sprintf("mounting host path %s is not allowed (use a named volume or a path inside the project)", src)
}

// transformCompose adapts the project for Insta Deploy. Returns an error
// if a public service can't join the tunnel network.
func transformCompose(p composeProject, d *Deployment) error {
	byName := map[string]Service{}
	for _, s := range d.Services {
		byName[s.Name] = s
	}
	services := p.services()
	anyPublic := false
	for name, svc := range services {
		// Public access goes through Pangolin. Publishing host ports would
		// expose services on the machine's network and clash between
		// projects, so they're removed.
		delete(svc, "ports")

		labels, _ := svc["labels"].(map[string]any)
		if labels == nil {
			labels = map[string]any{}
		}
		labels[labelManaged] = "true"
		labels[labelDeploymentID] = d.ID
		labels[labelService] = name

		want := byName[name]
		if want.HealthCheck != nil && want.Port > 0 {
			labels[labelHealthPath] = want.HealthCheck.Path
			labels[labelHealthPort] = strconv.Itoa(want.Port)
			labels[labelHealthEvery] = strconv.Itoa(want.HealthCheck.IntervalSeconds)
		}
		svc["labels"] = labels

		// Insta Deploy variables override values from the Compose file.
		if len(want.Env) > 0 {
			env, _ := svc["environment"].(map[string]any)
			if env == nil {
				env = map[string]any{}
			}
			for k, v := range want.Env {
				env[k] = v
			}
			svc["environment"] = env
		}

		if want.Public {
			if mode, _ := svc["network_mode"].(string); mode != "" && mode != "bridge" {
				return fmt.Errorf("%s is public, but it uses network_mode: %s, so the tunnel can't reach it", name, mode)
			}
			networks, _ := svc["networks"].(map[string]any)
			if networks == nil {
				networks = map[string]any{"default": nil}
			}
			networks[d.Network] = map[string]any{"aliases": []string{want.Alias}}
			svc["networks"] = networks
			anyPublic = true
		}
	}
	if anyPublic {
		nets, _ := p["networks"].(map[string]any)
		if nets == nil {
			nets = map[string]any{}
		}
		nets[d.Network] = map[string]any{"name": d.Network, "external": true}
		p["networks"] = nets
	}
	for name := range byName {
		if _, ok := services[name]; !ok && byName[name].Public {
			return fmt.Errorf("the Compose file has no service called %q", name)
		}
	}
	return nil
}

// composePorts lists each service's container ports (from ports: and
// expose:), for the dashboard's port detection.
func composePorts(svc map[string]any) []int {
	set := map[int]bool{}
	for _, p := range asMaps(svc["ports"]) {
		if t, ok := p["target"].(float64); ok {
			set[int(t)] = true
		}
	}
	for _, e := range asStrings(svc["expose"]) {
		port, _, _ := strings.Cut(e, "/")
		if n, err := strconv.Atoi(port); err == nil {
			set[n] = true
		}
	}
	ports := []int{}
	for n := range set {
		ports = append(ports, n)
	}
	sort.Ints(ports)
	return ports
}

func writeComposeFile(p composeProject, path string) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600) // contains variable values
}

func asMaps(v any) []map[string]any {
	var out []map[string]any
	if l, ok := v.([]any); ok {
		for _, x := range l {
			if m, ok := x.(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	return out
}

func asStrings(v any) []string {
	var out []string
	switch l := v.(type) {
	case []any:
		for _, x := range l {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	case map[string]any: // volumes_from etc. can't be maps, but be lenient
		for k := range l {
			out = append(out, k)
		}
	}
	return out
}
