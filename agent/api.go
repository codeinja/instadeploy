package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// API is the client for the backend's /api/agent endpoints.
type API struct {
	server   string
	token    string
	http     *http.Client // normal requests
	longPoll *http.Client // GET /commands waits up to 25s for work
}

var errUnauthorized = errors.New("the server rejected this agent's token (was the agent revoked or deleted in the dashboard?)")

func NewAPI(server, token string) *API {
	return &API{
		server:   server,
		token:    token,
		http:     &http.Client{Timeout: 30 * time.Second},
		longPoll: &http.Client{Timeout: 60 * time.Second},
	}
}

// Types below mirror backend/tasks.go.

type Tunnel struct {
	Endpoint   string `json:"endpoint"`
	NewtID     string `json:"newt_id"`
	NewtSecret string `json:"newt_secret"`
}

type RegisterResponse struct {
	AgentID     string  `json:"agent_id"`
	Name        string  `json:"name"`
	Tunnel      *Tunnel `json:"tunnel"`
	TunnelError string  `json:"tunnel_error"`
}

type Task struct {
	ID         string        `json:"id"`
	Type       string        `json:"type"`
	Deployment *Deployment   `json:"deployment"`
	Logs       *LogsRequest  `json:"logs"`
	Analyze    *AnalyzeInput `json:"analyze"`
}

type Deployment struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Type          string         `json:"type"` // IMAGE, DOCKERFILE, COMPOSE
	Environment   string         `json:"environment"`
	RevisionID    string         `json:"revision_id"`
	Revision      int            `json:"revision"`
	ContainerName string         `json:"container_name"`
	Network       string         `json:"network"`
	Image         string         `json:"image"`
	BuildTag      string         `json:"build_tag"`
	ReuseImage    string         `json:"reuse_image"`
	Source        *Source        `json:"source"`
	Services      []Service      `json:"services"`
	Volumes       []Volume       `json:"volumes"`
	RestartPolicy string         `json:"restart_policy"`
	CPUs          float64        `json:"cpus"`
	MemoryMB      int            `json:"memory_mb"`
	RegistryAuths []RegistryAuth `json:"registry_auths"`
	DeleteVolumes bool           `json:"delete_volumes"`
}

type Source struct {
	Kind      string `json:"kind"` // git, inline
	GitURL    string `json:"git_url"`
	GitRef    string `json:"git_branch"`
	GitCommit string `json:"git_commit"`
	GitToken  string `json:"git_token"` // short-lived, for private GitHub repositories
	Path      string `json:"path"`
	Compose   string `json:"compose"`
}

type Service struct {
	Name        string            `json:"name"`
	Port        int               `json:"port"`
	Public      bool              `json:"public"`
	Alias       string            `json:"alias"`
	Env         map[string]string `json:"env"`
	HealthCheck *HealthCheck      `json:"health_check"`
}

type HealthCheck struct {
	Path            string `json:"path"`
	IntervalSeconds int    `json:"interval_seconds"`
}

type Volume struct {
	Name     string `json:"name"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only"`
}

type RegistryAuth struct {
	Server   string `json:"server"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type LogsRequest struct {
	ContainerName string `json:"container_name"`
	Service       string `json:"service"`
	Compose       bool   `json:"compose"`
	Tail          int    `json:"tail"`
}

type AnalyzeInput struct {
	GitURL    string `json:"git_url"`
	GitRef    string `json:"git_branch"`
	GitCommit string `json:"git_commit"`
	GitToken  string `json:"git_token"`
}

type ContainerReport struct {
	DeploymentID string          `json:"deployment_id"`
	Service      string          `json:"service"`
	ContainerID  string          `json:"container_id"`
	State        string          `json:"state"`
	Health       string          `json:"health"`
	Stats        *ContainerStats `json:"stats,omitempty"`
}

type LogEntry struct {
	Stream string `json:"stream"` // build or deploy
	Text   string `json:"text"`
}

func (a *API) Register(ctx context.Context, info SystemInfo) (RegisterResponse, error) {
	var out RegisterResponse
	err := a.call(ctx, a.http, "POST", "/api/agent/register", map[string]any{"system": info}, &out)
	return out, err
}

func (a *API) Heartbeat(ctx context.Context, info SystemInfo, containers []ContainerReport) error {
	if containers == nil {
		containers = []ContainerReport{}
	}
	return a.call(ctx, a.http, "POST", "/api/agent/heartbeat", map[string]any{"system": info, "containers": containers}, nil)
}

// Commands waits up to 25 seconds for work.
func (a *API) Commands(ctx context.Context) ([]Task, error) {
	var out struct {
		Commands []Task `json:"commands"`
	}
	err := a.call(ctx, a.longPoll, "GET", "/api/agent/commands?wait=25", nil, &out)
	return out.Commands, err
}

// ReportTask tells the backend how a task is going: status is RUNNING
// (with phase BUILDING or DEPLOYING), DONE or FAILED.
func (a *API) ReportTask(ctx context.Context, taskID, status, phase, errMsg string, result any) error {
	body := map[string]any{"status": status, "phase": phase, "error": errMsg}
	if result != nil {
		body["result"] = result
	}
	return a.call(ctx, a.http, "POST", "/api/agent/tasks/"+taskID+"/status", body, nil)
}

func (a *API) SendLogs(ctx context.Context, deploymentID, revisionID string, lines []LogEntry) error {
	return a.call(ctx, a.http, "POST", "/api/agent/deployments/"+deploymentID+"/logs",
		map[string]any{"revision_id": revisionID, "lines": lines}, nil)
}

func (a *API) call(ctx context.Context, client *http.Client, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.server+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return errUnauthorized
	}
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, bytes.TrimSpace(raw))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
