package main

import (
	"net/url"
	"os"
	"strconv"
	"time"
)

type Config struct {
	ListenAddr  string
	DatabaseURL string

	// Holds the generated secrets key.
	DataDir      string
	SecretsKey   string // optional; generated into DataDir if empty
	GitPollEvery time.Duration

	// The dashboard's origin (scheme://host:port), if BETTER_AUTH_URL is
	// set. Browsers calling this API directly from any other origin with a
	// session cookie are refused (see crossOrigin).
	DashboardOrigin string
}

// Config holds what has to be known before the database is reachable.
// Everything else (Pangolin, the apps domain, the agent URL) is a setting
// edited in the dashboard: see settings.go.

func loadConfig() Config {
	c := Config{
		ListenAddr:   env("LISTEN_ADDR", ":8080"),
		DatabaseURL:  env("DATABASE_URL", "postgres://insta:insta@localhost:5432/insta?sslmode=disable"),
		DataDir:      env("DATA_DIR", "./data"),
		SecretsKey:   os.Getenv("SECRETS_KEY"),
		GitPollEvery: time.Duration(envInt("GIT_POLL_SECONDS", 60)) * time.Second,
	}

	if u, err := url.Parse(os.Getenv("BETTER_AUTH_URL")); err == nil && u.Host != "" {
		c.DashboardOrigin = u.Scheme + "://" + u.Host
	}

	return c
}

func envInt(key string, fallback int) int {
	if n, err := strconv.Atoi(os.Getenv(key)); err == nil && n > 0 {
		return n
	}
	return fallback
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
