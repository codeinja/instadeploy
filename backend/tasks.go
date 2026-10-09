package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// Task types. Everything the backend wants an agent to do is a row in
// agent_tasks. The agent long-polls GET /api/agent/commands to receive them.
const (
	TaskDeploy  = "DEPLOY"
	TaskStop    = "STOP"
	TaskStart   = "START"
	TaskRestart = "RESTART"
	TaskDelete  = "DELETE"
	TaskLogs    = "LOGS"    // fetch container logs (answered synchronously)
	TaskAnalyze = "ANALYZE" // clone a Git repo and report its services/ports
)

const (
	TaskPending    = "PENDING"
	TaskDispatched = "DISPATCHED"
	TaskDone       = "DONE"
	TaskFailed     = "FAILED"
	TaskCancelled  = "CANCELLED"
)

// Tasks that don't change containers can run alongside anything else.
// Everything else for one deployment runs strictly in order.
func taskIsIndependent(t string) bool { return t == TaskLogs || t == TaskAnalyze }

func (s *Server) createTask(ctx context.Context, tx dbtx, agentID, deploymentID, revisionID, typ string, payload any) (string, error) {
	p, _ := json.Marshal(payload)
	if payload == nil {
		p = []byte("{}")
	}
	var dep, rev any
	if deploymentID != "" {
		dep = deploymentID
	}
	if revisionID != "" {
		rev = revisionID
	}
	var id string
	err := tx.QueryRowContext(ctx, `INSERT INTO agent_tasks (agent_id, deployment_id, revision_id, type, payload)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`, agentID, dep, rev, typ, p).Scan(&id)
	return id, err
}

// agentNotifier wakes up an agent's long-poll when new work arrives.
type agentNotifier struct {
	mu    sync.Mutex
	chans map[string]chan struct{}
}

func (n *agentNotifier) channel(agentID string) chan struct{} {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.chans == nil {
		n.chans = map[string]chan struct{}{}
	}
	ch, ok := n.chans[agentID]
	if !ok {
		ch = make(chan struct{}, 1)
		n.chans[agentID] = ch
	}
	return ch
}

func (s *Server) notifyAgent(agentID string) {
	select {
	case s.notifier.channel(agentID) <- struct{}{}:
	default: // already notified
	}
}

// ------------------------------------------------------------ dispatching

// AgentTask is what the agent receives. Unlike agent_tasks.payload it can
// contain secrets (resolved just now), so it's never stored.
type AgentTask struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	Deployment *AgentDeployment  `json:"deployment,omitempty"`
	Logs       *AgentLogsRequest `json:"logs,omitempty"`
	Analyze    *AgentAnalyze     `json:"analyze,omitempty"`
}

type AgentDeployment struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Environment string `json:"environment"`
	RevisionID  string `json:"revision_id,omitempty"`
	Revision    int    `json:"revision,omitempty"`

	// IMAGE/DOCKERFILE: the container. COMPOSE: the Compose project name.
	ContainerName string `json:"container_name"`
	Network       string `json:"network"`

	// IMAGE: what to pull (pinned to a digest on rollback).
	Image string `json:"image,omitempty"`
	// DOCKERFILE: the tag to build. On rollback, ReuseImage is the image
	// built for the earlier revision; the agent uses it if it still exists.
	BuildTag   string `json:"build_tag,omitempty"`
	ReuseImage string `json:"reuse_image,omitempty"`

	Source        *AgentSource   `json:"source,omitempty"`
	Services      []AgentService `json:"services"`
	Volumes       []AgentVolume  `json:"volumes,omitempty"`
	RestartPolicy string         `json:"restart_policy,omitempty"`
	CPUs          float64        `json:"cpus,omitempty"`
	MemoryMB      int            `json:"memory_mb,omitempty"`
	RegistryAuths []RegistryAuth `json:"registry_auths,omitempty"`

	// DELETE only.
	DeleteVolumes bool `json:"delete_volumes,omitempty"`
}

