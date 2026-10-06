package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Deployments int       `json:"deployments"`
	Running     int       `json:"running"`
	Failed      int       `json:"failed"`
	CreatedAt   time.Time `json:"created_at"`
}

const projectSelect = `SELECT p.id, p.name, p.description, p.created_at,
	COUNT(d.id), COUNT(d.id) FILTER (WHERE d.status = 'RUNNING'), COUNT(d.id) FILTER (WHERE d.status = 'FAILED')
	FROM projects p LEFT JOIN deployments d ON d.project_id = p.id`

func scanProject(row interface{ Scan(...any) error }) (Project, error) {
	var p Project
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt, &p.Deployments, &p.Running, &p.Failed)
	return p, err
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), projectSelect+` WHERE p.user_id = $1 GROUP BY p.id ORDER BY p.name`, userID(r))
	if err != nil {
		internalError(w, err)
		return
	}
	defer rows.Close()
	list := []Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			internalError(w, err)
			return
		}
		list = append(list, p)
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	p, err := scanProject(s.db.QueryRowContext(r.Context(), projectSelect+` WHERE p.id = $1 AND p.user_id = $2 GROUP BY p.id`, id, userID(r)))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

type projectInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (in *projectInput) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if in.Name == "" || len(in.Name) > 64 {
		return errors.New("project name must be 1-64 characters")
	}
	if len(in.Description) > 500 {
		return errors.New("description is too long (max 500 characters)")
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var in projectInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := in.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var id string
	err := s.db.QueryRowContext(r.Context(), `INSERT INTO projects (user_id, name, description) VALUES ($1, $2, $3) RETURNING id`,
		userID(r), in.Name, in.Description).Scan(&id)
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "you already have a project called "+in.Name)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	s.event(r.Context(), userID(r), "info", "Created project "+in.Name, withProject(id))
	p, _ := scanProject(s.db.QueryRowContext(r.Context(), projectSelect+` WHERE p.id = $1 GROUP BY p.id`, id))
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var in projectInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := in.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := s.db.ExecContext(r.Context(), `UPDATE projects SET name = $1, description = $2, updated_at = now() WHERE id = $3 AND user_id = $4`,
		in.Name, in.Description, id, userID(r))
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "you already have a project called "+in.Name)
		return
	}
	if err != nil || !validUUID(id) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	s.handleGetProject(w, r)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	var name string
	var deployments int
	err := s.db.QueryRowContext(r.Context(), `SELECT p.name, (SELECT COUNT(*) FROM deployments WHERE project_id = p.id)
		FROM projects p WHERE p.id = $1 AND p.user_id = $2`, id, userID(r)).Scan(&name, &deployments)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	if deployments > 0 {
		writeError(w, http.StatusConflict, "delete this project's deployments first")
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM projects WHERE id = $1`, id); err != nil {
		internalError(w, err)
		return
	}
	s.event(r.Context(), userID(r), "info", "Deleted project "+name, eventRefs{})
	w.WriteHeader(http.StatusNoContent)
}

// resolveProject returns the user's project, or their "Default" project
// (created on first use) when id is empty.
func (s *Server) resolveProject(ctx context.Context, uid, id string) (Project, error) {
	var p Project
	if id == "" {
		err := s.db.QueryRowContext(ctx, `INSERT INTO projects (user_id, name) VALUES ($1, 'Default')
			ON CONFLICT (user_id, name) DO UPDATE SET name = EXCLUDED.name RETURNING id, name`, uid).Scan(&p.ID, &p.Name)
		return p, err
	}
	if !validUUID(id) {
		return p, errors.New("project not found")
	}
	err := s.db.QueryRowContext(ctx, `SELECT id, name FROM projects WHERE id = $1 AND user_id = $2`, id, uid).Scan(&p.ID, &p.Name)
	if err != nil {
		return p, errors.New("project not found")
	}
	return p, nil
}
