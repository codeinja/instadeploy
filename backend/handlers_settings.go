package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// The first account created is the server's admin: only it can change
// server settings (Pangolin, the apps domain, the agent URL).
func (s *Server) isAdmin(r *http.Request) bool {
	var first string
	s.db.QueryRowContext(r.Context(), `SELECT id FROM "user" ORDER BY "createdAt", id LIMIT 1`).Scan(&first)
	return first != "" && first == userID(r)
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !s.isAdmin(r) {
		writeError(w, http.StatusForbidden, "only the server's admin (the first account created) can change server settings")
		return false
	}
	return true
}

type settingsView struct {
	PublicAPIURL     string `json:"public_api_url"`
	AgentImage       string `json:"agent_image"`
	AppsDomain       string `json:"apps_domain"`
	PangolinEnabled  bool   `json:"pangolin_enabled"`
	PangolinAPIURL   string `json:"pangolin_api_url"`
	PangolinOrgID    string `json:"pangolin_org_id"`
	PangolinEndpoint string `json:"pangolin_endpoint"`
	// The API key is never sent back; this only says whether one is saved.
	PangolinKeySet bool `json:"pangolin_key_set"`
	SetupComplete  bool `json:"setup_complete"`
	IsAdmin        bool `json:"is_admin"`
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st := s.settings.get()
	writeJSON(w, http.StatusOK, settingsView{
		PublicAPIURL: st.PublicAPIURL, AgentImage: st.AgentImage, AppsDomain: st.AppsDomain,
		PangolinEnabled: st.PangolinEnabled(), PangolinAPIURL: st.PangolinAPIURL, PangolinOrgID: st.PangolinOrgID,
		PangolinEndpoint: st.PangolinEndpoint, PangolinKeySet: st.PangolinAPIKey != "",
		SetupComplete: st.SetupComplete, IsAdmin: s.isAdmin(r),
	})
}

type pangolinInput struct {
	APIURL     string `json:"api_url"`
	APIKey     string `json:"api_key"` // empty: keep the saved key
	OrgID      string `json:"org_id"`
	Endpoint   string `json:"endpoint"`
	AppsDomain string `json:"apps_domain"`
}

// candidate builds the settings Pangolin would use with this input.
func (s *Server) candidate(in pangolinInput) (*Settings, error) {
	st := *s.settings.get()
	in.APIURL = strings.TrimRight(strings.TrimSpace(in.APIURL), "/")
	in.Endpoint = strings.TrimRight(strings.TrimSpace(in.Endpoint), "/")
	in.OrgID = strings.TrimSpace(in.OrgID)
	if in.APIKey = strings.TrimSpace(in.APIKey); in.APIKey == "" {
		in.APIKey = st.PangolinAPIKey
	}
	for _, u := range []string{in.APIURL, in.Endpoint} {
		if p, err := url.Parse(u); err != nil || p.Scheme != "https" || p.Host == "" {
			return nil, fmt.Errorf("%q must be an https:// address", u)
		}
	}
	if in.OrgID == "" || in.APIKey == "" {
		return nil, fmt.Errorf("enter your organization ID and API key")
	}
	st.PangolinAPIURL, st.PangolinAPIKey, st.PangolinOrgID, st.PangolinEndpoint = in.APIURL, in.APIKey, in.OrgID, in.Endpoint
	st.AppsDomain = strings.ToLower(strings.TrimSpace(in.AppsDomain))
	st.PangolinDomainID = ""
	return &st, nil
}

// handleTestPangolin checks the connection and returns the organization's
// domains, so the setup wizard can offer them in a list.
func (s *Server) handleTestPangolin(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var in pangolinInput
	if !decodeJSON(w, r, &in) {
		return
	}
	st, err := s.candidate(in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	domains, err := NewPangolinService(func() *Settings { return st }).ListDomains(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, pangolinHint(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"domains": domains})
}

// pangolinHint turns common failures into something actionable.
func pangolinHint(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "401") || strings.Contains(msg, "403"):
		return "Pangolin rejected the API key. Check the key, and that it has permission to list domains. (" + msg + ")"
	case strings.Contains(msg, "404"):
		return "Pangolin couldn't find that organization. Check the organization ID and the API URL. (" + msg + ")"
	case strings.Contains(msg, "cannot reach"):
		return "Couldn't reach Pangolin at that API URL. (" + msg + ")"
	}
	return "Pangolin returned an error: " + msg
}

func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var req struct {
		PublicAPIURL   *string        `json:"public_api_url"`
		AgentImage     *string        `json:"agent_image"`
		Pangolin       *pangolinInput `json:"pangolin"`
		RemovePangolin bool           `json:"remove_pangolin"`
		SetupComplete  *bool          `json:"setup_complete"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	changes := map[string]string{}
	if req.PublicAPIURL != nil {
		v := strings.TrimRight(strings.TrimSpace(*req.PublicAPIURL), "/")
		if u, err := url.Parse(v); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			writeError(w, http.StatusBadRequest, "the server address must look like https://insta.example.com or http://192.168.1.10:8080")
			return
		}
		changes["public_api_url"] = v
	}
	if req.AgentImage != nil {
		if !validImage(strings.TrimSpace(*req.AgentImage)) {
			writeError(w, http.StatusBadRequest, "invalid agent image")
			return
		}
		changes["agent_image"] = strings.TrimSpace(*req.AgentImage)
	}
	if req.RemovePangolin {
		for _, k := range []string{"pangolin_api_url", "pangolin_api_key", "pangolin_org_id", "pangolin_endpoint", "pangolin_domain_id"} {
			changes[k] = ""
		}
	}
	if req.Pangolin != nil {
		st, err := s.candidate(*req.Pangolin)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if st.AppsDomain == "" {
			writeError(w, http.StatusBadRequest, "choose the domain your apps should use")
			return
		}
		// Only save a configuration that works.
		domains, err := NewPangolinService(func() *Settings { return st }).ListDomains(r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, pangolinHint(err))
			return
		}
		found := ""
		for _, d := range domains {
			if strings.EqualFold(d.Domain, st.AppsDomain) {
				found = d.ID
			}
		}
		if found == "" {
			writeError(w, http.StatusBadRequest, st.AppsDomain+" is not a domain in this Pangolin organization")
			return
		}
		changes["pangolin_api_url"] = st.PangolinAPIURL
		changes["pangolin_api_key"] = st.PangolinAPIKey
		changes["pangolin_org_id"] = st.PangolinOrgID
		changes["pangolin_endpoint"] = st.PangolinEndpoint
		changes["apps_domain"] = st.AppsDomain
		changes["pangolin_domain_id"] = found
	}
	if req.SetupComplete != nil {
		changes["setup_complete"] = fmt.Sprint(*req.SetupComplete)
	}
	if err := s.settings.save(r.Context(), changes); err != nil {
		internalError(w, err)
		return
	}
	if req.Pangolin != nil {
		// Machines without a tunnel get one on their next check-in, and
		// waiting public services get their URLs.
		s.db.ExecContext(r.Context(), `UPDATE agents SET tunnel_error = '' WHERE pangolin_site_id IS NULL`)
		s.db.ExecContext(r.Context(), `UPDATE routes SET status = 'PENDING', error = '' WHERE status IN ('DISABLED', 'FAILED')`)
		s.kickRoutes()
		s.event(r.Context(), userID(r), "success", "Connected Pangolin ("+req.Pangolin.AppsDomain+")", eventRefs{})
	}
	s.handleGetSettings(w, r)
}
