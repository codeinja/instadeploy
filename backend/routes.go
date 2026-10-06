package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// The route reconciler keeps public URLs in line with the database:
//
//   - every public service with a port gets a GENERATED route
//     (<name>-xxxx.<APPS_DOMAIN>) once it's running,
//   - every ACTIVE custom domain pointed at a service gets a CUSTOM route,
//   - routes whose service became private (or whose domain moved) are
//     removed, and routes whose port or machine changed are updated.
//
// It runs every 20 seconds and right after anything relevant changes
// (kickRoutes), so a deploy finishing creates its URL within moments.

func (s *Server) kickRoutes() {
	select {
	case s.routeKick <- struct{}{}:
	default:
	}
}

func (s *Server) runRouteReconciler() {
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		if err := s.reconcileRoutes(ctx); err != nil {
			log.Printf("route reconciler: %v", err)
		}
		cancel()
		select {
		case <-tick.C:
		case <-s.routeKick:
		}
	}
}

func (s *Server) reconcileRoutes(ctx context.Context) error {
	if err := s.ensureGeneratedRoutes(ctx); err != nil {
		return err
	}
	if err := s.ensureCustomRoutes(ctx); err != nil {
		return err
	}
	if err := s.removeStaleRoutes(ctx); err != nil {
		return err
	}
	return s.syncRoutes(ctx)
}

// ensureGeneratedRoutes adds a route row for public services that lack one.
func (s *Server) ensureGeneratedRoutes(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT sv.id, sv.name, sv.hostname, d.id, d.name, d.type FROM services sv
		JOIN deployments d ON d.id = sv.deployment_id
		WHERE sv.public AND sv.port IS NOT NULL AND d.status <> 'DELETING'
		  AND NOT EXISTS (SELECT 1 FROM routes r WHERE r.service_id = sv.id AND r.kind = 'GENERATED')`)
	if err != nil {
		return err
	}
	type cand struct{ serviceID, service, hostname, depID, depName, typ string }
	var list []cand
	for rows.Next() {
		var c cand
		if rows.Scan(&c.serviceID, &c.service, &c.hostname, &c.depID, &c.depName, &c.typ) == nil {
			list = append(list, c)
		}
	}
	rows.Close()

	initial := RoutePending
	if !s.pangolin.Enabled() {
		initial = RouteDisabled
	}
	for _, c := range list {
		label := s.hostLabel(ctx, c.depID, c.depName, c.typ, c.service)
		for attempt := 0; attempt < 5; attempt++ {
			// Reuse the service's earlier hostname, so its URL doesn't change.
			host := c.hostname
			if host == "" || attempt > 0 || !strings.HasSuffix(host, "."+s.settings.get().AppsDomain) {
				host = generatedHostname(label, s.settings.get().AppsDomain)
			}
			_, err := s.db.ExecContext(ctx, `INSERT INTO routes (service_id, deployment_id, kind, hostname, url, status)
				VALUES ($1, $2, 'GENERATED', $3, $4, $5)`, c.serviceID, c.depID, host, "https://"+host, initial)
			if err == nil {
				s.db.ExecContext(ctx, `UPDATE services SET hostname = $1 WHERE id = $2`, host, c.serviceID)
			}
			if err == nil || !isUniqueViolation(err) {
				break
			}
		}
	}
	return nil
}

var nonLabelRe = regexp.MustCompile(`[^a-z0-9-]+`)

// hostLabel is the first part of a generated hostname: the deployment's
// name, plus the service name when a Compose project has several public
// services (so each gets its own address).
func (s *Server) hostLabel(ctx context.Context, depID, depName, typ, service string) string {
	if typ != TypeCompose {
		return depName
	}
	var public int
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM services WHERE deployment_id = $1 AND public`, depID).Scan(&public)
	if public <= 1 {
		return depName
	}
	return depName + "-" + dnsLabel(service)
}

