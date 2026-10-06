package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// Variable is returned to the dashboard. Secret values are never returned:
// Value is empty and Secret is true.
type Variable struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"project_id"`
	DeploymentID *string   `json:"deployment_id"`
	Service      *string   `json:"service"`
	Environment  *string   `json:"environment"`
	Key          string    `json:"key"`
	Value        string    `json:"value"`
	Secret       bool      `json:"secret"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// GET /api/variables?project_id=... (project level) or ?deployment_id=...
func (s *Server) handleListVariables(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	uid := userID(r)
	var rows *sql.Rows
	var err error
	switch {
	case validUUID(q.Get("deployment_id")):
		rows, err = s.db.QueryContext(r.Context(), `SELECT v.id, v.project_id, v.deployment_id, v.service_name, v.environment,
			v.key, v.value_encrypted, v.is_secret, v.updated_at
			FROM variables v JOIN projects p ON p.id = v.project_id
			WHERE v.deployment_id = $1 AND p.user_id = $2 ORDER BY v.service_name NULLS FIRST, v.key`, q.Get("deployment_id"), uid)
	case validUUID(q.Get("project_id")):
		rows, err = s.db.QueryContext(r.Context(), `SELECT v.id, v.project_id, v.deployment_id, v.service_name, v.environment,
			v.key, v.value_encrypted, v.is_secret, v.updated_at
			FROM variables v JOIN projects p ON p.id = v.project_id
			WHERE v.project_id = $1 AND v.deployment_id IS NULL AND p.user_id = $2 ORDER BY v.environment NULLS FIRST, v.key`, q.Get("project_id"), uid)
	default:
		writeError(w, http.StatusBadRequest, "pass project_id or deployment_id")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	defer rows.Close()
	list := []Variable{}
	for rows.Next() {
		var v Variable
		var enc []byte
		if err := rows.Scan(&v.ID, &v.ProjectID, &v.DeploymentID, &v.Service, &v.Environment, &v.Key, &enc, &v.Secret, &v.UpdatedAt); err != nil {
			internalError(w, err)
			return
		}
		if !v.Secret {
			if v.Value, err = s.box.Decrypt(enc); err != nil {
				v.Value = "(cannot decrypt: " + err.Error() + ")"
			}
		}
		list = append(list, v)
	}
	writeJSON(w, http.StatusOK, list)
}

type variableRequest struct {
	ProjectID    string  `json:"project_id"`
	DeploymentID string  `json:"deployment_id"`
	Service      string  `json:"service"`
	Environment  string  `json:"environment"`
	Key          string  `json:"key"`
	Value        *string `json:"value"`
	Secret       *bool   `json:"secret"`
}

func (s *Server) handleCreateVariable(w http.ResponseWriter, r *http.Request) {
	var req variableRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	req.Key = strings.TrimSpace(req.Key)
	if err := validateEnvKey(req.Key); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Value == nil {
		writeError(w, http.StatusBadRequest, "value is required")
		return
	}
	if len(*req.Value) > 64*1024 {
		writeError(w, http.StatusBadRequest, "value is too long (max 64 KB)")
		return
	}

	var projectID string
	var deploymentID, service, environment any
	switch {
	case req.DeploymentID != "":
		d, err := s.getDeployment(ctx, userID(r), req.DeploymentID)
		if err != nil {
			writeError(w, http.StatusNotFound, "deployment not found")
			return
		}
		projectID, deploymentID = d.ProjectID, d.ID
		if req.Service != "" {
			if !hasService(d, req.Service) {
				writeError(w, http.StatusBadRequest, "no service called "+req.Service)
				return
			}
			service = req.Service
		}
	case req.ProjectID != "":
		p, err := s.resolveProject(ctx, userID(r), req.ProjectID)
		if err != nil {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		projectID = p.ID
		if req.Environment != "" {
			if !environments[req.Environment] {
				writeError(w, http.StatusBadRequest, "environment must be development, staging or production")
				return
			}
			environment = req.Environment
		}
	default:
		writeError(w, http.StatusBadRequest, "pass project_id or deployment_id")
		return
	}

	secret := req.Secret != nil && *req.Secret
	var id string
	err := s.db.QueryRowContext(ctx, `INSERT INTO variables (project_id, deployment_id, service_name, environment, key, value_encrypted, is_secret)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		projectID, deploymentID, service, environment, req.Key, s.box.Encrypt(*req.Value), secret).Scan(&id)
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, req.Key+" is already set here; edit it instead")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	kind := "variable"
	if secret {
		kind = "secret"
	}
	s.event(ctx, userID(r), "info", "Added "+kind+" "+req.Key, eventRefs{projectID: projectID, deploymentID: req.DeploymentID})
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func hasService(d *Deployment, name string) bool {
	for _, svc := range d.Spec.Services {
		if svc.Name == name {
			return true
		}
	}
	return false
}

// userVariable checks the variable belongs to the user.
func (s *Server) userVariable(ctx context.Context, uid, id string) (key string, secret bool, err error) {
	if !validUUID(id) {
		return "", false, sql.ErrNoRows
	}
	err = s.db.QueryRowContext(ctx, `SELECT v.key, v.is_secret FROM variables v JOIN projects p ON p.id = v.project_id
		WHERE v.id = $1 AND p.user_id = $2`, id, uid).Scan(&key, &secret)
	return
}

func (s *Server) handleUpdateVariable(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req variableRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	_, secret, err := s.userVariable(r.Context(), userID(r), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "variable not found")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	if req.Secret != nil {
		if secret && !*req.Secret && req.Value == nil {
			// Turning a secret into a plain variable would reveal it.
			writeError(w, http.StatusBadRequest, "enter a new value to turn a secret into a plain variable")
			return
		}
		secret = *req.Secret
	}
	if req.Value != nil {
		if len(*req.Value) > 64*1024 {
			writeError(w, http.StatusBadRequest, "value is too long (max 64 KB)")
			return
		}
		_, err = s.db.ExecContext(r.Context(), `UPDATE variables SET value_encrypted = $1, is_secret = $2, updated_at = now() WHERE id = $3`,
			s.box.Encrypt(*req.Value), secret, id)
	} else {
		_, err = s.db.ExecContext(r.Context(), `UPDATE variables SET is_secret = $1, updated_at = now() WHERE id = $2`, secret, id)
	}
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDeleteVariable(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	key, _, err := s.userVariable(r.Context(), userID(r), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "variable not found")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM variables WHERE id = $1`, id); err != nil {
		internalError(w, err)
		return
	}
	s.event(r.Context(), userID(r), "info", "Removed "+key, eventRefs{})
	w.WriteHeader(http.StatusNoContent)
}

// ------------------------------------------------------------ registries

type RegistryCredential struct {
	ID        string    `json:"id"`
	Server    string    `json:"server"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Server) handleListRegistries(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, server, username, created_at FROM registry_credentials WHERE user_id = $1 ORDER BY server`, userID(r))
	if err != nil {
		internalError(w, err)
		return
	}
	defer rows.Close()
	list := []RegistryCredential{}
	for rows.Next() {
		var c RegistryCredential
		if err := rows.Scan(&c.ID, &c.Server, &c.Username, &c.CreatedAt); err != nil {
			internalError(w, err)
			return
		}
		list = append(list, c)
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateRegistry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Server   string `json:"server"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Server = normalizeRegistry(req.Server)
	if !registryHostRe.MatchString(req.Server) {
		writeError(w, http.StatusBadRequest, "enter a registry host like ghcr.io, registry.gitlab.com or docker.io")
		return
	}
	if req.Username == "" || req.Password == "" || len(req.Username) > 255 || len(req.Password) > 4096 {
		writeError(w, http.StatusBadRequest, "username and password/token are required")
		return
	}
	_, err := s.db.ExecContext(r.Context(), `INSERT INTO registry_credentials (user_id, server, username, password_encrypted)
		VALUES ($1, $2, $3, $4) ON CONFLICT (user_id, server) DO UPDATE SET username = EXCLUDED.username,
		password_encrypted = EXCLUDED.password_encrypted`, userID(r), req.Server, req.Username, s.box.Encrypt(req.Password))
	if err != nil {
		internalError(w, err)
		return
	}
	s.event(r.Context(), userID(r), "info", "Saved credentials for registry "+req.Server, eventRefs{})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteRegistry(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	s.db.ExecContext(r.Context(), `DELETE FROM registry_credentials WHERE id = $1 AND user_id = $2`, id, userID(r))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) registryAuths(ctx context.Context, uid string) ([]RegistryAuth, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT server, username, password_encrypted FROM registry_credentials WHERE user_id = $1`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RegistryAuth
	for rows.Next() {
		var a RegistryAuth
		var enc []byte
		if err := rows.Scan(&a.Server, &a.Username, &enc); err != nil {
			return nil, err
		}
		if a.Password, err = s.box.Decrypt(enc); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
