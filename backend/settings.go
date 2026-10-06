package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"log"
	"os"
	"strings"
	"sync/atomic"
)

// Settings are the server-wide options the admin sets in the dashboard
// (the setup wizard and Settings page). They replace a .env file: values
// from the environment are only used to fill in missing settings the first
// time the server starts.
type Settings struct {
	PublicAPIURL string // where agents reach this API
	AgentImage   string
	AppsDomain   string // generated URLs: <name>-xxxx.<AppsDomain>

	PangolinAPIURL   string // e.g. https://api.pangolin.net/v1
	PangolinAPIKey   string
	PangolinOrgID    string
	PangolinEndpoint string // where newt connects, e.g. https://app.pangolin.net
	PangolinDomainID string // Pangolin's ID for AppsDomain (optional)

	SetupComplete bool
}

func (s *Settings) PangolinEnabled() bool {
	return s.PangolinAPIURL != "" && s.PangolinAPIKey != "" && s.PangolinOrgID != "" && s.AppsDomain != ""
}

// settingKeys maps database keys to fields, and to the environment
// variables that seed them.
var settingKeys = []struct {
	key, env string
	secret   bool
	field    func(*Settings) *string
}{
	{"public_api_url", "PUBLIC_API_URL", false, func(s *Settings) *string { return &s.PublicAPIURL }},
	{"agent_image", "AGENT_IMAGE", false, func(s *Settings) *string { return &s.AgentImage }},
	{"apps_domain", "APPS_DOMAIN", false, func(s *Settings) *string { return &s.AppsDomain }},
	{"pangolin_api_url", "PANGOLIN_API_URL", false, func(s *Settings) *string { return &s.PangolinAPIURL }},
	{"pangolin_api_key", "PANGOLIN_API_KEY", true, func(s *Settings) *string { return &s.PangolinAPIKey }},
	{"pangolin_org_id", "PANGOLIN_ORG_ID", false, func(s *Settings) *string { return &s.PangolinOrgID }},
	{"pangolin_endpoint", "PANGOLIN_ENDPOINT", false, func(s *Settings) *string { return &s.PangolinEndpoint }},
	{"pangolin_domain_id", "PANGOLIN_DOMAIN_ID", false, func(s *Settings) *string { return &s.PangolinDomainID }},
}

// Defaults when neither the dashboard nor the environment set a value.
var settingDefaults = map[string]string{
	// Works for an agent on the same machine as `docker compose up`.
	"public_api_url": "http://host.docker.internal:8080",
	"agent_image":    "insta-deploy/agent:latest",
}

type settingsStore struct {
	db  *sql.DB
	box *SecretBox
	cur atomic.Pointer[Settings]
}

func (st *settingsStore) get() *Settings { return st.cur.Load() }

// seed copies settings from the environment into the database, for keys
// that have never been set. It lets older installs keep their .env.
func (st *settingsStore) seed(ctx context.Context) error {
	for _, k := range settingKeys {
		v := strings.TrimRight(strings.TrimSpace(os.Getenv(k.env)), "/")
		if v == "" {
			continue
		}
		res, err := st.db.ExecContext(ctx, `INSERT INTO settings (key, value, encrypted) VALUES ($1, $2, $3) ON CONFLICT (key) DO NOTHING`,
			k.key, st.encode(v, k.secret), k.secret)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			log.Printf("settings: imported %s from the environment", k.env)
		}
	}
	return nil
}

func (st *settingsStore) encode(v string, secret bool) string {
	if !secret {
		return v
	}
	return base64.StdEncoding.EncodeToString(st.box.Encrypt(v))
}

func (st *settingsStore) load(ctx context.Context) error {
	rows, err := st.db.QueryContext(ctx, `SELECT key, value, encrypted FROM settings`)
	if err != nil {
		return err
	}
	defer rows.Close()
	values := map[string]string{}
	for rows.Next() {
		var key, value string
		var encrypted bool
		if err := rows.Scan(&key, &value, &encrypted); err != nil {
			return err
		}
		if encrypted {
			raw, err := base64.StdEncoding.DecodeString(value)
			if err == nil {
				value, err = st.box.Decrypt(raw)
			}
			if err != nil {
				log.Printf("settings: cannot decrypt %s: %v", key, err)
				value = ""
			}
		}
		values[key] = value
	}
	s := &Settings{SetupComplete: values["setup_complete"] == "true"}
	for _, k := range settingKeys {
		v := values[k.key]
		if v == "" {
			v = settingDefaults[k.key]
		}
		*k.field(s) = v
	}
	st.cur.Store(s)
	return rows.Err()
}

// save writes the given settings (only keys present in the map; an empty
// value clears a setting) and reloads.
func (st *settingsStore) save(ctx context.Context, changes map[string]string) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, value := range changes {
		secret := false
		for _, k := range settingKeys {
			if k.key == key {
				secret = k.secret
			}
		}
		if value == "" {
			if _, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE key = $1`, key); err != nil {
				return err
			}
			continue
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value, encrypted, updated_at) VALUES ($1, $2, $3, now())
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, encrypted = EXCLUDED.encrypted, updated_at = now()`,
			key, st.encode(value, secret), secret)
		if err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return st.load(ctx)
}
