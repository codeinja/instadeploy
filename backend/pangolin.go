package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// PangolinService is the only code that knows how Pangolin works. It talks
// to Pangolin's Integration API (https://docs.pangolin.net) and offers the
// rest of the backend a few provider-neutral operations:
//
//	Tunnels  CreateSite / DeleteSite        one Pangolin "site" per agent
//	Routes   CreateRoute / UpdateRoute /    one Pangolin "resource" (public
//	         DeleteRoute / GetRoute /       hostname) with one "target"
//	         GetRouteStatus                 (container:port via the site)
//	Domains  CreateDomain / GetDomain /     custom domains like app.example.com
//	         DeleteDomain
//
// IDs are passed around as opaque strings ("refs"), so another tunnel
// provider could implement the same operations later.
type PangolinService struct {
	settings func() *Settings // read on every call, so dashboard changes apply at once
	client   *http.Client

	mu          sync.Mutex
	domainID    string // Pangolin domain ID for the apps domain, looked up once
	domainIDFor string // the org/domain that domainID was looked up for
}

var ErrPangolinDisabled = errors.New("Pangolin isn't connected yet. Connect it in Settings → Public URLs")

func NewPangolinService(settings func() *Settings) *PangolinService {
	return &PangolinService{settings: settings, client: &http.Client{Timeout: 20 * time.Second}}
}

func (p *PangolinService) st() *Settings { return p.settings() }

func (p *PangolinService) Enabled() bool { return p.st().PangolinEnabled() }

// Tunnel is what an agent needs to connect its machine to Pangolin.
type Tunnel struct {
	SiteRef    string `json:"-"`
	Endpoint   string `json:"endpoint"`
	NewtID     string `json:"newt_id"`
	NewtSecret string `json:"newt_secret"`
}

// RouteTarget is where public traffic for a route goes: Host:Port, reached
// through the tunnel (site) of the agent running the container.
type RouteTarget struct {
	SiteRef string
	Host    string
	Port    int
}

type RouteRequest struct {
	Name string
	// Either a subdomain of APPS_DOMAIN ("nginx-a81f") or a custom domain
	// registered with CreateDomain (DomainRef set, Subdomain empty).
	Subdomain string
	DomainRef string
	Target    RouteTarget
}

// ProviderRoute is a route as the provider knows it.
type ProviderRoute struct {
	Ref string
	URL string
}

type RouteStatus struct {
	Enabled bool
	SSL     bool
	Health  string
}

type DNSRecord struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

type DomainRegistration struct {
	Ref     string
	Records []DNSRecord
}

type DomainStatus struct {
	Verified bool
	Failed   bool
	Message  string
}

// ---------------------------------------------------------------- tunnels

// CreateSite creates a Pangolin site (one per agent) and returns the newt
// credentials the agent uses to open the tunnel.
func (p *PangolinService) CreateSite(ctx context.Context, name string) (Tunnel, error) {
	if !p.Enabled() {
		return Tunnel{}, ErrPangolinDisabled
	}
	org := p.st().PangolinOrgID

	var defaults struct {
		ExitNodeID int    `json:"exitNodeId"`
		Subnet     string `json:"subnet"`
		NewtID     string `json:"newtId"`
		NewtSecret string `json:"newtSecret"`
		Address    string `json:"clientAddress"`
	}
	if err := p.do(ctx, "GET", "/org/"+org+"/pick-site-defaults", nil, &defaults); err != nil {
		return Tunnel{}, fmt.Errorf("pick site defaults: %w", err)
	}

	body := map[string]any{
		"name":       "insta-deploy: " + name,
		"type":       "newt",
		"exitNodeId": defaults.ExitNodeID,
		"subnet":     defaults.Subnet,
		"newtId":     defaults.NewtID,
		"secret":     defaults.NewtSecret,
	}
	if defaults.Address != "" {
		body["address"] = defaults.Address
	}
	var site struct {
		SiteID int `json:"siteId"`
	}
	if err := p.do(ctx, "PUT", "/org/"+org+"/site", body, &site); err != nil {
		return Tunnel{}, fmt.Errorf("create site: %w", err)
	}
	return Tunnel{
		SiteRef:    strconv.Itoa(site.SiteID),
		Endpoint:   p.st().PangolinEndpoint,
		NewtID:     defaults.NewtID,
		NewtSecret: defaults.NewtSecret,
	}, nil
}

