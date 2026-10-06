package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// Custom domains (app.example.com). Insta Deploy registers the domain with
// Pangolin, shows the DNS records to create, waits for Pangolin to verify
// them, then routes the domain to the chosen service. DNS providers aren't
// automated yet: the user creates the records by hand.

type Domain struct {
	ID           string      `json:"id"`
	Hostname     string      `json:"hostname"`
	ProjectID    *string     `json:"project_id"`
	ServiceID    *string     `json:"service_id"`
	Target       *string     `json:"target"` // "deployment / service", for display
	DeploymentID *string     `json:"deployment_id"`
	Status       string      `json:"status"`
	Error        string      `json:"error"`
	DNSRecords   []DNSRecord `json:"dns_records"`
	SSLStatus    string      `json:"ssl_status"`
	RouteStatus  *string     `json:"route_status"`
	RouteError   *string     `json:"route_error"`
	CreatedAt    time.Time   `json:"created_at"`
}

const domainSelect = `SELECT dm.id, dm.hostname, dm.project_id, dm.service_id,
	CASE WHEN sv.id IS NULL THEN NULL ELSE d.name || ' / ' || sv.name END, d.id,
	dm.status, dm.error, dm.dns_records, dm.ssl_status, r.status, r.error, dm.created_at
	FROM domains dm
	LEFT JOIN services sv ON sv.id = dm.service_id
	LEFT JOIN deployments d ON d.id = sv.deployment_id
	LEFT JOIN routes r ON r.domain_id = dm.id`

func scanDomain(row interface{ Scan(...any) error }) (Domain, error) {
	var d Domain
	var records []byte
	err := row.Scan(&d.ID, &d.Hostname, &d.ProjectID, &d.ServiceID, &d.Target, &d.DeploymentID,
		&d.Status, &d.Error, &records, &d.SSLStatus, &d.RouteStatus, &d.RouteError, &d.CreatedAt)
	json.Unmarshal(records, &d.DNSRecords)
	if d.DNSRecords == nil {
		d.DNSRecords = []DNSRecord{}
	}
	return d, err
}

func (s *Server) handleListDomains(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), domainSelect+` WHERE dm.user_id = $1 ORDER BY dm.hostname`, userID(r))
	if err != nil {
		internalError(w, err)
		return
	}
	defer rows.Close()
	list := []Domain{}
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			internalError(w, err)
			return
		}
		list = append(list, d)
	}
	writeJSON(w, http.StatusOK, list)
}

// userService checks a service belongs to the user and returns its project.
func (s *Server) userService(r *http.Request, serviceID string) (projectID string, err error) {
	if !validUUID(serviceID) {
		return "", sql.ErrNoRows
	}
	err = s.db.QueryRowContext(r.Context(), `SELECT d.project_id FROM services sv JOIN deployments d ON d.id = sv.deployment_id
		JOIN projects p ON p.id = d.project_id WHERE sv.id = $1 AND p.user_id = $2`, serviceID, userID(r)).Scan(&projectID)
	return
}