func dnsLabel(s string) string {
	return strings.Trim(nonLabelRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// generatedHostname returns "<label>-<4 hex>.<domain>", keeping the first
// DNS label within 63 characters.
func generatedHostname(label, domain string) string {
	if len(label) > 58 {
		label = strings.TrimRight(label[:58], "-")
	}
	return label + "-" + randomHex(2) + "." + domain
}

// ensureCustomRoutes adds a route for each ACTIVE domain that points at a
// service.
func (s *Server) ensureCustomRoutes(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO routes (service_id, deployment_id, kind, domain_id, hostname, url, status)
		SELECT sv.id, sv.deployment_id, 'CUSTOM', dm.id, dm.hostname, 'https://' || dm.hostname, 'PENDING'
		FROM domains dm JOIN services sv ON sv.id = dm.service_id
		WHERE dm.status = 'ACTIVE' AND sv.port IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM routes r WHERE r.domain_id = dm.id)
		ON CONFLICT (hostname) DO NOTHING`)
	return err
}

// removeStaleRoutes deletes routes that shouldn't exist any more.
func (s *Server) removeStaleRoutes(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT r.id, r.provider_ref FROM routes r
		JOIN services sv ON sv.id = r.service_id
		LEFT JOIN domains dm ON dm.id = r.domain_id
		WHERE (r.kind = 'GENERATED' AND (NOT sv.public OR sv.port IS NULL))
		   OR (r.kind = 'CUSTOM' AND (dm.id IS NULL OR dm.service_id IS DISTINCT FROM r.service_id OR dm.status <> 'ACTIVE' OR sv.port IS NULL))`)
	if err != nil {
		return err
	}
	type stale struct{ id, ref string }
	var list []stale
	for rows.Next() {
		var x stale
		if rows.Scan(&x.id, &x.ref) == nil {
			list = append(list, x)
		}
	}
	rows.Close()
	for _, x := range list {
		if x.ref != "" {
			if err := s.pangolin.DeleteRoute(ctx, x.ref); err != nil && err != ErrPangolinDisabled {
				log.Printf("delete route %s: %v", x.ref, err)
				continue // try again next round
			}
		}
		s.db.ExecContext(ctx, `DELETE FROM routes WHERE id = $1`, x.id)
	}
	return nil
}

type routeWork struct {
	id, kind, hostname, status, ref         string
	targetHost, siteRef                     string
	targetPort                              int
	wantHost, wantSite, depID, depName, uid string
	domainRef                               sql.NullString
	wantPort                                int
	running                                 bool
}