func (p *PangolinService) DeleteSite(ctx context.Context, siteRef string) error {
	if !p.Enabled() {
		return ErrPangolinDisabled
	}
	return p.do(ctx, "DELETE", "/site/"+siteRef, nil, nil)
}

// ----------------------------------------------------------------- routes

// CreateRoute publishes a hostname and forwards it through the agent's
// tunnel to the target.
func (p *PangolinService) CreateRoute(ctx context.Context, req RouteRequest) (ProviderRoute, error) {
	if !p.Enabled() {
		return ProviderRoute{}, ErrPangolinDisabled
	}
	siteID, err := strconv.Atoi(req.Target.SiteRef)
	if err != nil {
		return ProviderRoute{}, fmt.Errorf("the agent has no tunnel yet")
	}

	body := map[string]any{
		"name":     "insta-deploy: " + req.Name,
		"http":     true,
		"protocol": "tcp",
	}
	if req.DomainRef != "" {
		body["domainId"] = req.DomainRef
	} else {
		domainID, err := p.appsDomainID(ctx)
		if err != nil {
			return ProviderRoute{}, err
		}
		body["domainId"] = domainID
		body["subdomain"] = req.Subdomain
	}

	var res struct {
		ResourceID int    `json:"resourceId"`
		FullDomain string `json:"fullDomain"`
	}
	if err := p.do(ctx, "PUT", "/org/"+p.st().PangolinOrgID+"/resource", body, &res); err != nil {
		return ProviderRoute{}, fmt.Errorf("create resource: %w", err)
	}
	ref := strconv.Itoa(res.ResourceID)

	// From here on, clean up the half-created resource if anything fails.
	fail := func(step string, err error) (ProviderRoute, error) {
		p.DeleteRoute(context.WithoutCancel(ctx), ref)
		return ProviderRoute{}, fmt.Errorf("%s: %w", step, err)
	}

	err = p.do(ctx, "PUT", "/resource/"+ref+"/target", map[string]any{
		"siteId":  siteID,
		"ip":      req.Target.Host,
		"port":    req.Target.Port,
		"method":  "http",
		"enabled": true,
	}, nil)
	if err != nil {
		return fail("create target", err)
	}

	// Pangolin protects new resources with its own login by default.
	// Insta Deploy URLs are meant to be public, so turn that off.
	if err := p.do(ctx, "POST", "/resource/"+ref, map[string]any{"sso": false}, nil); err != nil {
		return fail("make resource public", err)
	}

	host := res.FullDomain
	if host == "" {
		host = req.Subdomain + "." + p.st().AppsDomain
	}
	return ProviderRoute{Ref: ref, URL: "https://" + host}, nil
}

// UpdateRoute points an existing route at a new target (for example when a
// service's port changes).
func (p *PangolinService) UpdateRoute(ctx context.Context, ref string, target RouteTarget) error {
	if !p.Enabled() {
		return ErrPangolinDisabled
	}
	siteID, err := strconv.Atoi(target.SiteRef)
	if err != nil {
		return fmt.Errorf("the agent has no tunnel yet")
	}
	var targets struct {
		Targets []struct {
			TargetID int `json:"targetId"`
		} `json:"targets"`
	}
	if err := p.do(ctx, "GET", "/resource/"+ref+"/targets", nil, &targets); err != nil {
		return fmt.Errorf("list targets: %w", err)
	}
	body := map[string]any{"siteId": siteID, "ip": target.Host, "port": target.Port, "method": "http", "enabled": true}
	if len(targets.Targets) == 0 {
		return p.do(ctx, "PUT", "/resource/"+ref+"/target", body, nil)
	}
	return p.do(ctx, "POST", fmt.Sprintf("/target/%d", targets.Targets[0].TargetID), body, nil)
}

