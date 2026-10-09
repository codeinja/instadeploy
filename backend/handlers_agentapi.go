package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// The agent API is called by the Insta Deploy agent on the user's machine.
// The agent always connects outbound to us; we never connect to it.

type agentSystemInfo struct {
	Hostname      string `json:"hostname"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	DockerVersion string `json:"docker_version"`
	AgentVersion  string `json:"agent_version"`
}

func (s *Server) saveAgentInfo(ctx context.Context, id string, info agentSystemInfo) {
	if info.Hostname == "" && info.DockerVersion == "" {
		return
	}
	s.db.ExecContext(ctx, `UPDATE agents SET hostname = $1, os = $2, arch = $3, docker_version = $4, agent_version = $5 WHERE id = $6`,
		trunc(info.Hostname, 255), trunc(info.OS, 100), trunc(info.Arch, 50), trunc(info.DockerVersion, 50), trunc(info.AgentVersion, 50), id)
}

func (s *Server) handleAgentRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		System agentSystemInfo `json:"system"`
	}
	json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req) // body is optional
	ctx := r.Context()
	id := agentID(r)

	var name, uid string
	if err := s.db.QueryRowContext(ctx, `SELECT name, user_id FROM agents WHERE id = $1`, id).Scan(&name, &uid); err != nil {
		internalError(w, err)
		return
	}
	s.saveAgentInfo(ctx, id, req.System)

	// The agent (re)started, so anything it was working on was lost.
	// Hand it out again; deploys are safe to repeat.
	s.db.ExecContext(ctx, `UPDATE agent_tasks SET status = 'PENDING', dispatched_at = NULL
		WHERE agent_id = $1 AND status = 'DISPATCHED' AND type NOT IN ('LOGS', 'ANALYZE')`, id)

	tunnel := s.ensureTunnel(ctx, id)
	var tunnelErr string
	s.db.QueryRowContext(ctx, `SELECT tunnel_error FROM agents WHERE id = $1`, id).Scan(&tunnelErr)
	s.event(ctx, uid, "success", fmt.Sprintf("Agent %s connected (%s, Docker %s)", name, req.System.Hostname, req.System.DockerVersion), withAgent(id))

	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id":     id,
		"name":         name,
		"tunnel":       tunnel,
		"tunnel_error": tunnelErr,
	})
}

type containerReport struct {
	DeploymentID string `json:"deployment_id"`
	Service      string `json:"service"`
	ContainerID  string `json:"container_id"`
	State        string `json:"state"`  // Docker state: running, exited, restarting, ...
	Health       string `json:"health"` // HEALTHY, UNHEALTHY, STARTING, NONE
	Stats        *Stats `json:"stats"`
}

func (s *Server) handleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		System     agentSystemInfo   `json:"system"`
		Containers []containerReport `json:"containers"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	aid := agentID(r)
	s.saveAgentInfo(ctx, aid, req.System)

	// Update each service's state, health and current stats.
	seen := map[string]bool{}
	for _, c := range req.Containers {
		if !validUUID(c.DeploymentID) {
			continue
		}
		if c.Service == "" {
			c.Service = "web" // containers created before services existed
		}
		var serviceID string
		err := s.db.QueryRowContext(ctx, `UPDATE services s SET state = $1, health = $2, container_id = $3, updated_at = now()
			FROM deployments d WHERE s.deployment_id = d.id AND d.agent_id = $4 AND d.id = $5 AND s.name = $6
			RETURNING s.id`, trunc(c.State, 30), healthOrNone(c.Health), trunc(c.ContainerID, 100), aid, c.DeploymentID, c.Service).Scan(&serviceID)
		if err != nil {
			continue
		}
		seen[c.DeploymentID] = true
		if c.Stats != nil {
			s.stats.set(serviceID, *c.Stats)
		}
	}

	// A RUNNING deployment whose containers all stopped (or vanished) has
	// failed. Skip recently changed ones: a deploy may have finished after
	// the agent took this snapshot.
	rows, err := s.db.QueryContext(ctx, `SELECT d.id, d.name, p.user_id,
		COUNT(sv.id) FILTER (WHERE sv.state = 'running')
		FROM deployments d JOIN projects p ON p.id = d.project_id LEFT JOIN services sv ON sv.deployment_id = d.id
		WHERE d.agent_id = $1 AND d.status = $2 AND d.updated_at < now() - interval '30 seconds'
		  AND NOT EXISTS (SELECT 1 FROM agent_tasks t WHERE t.deployment_id = d.id AND t.status IN ('PENDING', 'DISPATCHED') AND t.type <> 'LOGS')
		GROUP BY d.id, d.name, p.user_id`, aid, StatusRunning)
	if err != nil {
		internalError(w, err)
		return
	}
	type failed struct{ id, name, uid string }
	var failures []failed
	for rows.Next() {
		var f failed
		var running int
		if rows.Scan(&f.id, &f.name, &f.uid, &running) == nil && (!seen[f.id] || running == 0) {
			failures = append(failures, f)
		}
	}
	rows.Close()
	for _, f := range failures {
		msg := "The container stopped unexpectedly. Check the container logs."
		if !seen[f.id] {
			msg = "The container no longer exists on this machine."
			s.db.ExecContext(ctx, `UPDATE services SET state = 'missing' WHERE deployment_id = $1`, f.id)
		}
		s.setDeploymentStatus(ctx, f.id, StatusFailed, msg)
		s.event(ctx, f.uid, "error", f.name+": "+msg, eventRefs{deploymentID: f.id})
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func healthOrNone(h string) string {
	switch h {
	case "HEALTHY", "UNHEALTHY", "STARTING":
		return h
	}
	return "NONE"
}

// handleAgentCommands long-polls: it returns as soon as there's work, or
// after ?wait seconds (max 30) with an empty list.
func (s *Server) handleAgentCommands(w http.ResponseWriter, r *http.Request) {
	aid := agentID(r)
	wait, _ := strconv.Atoi(r.URL.Query().Get("wait"))
	wait = max(0, min(wait, 30))
	deadline := time.Now().Add(time.Duration(wait) * time.Second)
	wake := s.notifier.channel(aid)

	for {
		tasks, err := s.dispatchTasks(r.Context(), aid)
		if err != nil {
			internalError(w, err)
			return
		}
		remaining := time.Until(deadline)
		if len(tasks) > 0 || remaining <= 0 {
			if tasks == nil {
				tasks = []AgentTask{}
			}
			writeJSON(w, http.StatusOK, map[string]any{"commands": tasks})
			return
		}
		select {
		case <-wake:
		case <-time.After(remaining):
		case <-r.Context().Done():
			return
		}
	}
}

type taskReport struct {
	// RUNNING (still working; Phase says how far), DONE or FAILED.
	Status string `json:"status"`
	// For deploys: BUILDING or DEPLOYING.
	Phase  string          `json:"phase"`
	Error  string          `json:"error"`
	Result json.RawMessage `json:"result"`
}

type deployResult struct {
	ImageDigest string `json:"image_digest"`
	GitCommit   string `json:"git_commit"`
	Services    []struct {
		Name          string `json:"name"`
		ContainerID   string `json:"container_id"`
		State         string `json:"state"`
		Image         string `json:"image"`
		DetectedPorts []int  `json:"detected_ports"`
	} `json:"services"`
}

func (s *Server) handleAgentTaskStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	var req taskReport
	if !decodeJSON(w, r, &req) {
		return
	}

	var typ, status string
	var depID, revID sql.NullString
	err := sql.ErrNoRows
	if validUUID(id) {
		err = s.db.QueryRowContext(ctx, `SELECT type, status, deployment_id, revision_id FROM agent_tasks WHERE id = $1 AND agent_id = $2`,
			id, agentID(r)).Scan(&typ, &status, &depID, &revID)
	}
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	if status == TaskCancelled || status == TaskDone || status == TaskFailed {
		writeJSON(w, http.StatusOK, map[string]any{"ignored": true, "status": status})
		return
	}

	var d *Deployment
	var secrets []string
	if depID.Valid {
		if d, err = s.getDeploymentByID(ctx, depID.String); err != nil {
			internalError(w, err)
			return
		}
		secrets = s.secretValues(ctx, d)
	}
	req.Error = redact(trunc(req.Error, 4000), secrets)

	switch req.Status {
	case "RUNNING":
		if typ == TaskDeploy && (req.Phase == StatusBuilding || req.Phase == StatusDeploying) {
			s.setDeploymentStatus(ctx, d.ID, req.Phase, "")
			s.db.ExecContext(ctx, `UPDATE deployment_revisions SET status = $1 WHERE id = $2`, req.Phase, revID.String)
		} else if typ == TaskStart || typ == TaskRestart {
			s.setDeploymentStatus(ctx, d.ID, StatusDeploying, "")
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	case TaskDone, TaskFailed:
	default:
		writeError(w, http.StatusBadRequest, "status must be RUNNING, DONE or FAILED")
		return
	}

	result := []byte("null")
	if len(req.Result) > 0 {
		result = []byte(redact(string(req.Result), secrets))
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE agent_tasks SET status = $1, error = $2, result = $3, finished_at = now() WHERE id = $4`,
		req.Status, req.Error, result, id); err != nil {
		internalError(w, err)
		return
	}
	if d != nil {
		s.applyTaskResult(ctx, d, typ, revID.String, req.Status == TaskDone, req.Error, result)
	}
	s.notifyAgent(agentID(r)) // the next task for this deployment may now run
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// applyTaskResult updates the deployment after the agent finished a task.
func (s *Server) applyTaskResult(ctx context.Context, d *Deployment, typ, revisionID string, ok bool, errMsg string, result []byte) {
	refs := withDeployment(d)
	if !ok {
		switch typ {
		case TaskDeploy:
			s.failRevision(ctx, s.db, d.ID, revisionID, errMsg)
			s.logLine(ctx, d.ID, revisionID, "deploy", "Deployment failed: "+errMsg)
			s.event(ctx, d.userID, "error", fmt.Sprintf("Deployment of %s failed: %s", d.Name, errMsg), refs)
		case TaskDelete:
			s.setDeploymentStatus(ctx, d.ID, StatusFailed, "Could not delete: "+errMsg)
			s.event(ctx, d.userID, "error", fmt.Sprintf("Could not delete %s: %s", d.Name, errMsg), refs)
		default:
			// Stop/start/restart failed: keep the status, show the error.
			s.db.ExecContext(ctx, `UPDATE deployments SET error = $1, updated_at = now() WHERE id = $2`, errMsg, d.ID)
			if typ != TaskStop {
				s.setDeploymentStatus(ctx, d.ID, StatusFailed, errMsg)
			}
			s.event(ctx, d.userID, "error", fmt.Sprintf("%s of %s failed: %s", taskVerb(typ), d.Name, errMsg), refs)
		}
		return
	}

	switch typ {
	case TaskDeploy:
		var res deployResult
		json.Unmarshal(result, &res)
		s.db.ExecContext(ctx, `UPDATE deployment_revisions SET status = 'STOPPED' WHERE deployment_id = $1 AND id <> $2 AND status = 'RUNNING'`,
			d.ID, revisionID)
		s.db.ExecContext(ctx, `UPDATE deployment_revisions SET status = 'RUNNING', error = '', finished_at = now(),
			image_digest = CASE WHEN $1 <> '' THEN $1 ELSE image_digest END,
			git_commit = CASE WHEN $2 <> '' THEN $2 ELSE git_commit END WHERE id = $3`,
			trunc(res.ImageDigest, 100), trunc(res.GitCommit, 64), revisionID)
		for _, svc := range res.Services {
			ports := make([]int64, 0, len(svc.DetectedPorts))
			for _, p := range svc.DetectedPorts {
				ports = append(ports, int64(p))
			}
			s.db.ExecContext(ctx, `UPDATE services SET container_id = $1, state = $2, image = CASE WHEN $3 <> '' THEN $3 ELSE image END,
				detected_ports = CASE WHEN cardinality($4::int[]) > 0 THEN $4::int[] ELSE detected_ports END, updated_at = now()
				WHERE deployment_id = $5 AND name = $6`,
				trunc(svc.ContainerID, 100), trunc(svc.State, 30), trunc(svc.Image, 255), int64Array(ports), d.ID, svc.Name)
		}
		s.setDeploymentStatus(ctx, d.ID, StatusRunning, "")
		s.logLine(ctx, d.ID, revisionID, "deploy", "Deployment is running.")
		s.event(ctx, d.userID, "success", fmt.Sprintf("Deployed %s (#%d)", d.Name, revisionNumber(ctx, s.db, revisionID)), refs)
		s.kickRoutes()
	case TaskStop:
		s.setDeploymentStatus(ctx, d.ID, StatusStopped, "")
		s.db.ExecContext(ctx, `UPDATE services SET state = 'exited' WHERE deployment_id = $1`, d.ID)
		s.event(ctx, d.userID, "info", "Stopped "+d.Name, refs)
	case TaskStart, TaskRestart:
		s.setDeploymentStatus(ctx, d.ID, StatusRunning, "")
		s.db.ExecContext(ctx, `UPDATE services SET state = 'running' WHERE deployment_id = $1`, d.ID)
		s.event(ctx, d.userID, "success", fmt.Sprintf("%s %s", map[string]string{TaskStart: "Started", TaskRestart: "Restarted"}[typ], d.Name), refs)
		s.kickRoutes()
	case TaskDelete:
		s.deleteDeploymentRoutes(ctx, d.ID)
		s.db.ExecContext(ctx, `DELETE FROM deployments WHERE id = $1`, d.ID)
		s.event(ctx, d.userID, "info", "Deleted "+d.Name, withProject(d.ProjectID))
	}
}

func taskVerb(typ string) string {
	return map[string]string{TaskStop: "Stop", TaskStart: "Start", TaskRestart: "Restart", TaskDeploy: "Deploy", TaskDelete: "Delete"}[typ]
}

func revisionNumber(ctx context.Context, db dbtx, id string) int {
	var n int
	db.QueryRowContext(ctx, `SELECT number FROM deployment_revisions WHERE id = $1`, id).Scan(&n)
	return n
}

// handleAgentLogs receives build and deploy output. Lines are redacted
// before they're stored, so secrets never reach the database or the UI.
func (s *Server) handleAgentLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req struct {
		RevisionID string `json:"revision_id"`
		Lines      []struct {
			Stream string `json:"stream"`
			Text   string `json:"text"`
		} `json:"lines"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	id := chi.URLParam(r, "id")
	var owns bool
	if validUUID(id) {
		s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM deployments WHERE id = $1 AND agent_id = $2)`, id, agentID(r)).Scan(&owns)
	}
	if !owns {
		writeError(w, http.StatusNotFound, "deployment not found")
		return
	}
	d, err := s.getDeploymentByID(ctx, id)
	if err != nil {
		internalError(w, err)
		return
	}
	secrets := s.secretValues(ctx, d)
	var rev any
	if validUUID(req.RevisionID) {
		rev = req.RevisionID
	}
	for i, line := range req.Lines {
		if i >= 1000 {
			break
		}
		stream := line.Stream
		if stream != "build" {
			stream = "deploy"
		}
		s.db.ExecContext(ctx, `INSERT INTO deployment_logs (deployment_id, revision_id, stream, line) VALUES ($1, $2, $3, $4)`,
			id, rev, stream, redact(trunc(line.Text, 8000), secrets))
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) logLine(ctx context.Context, deploymentID, revisionID, stream, text string) {
	var rev any
	if revisionID != "" {
		rev = revisionID
	}
	s.db.ExecContext(ctx, `INSERT INTO deployment_logs (deployment_id, revision_id, stream, line) VALUES ($1, $2, $3, $4)`,
		deploymentID, rev, stream, text)
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