type AgentSource struct {
	Kind      string `json:"kind"`
	GitURL    string `json:"git_url,omitempty"`
	GitRef    string `json:"git_branch,omitempty"`
	GitCommit string `json:"git_commit,omitempty"`
	// Read-only GitHub installation token for private repositories, minted
	// when the task is handed out (it expires within the hour).
	GitToken string `json:"git_token,omitempty"`
	Path     string `json:"path,omitempty"`
	Compose  string `json:"compose,omitempty"`
}

type AgentService struct {
	Name        string            `json:"name"`
	Port        int               `json:"port,omitempty"`
	Public      bool              `json:"public"`
	Alias       string            `json:"alias"`
	Env         map[string]string `json:"env"`
	HealthCheck *HealthCheck      `json:"health_check,omitempty"`
}

type AgentVolume struct {
	Name     string `json:"name"` // Docker volume name, or absolute host path
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only,omitempty"`
}

type RegistryAuth struct {
	Server   string `json:"server"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type AgentLogsRequest struct {
	ContainerName string `json:"container_name"` // or Compose project
	Service       string `json:"service,omitempty"`
	Compose       bool   `json:"compose"`
	Tail          int    `json:"tail"`
}

type AgentAnalyze struct {
	GitURL    string `json:"git_url"`
	GitRef    string `json:"git_branch"`
	GitCommit string `json:"git_commit"`
	// Set when the task is handed out, never stored (see AgentSource).
	GitToken string `json:"git_token,omitempty"`
}

type taskRow struct {
	ID, Type, Status       string
	DeploymentID, Revision sql.NullString
	Payload                []byte
}

// dispatchTasks hands the agent everything it can work on now and marks
// it DISPATCHED. A deployment's tasks are released one at a time, in order.
func (s *Server) dispatchTasks(ctx context.Context, agentID string) ([]AgentTask, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `SELECT id, type, status, deployment_id, revision_id, payload FROM agent_tasks
		WHERE agent_id = $1 AND status IN ('PENDING', 'DISPATCHED') ORDER BY created_at FOR UPDATE`, agentID)
	if err != nil {
		return nil, err
	}
	var ready []taskRow
	busy := map[string]bool{}
	for rows.Next() {
		var t taskRow
		if err := rows.Scan(&t.ID, &t.Type, &t.Status, &t.DeploymentID, &t.Revision, &t.Payload); err != nil {
			rows.Close()
			return nil, err
		}
		if taskIsIndependent(t.Type) {
			if t.Status == TaskPending {
				ready = append(ready, t)
			}
			continue
		}
		dep := t.DeploymentID.String
		if t.Status == TaskPending && !busy[dep] {
			ready = append(ready, t)
		}
		busy[dep] = true
	}
	rows.Close()

	var out []AgentTask
	for _, t := range ready {
		task, err := s.materialize(ctx, agentID, t)
		if err != nil {
			log.Printf("task %s: %v", t.ID, err)
			tx.ExecContext(ctx, `UPDATE agent_tasks SET status = 'FAILED', error = $1, finished_at = now() WHERE id = $2`, err.Error(), t.ID)
			if t.DeploymentID.Valid && t.Type == TaskDeploy {
				s.failRevision(ctx, tx, t.DeploymentID.String, t.Revision.String, err.Error())
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE agent_tasks SET status = 'DISPATCHED', dispatched_at = now() WHERE id = $1`, t.ID); err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, tx.Commit()
}

