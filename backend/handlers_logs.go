package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type logLine struct {
	ID         int64     `json:"id"`
	RevisionID *string   `json:"revision_id"`
	Stream     string    `json:"stream"`
	Line       string    `json:"line"`
	CreatedAt  time.Time `json:"created_at"`
}

func (s *Server) queryLogs(r *http.Request, deploymentID string, after int64, limit int) ([]logLine, error) {
	q := `SELECT id, revision_id, stream, line, created_at FROM deployment_logs WHERE deployment_id = $1 AND id > $2`
	args := []any{deploymentID, after}
	if rev := r.URL.Query().Get("revision"); validUUID(rev) {
		q += ` AND revision_id = $3`
		args = append(args, rev)
	}
	rows, err := s.db.QueryContext(r.Context(), q+fmt.Sprintf(` ORDER BY id LIMIT %d`, limit), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lines := []logLine{}
	for rows.Next() {
		var l logLine
		if err := rows.Scan(&l.ID, &l.RevisionID, &l.Stream, &l.Line, &l.CreatedAt); err != nil {
			return nil, err
		}
		lines = append(lines, l)
	}
	return lines, rows.Err()
}

// handleDeploymentLogs returns build/deploy log lines after ?after=<id>.
func (s *Server) handleDeploymentLogs(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDeployment(w, r)
	if !ok {
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	lines, err := s.queryLogs(r, d.ID, after, 5000)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, lines)
}

// handleDeploymentLogStream streams new log lines as Server-Sent Events.
// It polls the database, which is plenty for a handful of viewers and
// needs no extra infrastructure.
func (s *Server) handleDeploymentLogStream(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDeployment(w, r)
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if id, err := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64); err == nil {
		after = id
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	tick := time.NewTicker(700 * time.Millisecond)
	defer tick.Stop()
	idle := 0
	for {
		lines, err := s.queryLogs(r, d.ID, after, 1000)
		if err != nil {
			return
		}
		for _, l := range lines {
			b, _ := json.Marshal(l)
			fmt.Fprintf(w, "id: %d\ndata: %s\n\n", l.ID, b)
			after = l.ID
		}
		if len(lines) > 0 {
			idle = 0
			flusher.Flush()
		} else if idle++; idle%20 == 0 {
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
		}
	}
}

// handleContainerLogs asks the agent for a service's recent container
// output (like `docker logs --tail`). Secret values are redacted.
func (s *Server) handleContainerLogs(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDeployment(w, r)
	if !ok {
		return
	}
	service := chi.URLParam(r, "service")
	found := false
	for _, svc := range d.Services {
		found = found || svc.Name == service
	}
	if !found {
		writeError(w, http.StatusNotFound, "service not found")
		return
	}
	if d.AgentStatus != "ONLINE" {
		writeError(w, http.StatusConflict, "This machine is currently offline, so its logs can't be read.")
		return
	}
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	if tail <= 0 || tail > 5000 {
		tail = 300
	}
	req := AgentLogsRequest{ContainerName: containerName(d.Name, d.ID), Service: service, Compose: d.Type == TypeCompose, Tail: tail}
	taskID, err := s.createTask(r.Context(), s.db, d.AgentID, d.ID, "", TaskLogs, req)
	if err != nil {
		internalError(w, err)
		return
	}
	s.notifyAgent(d.AgentID)

	status, result, errMsg, err := s.waitForTask(r.Context(), taskID, 20*time.Second)
	if err != nil {
		writeError(w, http.StatusGatewayTimeout, "The machine didn't send the logs in time. Try again.")
		return
	}
	if status == TaskFailed {
		writeError(w, http.StatusBadGateway, errMsg)
		return
	}
	var out struct {
		Lines string `json:"lines"`
	}
	json.Unmarshal(result, &out)
	lines := strings.Split(strings.TrimRight(out.Lines, "\n"), "\n")
	if out.Lines == "" {
		lines = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"service": service, "lines": lines})
}
