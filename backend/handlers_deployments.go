package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

type variableInput struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Secret  bool   `json:"secret"`
	Service string `json:"service,omitempty"`
}

type createDeploymentRequest struct {
	ProjectID   string          `json:"project_id"`
	AgentID     string          `json:"agent_id"`
	Name        string          `json:"name"`
	Environment string          `json:"environment"`
	AutoDeploy  bool            `json:"auto_deploy"`
	Spec        Spec            `json:"spec"`
	Variables   []variableInput `json:"variables"`
	// Optional access protection for public services.
	Access []accessInput `json:"access"`
}

func (s *Server) handleCreateDeployment(w http.ResponseWriter, r *http.Request) {
	var req createDeploymentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	uid := userID(r)

	req.Name = strings.TrimSpace(req.Name)
	if req.Environment == "" {
		req.Environment = "production"
	}
	if err := validateName(req.Name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !environments[req.Environment] {
		writeError(w, http.StatusBadRequest, "environment must be development, staging or production")
		return
	}
	if req.Spec.Type == TypeImage || req.Spec.Type == TypeDockerfile {
		// Single-container deployments always have one service called "web".
		if len(req.Spec.Services) == 1 {
			req.Spec.Services[0].Name = "web"
		}
	}
	if err := req.Spec.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.AutoDeploy && (req.Spec.Source == nil || req.Spec.Source.Kind != "git") {
		writeError(w, http.StatusBadRequest, "auto deploy is only available for Git deployments")
		return
	}
	for _, a := range req.Access {
		if err := validateAccess(a.Mode, a.Secret); err != nil {
			writeError(w, http.StatusBadRequest, a.Service+": "+err.Error())
			return
		}
	}
	for _, v := range req.Variables {
		if err := validateEnvKey(v.Key); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	agent, err := s.userAgent(ctx, uid, req.AgentID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "choose one of your machines to deploy to")
		return
	}
	if agent.Status == "REVOKED" {
		writeError(w, http.StatusBadRequest, "this machine's access was revoked; reconnect it first")
		return
	}
	project, err := s.resolveProject(ctx, uid, req.ProjectID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Resolve the Git commit before creating anything, so a typo in the
	// URL or branch fails here with a clear message.
	gitCommit := ""
	if req.Spec.Source != nil && req.Spec.Source.Kind == "git" {
		if gitCommit, err = s.resolveGitCommit(ctx, uid, req.Spec.Source.GitURL, req.Spec.Source.GitRef); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		internalError(w, err)
		return
	}
	defer tx.Rollback()

	var exists bool
	tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM deployments WHERE project_id = $1 AND name = $2 AND environment = $3)`,
		project.ID, req.Name, req.Environment).Scan(&exists)
	if exists {
		writeError(w, http.StatusConflict, fmt.Sprintf("%s already has a %s deployment called %q", project.Name, req.Environment, req.Name))
		return
	}

	specJSON, _ := json.Marshal(req.Spec)
	d := &Deployment{ProjectID: project.ID, AgentID: agent.ID, Name: req.Name, Type: req.Spec.Type,
		Environment: req.Environment, Spec: req.Spec, userID: uid}
	err = tx.QueryRowContext(ctx, `INSERT INTO deployments (project_id, agent_id, name, type, environment, spec, auto_deploy)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		project.ID, agent.ID, req.Name, req.Spec.Type, req.Environment, specJSON, req.AutoDeploy).Scan(&d.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	if err := syncServices(ctx, tx, d); err != nil {
		internalError(w, err)
		return
	}
	for _, a := range req.Access {
		if req.Spec.Type != TypeCompose {
			a.Service = "web"
		}
		found, err := s.setServiceAccess(ctx, tx, d.ID, a)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !found {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("access: there is no service called %q", a.Service))
			return
		}
	}
	for _, v := range req.Variables {
		var svc any
		if v.Service != "" {
			svc = v.Service
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO variables (project_id, deployment_id, service_name, key, value_encrypted, is_secret)
			VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT DO NOTHING`, project.ID, d.ID, svc, v.Key, s.box.Encrypt(v.Value), v.Secret)
		if err != nil {
			internalError(w, err)
			return
		}
	}
	if _, err := s.newRevision(ctx, tx, d, req.Spec, "deploy", "", gitCommit, nil); err != nil {
		internalError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, err)
		return
	}
	s.notifyAgent(agent.ID)
	s.event(ctx, uid, "info", fmt.Sprintf("Deployment %s created on %s", req.Name, agent.Name), withDeployment(d))

	full, err := s.getDeployment(ctx, uid, d.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, full)
}

func (s *Server) handleListDeployments(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	if projectID != "" && !validUUID(projectID) {
		writeError(w, http.StatusBadRequest, "invalid project_id")
		return
	}
	list, err := s.loadDeployments(r.Context(), userID(r), projectID)
	if err != nil {
		internalError(w, err)
		return
	}
	if list == nil {
		list = []*Deployment{}
	}
	writeJSON(w, http.StatusOK, list)
}

// loadDeployment fetches the user's deployment or writes a 404/500.
func (s *Server) loadDeployment(w http.ResponseWriter, r *http.Request) (*Deployment, bool) {
	d, err := s.getDeployment(r.Context(), userID(r), chi.URLParam(r, "id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "deployment not found")
		return nil, false
	}
	if err != nil {
		internalError(w, err)
		return nil, false
	}
	return d, true
}

func (s *Server) handleGetDeployment(w http.ResponseWriter, r *http.Request) {
	if d, ok := s.loadDeployment(w, r); ok {
		writeJSON(w, http.StatusOK, d)
	}
}

// handleUpdateDeployment changes settings. Spec changes take effect on the
// next deploy; pass "deploy": true to deploy them right away.
func (s *Server) handleUpdateDeployment(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDeployment(w, r)
	if !ok {
		return
	}
	var req struct {
		Spec        *Spec   `json:"spec"`
		Environment *string `json:"environment"`
		AutoDeploy  *bool   `json:"auto_deploy"`
		Deploy      bool    `json:"deploy"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	if d.Status == StatusDeleting {
		writeError(w, http.StatusConflict, "this deployment is being deleted")
		return
	}
	if req.Spec != nil {
		if req.Spec.Type != d.Type {
			writeError(w, http.StatusBadRequest, "the deployment type can't be changed; create a new deployment instead")
			return
		}
		if d.Type != TypeCompose && len(req.Spec.Services) == 1 {
			req.Spec.Services[0].Name = "web"
		}
		if err := req.Spec.Validate(); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		d.Spec = *req.Spec
	}
	if req.Environment != nil {
		if !environments[*req.Environment] {
			writeError(w, http.StatusBadRequest, "environment must be development, staging or production")
			return
		}
		d.Environment = *req.Environment
	}
	if req.AutoDeploy != nil {
		if *req.AutoDeploy && (d.Spec.Source == nil || d.Spec.Source.Kind != "git") {
			writeError(w, http.StatusBadRequest, "auto deploy is only available for Git deployments")
			return
		}
		d.AutoDeploy = *req.AutoDeploy
	}

	specJSON, _ := json.Marshal(d.Spec)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		internalError(w, err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE deployments SET spec = $1, environment = $2, auto_deploy = $3, updated_at = now() WHERE id = $4`,
		specJSON, d.Environment, d.AutoDeploy, d.ID); err != nil {
		internalError(w, err)
		return
	}
	if err := syncServices(ctx, tx, d); err != nil {
		internalError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, err)
		return
	}
	s.kickRoutes()

	if req.Deploy {
		if _, err := s.deploy(ctx, d, d.Spec, "config", "", "", nil); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.event(ctx, d.userID, "info", fmt.Sprintf("Redeploying %s with new settings", d.Name), withDeployment(d))
	}
	s.respondDeployment(w, r, d.ID)
}

// handleUpdateService changes one service's public access, port or health
// check. For single-container deployments the public URL is created or
// removed right away; Compose services need a redeploy to join or leave the
// tunnel network, which happens automatically.
func (s *Server) handleUpdateService(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDeployment(w, r)
	if !ok {
		return
	}
	name := chi.URLParam(r, "service")
	var req struct {
		Public      *bool        `json:"public"`
		Port        *int         `json:"port"`
		HealthCheck *HealthCheck `json:"health_check"`
		// Set to remove the health check.
		ClearHealthCheck bool `json:"clear_health_check"`
		// Access protection: {"mode": "password", "secret": "..."}.
		Access *accessInput `json:"access"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	idx := -1
	for i, svc := range d.Spec.Services {
		if svc.Name == name {
			idx = i
		}
	}
	if idx < 0 {
		writeError(w, http.StatusNotFound, "service not found")
		return
	}
	svc := &d.Spec.Services[idx]
	wasPublic := svc.Public
	if req.Public != nil {
		svc.Public = *req.Public
	}
	if req.Port != nil {
		svc.Port = *req.Port
	}
	if req.HealthCheck != nil {
		svc.HealthCheck = req.HealthCheck
	}
	if req.ClearHealthCheck {
		svc.HealthCheck = nil
	}
	if err := d.Spec.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	specJSON, _ := json.Marshal(d.Spec)
	if _, err := s.db.ExecContext(r.Context(), `UPDATE deployments SET spec = $1, updated_at = now() WHERE id = $2`, specJSON, d.ID); err != nil {
		internalError(w, err)
		return
	}
	if err := syncServices(r.Context(), s.db, d); err != nil {
		internalError(w, err)
		return
	}
	if req.Access != nil {
		req.Access.Service = name
		if _, err := s.setServiceAccess(r.Context(), s.db, d.ID, *req.Access); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		msg := map[string]string{
			AccessNone:     "%s / %s: access protection removed",
			AccessPassword: "%s / %s is now protected with a password",
			AccessPincode:  "%s / %s is now protected with a PIN",
		}[req.Access.Mode]
		s.event(r.Context(), d.userID, "info", fmt.Sprintf(msg, d.Name, name), withDeployment(d))
	}
	needsRedeploy := (d.Type == TypeCompose && wasPublic != svc.Public) || req.HealthCheck != nil || req.ClearHealthCheck
	if needsRedeploy && d.Status != StatusStopped {
		if _, err := s.deploy(r.Context(), d, d.Spec, "config", "", "", nil); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if wasPublic != svc.Public {
		verb := "private"
		if svc.Public {
			verb = "public"
		}
		s.event(r.Context(), d.userID, "info", fmt.Sprintf("%s / %s is now %s", d.Name, name, verb), withDeployment(d))
	}
	s.kickRoutes()
	s.respondDeployment(w, r, d.ID)
}

func (s *Server) respondDeployment(w http.ResponseWriter, r *http.Request, id string) {
	d, err := s.getDeployment(r.Context(), userID(r), id)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleRedeploy(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDeployment(w, r)
	if !ok {
		return
	}
	if d.Status == StatusDeleting {
		writeError(w, http.StatusConflict, "this deployment is being deleted")
		return
	}
	if _, err := s.deploy(r.Context(), d, d.Spec, "redeploy", "", "", nil); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.event(r.Context(), d.userID, "info", "Redeploying "+d.Name, withDeployment(d))
	s.respondDeployment(w, r, d.ID)
}

// handleLifecycle queues stop, start or restart.
func (s *Server) handleLifecycle(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d, ok := s.loadDeployment(w, r)
		if !ok {
			return
		}
		ctx := r.Context()
		switch {
		case d.Status == StatusDeleting:
			writeError(w, http.StatusConflict, "this deployment is being deleted")
			return
		case action == TaskStart && d.Status == StatusRunning:
			writeError(w, http.StatusConflict, "already running")
			return
		case action == TaskStop && d.Status == StatusStopped:
			writeError(w, http.StatusConflict, "already stopped")
			return
		case action != TaskStop && d.Revision == nil:
			writeError(w, http.StatusConflict, "this deployment has never been deployed")
			return
		}
		if action == TaskStop && d.Status == StatusQueued && s.cancelUndeliveredDeploy(ctx, d.ID) {
			// The agent never received the deploy: cancel it instead of stopping.
			s.setDeploymentStatus(ctx, d.ID, StatusStopped, "")
			s.event(ctx, d.userID, "info", "Cancelled the queued deployment of "+d.Name, withDeployment(d))
			s.respondDeployment(w, r, d.ID)
			return
		}
		if _, err := s.createTask(ctx, s.db, d.AgentID, d.ID, "", action, nil); err != nil {
			internalError(w, err)
			return
		}
		s.notifyAgent(d.AgentID)
		verbs := map[string]string{TaskStop: "Stopping", TaskStart: "Starting", TaskRestart: "Restarting"}
		s.event(ctx, d.userID, "info", verbs[action]+" "+d.Name, withDeployment(d))
		s.respondDeployment(w, r, d.ID)
	}
}

// cancelUndeliveredDeploy cancels a deployment's queued tasks if none has
// reached the agent yet. Once the agent has a deploy, it must be stopped
// the normal way (after it finishes).
func (s *Server) cancelUndeliveredDeploy(ctx context.Context, deploymentID string) bool {
	res, err := s.db.ExecContext(ctx, `UPDATE agent_tasks SET status = 'CANCELLED', finished_at = now()
		WHERE deployment_id = $1 AND status = 'PENDING' AND type <> 'LOGS'
		  AND NOT EXISTS (SELECT 1 FROM agent_tasks t WHERE t.deployment_id = $1 AND t.status = 'DISPATCHED' AND t.type <> 'LOGS')`, deploymentID)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// handleDeleteDeployment removes the public routes first, then asks the
// agent to remove the containers; the deployment's records are deleted once
// the agent confirms. ?force=true deletes the records right away (for
// machines that are gone for good); containers are then left behind.
func (s *Server) handleDeleteDeployment(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDeployment(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	force := r.URL.Query().Get("force") == "true"
	deleteVolumes := r.URL.Query().Get("delete_volumes") == "true"

	if err := s.deleteDeploymentRoutes(ctx, d.ID); err != nil {
		writeError(w, http.StatusBadGateway, "Could not remove the public URL: "+err.Error())
		return
	}
	s.db.ExecContext(ctx, `UPDATE agent_tasks SET status = 'CANCELLED', finished_at = now()
		WHERE deployment_id = $1 AND status = 'PENDING'`, d.ID)

	if force || d.Revision == nil {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM deployments WHERE id = $1`, d.ID); err != nil {
			internalError(w, err)
			return
		}
		s.event(ctx, d.userID, "info", "Deleted "+d.Name, withProject(d.ProjectID))
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := s.setDeploymentStatus(ctx, d.ID, StatusDeleting, ""); err != nil {
		internalError(w, err)
		return
	}
	if _, err := s.createTask(ctx, s.db, d.AgentID, d.ID, "", TaskDelete, map[string]bool{"delete_volumes": deleteVolumes}); err != nil {
		internalError(w, err)
		return
	}
	s.notifyAgent(d.AgentID)
	s.event(ctx, d.userID, "info", "Deleting "+d.Name, withDeployment(d))
	w.WriteHeader(http.StatusAccepted)
}

// deleteDeploymentRoutes removes every public URL of a deployment.
func (s *Server) deleteDeploymentRoutes(ctx context.Context, deploymentID string) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, provider_ref FROM routes WHERE deployment_id = $1`, deploymentID)
	if err != nil {
		return err
	}
	type rt struct{ id, ref string }
	var list []rt
	for rows.Next() {
		var x rt
		rows.Scan(&x.id, &x.ref)
		list = append(list, x)
	}
	rows.Close()
	for _, x := range list {
		if x.ref != "" {
			if err := s.pangolin.DeleteRoute(ctx, x.ref); err != nil && !errors.Is(err, ErrPangolinDisabled) {
				return err
			}
		}
		s.db.ExecContext(ctx, `DELETE FROM routes WHERE id = $1`, x.id)
	}
	return nil
}

func (s *Server) handleListRevisions(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDeployment(w, r)
	if !ok {
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT r.id, r.number, r.spec, r.image_digest, r.git_commit, r.trigger, rb.number,
		r.status, r.error, r.created_at, r.finished_at
		FROM deployment_revisions r LEFT JOIN deployment_revisions rb ON rb.id = r.rollback_of
		WHERE r.deployment_id = $1 ORDER BY r.number DESC LIMIT 50`, d.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	defer rows.Close()
	list := []Revision{}
	for rows.Next() {
		var rev Revision
		var spec []byte
		if err := rows.Scan(&rev.ID, &rev.Number, &spec, &rev.ImageDigest, &rev.GitCommit, &rev.Trigger, &rev.RollbackOf,
			&rev.Status, &rev.Error, &rev.CreatedAt, &rev.FinishedAt); err != nil {
			internalError(w, err)
			return
		}
		json.Unmarshal(spec, &rev.Spec)
		list = append(list, rev)
	}
	writeJSON(w, http.StatusOK, list)
}

// handleRollback redeploys an earlier revision: the same image digest,
// build context or Compose file, and Git commit. Variables are the current
// ones (they aren't versioned).
func (s *Server) handleRollback(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDeployment(w, r)
	if !ok {
		return
	}
	var req struct {
		RevisionID string `json:"revision_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	var spec []byte
	var number int
	var digest, commit string
	err := sql.ErrNoRows
	if validUUID(req.RevisionID) {
		err = s.db.QueryRowContext(r.Context(), `SELECT number, spec, image_digest, git_commit FROM deployment_revisions
			WHERE id = $1 AND deployment_id = $2`, req.RevisionID, d.ID).Scan(&number, &spec, &digest, &commit)
	}
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "revision not found")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	var target Spec
	json.Unmarshal(spec, &target)
	if d.Type == TypeImage && digest == "" {
		log.Printf("rollback of %s to #%d: no digest recorded, using tag %s", d.ID, number, target.Image)
	}
	if target.Source != nil && target.Source.Kind == "git" && commit == "" {
		writeError(w, http.StatusConflict, fmt.Sprintf("revision #%d has no recorded Git commit to roll back to", number))
		return
	}

	// The rolled-back spec becomes the current one.
	if _, err := s.db.ExecContext(r.Context(), `UPDATE deployments SET spec = $1 WHERE id = $2`, spec, d.ID); err != nil {
		internalError(w, err)
		return
	}
	d.Spec = target
	syncServices(r.Context(), s.db, d)
	if _, err := s.deploy(r.Context(), d, target, "rollback", digest, commit, &req.RevisionID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.event(r.Context(), d.userID, "info", fmt.Sprintf("Rolling back %s to #%d", d.Name, number), withDeployment(d))
	s.kickRoutes()
	s.respondDeployment(w, r, d.ID)
}