func (s *Server) handleCreateDomain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Hostname  string `json:"hostname"`
		ServiceID string `json:"service_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(req.Hostname), "."))
	if err := validateHostname(host); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if host == s.settings.get().AppsDomain || strings.HasSuffix(host, "."+s.settings.get().AppsDomain) {
		writeError(w, http.StatusBadRequest, "addresses under "+s.settings.get().AppsDomain+" are generated automatically; use a domain of your own")
		return
	}
	var serviceID, projectID any
	if req.ServiceID != "" {
		p, err := s.userService(r, req.ServiceID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "choose one of your services")
			return
		}
		serviceID, projectID = req.ServiceID, p
	}
	if !s.pangolin.Enabled() {
		writeError(w, http.StatusConflict, "Custom domains need Pangolin. Configure it first (see docs/pangolin.md).")
		return
	}

	var id string
	err := s.db.QueryRowContext(r.Context(), `INSERT INTO domains (user_id, project_id, hostname, service_id) VALUES ($1, $2, $3, $4) RETURNING id`,
		userID(r), projectID, host, serviceID).Scan(&id)
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, host+" has already been added")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}

	reg, err := s.pangolin.CreateDomain(r.Context(), host)
	if err != nil {
		s.db.ExecContext(r.Context(), `DELETE FROM domains WHERE id = $1`, id)
		writeError(w, http.StatusBadGateway, "Pangolin could not add the domain: "+err.Error())
		return
	}
	records, _ := json.Marshal(reg.Records)
	s.db.ExecContext(r.Context(), `UPDATE domains SET provider_ref = $1, dns_records = $2 WHERE id = $3`, reg.Ref, records, id)
	s.event(r.Context(), userID(r), "info", "Added domain "+host, eventRefs{})

	d, _ := scanDomain(s.db.QueryRowContext(r.Context(), domainSelect+` WHERE dm.id = $1`, id))
	writeJSON(w, http.StatusCreated, d)
}

// handleUpdateDomain points the domain at a different service (or none).
func (s *Server) handleUpdateDomain(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		ServiceID *string `json:"service_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	var serviceID, projectID any
	if req.ServiceID != nil && *req.ServiceID != "" {
		p, err := s.userService(r, *req.ServiceID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "choose one of your services")
			return
		}
		serviceID, projectID = *req.ServiceID, p
	}
	res, err := s.db.ExecContext(r.Context(), `UPDATE domains SET service_id = $1, project_id = COALESCE($2, project_id), updated_at = now()
		WHERE id = $3 AND user_id = $4`, serviceID, projectID, id, userID(r))
	if err != nil || !validUUID(id) {
		writeError(w, http.StatusNotFound, "domain not found")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "domain not found")
		return
	}
	s.kickRoutes()
	d, _ := scanDomain(s.db.QueryRowContext(r.Context(), domainSelect+` WHERE dm.id = $1`, id))
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleDeleteDomain(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var host, ref string
	err := sql.ErrNoRows
	if validUUID(id) {
		err = s.db.QueryRowContext(r.Context(), `SELECT hostname, provider_ref FROM domains WHERE id = $1 AND user_id = $2`, id, userID(r)).Scan(&host, &ref)
	}
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "domain not found")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	// Route first, then the domain itself.
	var routeRef string
	s.db.QueryRowContext(r.Context(), `SELECT provider_ref FROM routes WHERE domain_id = $1`, id).Scan(&routeRef)
	if routeRef != "" {
		if err := s.pangolin.DeleteRoute(r.Context(), routeRef); err != nil {
			writeError(w, http.StatusBadGateway, "Could not remove the route from Pangolin: "+err.Error())
			return
		}
	}
	if ref != "" {
		if err := s.pangolin.DeleteDomain(r.Context(), ref); err != nil && !errors.Is(err, ErrPangolinDisabled) {
			writeError(w, http.StatusBadGateway, "Could not remove the domain from Pangolin: "+err.Error())
			return
		}
	}
	s.db.ExecContext(r.Context(), `DELETE FROM domains WHERE id = $1`, id)
	s.event(r.Context(), userID(r), "info", "Removed domain "+host, eventRefs{})
	w.WriteHeader(http.StatusNoContent)
}

// handleRetryDeploymentRoutes retries FAILED public URLs of a deployment.
func (s *Server) handleRetryDeploymentRoutes(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDeployment(w, r)
	if !ok {
		return
	}
	s.retryRoutes(r.Context(), d.ID)
	s.respondDeployment(w, r, d.ID)
}

// handleVerifyDomain asks Pangolin to check the DNS records again now
// (the "Check again" button).
func (s *Server) handleVerifyDomain(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var ref, status string
	var rawRecords []byte
	err := sql.ErrNoRows
	if validUUID(id) {
		err = s.db.QueryRowContext(r.Context(), `SELECT provider_ref, status, dns_records FROM domains WHERE id = $1 AND user_id = $2`,
			id, userID(r)).Scan(&ref, &status, &rawRecords)
	}
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "domain not found")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	if status == "PENDING" {
		var records []DNSRecord
		json.Unmarshal(rawRecords, &records)
		missing, err := missingDNSRecords(r.Context(), records)
		if err != nil {
			writeError(w, http.StatusBadGateway, "Could not check DNS: "+err.Error())
			return
		}
		if len(missing) > 0 {
			msg := "Waiting for DNS: " + strings.Join(missing, "; ")
			s.db.ExecContext(r.Context(), `UPDATE domains SET error = $1, updated_at = now() WHERE id = $2`, msg, id)
			writeError(w, http.StatusConflict, msg+". New records can take a few minutes to appear.")
			return
		}
		if err := s.pangolin.RestartDomain(r.Context(), ref); err != nil {
			writeError(w, http.StatusBadGateway, "Pangolin could not re-check the domain: "+err.Error())
			return
		}
		s.db.ExecContext(r.Context(), `UPDATE domains SET error = 'The DNS records are in place; waiting for Pangolin to verify them.', updated_at = now() WHERE id = $1`, id)
	}
	d, _ := scanDomain(s.db.QueryRowContext(r.Context(), domainSelect+` WHERE dm.id = $1`, id))
	writeJSON(w, http.StatusOK, d)
}
