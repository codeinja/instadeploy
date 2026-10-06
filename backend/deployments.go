package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"
)

// Deployment states.
const (
	StatusQueued    = "QUEUED"
	StatusBuilding  = "BUILDING"
	StatusDeploying = "DEPLOYING"
	StatusRunning   = "RUNNING"
	StatusStopped   = "STOPPED"
	StatusFailed    = "FAILED"
	StatusDeleting  = "DELETING"
)

// Route states.
const (
	RoutePending  = "PENDING"
	RouteCreating = "CREATING"
	RouteReady    = "READY"
	RouteFailed   = "FAILED"
	RouteDisabled = "DISABLED"
)

type Deployment struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	ProjectName string    `json:"project_name"`
	AgentID     string    `json:"agent_id"`
	AgentName   string    `json:"agent_name"`
	AgentStatus string    `json:"agent_status"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Environment string    `json:"environment"`
	Spec        Spec      `json:"spec"`
	Status      string    `json:"status"`
	Error       string    `json:"error"`
	AutoDeploy  bool      `json:"auto_deploy"`
	Revision    *Revision `json:"revision"`
	Services    []Service `json:"services"`
	// Type of a stop/start/restart/deploy the agent hasn't finished yet.
	PendingAction string    `json:"pending_action"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`

	userID string
}

type Revision struct {
	ID          string     `json:"id"`
	Number      int        `json:"number"`
	Spec        Spec       `json:"spec"`
	ImageDigest string     `json:"image_digest"`
	GitCommit   string     `json:"git_commit"`
	Trigger     string     `json:"trigger"`
	RollbackOf  *int       `json:"rollback_of"`
	Status      string     `json:"status"`
	Error       string     `json:"error"`
	CreatedAt   time.Time  `json:"created_at"`
	FinishedAt  *time.Time `json:"finished_at"`
}

type Service struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Image         string  `json:"image"`
	Port          *int    `json:"port"`
	Public        bool    `json:"public"`
	TargetHost    string  `json:"target_host"`
	ContainerID   string  `json:"container_id"`
	State         string  `json:"state"`
	Health        string  `json:"health"`
	DetectedPorts []int64 `json:"detected_ports"`
	Routes        []Route `json:"routes"`
	Stats         *Stats  `json:"stats"`
	deploymentID  string
}

type Route struct {
	ID       string  `json:"id"`
	Kind     string  `json:"kind"`
	DomainID *string `json:"domain_id"`
	Hostname string  `json:"hostname"`
	URL      string  `json:"url"`
	Status   string  `json:"status"`
	Error    string  `json:"error"`
}

const deploymentSelect = `SELECT d.id, d.project_id, p.name, d.agent_id, a.name, a.status, d.name, d.type,
	d.environment, d.spec, d.status, d.error, d.auto_deploy, d.created_at, d.updated_at, p.user_id,
	r.id, r.number, r.spec, r.image_digest, r.git_commit, r.trigger, rb.number, r.status, r.error, r.created_at, r.finished_at,
	(SELECT t.type FROM agent_tasks t WHERE t.deployment_id = d.id AND t.status IN ('PENDING', 'DISPATCHED')
	   AND t.type IN ('DEPLOY', 'STOP', 'START', 'RESTART', 'DELETE') ORDER BY t.created_at LIMIT 1)
	FROM deployments d
	JOIN projects p ON p.id = d.project_id
	JOIN agents a ON a.id = d.agent_id
	LEFT JOIN deployment_revisions r ON r.id = d.current_revision_id
	LEFT JOIN deployment_revisions rb ON rb.id = r.rollback_of`

func scanDeployment(row interface{ Scan(...any) error }) (*Deployment, error) {
	var d Deployment
	var spec []byte
	var revID, revTrigger, revDigest, revCommit, revStatus, revError sql.NullString
	var revNumber, rollbackOf sql.NullInt64
	var revSpec []byte
	var revCreated, revFinished sql.NullTime
	var pending sql.NullString
	err := row.Scan(&d.ID, &d.ProjectID, &d.ProjectName, &d.AgentID, &d.AgentName, &d.AgentStatus, &d.Name, &d.Type,
		&d.Environment, &spec, &d.Status, &d.Error, &d.AutoDeploy, &d.CreatedAt, &d.UpdatedAt, &d.userID,
		&revID, &revNumber, &revSpec, &revDigest, &revCommit, &revTrigger, &rollbackOf, &revStatus, &revError, &revCreated, &revFinished,
		&pending)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(spec, &d.Spec); err != nil {
		return nil, fmt.Errorf("deployment %s: bad spec: %w", d.ID, err)
	}
	if revID.Valid {
		r := &Revision{ID: revID.String, Number: int(revNumber.Int64), ImageDigest: revDigest.String, GitCommit: revCommit.String,
			Trigger: revTrigger.String, Status: revStatus.String, Error: revError.String, CreatedAt: revCreated.Time}
		json.Unmarshal(revSpec, &r.Spec)
		if rollbackOf.Valid {
			n := int(rollbackOf.Int64)
			r.RollbackOf = &n
		}
		if revFinished.Valid {
			r.FinishedAt = &revFinished.Time
		}
		d.Revision = r
	}
	d.PendingAction = pending.String
	d.Services = []Service{}
	return &d, nil
}

// loadDeployments returns the user's deployments (optionally just one
// project's, or just the given IDs) with their services and routes.
func (s *Server) loadDeployments(ctx context.Context, userID, projectID string, ids ...string) ([]*Deployment, error) {
	query := deploymentSelect + ` WHERE p.user_id = $1`
	args := []any{userID}
	if projectID != "" {
		query += ` AND d.project_id = $2`
		args = append(args, projectID)
	}
	if len(ids) > 0 {
		args = append(args, pq.Array(ids))
		query += fmt.Sprintf(` AND d.id = ANY($%d::uuid[])`, len(args))
	}
	rows, err := s.db.QueryContext(ctx, query+` ORDER BY d.created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*Deployment
	byID := map[string]*Deployment{}
	for rows.Next() {
		d, err := scanDeployment(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, d)
		byID[d.ID] = d
	}
	if err := rows.Err(); err != nil || len(list) == 0 {
		return list, err
	}
	return list, s.attachServices(ctx, byID)
}