// syncRoutes creates pending routes for running services and updates
// routes whose target changed.
func (s *Server) syncRoutes(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT r.id, r.kind, r.hostname, r.status, r.provider_ref, r.target_host, r.target_port, r.site_ref,
		sv.target_host, sv.port, COALESCE(a.pangolin_site_id::text, ''), d.id, d.name, p.user_id, dm.provider_ref,
		(sv.state = 'running' OR d.status = 'RUNNING')
		FROM routes r
		JOIN services sv ON sv.id = r.service_id
		JOIN deployments d ON d.id = r.deployment_id
		JOIN projects p ON p.id = d.project_id
		JOIN agents a ON a.id = d.agent_id
		LEFT JOIN domains dm ON dm.id = r.domain_id
		WHERE d.status <> 'DELETING' AND sv.port IS NOT NULL
		  AND (r.status IN ('PENDING', 'DISABLED')
		       OR (r.status = 'READY' AND (r.target_host <> sv.target_host OR r.target_port <> sv.port OR r.site_ref <> COALESCE(a.pangolin_site_id::text, ''))))`)
	if err != nil {
		return err
	}
	var list []routeWork
	for rows.Next() {
		var w routeWork
		if err := rows.Scan(&w.id, &w.kind, &w.hostname, &w.status, &w.ref, &w.targetHost, &w.targetPort, &w.siteRef,
			&w.wantHost, &w.wantPort, &w.wantSite, &w.depID, &w.depName, &w.uid, &w.domainRef, &w.running); err != nil {
			rows.Close()
			return err
		}
		list = append(list, w)
	}
	rows.Close()

	for _, w := range list {
		if !s.pangolin.Enabled() {
			s.db.ExecContext(ctx, `UPDATE routes SET status = 'DISABLED', error = '' WHERE id = $1 AND status <> 'DISABLED'`, w.id)
			continue
		}
		target := RouteTarget{SiteRef: w.wantSite, Host: w.wantHost, Port: w.wantPort}
		if w.status == RouteReady {
			if err := s.pangolin.UpdateRoute(ctx, w.ref, target); err != nil {
				s.routeFailed(ctx, w, "Could not update the public route: "+err.Error())
				continue
			}
			s.db.ExecContext(ctx, `UPDATE routes SET target_host = $1, target_port = $2, site_ref = $3, updated_at = now() WHERE id = $4`,
				w.wantHost, w.wantPort, w.wantSite, w.id)
			continue
		}
		if !w.running {
			continue // wait until the container is up
		}
		if w.wantSite == "" {
			s.routeFailed(ctx, w, "The machine has no Pangolin tunnel. See the Agents page for details.")
			continue
		}
		s.createRoute(ctx, w, target)
	}
	return nil
}

func (s *Server) createRoute(ctx context.Context, w routeWork, target RouteTarget) {
	// Claim it, so overlapping runs can't create it twice.
	res, err := s.db.ExecContext(ctx, `UPDATE routes SET status = 'CREATING', error = '' WHERE id = $1 AND status IN ('PENDING', 'DISABLED')`, w.id)
	if n, _ := res.RowsAffected(); err != nil || n == 0 {
		return
	}
	req := RouteRequest{Name: w.depName, Target: target}
	if w.kind == "CUSTOM" {
		req.DomainRef = w.domainRef.String
	} else {
		req.Subdomain = strings.TrimSuffix(w.hostname, "."+s.settings.get().AppsDomain)
	}
	route, err := s.pangolin.CreateRoute(ctx, req)
	if err != nil {
		s.routeFailed(ctx, w, "Container started, but the public route could not be created: "+err.Error())
		return
	}
	res, err = s.db.ExecContext(ctx, `UPDATE routes SET status = 'READY', url = $1, provider_ref = $2, target_host = $3,
		target_port = $4, site_ref = $5, error = '', updated_at = now() WHERE id = $6`,
		route.URL, route.Ref, target.Host, target.Port, target.SiteRef, w.id)
	if n, _ := res.RowsAffected(); err != nil || n == 0 {
		s.pangolin.DeleteRoute(context.WithoutCancel(ctx), route.Ref) // route row vanished meanwhile
		return
	}
	s.logLine(ctx, w.depID, "", "deploy", "Pangolin route created. Public URL: "+route.URL)
	s.event(ctx, w.uid, "success", fmt.Sprintf("Public URL created for %s: %s", w.depName, route.URL), eventRefs{deploymentID: w.depID})
}

func (s *Server) routeFailed(ctx context.Context, w routeWork, msg string) {
	log.Printf("route %s (%s): %s", w.id, w.hostname, msg)
	s.db.ExecContext(ctx, `UPDATE routes SET status = 'FAILED', error = $1, updated_at = now() WHERE id = $2`, trunc(msg, 2000), w.id)
	s.logLine(ctx, w.depID, "", "deploy", msg)
	s.event(ctx, w.uid, "error", w.depName+": "+msg, eventRefs{deploymentID: w.depID})
}

// retryRoutes puts a deployment's FAILED routes back in the queue.
func (s *Server) retryRoutes(ctx context.Context, deploymentID string) {
	s.db.ExecContext(ctx, `UPDATE routes SET status = 'PENDING', error = '' WHERE deployment_id = $1 AND status = 'FAILED'`, deploymentID)
	s.kickRoutes()
}

// ---------------------------------------------------------------- domains

// runDomainVerifier checks pending custom domains with Pangolin and checks
// active domains' certificates.
func (s *Server) runDomainVerifier() {
	for range time.Tick(30 * time.Second) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		s.verifyDomains(ctx)
		cancel()
	}
}

func (s *Server) verifyDomains(ctx context.Context) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, hostname, status, provider_ref, ssl_status, updated_at, dns_records FROM domains
		WHERE provider_ref <> '' AND (status = 'PENDING' OR (status = 'ACTIVE' AND (ssl_status <> 'VALID' OR updated_at < now() - interval '6 hours')))`)
	if err != nil {
		log.Printf("domain verifier: %v", err)
		return
	}
	type dom struct {
		id, uid, host, status, ref, ssl string
		records                         []DNSRecord
	}
	var list []dom
	for rows.Next() {
		var d dom
		var updated time.Time
		var records []byte
		if rows.Scan(&d.id, &d.uid, &d.host, &d.status, &d.ref, &d.ssl, &updated, &records) == nil {
			json.Unmarshal(records, &d.records)
			list = append(list, d)
		}
	}
	rows.Close()

	for _, d := range list {
		if d.status == "PENDING" {
			st, err := s.pangolin.GetDomain(ctx, d.ref)
			switch {
			case err != nil:
				log.Printf("check domain %s: %v", d.host, err)
			case st.Verified:
				s.db.ExecContext(ctx, `UPDATE domains SET status = 'ACTIVE', error = '', updated_at = now() WHERE id = $1`, d.id)
				s.event(ctx, d.uid, "success", "Domain "+d.host+" verified", eventRefs{})
				s.kickRoutes()
			default:
				s.recheckDomain(ctx, d.id, d.ref, d.records, st)
			}
			continue
		}
		ssl := checkTLS(d.host)
		s.db.ExecContext(ctx, `UPDATE domains SET ssl_status = $1, updated_at = now() WHERE id = $2`, ssl, d.id)
	}
}

