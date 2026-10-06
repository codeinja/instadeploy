package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func (s *Server) uploadsDir() string { return filepath.Join(s.cfg.DataDir, "uploads") }

// handleUpload accepts a ZIP build context (multipart field "file"),
// checks it, stores it in DATA_DIR/uploads and returns what's inside.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	limit := s.cfg.MaxUploadMB << 20
	r.Body = http.MaxBytesReader(w, r.Body, limit+1<<20)
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "send the file as multipart/form-data")
		return
	}
	var part interface {
		io.Reader
		FileName() string
		FormName() string
	}
	for {
		p, err := mr.NextPart()
		if err != nil {
			writeError(w, http.StatusBadRequest, "no file in the upload")
			return
		}
		if p.FormName() == "file" {
			part = p
			break
		}
	}
	filename := filepath.Base(part.FileName())
	if !strings.HasSuffix(strings.ToLower(filename), ".zip") {
		writeError(w, http.StatusBadRequest, "upload a .zip file")
		return
	}

	if err := os.MkdirAll(s.uploadsDir(), 0o700); err != nil {
		internalError(w, err)
		return
	}
	tmp, err := os.CreateTemp(s.uploadsDir(), "incoming-*.zip")
	if err != nil {
		internalError(w, err)
		return
	}
	defer os.Remove(tmp.Name()) // no-op once renamed
	defer tmp.Close()

	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(part, limit+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "upload interrupted: "+err.Error())
		return
	}
	if n > limit {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("the file is larger than %d MB", s.cfg.MaxUploadMB))
		return
	}

	zr, err := zip.NewReader(tmp, n)
	if err != nil {
		writeError(w, http.StatusBadRequest, "this isn't a valid ZIP file")
		return
	}
	analysis, err := analyzeZip(zr)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if analysis.Compose != nil {
		s.enrichComposePorts(r.Context(), userID(r), analysis.Compose)
	}
	analysisJSON, _ := json.Marshal(analysis)

	var id string
	err = s.db.QueryRowContext(r.Context(), `INSERT INTO uploads (user_id, filename, size, sha256, analysis) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		userID(r), trunc(filename, 255), n, hex.EncodeToString(hash.Sum(nil)), analysisJSON).Scan(&id)
	if err != nil {
		internalError(w, err)
		return
	}
	tmp.Close()
	if err := os.Rename(tmp.Name(), filepath.Join(s.uploadsDir(), id+".zip")); err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "filename": filename, "size": n, "analysis": analysis})
}

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
	commit, err := gitResolve(r.Context(), req.GitURL, req.GitRef)
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