// materialize turns a task row into what the agent needs, resolving
// variables, secrets and registry credentials.
func (s *Server) materialize(ctx context.Context, agentID string, t taskRow) (AgentTask, error) {
	task := AgentTask{ID: t.ID, Type: t.Type}
	switch t.Type {
	case TaskAnalyze:
		var a AgentAnalyze
		json.Unmarshal(t.Payload, &a)
		var uid string
		if err := s.db.QueryRowContext(ctx, `SELECT user_id FROM agents WHERE id = $1`, agentID).Scan(&uid); err != nil {
			return task, err
		}
		token, err := s.gitToken(ctx, uid, a.GitURL)
		if err != nil {
			return task, fmt.Errorf("GitHub App: %w", err)
		}
		a.GitToken = token
		task.Analyze = &a
		return task, nil
	case TaskLogs:
		var l AgentLogsRequest
		json.Unmarshal(t.Payload, &l)
		task.Logs = &l
		return task, nil
	}

	d, err := s.getDeploymentByID(ctx, t.DeploymentID.String)
	if err != nil {
		return task, fmt.Errorf("load deployment: %w", err)
	}
	ad := &AgentDeployment{
		ID: d.ID, Name: d.Name, Type: d.Type, Environment: d.Environment,
		ContainerName: containerName(d.Name, d.ID), Network: agentNetwork,
	}
	spec := d.Spec
	switch t.Type {
	case TaskDelete:
		var p struct {
			DeleteVolumes bool `json:"delete_volumes"`
		}
		json.Unmarshal(t.Payload, &p)
		ad.DeleteVolumes = p.DeleteVolumes
		for _, v := range spec.Volumes {
			if !strings.HasPrefix(v.Source, "/") {
				ad.Volumes = append(ad.Volumes, AgentVolume{Name: dockerVolumeName(d.ID, v.Source), Target: v.Target})
			}
		}
	case TaskDeploy:
		var rev Revision
		var revSpec []byte
		var rollbackOf sql.NullInt64
		err := s.db.QueryRowContext(ctx, `SELECT r.id, r.number, r.spec, r.image_digest, r.git_commit, rb.number
			FROM deployment_revisions r LEFT JOIN deployment_revisions rb ON rb.id = r.rollback_of WHERE r.id = $1`,
			t.Revision.String).Scan(&rev.ID, &rev.Number, &revSpec, &rev.ImageDigest, &rev.GitCommit, &rollbackOf)
		if err != nil {
			return task, fmt.Errorf("load revision: %w", err)
		}
		if err := json.Unmarshal(revSpec, &spec); err != nil {
			return task, err
		}
		ad.RevisionID, ad.Revision = rev.ID, rev.Number
		if err := s.fillDeployPayload(ctx, d, spec, rev, ad); err != nil {
			return task, err
		}
		if d.Type == TypeDockerfile && rollbackOf.Valid {
			ad.ReuseImage = builtImageTag(d.ID, int(rollbackOf.Int64))
		}
	}
	// STOP/START/RESTART only need names.
	if ad.Services == nil {
		for _, svc := range spec.Services {
			ad.Services = append(ad.Services, AgentService{Name: svc.Name, Port: svc.Port, Public: svc.Public, Alias: targetHost(d, svc.Name)})
		}
	}
	task.Deployment = ad
	return task, nil
}

const agentNetwork = "insta-deploy"