func (p *PangolinService) DeleteRoute(ctx context.Context, ref string) error {
	if !p.Enabled() {
		return ErrPangolinDisabled
	}
	err := p.do(ctx, "DELETE", "/resource/"+ref, nil, nil)
	if isNotFound(err) {
		return nil // already gone
	}
	return err
}

func (p *PangolinService) GetRoute(ctx context.Context, ref string) (ProviderRoute, error) {
	if !p.Enabled() {
		return ProviderRoute{}, ErrPangolinDisabled
	}
	var res struct {
		FullDomain string `json:"fullDomain"`
	}
	if err := p.do(ctx, "GET", "/resource/"+ref, nil, &res); err != nil {
		return ProviderRoute{}, err
	}
	return ProviderRoute{Ref: ref, URL: "https://" + res.FullDomain}, nil
}

func (p *PangolinService) GetRouteStatus(ctx context.Context, ref string) (RouteStatus, error) {
	if !p.Enabled() {
		return RouteStatus{}, ErrPangolinDisabled
	}
	var res struct {
		Enabled bool   `json:"enabled"`
		SSL     bool   `json:"ssl"`
		Health  string `json:"health"`
	}
	if err := p.do(ctx, "GET", "/resource/"+ref, nil, &res); err != nil {
		return RouteStatus{}, err
	}
	return RouteStatus{Enabled: res.Enabled, SSL: res.SSL, Health: res.Health}, nil
}

// ---------------------------------------------------------------- domains

// CreateDomain registers a custom domain with Pangolin and returns the DNS
// records the user has to create. Pangolin Cloud accepts single-hostname
// CNAME domains; self-hosted Pangolin only accepts "wildcard" domains, so
// fall back to that.
func (p *PangolinService) CreateDomain(ctx context.Context, hostname string) (DomainRegistration, error) {
	if !p.Enabled() {
		return DomainRegistration{}, ErrPangolinDisabled
	}
	type record struct {
		BaseDomain string `json:"baseDomain"`
		Value      string `json:"value"`
	}
	var res struct {
		DomainID     string   `json:"domainId"`
		NSRecords    []string `json:"nsRecords"`
		CNAMERecords []record `json:"cnameRecords"`
		ARecords     []record `json:"aRecords"`
		TXTRecords   []record `json:"txtRecords"`
	}
	path := "/org/" + p.st().PangolinOrgID + "/domain"
	err := p.do(ctx, "PUT", path, map[string]any{"type": "cname", "baseDomain": hostname}, &res)
	if err != nil && strings.Contains(err.Error(), "not supported") {
		err = p.do(ctx, "PUT", path, map[string]any{"type": "wildcard", "baseDomain": hostname}, &res)
	}
	if err != nil {
		return DomainRegistration{}, err
	}

	reg := DomainRegistration{Ref: res.DomainID}
	for _, ns := range res.NSRecords {
		reg.Records = append(reg.Records, DNSRecord{Type: "NS", Name: hostname, Value: ns})
	}
	for _, r := range res.CNAMERecords {
		reg.Records = append(reg.Records, DNSRecord{Type: "CNAME", Name: r.BaseDomain, Value: r.Value})
	}
	for _, r := range res.ARecords {
		reg.Records = append(reg.Records, DNSRecord{Type: "A", Name: r.BaseDomain, Value: r.Value})
	}
	for _, r := range res.TXTRecords {
		reg.Records = append(reg.Records, DNSRecord{Type: "TXT", Name: r.BaseDomain, Value: r.Value})
	}
	return reg, nil
}

func (p *PangolinService) GetDomain(ctx context.Context, ref string) (DomainStatus, error) {
	if !p.Enabled() {
		return DomainStatus{}, ErrPangolinDisabled
	}
	var res struct {
		Verified     bool   `json:"verified"`
		Failed       bool   `json:"failed"`
		ErrorMessage string `json:"errorMessage"`
	}
	if err := p.do(ctx, "GET", "/org/"+p.st().PangolinOrgID+"/domain/"+ref, nil, &res); err != nil {
		return DomainStatus{}, err
	}
	return DomainStatus{Verified: res.Verified, Failed: res.Failed, Message: res.ErrorMessage}, nil
}