// recheckDomain handles a domain Pangolin hasn't verified yet. While the
// records aren't in DNS there's nothing to do but wait (and say which
// records are missing). Once they are, and Pangolin has given up checking,
// ask it to check again.
func (s *Server) recheckDomain(ctx context.Context, id, ref string, records []DNSRecord, st DomainStatus) {
	missing, err := missingDNSRecords(ctx, records)
	msg := ""
	switch {
	case err != nil:
		log.Printf("check DNS records: %v", err)
	case len(missing) > 0:
		msg = "Waiting for DNS: " + strings.Join(missing, "; ")
	case st.Failed:
		msg = "The DNS records are in place; waiting for Pangolin to verify them."
		s.domainRetry.Lock()
		due := time.Since(s.domainRetry.last[id]) > 2*time.Minute
		if due {
			if s.domainRetry.last == nil {
				s.domainRetry.last = map[string]time.Time{}
			}
			s.domainRetry.last[id] = time.Now()
		}
		s.domainRetry.Unlock()
		if due {
			if err := s.pangolin.RestartDomain(ctx, ref); err != nil {
				log.Printf("restart verification of domain %s: %v", id, err)
				msg = "Pangolin could not re-check the DNS records: " + err.Error()
			}
		}
	default:
		msg = "The DNS records are in place; waiting for Pangolin to verify them."
	}
	s.db.ExecContext(ctx, `UPDATE domains SET error = $1, updated_at = now() WHERE id = $2`, msg, id)
}

// missingDNSRecords checks each record against public DNS and describes
// the ones that are missing or point somewhere else. It asks public
// resolvers over HTTPS, so a stale "not found" cached by the server's own
// resolver (from before the user added the records) doesn't get in the way.
func missingDNSRecords(ctx context.Context, records []DNSRecord) ([]string, error) {
	var missing []string
	for _, r := range records {
		if r.Type != "CNAME" && r.Type != "A" && r.Type != "TXT" {
			continue
		}
		values, err := publicDNSLookup(ctx, r.Name, r.Type)
		if err != nil {
			return nil, err
		}
		want := normalizeDNS(r.Value)
		found := false
		for _, v := range values {
			found = found || normalizeDNS(v) == want
		}
		switch {
		case found:
		case len(values) > 0:
			missing = append(missing, fmt.Sprintf("%s %s points to %s instead of %s", r.Type, r.Name, strings.TrimSuffix(values[0], "."), r.Value))
		default:
			missing = append(missing, fmt.Sprintf("%s %s not found", r.Type, r.Name))
		}
	}
	return missing, nil
}

func normalizeDNS(s string) string {
	return strings.ToLower(strings.Trim(strings.TrimSuffix(strings.TrimSpace(s), "."), `"`))
}

var dnsTypes = map[string]int{"A": 1, "CNAME": 5, "TXT": 16}

// publicDNSLookup queries Cloudflare's DNS-over-HTTPS JSON API, falling
// back to Google's.
func publicDNSLookup(ctx context.Context, name, typ string) ([]string, error) {
	var lastErr error
	for _, base := range []string{"https://cloudflare-dns.com/dns-query", "https://dns.google/resolve"} {
		ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		resp, err := safeGet(ctx, base+"?name="+url.QueryEscape(name)+"&type="+typ, http.Header{"Accept": {"application/dns-json"}})
		if err != nil {
			cancel()
			lastErr = err
			continue
		}
		var out struct {
			Status int `json:"Status"`
			Answer []struct {
				Type int    `json:"type"`
				Data string `json:"data"`
			} `json:"Answer"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out)
		resp.Body.Close()
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		var values []string
		for _, a := range out.Answer {
			if a.Type == dnsTypes[typ] {
				values = append(values, a.Data)
			}
		}
		return values, nil
	}
	return nil, fmt.Errorf("public DNS lookup failed: %w", lastErr)
}

// checkTLS connects to host:443 and reports VALID if it serves a trusted
// certificate for that name, PENDING otherwise.
func checkTLS(host string) string {
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", host+":443", &tls.Config{ServerName: host})
	if err != nil {
		return "PENDING"
	}
	conn.Close()
	return "VALID"
}
