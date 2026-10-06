package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type Agent struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Status        string     `json:"status"` // ONLINE, OFFLINE, REVOKED
	LastSeen      *time.Time `json:"last_seen"`
	Hostname      string     `json:"hostname"`
	OS            string     `json:"os"`
	Arch          string     `json:"arch"`
	DockerVersion string     `json:"docker_version"`
	AgentVersion  string     `json:"agent_version"`
	Deployments   int        `json:"deployments"`
	TunnelReady   bool       `json:"tunnel_ready"`
	TunnelError   string     `json:"tunnel_error"`
	CreatedAt     time.Time  `json:"created_at"`
}

const agentSelect = `SELECT a.id, a.name,
	CASE WHEN a.revoked_at IS NOT NULL THEN 'REVOKED' ELSE a.status END,
	a.last_seen, a.hostname, a.os, a.arch, a.docker_version, a.agent_version,
	(SELECT COUNT(*) FROM deployments d WHERE d.agent_id = a.id),
	a.pangolin_site_id IS NOT NULL, a.tunnel_error, a.created_at
	FROM agents a`

func scanAgent(row interface{ Scan(...any) error }) (Agent, error) {
	var a Agent
	err := row.Scan(&a.ID, &a.Name, &a.Status, &a.LastSeen, &a.Hostname, &a.OS, &a.Arch, &a.DockerVersion,
		&a.AgentVersion, &a.Deployments, &a.TunnelReady, &a.TunnelError, &a.CreatedAt)
	return a, err
}

func (s *Server) userAgent(ctx context.Context, uid, id string) (Agent, error) {
	if !validUUID(id) {
		return Agent{}, errNoAgent
	}
	a, err := scanAgent(s.db.QueryRowContext(ctx, agentSelect+` WHERE a.id = $1 AND a.user_id = $2`, id, uid))
	if errors.Is(err, sql.ErrNoRows) {
		return a, errNoAgent
	}
	return a, err
}

func validAgentName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return "", errors.New("name must be 1-64 characters")
	}
	return name, nil
}

func (s *Server) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	name, err := validAgentName(req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	token, hash := newAgentToken()
	var id string
	err = s.db.QueryRowContext(r.Context(),
		`INSERT INTO agents (user_id, name, token_hash) VALUES ($1, $2, $3) RETURNING id`,
		userID(r), name, hash).Scan(&id)
	if err != nil {
		internalError(w, err)
		return
	}

	// Set up the Pangolin tunnel now so the agent can connect right away.
	// If this fails, the agent retries when it registers.
	s.ensureTunnel(r.Context(), id)
	s.event(r.Context(), userID(r), "info", "Created agent "+name, withAgent(id))

	agent, err := s.userAgent(r.Context(), userID(r), id)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"agent": agent, "token": token, "command": s.agentRunCommand(token)})
}

func (s *Server) agentRunCommand(token string) string {
	lines := []string{
		"docker run -d",
		"--name insta-deploy-agent",
		"--restart unless-stopped",
		"-e INSTA_DEPLOY_TOKEN=" + token,
		"-e INSTA_DEPLOY_SERVER=" + s.settings.get().PublicAPIURL,
	}
	if strings.HasPrefix(s.settings.get().PublicAPIURL, "http://") {
		lines = append(lines, "-e INSTA_DEPLOY_ALLOW_HTTP=true")
	}
	if strings.Contains(s.settings.get().PublicAPIURL, "host.docker.internal") {
		lines = append(lines, "--add-host host.docker.internal:host-gateway")
	}
	lines = append(lines,
		"-v /var/run/docker.sock:/var/run/docker.sock",
		// Same path inside and outside the container, so files from
		// uploaded projects can be mounted into deployed containers.
		"-v /var/lib/insta-deploy:/var/lib/insta-deploy",
		s.settings.get().AgentImage,
	)
	return strings.Join(lines, " \\\n  ")
}

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), agentSelect+` WHERE a.user_id = $1 ORDER BY a.created_at`, userID(r))
	if err != nil {
		internalError(w, err)
		return
	}
	defer rows.Close()
	agents := []Agent{}
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			internalError(w, err)
			return
		}
		agents = append(agents, a)
	}
	writeJSON(w, http.StatusOK, agents)
}

func (s *Server) loadAgent(w http.ResponseWriter, r *http.Request) (Agent, bool) {
	a, err := s.userAgent(r.Context(), userID(r), chi.URLParam(r, "id"))
	if errors.Is(err, errNoAgent) {
		writeError(w, http.StatusNotFound, "agent not found")
		return a, false
	}
	if err != nil {
		internalError(w, err)
		return a, false
	}
	return a, true
}

func (s *Server) handleRenameAgent(w http.ResponseWriter, r *http.Request) {
	a, ok := s.loadAgent(w, r)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	name, err := validAgentName(req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `UPDATE agents SET name = $1 WHERE id = $2`, name, a.ID); err != nil {
		internalError(w, err)
		return
	}
	s.event(r.Context(), userID(r), "info", fmt.Sprintf("Renamed agent %s to %s", a.Name, name), withAgent(a.ID))
	a.Name = name
	writeJSON(w, http.StatusOK, a)
}