// RestartDomain asks Pangolin to verify a domain's DNS records again.
// Pangolin stops checking after many failed attempts, so this is needed
// once the user has actually created the records.
func (p *PangolinService) RestartDomain(ctx context.Context, ref string) error {
	if !p.Enabled() {
		return ErrPangolinDisabled
	}
	return p.do(ctx, "POST", "/org/"+p.st().PangolinOrgID+"/domain/"+ref+"/restart", nil, nil)
}

func (p *PangolinService) DeleteDomain(ctx context.Context, ref string) error {
	if !p.Enabled() {
		return ErrPangolinDisabled
	}
	err := p.do(ctx, "DELETE", "/org/"+p.st().PangolinOrgID+"/domain/"+ref, nil, nil)
	if isNotFound(err) {
		return nil
	}
	return err
}

type PangolinDomain struct {
	ID       string `json:"id"`
	Domain   string `json:"domain"`
	Type     string `json:"type"`
	Verified bool   `json:"verified"`
}

// ListDomains returns the org's domains (used by the setup wizard to
// check the connection and let the admin pick the apps domain).
func (p *PangolinService) ListDomains(ctx context.Context) ([]PangolinDomain, error) {
	var out struct {
		Domains []struct {
			DomainID   string `json:"domainId"`
			BaseDomain string `json:"baseDomain"`
			Type       string `json:"type"`
			Verified   bool   `json:"verified"`
		} `json:"domains"`
	}
	if err := p.do(ctx, "GET", "/org/"+p.st().PangolinOrgID+"/domains?limit=1000", nil, &out); err != nil {
		return nil, err
	}
	list := []PangolinDomain{}
	for _, d := range out.Domains {
		list = append(list, PangolinDomain{ID: d.DomainID, Domain: d.BaseDomain, Type: d.Type, Verified: d.Verified})
	}
	return list, nil
}

// appsDomainID finds the Pangolin domain matching APPS_DOMAIN.
func (p *PangolinService) appsDomainID(ctx context.Context) (string, error) {
	st := p.st()
	if st.PangolinDomainID != "" {
		return st.PangolinDomainID, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	key := st.PangolinOrgID + "/" + st.AppsDomain
	if p.domainID != "" && p.domainIDFor == key {
		return p.domainID, nil
	}

	var out struct {
		Domains []struct {
			DomainID   string `json:"domainId"`
			BaseDomain string `json:"baseDomain"`
		} `json:"domains"`
	}
	if err := p.do(ctx, "GET", "/org/"+p.st().PangolinOrgID+"/domains?limit=1000", nil, &out); err != nil {
		return "", fmt.Errorf("list domains: %w", err)
	}
	var seen []string
	for _, d := range out.Domains {
		if strings.EqualFold(d.BaseDomain, p.st().AppsDomain) {
			p.domainID, p.domainIDFor = d.DomainID, key
			return d.DomainID, nil
		}
		seen = append(seen, d.BaseDomain)
	}
	return "", fmt.Errorf("the apps domain %q is not a domain in Pangolin org %q (found: %s)",
		p.st().AppsDomain, p.st().PangolinOrgID, strings.Join(seen, ", "))
}

// ------------------------------------------------------------------- http

type pangolinError struct {
	Status  int
	Message string
}

func (e *pangolinError) Error() string {
	return fmt.Sprintf("Pangolin returned %d: %s", e.Status, e.Message)
}

func isNotFound(err error) bool {
	var pe *pangolinError
	return errors.As(err, &pe) && pe.Status == http.StatusNotFound
}

// do calls the Pangolin Integration API. Responses look like
// {"data": ..., "success": true, "error": false, "message": "..."}.
func (p *PangolinService) do(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.st().PangolinAPIURL+path, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.st().PangolinAPIKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach Pangolin: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var envelope struct {
		Data    json.RawMessage `json:"data"`
		Message string          `json:"message"`
	}
	json.Unmarshal(raw, &envelope)

	if resp.StatusCode >= 300 {
		msg := envelope.Message
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		return &pangolinError{Status: resp.StatusCode, Message: msg}
	}
	if out != nil && len(envelope.Data) > 0 {
		return json.Unmarshal(envelope.Data, out)
	}
	return nil
}