func (s *Server) attachServices(ctx context.Context, byID map[string]*Deployment) error {
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, deployment_id, name, image, port, public, target_host,
		container_id, state, health, detected_ports FROM services WHERE deployment_id = ANY($1::uuid[]) ORDER BY name`, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()
	services := map[string]*Service{}
	var order []*Service
	for rows.Next() {
		var svc Service
		var port sql.NullInt64
		if err := rows.Scan(&svc.ID, &svc.deploymentID, &svc.Name, &svc.Image, &port, &svc.Public, &svc.TargetHost,
			&svc.ContainerID, &svc.State, &svc.Health, pq.Array(&svc.DetectedPorts)); err != nil {
			return err
		}
		if port.Valid {
			p := int(port.Int64)
			svc.Port = &p
		}
		svc.Routes = []Route{}
		svc.Stats = s.stats.get(svc.ID)
		services[svc.ID] = &svc
		order = append(order, &svc)
	}
	rows.Close()

	rrows, err := s.db.QueryContext(ctx, `SELECT id, service_id, kind, domain_id, hostname, url, status, error
		FROM routes WHERE deployment_id = ANY($1::uuid[]) ORDER BY kind DESC, created_at`, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rrows.Close()
	for rrows.Next() {
		var r Route
		var serviceID string
		if err := rrows.Scan(&r.ID, &serviceID, &r.Kind, &r.DomainID, &r.Hostname, &r.URL, &r.Status, &r.Error); err != nil {
			return err
		}
		if svc := services[serviceID]; svc != nil {
			svc.Routes = append(svc.Routes, r)
		}
	}
	for _, svc := range order {
		d := byID[svc.deploymentID]
		d.Services = append(d.Services, *svc)
	}
	return rrows.Err()
}

func (s *Server) getDeployment(ctx context.Context, userID, id string) (*Deployment, error) {
	if !validUUID(id) {
		return nil, sql.ErrNoRows
	}
	list, err := s.loadDeployments(ctx, userID, "", id)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, sql.ErrNoRows
	}
	return list[0], nil
}

// getDeploymentByID loads a deployment without a user check, for background
// work and the agent API (which checks agent ownership itself).
func (s *Server) getDeploymentByID(ctx context.Context, id string) (*Deployment, error) {
	d, err := scanDeployment(s.db.QueryRowContext(ctx, deploymentSelect+` WHERE d.id = $1`, id))
	if err != nil {
		return nil, err
	}
	return d, s.attachServices(ctx, map[string]*Deployment{d.ID: d})
}

type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// syncServices makes the services table match the spec: adds new services,
// updates port/public, and removes services that are no longer listed.
func syncServices(ctx context.Context, tx dbtx, d *Deployment) error {
	names := make([]string, 0, len(d.Spec.Services))
	for _, svc := range d.Spec.Services {
		names = append(names, svc.Name)
		var port any
		if svc.Port != 0 {
			port = svc.Port
		}
		image := ""
		if d.Type == TypeImage {
			image = d.Spec.Image
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO services (deployment_id, name, image, port, public, target_host)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (deployment_id, name) DO UPDATE SET port = EXCLUDED.port, public = EXCLUDED.public,
				image = CASE WHEN EXCLUDED.image <> '' THEN EXCLUDED.image ELSE services.image END,
				target_host = EXCLUDED.target_host, updated_at = now()`,
			d.ID, svc.Name, image, port, svc.Public, targetHost(d, svc.Name))
		if err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM services WHERE deployment_id = $1 AND NOT (name = ANY($2))`, d.ID, pq.Array(names))
	return err
}

// newRevision records a new version of the deployment and queues it.
// trigger is deploy, redeploy, rollback, auto or config.
func (s *Server) newRevision(ctx context.Context, tx *sql.Tx, d *Deployment, spec Spec, trigger, imageDigest, gitCommit string, rollbackOf *string) (*Revision, error) {
	specJSON, _ := json.Marshal(spec)
	r := &Revision{Spec: spec, Trigger: trigger, Status: StatusQueued, ImageDigest: imageDigest, GitCommit: gitCommit}
	err := tx.QueryRowContext(ctx, `INSERT INTO deployment_revisions (deployment_id, number, spec, trigger, image_digest, git_commit, rollback_of)
		VALUES ($1, COALESCE((SELECT max(number) FROM deployment_revisions WHERE deployment_id = $1), 0) + 1, $2, $3, $4, $5, $6)
		RETURNING id, number, created_at`, d.ID, specJSON, trigger, imageDigest, gitCommit, rollbackOf).Scan(&r.ID, &r.Number, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE deployments SET current_revision_id = $1, status = $2, error = '', updated_at = now() WHERE id = $3`,
		r.ID, StatusQueued, d.ID)
	if err != nil {
		return nil, err
	}
	if _, err := s.createTask(ctx, tx, d.AgentID, d.ID, r.ID, TaskDeploy, nil); err != nil {
		return nil, err
	}
	return r, nil
}

// deploy creates a revision of d with the given spec and queues it. For Git
// sources it first resolves the branch to a commit, so the revision records
// exactly what will be built.
func (s *Server) deploy(ctx context.Context, d *Deployment, spec Spec, trigger, imageDigest, gitCommit string, rollbackOf *string) (*Revision, error) {
	if spec.Source != nil && spec.Source.Kind == "git" && gitCommit == "" {
		commit, err := gitResolve(ctx, spec.Source.GitURL, spec.Source.GitRef)
		if err != nil {
			return nil, err
		}
		gitCommit = commit
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	r, err := s.newRevision(ctx, tx, d, spec, trigger, imageDigest, gitCommit, rollbackOf)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.notifyAgent(d.AgentID)
	return r, nil
}

func (s *Server) setDeploymentStatus(ctx context.Context, id, status, errMsg string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE deployments SET status = $1, error = $2, updated_at = now() WHERE id = $3`, status, errMsg, id)
	return err
}

var errNotFound = errors.New("not found")

func int64Array(a []int64) any { return pq.Array(a) }