// handleRevokeAgent invalidates the agent's token. The machine is
// disconnected immediately; its containers keep running.
func (s *Server) handleRevokeAgent(w http.ResponseWriter, r *http.Request) {
	a, ok := s.loadAgent(w, r)
	if !ok {
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `UPDATE agents SET token_hash = NULL, revoked_at = now(), status = 'OFFLINE' WHERE id = $1`, a.ID); err != nil {
		internalError(w, err)
		return
	}
	s.event(r.Context(), userID(r), "info", "Revoked access for agent "+a.Name, withAgent(a.ID))
	a, _ = s.userAgent(r.Context(), userID(r), a.ID)
	writeJSON(w, http.StatusOK, a)
}

// handleRotateAgentToken issues a new token (and un-revokes the agent).
// The old token stops working right away.
func (s *Server) handleRotateAgentToken(w http.ResponseWriter, r *http.Request) {
	a, ok := s.loadAgent(w, r)
	if !ok {
		return
	}
	token, hash := newAgentToken()
	if _, err := s.db.ExecContext(r.Context(), `UPDATE agents SET token_hash = $1, revoked_at = NULL WHERE id = $2`, hash, a.ID); err != nil {
		internalError(w, err)
		return
	}
	s.event(r.Context(), userID(r), "info", "Issued a new token for agent "+a.Name, withAgent(a.ID))
	a, _ = s.userAgent(r.Context(), userID(r), a.ID)
	writeJSON(w, http.StatusOK, map[string]any{"agent": a, "token": token, "command": s.agentRunCommand(token)})
}

func (s *Server) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	a, ok := s.loadAgent(w, r)
	if !ok {
		return
	}
	ctx := context.WithoutCancel(r.Context())
	var siteID sql.NullInt64
	s.db.QueryRowContext(ctx, `SELECT pangolin_site_id FROM agents WHERE id = $1`, a.ID).Scan(&siteID)

	// Remove the Pangolin side first: every route of every deployment.
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM deployments WHERE agent_id = $1`, a.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	var deps []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		deps = append(deps, id)
	}
	rows.Close()
	for _, id := range deps {
		if err := s.deleteDeploymentRoutes(ctx, id); err != nil {
			log.Printf("delete routes of %s: %v", id, err)
		}
	}
	if siteID.Valid {
		if err := s.pangolin.DeleteSite(ctx, strconv.FormatInt(siteID.Int64, 10)); err != nil {
			log.Printf("delete pangolin site %d: %v", siteID.Int64, err)
		}
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM agents WHERE id = $1`, a.ID); err != nil {
		internalError(w, err)
		return
	}
	s.event(ctx, userID(r), "info", fmt.Sprintf("Deleted agent %s and its %d deployment(s)", a.Name, len(deps)), eventRefs{})
	w.WriteHeader(http.StatusNoContent)
}

// ensureTunnel creates the agent's Pangolin site if it doesn't have one yet,
// and returns the newt credentials (or nil if there's no tunnel).
func (s *Server) ensureTunnel(ctx context.Context, agentID string) *Tunnel {
	var siteID sql.NullInt64
	var name, newtID, newtSecret string
	err := s.db.QueryRowContext(ctx,
		`SELECT name, pangolin_site_id, newt_id, newt_secret FROM agents WHERE id = $1`, agentID).
		Scan(&name, &siteID, &newtID, &newtSecret)
	if err != nil {
		log.Printf("ensure tunnel: %v", err)
		return nil
	}
	if siteID.Valid {
		return &Tunnel{SiteRef: strconv.FormatInt(siteID.Int64, 10), Endpoint: s.settings.get().PangolinEndpoint, NewtID: newtID, NewtSecret: newtSecret}
	}
	if !s.pangolin.Enabled() {
		s.db.ExecContext(ctx, `UPDATE agents SET tunnel_error = $1 WHERE id = $2`, ErrPangolinDisabled.Error(), agentID)
		return nil
	}

	t, err := s.pangolin.CreateSite(ctx, name)
	if err != nil {
		log.Printf("create pangolin site for agent %s: %v", agentID, err)
		s.db.ExecContext(ctx, `UPDATE agents SET tunnel_error = $1 WHERE id = $2`,
			fmt.Sprintf("Could not create Pangolin tunnel: %v", err), agentID)
		return nil
	}
	siteRef, _ := strconv.Atoi(t.SiteRef)
	_, err = s.db.ExecContext(ctx,
		`UPDATE agents SET pangolin_site_id = $1, newt_id = $2, newt_secret = $3, tunnel_error = '' WHERE id = $4`,
		siteRef, t.NewtID, t.NewtSecret, agentID)
	if err != nil {
		log.Printf("save tunnel for agent %s: %v", agentID, err)
		return nil
	}
	return &t
}

// markOfflineAgents runs in the background. Agents heartbeat every ~10s;
// after 30s of silence they're shown as offline.
func (s *Server) markOfflineAgents() {
	for range time.Tick(10 * time.Second) {
		rows, err := s.db.Query(`UPDATE agents SET status = 'OFFLINE'
			WHERE status = 'ONLINE' AND (last_seen IS NULL OR last_seen < now() - interval '30 seconds')
			RETURNING id, user_id, name`)
		if err != nil {
			log.Printf("mark offline agents: %v", err)
			continue
		}
		type off struct{ id, uid, name string }
		var list []off
		for rows.Next() {
			var o off
			if rows.Scan(&o.id, &o.uid, &o.name) == nil {
				list = append(list, o)
			}
		}
		rows.Close()
		for _, o := range list {
			s.event(context.Background(), o.uid, "error", "Agent "+o.name+" went offline", withAgent(o.id))
		}
	}
}