func (s *Server) fillDeployPayload(ctx context.Context, d *Deployment, spec Spec, rev Revision, ad *AgentDeployment) error {
	vars, err := s.resolveVariables(ctx, d)
	if err != nil {
		return err
	}
	// Apps that need to know their own address (for links, cookies or
	// CSRF checks) can use ${INSTA_PUBLIC_URL} and ${INSTA_PUBLIC_HOST}.
	if host := s.publicAddress(ctx, d, spec); host != "" {
		if _, set := vars.common["INSTA_PUBLIC_URL"]; !set {
			vars.common["INSTA_PUBLIC_URL"] = "https://" + host
		}
		if _, set := vars.common["INSTA_PUBLIC_HOST"]; !set {
			vars.common["INSTA_PUBLIC_HOST"] = host
		}
	}
	for _, svc := range spec.Services {
		ad.Services = append(ad.Services, AgentService{
			Name: svc.Name, Port: svc.Port, Public: svc.Public, Alias: targetHost(d, svc.Name),
			Env: vars.forService(svc.Name), HealthCheck: svc.HealthCheck,
		})
	}

	switch d.Type {
	case TypeImage:
		ad.Image = spec.Image
		if rev.ImageDigest != "" {
			// Rollbacks run exactly the image that ran before.
			ad.Image = imageRepository(spec.Image) + "@" + rev.ImageDigest
		}
	case TypeDockerfile:
		ad.BuildTag = builtImageTag(d.ID, rev.Number)
	}
	if src := spec.Source; src != nil {
		as := &AgentSource{Kind: src.Kind, Path: src.Path, Compose: src.Compose}
		switch src.Kind {
		case "git":
			as.GitURL, as.GitRef, as.GitCommit = src.GitURL, src.GitRef, rev.GitCommit
			if as.GitToken, err = s.gitToken(ctx, d.userID, src.GitURL); err != nil {
				return fmt.Errorf("GitHub App: %w", err)
			}
		}
		ad.Source = as
	}
	for _, v := range spec.Volumes {
		name := v.Source
		if !strings.HasPrefix(name, "/") {
			name = dockerVolumeName(d.ID, v.Source)
		}
		ad.Volumes = append(ad.Volumes, AgentVolume{Name: name, Target: v.Target, ReadOnly: v.ReadOnly})
	}
	ad.RestartPolicy = spec.RestartPolicy
	if ad.RestartPolicy == "" {
		ad.RestartPolicy = "unless-stopped"
	}
	ad.CPUs, ad.MemoryMB = spec.CPUs, spec.MemoryMB

	auths, err := s.registryAuths(ctx, d.userID)
	if err != nil {
		return err
	}
	ad.RegistryAuths = auths
	return nil
}

// ------------------------------------------------------------- variables

type resolvedVars struct {
	common     map[string]string
	perService map[string]map[string]string
	secrets    []string
}

func (v resolvedVars) forService(name string) map[string]string {
	env := map[string]string{}
	for k, val := range v.common {
		env[k] = val
	}
	for k, val := range v.perService[name] {
		env[k] = val
	}
	return env
}

