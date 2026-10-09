package main

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// Spec is the internal description of a deployment. Every deployment type
// (a Docker image, a Dockerfile, a Compose project) is described with it,
// and the agent receives a resolved version of it (see tasks.go).
//
// Example:
//
//	{
//	  "type": "IMAGE",
//	  "image": "nginx:latest",
//	  "services": [{"name": "web", "port": 80, "public": true}],
//	  "volumes": [{"source": "html", "target": "/usr/share/nginx/html"}]
//	}
//
// Specs never contain secret values: variables and secrets are stored
// separately (encrypted) and resolved when a deployment runs.
type Spec struct {
	Type string `json:"type"`

	// IMAGE: the image to run, e.g. "nginx:latest" or "ghcr.io/acme/api:1.2".
	Image string `json:"image,omitempty"`

	// DOCKERFILE and COMPOSE: where the files come from.
	Source *Source `json:"source,omitempty"`

	// One entry for IMAGE/DOCKERFILE ("web"), one per Compose service.
	Services []ServiceSpec `json:"services"`

	// IMAGE/DOCKERFILE only; Compose files declare their own.
	Volumes       []VolumeSpec `json:"volumes,omitempty"`
	RestartPolicy string       `json:"restart_policy,omitempty"`
	CPUs          float64      `json:"cpus,omitempty"`
	MemoryMB      int          `json:"memory_mb,omitempty"`
}

type Source struct {
	// git, or inline (Compose YAML pasted or opened from a single file).
	// Deployments made before ZIP uploads were removed may still say
	// "upload"; they can't be redeployed until they switch to Git.
	Kind   string `json:"kind"`
	GitURL string `json:"git_url,omitempty"`
	GitRef string `json:"git_branch,omitempty"`
	// Path of the Dockerfile or Compose file inside the repository.
	Path    string `json:"path,omitempty"`
	Compose string `json:"compose,omitempty"` // inline Compose YAML
}

type ServiceSpec struct {
	Name        string       `json:"name"`
	Port        int          `json:"port,omitempty"`
	Public      bool         `json:"public"`
	HealthCheck *HealthCheck `json:"health_check,omitempty"`
}

// HealthCheck is an optional HTTP check for services without a Docker
// HEALTHCHECK. The agent requests Path on the service's port.
type HealthCheck struct {
	Path            string `json:"path"`
	IntervalSeconds int    `json:"interval_seconds"`
}

type VolumeSpec struct {
	// A named volume ("postgres-data") or, if the agent allows it, an
	// absolute host path.
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only,omitempty"`
}

const (
	TypeImage      = "IMAGE"
	TypeDockerfile = "DOCKERFILE"
	TypeCompose    = "COMPOSE"
)

var restartPolicies = map[string]bool{"": true, "no": true, "always": true, "unless-stopped": true, "on-failure": true}
var environments = map[string]bool{"development": true, "staging": true, "production": true}

func (s *Spec) Validate() error {
	switch s.Type {
	case TypeImage:
		if !validImage(s.Image) {
			return fmt.Errorf("%q is not a valid Docker image name (for example nginx:latest)", s.Image)
		}
	case TypeDockerfile, TypeCompose:
		if s.Source == nil {
			return fmt.Errorf("choose where the %s comes from", strings.ToLower(s.Type))
		}
		if err := s.Source.validate(s.Type); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown deployment type %q", s.Type)
	}

	if len(s.Services) == 0 {
		return fmt.Errorf("a deployment needs at least one service")
	}
	if s.Type != TypeCompose && len(s.Services) != 1 {
		return fmt.Errorf("%s deployments have exactly one service", strings.ToLower(s.Type))
	}
	seen := map[string]bool{}
	for _, svc := range s.Services {
		if !serviceNameRe.MatchString(svc.Name) {
			return fmt.Errorf("invalid service name %q", svc.Name)
		}
		if seen[svc.Name] {
			return fmt.Errorf("service %q is listed twice", svc.Name)
		}
		seen[svc.Name] = true
		if svc.Port != 0 && !validPort(svc.Port) {
			return fmt.Errorf("service %s: port must be between 1 and 65535", svc.Name)
		}
		if svc.Public && svc.Port == 0 {
			return fmt.Errorf("service %s is public, so choose which port to expose", svc.Name)
		}
		if hc := svc.HealthCheck; hc != nil {
			if !strings.HasPrefix(hc.Path, "/") || len(hc.Path) > 200 || strings.ContainsAny(hc.Path, " \r\n") {
				return fmt.Errorf("service %s: health check path must start with / (for example /health)", svc.Name)
			}
			if hc.IntervalSeconds < 5 || hc.IntervalSeconds > 3600 {
				return fmt.Errorf("service %s: health check interval must be 5-3600 seconds", svc.Name)
			}
			if svc.Port == 0 {
				return fmt.Errorf("service %s: an HTTP health check needs a port", svc.Name)
			}
		}
	}

	if s.Type == TypeCompose && (len(s.Volumes) > 0 || s.CPUs != 0 || s.MemoryMB != 0) {
		return fmt.Errorf("for Compose deployments, set volumes and limits in the Compose file")
	}
	targets := map[string]bool{}
	for _, v := range s.Volumes {
		if err := validateVolume(v); err != nil {
			return err
		}
		if targets[v.Target] {
			return fmt.Errorf("two volumes are mounted at %s", v.Target)
		}
		targets[v.Target] = true
	}
	if !restartPolicies[s.RestartPolicy] {
		return fmt.Errorf("restart policy must be one of: no, always, unless-stopped, on-failure")
	}
	if s.CPUs < 0 || s.CPUs > 256 {
		return fmt.Errorf("CPU limit must be between 0 and 256")
	}
	if s.MemoryMB < 0 || (s.MemoryMB > 0 && s.MemoryMB < 6) || s.MemoryMB > 1<<20 {
		return fmt.Errorf("memory limit must be at least 6 MB")
	}
	return nil
}

