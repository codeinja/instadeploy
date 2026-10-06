package main

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"
)

// Events feed the Activity page: short, human-readable lines like
// "Deployed nginx (#3)". This is a simple log, not an audit system.

type eventRefs struct {
	projectID, deploymentID, agentID string
}

func withDeployment(d *Deployment) eventRefs {
	return eventRefs{projectID: d.ProjectID, deploymentID: d.ID, agentID: d.AgentID}
}
func withProject(id string) eventRefs { return eventRefs{projectID: id} }
func withAgent(id string) eventRefs   { return eventRefs{agentID: id} }

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Server) event(ctx context.Context, userID, level, message string, refs eventRefs) {
	_, err := s.db.ExecContext(context.WithoutCancel(ctx),
		`INSERT INTO events (user_id, project_id, deployment_id, agent_id, level, message) VALUES ($1, $2, $3, $4, $5, $6)`,
		userID, nullable(refs.projectID), nullable(refs.deploymentID), nullable(refs.agentID), level, trunc(message, 1000))
	if err != nil {
		log.Printf("record event: %v", err)
	}
}

type Event struct {
	ID           int64     `json:"id"`
	Level        string    `json:"level"`
	Message      string    `json:"message"`
	ProjectID    *string   `json:"project_id"`
	DeploymentID *string   `json:"deployment_id"`
	AgentID      *string   `json:"agent_id"`
	CreatedAt    time.Time `json:"created_at"`
}

func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id, level, message, project_id, deployment_id, agent_id, created_at FROM events WHERE user_id = $1`
	args := []any{userID(r)}
	if dep := r.URL.Query().Get("deployment_id"); validUUID(dep) {
		q += ` AND deployment_id = $2`
		args = append(args, dep)
	} else if proj := r.URL.Query().Get("project_id"); validUUID(proj) {
		q += ` AND project_id = $2`
		args = append(args, proj)
	}
	rows, err := s.db.QueryContext(r.Context(), q+` ORDER BY id DESC LIMIT `+strconv.Itoa(limit), args...)
	if err != nil {
		internalError(w, err)
		return
	}
	defer rows.Close()
	list := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.Level, &e.Message, &e.ProjectID, &e.DeploymentID, &e.AgentID, &e.CreatedAt); err != nil {
			internalError(w, err)
			return
		}
		list = append(list, e)
	}
	writeJSON(w, http.StatusOK, list)
}
