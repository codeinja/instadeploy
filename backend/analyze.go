package main

import (
	"archive/zip"
	"bufio"
	"fmt"
	"io"
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

type DockerfileInfo struct {
	Path  string `json:"path"`
	Ports []int  `json:"ports"`
}

type UploadAnalysis struct {
	// A ZIP of a folder has every file under "folder/"; paths below are
	// relative to that folder, and the agent strips it when extracting.
	Root         string           `json:"root"`
	Files        int              `json:"files"`
	Dockerfiles  []DockerfileInfo `json:"dockerfiles"`
	ComposeFiles []string         `json:"compose_files"`
	// Analysis of the first Compose file found (the likely choice).
	Compose     *ComposeAnalysis `json:"compose,omitempty"`
	ComposePath string           `json:"compose_path,omitempty"`
}

var composeFileNames = map[string]bool{
	"compose.yaml": true, "compose.yml": true, "docker-compose.yaml": true, "docker-compose.yml": true,
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

// dockerfileExposes reads EXPOSE instructions from a Dockerfile.
func dockerfileExposes(r io.Reader) []int {
	ports := map[int]bool{}
	sc := bufio.NewScanner(io.LimitReader(r, 1<<20))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || !strings.EqualFold(fields[0], "EXPOSE") {
			continue
		}
		for _, f := range fields[1:] {
			if n := containerPort(f); n > 0 {
				ports[n] = true
			}
		}
	}
	out := []int{}
	for n := range ports {
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}

const (
	maxZipFiles     = 20000
	maxZipUnpacked  = 2 << 30 // 2 GB
	maxAnalyzedFile = 1 << 20
)

// checkZip rejects archives that could escape the build directory or
// explode on extraction. The agent repeats these checks when it extracts.
func checkZip(zr *zip.Reader) error {
	if len(zr.File) > maxZipFiles {
		return fmt.Errorf("the ZIP has too many files (max %d)", maxZipFiles)
	}
	var total uint64
	for _, f := range zr.File {
		if err := safeArchivePath(f.Name); err != nil {
			return err
		}
		if f.Mode()&0o170000 == 0o120000 {
			return fmt.Errorf("the ZIP contains a symbolic link (%s), which isn't supported", f.Name)
		}
		total += f.UncompressedSize64
		if total > maxZipUnpacked {
			return fmt.Errorf("the ZIP unpacks to more than 2 GB")
		}
	}
	return nil
}

func safeArchivePath(name string) error {
	clean := path.Clean(strings.ReplaceAll(name, "\\", "/"))
	if strings.HasPrefix(clean, "/") || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(name, "\x00") {
		return fmt.Errorf("the ZIP contains an unsafe path: %q", name)
	}
	return nil
}

// zipRoot handles ZIPs made by zipping a folder ("my-app/Dockerfile"):
// it returns "my-app/" if every file is inside that one folder.
func zipRoot(zr *zip.Reader) string {
	root := ""
	for _, f := range zr.File {
		first, _, ok := strings.Cut(f.Name, "/")
		if !ok {
			return "" // a file at the top level
		}
		if root == "" {
			root = first
		} else if root != first {
			return ""
		}
	}
	if root == "" || root == "__MACOSX" {
		return ""
	}
	return root + "/"
}

func analyzeZip(zr *zip.Reader) (*UploadAnalysis, error) {
	if err := checkZip(zr); err != nil {
		return nil, err
	}
	root := zipRoot(zr)
	a := &UploadAnalysis{Root: root, Dockerfiles: []DockerfileInfo{}, ComposeFiles: []string{}}
	dockerfilePorts := map[string][]int{}
	composeContent := map[string]string{}

	for _, f := range zr.File {
		if f.FileInfo().IsDir() || strings.HasPrefix(f.Name, "__MACOSX/") {
			continue
		}
		a.Files++
		rel := strings.TrimPrefix(f.Name, root)
		base := path.Base(rel)
		if strings.Contains(rel, "node_modules/") || strings.Contains(rel, ".git/") {
			continue
		}
		isDockerfile := base == "Dockerfile" || strings.HasSuffix(base, ".Dockerfile") || strings.HasPrefix(base, "Dockerfile.")
		if !isDockerfile && !composeFileNames[base] {
			continue
		}
		if f.UncompressedSize64 > maxAnalyzedFile {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("the ZIP is damaged: %w", err)
		}
		body, _ := io.ReadAll(io.LimitReader(rc, maxAnalyzedFile))
		rc.Close()
		if isDockerfile {
			ports := dockerfileExposes(strings.NewReader(string(body)))
			a.Dockerfiles = append(a.Dockerfiles, DockerfileInfo{Path: rel, Ports: ports})
			if base == "Dockerfile" {
				dockerfilePorts[path.Dir(rel)] = ports
			}
		} else {
			a.ComposeFiles = append(a.ComposeFiles, rel)
			composeContent[rel] = string(body)
		}
	}
	sort.Slice(a.Dockerfiles, func(i, j int) bool { return depth(a.Dockerfiles[i].Path) < depth(a.Dockerfiles[j].Path) })
	sort.Slice(a.ComposeFiles, func(i, j int) bool { return depth(a.ComposeFiles[i]) < depth(a.ComposeFiles[j]) })

	if len(a.ComposeFiles) > 0 {
		a.ComposePath = a.ComposeFiles[0]
		// Build contexts in a Compose file are relative to the file.
		dir := path.Dir(a.ComposePath)
		rel := map[string][]int{}
		for ctx, ports := range dockerfilePorts {
			if r, err := relPath(dir, ctx); err == nil {
				rel[r] = ports
			}
		}
		if c, err := analyzeCompose(composeContent[a.ComposePath], rel); err == nil {
			a.Compose = c
		}
	}
	return a, nil
}

func depth(p string) int { return strings.Count(p, "/") }

func relPath(base, target string) (string, error) {
	if base == "." {
		return path.Clean(target), nil
	}
	if target == base {
		return ".", nil
	}
	if strings.HasPrefix(target, base+"/") {
		return strings.TrimPrefix(target, base+"/"), nil
	}
	return "", fmt.Errorf("outside")
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
