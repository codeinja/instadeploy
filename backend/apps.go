package main

import (
	_ "embed"
	"log"
	"net/http"

	"gopkg.in/yaml.v3"
)

// The App Store catalog (catalog.yaml), embedded in the binary.

//go:embed catalog.yaml
var catalogYAML []byte

type AppInput struct {
	Key         string `yaml:"key" json:"key"`
	Label       string `yaml:"label" json:"label"`
	Description string `yaml:"description" json:"description,omitempty"`
	// Default value. "{tz}" means the browser's time zone.
	Default string `yaml:"default" json:"default,omitempty"`
	// "password" or "secret": filled with a random value in the browser.
	Generate string `yaml:"generate" json:"generate,omitempty"`
	Secret   bool   `yaml:"secret" json:"secret"`
	// Generated values the user doesn't need to see.
	Hidden bool `yaml:"hidden" json:"hidden"`
}

type App struct {
	ID          string `yaml:"id" json:"id"`
	Name        string `yaml:"name" json:"name"`
	Category    string `yaml:"category" json:"category"`
	Icon        string `yaml:"icon" json:"icon"`
	Website     string `yaml:"website" json:"website"`
	Tagline     string `yaml:"tagline" json:"tagline"`
	Description string `yaml:"description" json:"description"`
	Notes       string `yaml:"notes" json:"notes,omitempty"`
	// How the app protects itself: login, setup (first visitor claims it)
	// or none. Shown before the app is made public.
	Auth     string `yaml:"auth" json:"auth"`
	AuthNote string `yaml:"auth_note" json:"auth_note,omitempty"`
	Public   struct {
		Service string `yaml:"service" json:"service"`
		Port    int    `yaml:"port" json:"port"`
	} `yaml:"public" json:"public"`
	Inputs  []AppInput `yaml:"inputs" json:"inputs"`
	Compose string     `yaml:"compose" json:"compose"`
}

var appCatalog = func() []App {
	var apps []App
	if err := yaml.Unmarshal(catalogYAML, &apps); err != nil {
		log.Fatalf("catalog.yaml: %v", err)
	}
	for i := range apps {
		switch apps[i].Auth {
		case "login", "setup", "none":
		default:
			log.Fatalf("catalog.yaml: %s: auth must be login, setup or none", apps[i].ID)
		}
		if apps[i].Inputs == nil {
			apps[i].Inputs = []AppInput{}
		}
	}
	return apps
}()

func (s *Server) handleListApps(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, appCatalog)
}