// resolveVariables merges variables for a deployment, narrowest scope wins:
// project (all environments) < project (this environment) < deployment < service.
func (s *Server) resolveVariables(ctx context.Context, d *Deployment) (resolvedVars, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value_encrypted, is_secret, deployment_id IS NOT NULL, service_name, environment IS NOT NULL
		FROM variables
		WHERE project_id = $1 AND (
			(deployment_id IS NULL AND (environment IS NULL OR environment = $2))
			OR deployment_id = $3)`, d.ProjectID, d.Environment, d.ID)
	if err != nil {
		return resolvedVars{}, err
	}
	defer rows.Close()

	type entry struct {
		key, value, service string
		rank                int
	}
	var entries []entry
	out := resolvedVars{common: map[string]string{}, perService: map[string]map[string]string{}}
	for rows.Next() {
		var key string
		var enc []byte
		var secret, deploymentLevel, envSpecific bool
		var service sql.NullString
		if err := rows.Scan(&key, &enc, &secret, &deploymentLevel, &service, &envSpecific); err != nil {
			return out, err
		}
		value, err := s.box.Decrypt(enc)
		if err != nil {
			return out, fmt.Errorf("variable %s: %w", key, err)
		}
		if secret {
			out.secrets = append(out.secrets, value)
		}
		rank := 0
		switch {
		case service.Valid:
			rank = 3
		case deploymentLevel:
			rank = 2
		case envSpecific:
			rank = 1
		}
		entries = append(entries, entry{key, value, service.String, rank})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rank < entries[j].rank })
	for _, e := range entries {
		if e.rank == 3 {
			if out.perService[e.service] == nil {
				out.perService[e.service] = map[string]string{}
			}
			out.perService[e.service][e.key] = e.value
		} else {
			out.common[e.key] = e.value
		}
	}
	return out, rows.Err()
}

// secretValues lists everything that must never appear in logs or errors
// for this deployment: secret variables and registry passwords.
func (s *Server) secretValues(ctx context.Context, d *Deployment) []string {
	vars, err := s.resolveVariables(ctx, d)
	if err != nil {
		log.Printf("resolve secrets for redaction: %v", err)
	}
	secrets := vars.secrets
	auths, _ := s.registryAuths(ctx, d.userID)
	for _, a := range auths {
		secrets = append(secrets, a.Password)
	}
	return secrets
}

// redact replaces secret values in text. Very short values are skipped
// because replacing them would mangle normal output.
func redact(text string, secrets []string) string {
	for _, secret := range secrets {
		if len(secret) >= 4 {
			text = strings.ReplaceAll(text, secret, "••••••••")
		}
	}
	return text
}

// ------------------------------------------------------------ task results

func (s *Server) failRevision(ctx context.Context, tx dbtx, deploymentID, revisionID, msg string) {
	tx.ExecContext(ctx, `UPDATE deployments SET status = $1, error = $2, updated_at = now() WHERE id = $3`, StatusFailed, msg, deploymentID)
	if revisionID != "" {
		tx.ExecContext(ctx, `UPDATE deployment_revisions SET status = $1, error = $2, finished_at = now() WHERE id = $3`, StatusFailed, msg, revisionID)
	}
}

// waitForTask polls until an independent task (LOGS, ANALYZE) finishes.
func (s *Server) waitForTask(ctx context.Context, taskID string, timeout time.Duration) (status string, result []byte, errMsg string, err error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		err = s.db.QueryRowContext(ctx, `SELECT status, COALESCE(result, 'null'), error FROM agent_tasks WHERE id = $1`, taskID).
			Scan(&status, &result, &errMsg)
		if err != nil {
			break
		}
		if status == TaskDone || status == TaskFailed {
			return
		}
		select {
		case <-ctx.Done():
			err = ctx.Err()
		case <-time.After(300 * time.Millisecond):
			continue
		}
		break
	}
	// Timed out or cancelled: don't leave it for the agent to pick up later.
	s.db.ExecContext(context.WithoutCancel(ctx), `UPDATE agent_tasks SET status = 'CANCELLED', finished_at = now()
		WHERE id = $1 AND status IN ('PENDING', 'DISPATCHED')`, taskID)
	return
}

// publicAddress returns the hostname of the deployment's first public
// service: its custom domain if one is connected, otherwise its generated
// address, which is assigned now if it doesn't exist yet (the route is
// created with the same name once the container runs).
func (s *Server) publicAddress(ctx context.Context, d *Deployment, spec Spec) string {
	var name string
	for _, svc := range spec.Services {
		if svc.Public && svc.Port != 0 {
			name = svc.Name
			break
		}
	}
	if name == "" {
		return ""
	}
	var serviceID, hostname string
	if err := s.db.QueryRowContext(ctx, `SELECT id, hostname FROM services WHERE deployment_id = $1 AND name = $2`, d.ID, name).
		Scan(&serviceID, &hostname); err != nil {
		return ""
	}
	var custom string
	s.db.QueryRowContext(ctx, `SELECT hostname FROM routes WHERE service_id = $1 AND kind = 'CUSTOM' AND status = 'READY' LIMIT 1`, serviceID).Scan(&custom)
	if custom != "" {
		return custom
	}
	apps := s.settings.get().AppsDomain
	if hostname != "" || apps == "" {
		return hostname
	}
	label := s.hostLabel(ctx, d.ID, d.Name, d.Type, name)
	for attempt := 0; attempt < 5; attempt++ {
		candidate := generatedHostname(label, apps)
		var taken bool
		s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM routes WHERE hostname = $1) OR EXISTS(SELECT 1 FROM services WHERE hostname = $1)`, candidate).Scan(&taken)
		if !taken {
			s.db.ExecContext(ctx, `UPDATE services SET hostname = $1 WHERE id = $2`, candidate, serviceID)
			return candidate
		}
	}
	return ""
}