var errUploadsRemoved = errors.New("ZIP uploads are no longer supported. Deploy from a Git repository instead (private GitHub repositories work through a GitHub App, see Settings > GitHub)")

func (src *Source) validate(typ string) error {
	switch src.Kind {
	case "upload":
		return errUploadsRemoved
	case "git":
		if err := validateGitURL(src.GitURL); err != nil {
			return err
		}
		if src.GitRef == "" || !gitRefRe.MatchString(src.GitRef) {
			return fmt.Errorf("enter a valid branch name")
		}
	case "inline":
		if typ != TypeCompose {
			return fmt.Errorf("only Compose files can be provided inline")
		}
		if strings.TrimSpace(src.Compose) == "" {
			return fmt.Errorf("paste or upload a Compose file")
		}
		if len(src.Compose) > 512*1024 {
			return fmt.Errorf("Compose file is too large (max 512 KB)")
		}
	default:
		return fmt.Errorf("unknown source %q", src.Kind)
	}
	if src.Kind != "inline" {
		if err := validateRelPath(src.Path); err != nil {
			return err
		}
	}
	return nil
}

// validateRelPath accepts a path like "Dockerfile" or "docker/compose.yaml"
// that stays inside the project.
func validateRelPath(p string) error {
	if p == "" {
		return nil
	}
	clean := path.Clean(p)
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") || strings.ContainsAny(p, "\\\x00") || len(p) > 255 {
		return fmt.Errorf("%q must be a path inside the project, like Dockerfile or deploy/compose.yaml", p)
	}
	return nil
}

func validateVolume(v VolumeSpec) error {
	if !path.IsAbs(v.Target) || path.Clean(v.Target) == "/" || strings.ContainsAny(v.Target, ":\x00") {
		return fmt.Errorf("volume target %q must be an absolute path inside the container, like /data", v.Target)
	}
	if strings.HasPrefix(v.Source, "/") {
		// Host paths are only honoured if the agent's operator allows them
		// (INSTA_DEPLOY_ALLOWED_HOST_PATHS); the agent enforces that.
		if path.Clean(v.Source) != v.Source || strings.ContainsAny(v.Source, ":\x00") {
			return fmt.Errorf("invalid host path %q", v.Source)
		}
		return nil
	}
	if !volumeNameRe.MatchString(v.Source) {
		return fmt.Errorf("volume name %q may only contain letters, numbers, dots, dashes and underscores", v.Source)
	}
	return nil
}

// Names on the agent's machine. They all derive from the deployment's name
// and ID, so they stay the same across redeploys (and Pangolin routes keep
// pointing at the right place).

func shortID(id string) string { return strings.ReplaceAll(id, "-", "")[:8] }

// containerName is the container for IMAGE/DOCKERFILE deployments, and the
// Compose project name for COMPOSE deployments.
func containerName(name, id string) string { return "insta-" + name + "-" + shortID(id) }

// targetHost is how the tunnel reaches a service on the shared network.
func targetHost(d *Deployment, service string) string {
	if d.Type == TypeCompose {
		return containerName(d.Name, d.ID) + "-" + service
	}
	return containerName(d.Name, d.ID)
}

// projectNetwork is the Docker network shared by the deployments of one project and
// environment (on the same agent), so they reach each other privately by name without
// being public. Other projects, and other environments of this one, are not on it.
func projectNetwork(d *Deployment) string {
	return "insta-p-" + shortID(d.ProjectID) + "-" + d.Environment
}

// privateHost is the name other deployments of the project use to reach a service on
// the project network: the deployment name for a single container, and
// "<service>.<deployment>" for a Compose service (e.g. "iris" or "legion.legion").
func privateHost(d *Deployment, service string) string {
	if d.Type == TypeCompose {
		return strings.ToLower(service) + "." + d.Name
	}
	return d.Name
}

// dockerVolumeName prefixes named volumes per deployment so two
// deployments that both use "data" don't share it by accident.
func dockerVolumeName(deploymentID, volume string) string {
	return "insta-" + shortID(deploymentID) + "-" + volume
}

func builtImageTag(deploymentID string, revision int) string {
	return fmt.Sprintf("insta-deploy/%s:r%d", shortID(deploymentID), revision)
}
