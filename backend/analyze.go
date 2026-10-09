package main

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Analysis helps the dashboard fill in the deploy form: which services a
// Compose file has, which ports they listen on, and which Dockerfiles exist.
// The agent re-validates everything before deploying (with `docker compose
// config`), so this only needs to be good enough for suggestions.

type ComposeService struct {
	Name        string   `json:"name"`
	Image       string   `json:"image"`
	Build       string   `json:"build,omitempty"` // build context, if the service is built
	Ports       []int    `json:"ports"`           // container ports from ports: and expose:
	Environment []string `json:"environment"`     // variable names only
	Volumes     []string `json:"volumes"`
	DependsOn   []string `json:"depends_on"`
	Healthcheck bool     `json:"healthcheck"`
}

type ComposeAnalysis struct {
	Services []ComposeService `json:"services"`
	Warnings []string         `json:"warnings"`
}

// analyzeCompose parses a Compose file. dockerfilePorts maps a build
// context directory to the ports its Dockerfile EXPOSEs.
func analyzeCompose(content string, dockerfilePorts map[string][]int) (*ComposeAnalysis, error) {
	var doc struct {
		Services map[string]map[string]any `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return nil, fmt.Errorf("this isn't a valid Compose file: %v", firstLine(err.Error()))
	}
	if len(doc.Services) == 0 {
		return nil, fmt.Errorf("the Compose file has no services")
	}

	out := &ComposeAnalysis{Warnings: []string{}}
	names := make([]string, 0, len(doc.Services))
	for name := range doc.Services {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		raw := doc.Services[name]
		svc := ComposeService{Name: name, Ports: []int{}, Environment: []string{}, Volumes: []string{}, DependsOn: []string{}}
		svc.Image, _ = raw["image"].(string)
		switch b := raw["build"].(type) {
		case string:
			svc.Build = b
		case map[string]any:
			svc.Build, _ = b["context"].(string)
			if svc.Build == "" {
				svc.Build = "."
			}
		}
		ports := map[int]bool{}
		for _, p := range asList(raw["ports"]) {
			if n := containerPort(p); n > 0 {
				ports[n] = true
			}
		}
		for _, p := range asList(raw["expose"]) {
			if n := containerPort(p); n > 0 {
				ports[n] = true
			}
		}
		if svc.Build != "" && dockerfilePorts != nil {
			for _, n := range dockerfilePorts[path.Clean(svc.Build)] {
				ports[n] = true
			}
		}
		for n := range ports {
			svc.Ports = append(svc.Ports, n)
		}
		sort.Ints(svc.Ports)

		switch env := raw["environment"].(type) {
		case map[string]any:
			for k := range env {
				svc.Environment = append(svc.Environment, k)
			}
		case []any:
			for _, e := range env {
				if s, ok := e.(string); ok {
					k, _, _ := strings.Cut(s, "=")
					svc.Environment = append(svc.Environment, k)
				}
			}
		}
		sort.Strings(svc.Environment)

		for _, v := range asList(raw["volumes"]) {
			switch vv := v.(type) {
			case string:
				svc.Volumes = append(svc.Volumes, vv)
			case map[string]any:
				src, _ := vv["source"].(string)
				dst, _ := vv["target"].(string)
				svc.Volumes = append(svc.Volumes, src+":"+dst)
			}
		}
		switch dep := raw["depends_on"].(type) {
		case []any:
			for _, x := range dep {
				if s, ok := x.(string); ok {
					svc.DependsOn = append(svc.DependsOn, s)
				}
			}
		case map[string]any:
			for k := range dep {
				svc.DependsOn = append(svc.DependsOn, k)
			}
		}
		sort.Strings(svc.DependsOn)
		_, svc.Healthcheck = raw["healthcheck"]

		out.Warnings = append(out.Warnings, composeWarnings(name, raw, svc.Volumes)...)
		out.Services = append(out.Services, svc)
	}
	return out, nil
}

// composeWarnings flags settings the agent will refuse, so the user finds
// out before deploying.
func composeWarnings(name string, raw map[string]any, volumes []string) []string {
	var w []string
	if v, _ := raw["privileged"].(bool); v {
		w = append(w, name+": privileged containers are not allowed")
	}
	for _, key := range []string{"network_mode", "pid", "ipc", "userns_mode"} {
		if v, _ := raw[key].(string); v == "host" || strings.HasPrefix(v, "container:") {
			w = append(w, fmt.Sprintf("%s: %s: %s is not allowed", name, key, v))
		}
	}
	for _, key := range []string{"cap_add", "devices", "security_opt"} {
		if raw[key] != nil {
			w = append(w, fmt.Sprintf("%s: %s is not allowed", name, key))
		}
	}
	for _, v := range volumes {
		src, _, _ := strings.Cut(v, ":")
		if strings.HasPrefix(src, "/") || strings.HasPrefix(src, "~") {
			w = append(w, fmt.Sprintf("%s: mounting host path %s is not allowed (use a named volume or a path inside the project)", name, src))
		}
		if strings.Contains(src, "docker.sock") {
			w = append(w, name+": mounting the Docker socket is not allowed")
		}
	}
	if raw["ports"] != nil {
		w = append(w, name+": published ports are ignored; make the service public instead")
	}
	return w
}

func asList(v any) []any {
	if l, ok := v.([]any); ok {
		return l
	}
	return nil
}

var interpDefaultRe = regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*:?-([^}]*)\}`)

// containerPort extracts the container side of a Compose port entry:
// 80, "80", "8080:80", "127.0.0.1:8080:80/tcp", {target: 80}, "${PORT:-3000}:3000".
func containerPort(v any) int {
	switch p := v.(type) {
	case int:
		return p
	case map[string]any:
		switch t := p["target"].(type) {
		case int:
			return t
		case string:
			n, _ := strconv.Atoi(t)
			return n
		}
	case string:
		s := interpDefaultRe.ReplaceAllString(p, "$1")
		s, proto, _ := strings.Cut(s, "/")
		if proto != "" && proto != "tcp" {
			return 0
		}
		parts := strings.Split(s, ":")
		n, err := strconv.Atoi(parts[len(parts)-1])
		if err != nil || !validPort(n) {
			return 0 // ranges like 3000-3005 aren't supported
		}
		return n
	}
	return 0
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
