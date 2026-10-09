package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// POST /api/analyze/image {"image": "nginx:latest", "agent_id": "..."}
func (s *Server) handleAnalyzeImage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Image   string `json:"image"`
		AgentID string `json:"agent_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Image = strings.TrimSpace(req.Image)
	if !validImage(req.Image) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("%q is not a valid Docker image name", req.Image))
		return
	}
	arch := ""
	if a, err := s.userAgent(r.Context(), userID(r), req.AgentID); err == nil {
		arch = a.Arch
	}
	auths, err := s.registryAuths(r.Context(), userID(r))
	if err != nil {
		internalError(w, err)
		return
	}
	info, err := inspectImage(r.Context(), req.Image, auths, arch)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("Could not inspect %s: %v", req.Image, err))
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// POST /api/analyze/compose {"content": "services: ..."}
func (s *Server) handleAnalyzeCompose(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	a, err := analyzeCompose(req.Content, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.enrichComposePorts(r.Context(), userID(r), a)
	writeJSON(w, http.StatusOK, a)
}

// POST /api/analyze/git {"git_url", "git_branch", "agent_id"} resolves the
// branch, then asks the agent to clone it and report what it finds.
func (s *Server) handleAnalyzeGit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GitURL  string `json:"git_url"`
		GitRef  string `json:"git_branch"`
		AgentID string `json:"agent_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.GitRef == "" {
		req.GitRef = "main"
	}
	if !gitRefRe.MatchString(req.GitRef) {
		writeError(w, http.StatusBadRequest, "enter a valid branch name")
		return
	}
	commit, err := s.resolveGitCommit(r.Context(), userID(r), req.GitURL, req.GitRef)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	agent, err := s.userAgent(r.Context(), userID(r), req.AgentID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "choose a machine first")
		return
	}
	if agent.Status != "ONLINE" {
		writeError(w, http.StatusConflict, "This machine is currently offline. It needs to be online to read the repository.")
		return
	}
	taskID, err := s.createTask(r.Context(), s.db, agent.ID, "", "", TaskAnalyze, AgentAnalyze{GitURL: req.GitURL, GitRef: req.GitRef, GitCommit: commit})
	if err != nil {
		internalError(w, err)
		return
	}
	s.notifyAgent(agent.ID)
	status, result, errMsg, err := s.waitForTask(r.Context(), taskID, 90*time.Second)
	if err != nil {
		writeError(w, http.StatusGatewayTimeout, "The machine took too long to read the repository.")
		return
	}
	if status == TaskFailed {
		writeError(w, http.StatusUnprocessableEntity, errMsg)
		return
	}
	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil || out == nil {
		writeError(w, http.StatusBadGateway, "the agent sent an unexpected answer")
		return
	}
	out["git_commit"] = commit
	writeJSON(w, http.StatusOK, out)
}

var errNoAgent = errors.New("agent not found")

// enrichComposePorts fills in ports for services whose Compose entry
// doesn't list any, from their image's metadata (EXPOSE). Lookups run in
// parallel and give up quickly; this is only a suggestion for the form.
func (s *Server) enrichComposePorts(ctx context.Context, uid string, a *ComposeAnalysis) {
	auths, _ := s.registryAuths(ctx, uid)
	var wg sync.WaitGroup
	for i := range a.Services {
		svc := &a.Services[i]
		if len(svc.Ports) > 0 || !validImage(svc.Image) {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			ictx, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			if info, err := inspectImage(ictx, svc.Image, auths, ""); err == nil {
				svc.Ports = info.ExposedPorts
			}
		}()
	}
	wg.Wait()
}
