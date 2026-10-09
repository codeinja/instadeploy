package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Server struct {
	cfg       Config
	settings  *settingsStore
	db        *sql.DB
	box       *SecretBox
	pangolin  *PangolinService
	notifier  agentNotifier
	stats     statsStore
	routeKick chan struct{}

	// GitHub App installation tokens (see github.go).
	github githubTokens
	// Serializes auto deploys from polling and webhooks.
	autoDeployMu sync.Mutex

	// When each domain's verification was last restarted (see recheckDomain).
	domainRetry struct {
		sync.Mutex
		last map[string]time.Time
	}
}

func main() {
	cfg := loadConfig()

	db, err := openDB(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	box, err := loadSecretBox(cfg)
	if err != nil {
		log.Fatalf("secrets key: %v", err)
	}

	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "migrate":
		if err := migrate(db); err != nil {
			log.Fatalf("migrate: %v", err)
		}
		if err := migrateLegacyEnv(db, box); err != nil {
			log.Fatalf("migrate: %v", err)
		}
		return
	case "serve":
	default:
		log.Fatalf("unknown command %q (use \"serve\" or \"migrate\")", cmd)
	}

	pending, err := pendingMigrations(db)
	if err != nil {
		log.Fatalf("check migrations: %v", err)
	}
	if len(pending) > 0 {
		log.Fatalf("database has %d pending migration(s) (%s). Run `backend migrate` first "+
			"(docker compose runs it automatically as the \"migrate\" service).", len(pending), pending[0])
	}

	settings := &settingsStore{db: db, box: box}
	if err := settings.seed(context.Background()); err != nil {
		log.Fatalf("settings: %v", err)
	}
	if err := settings.load(context.Background()); err != nil {
		log.Fatalf("settings: %v", err)
	}
	if !settings.get().PangolinEnabled() {
		log.Println("Pangolin is not connected yet: deployments run without public URLs until it's set up in the dashboard")
	}

	s := &Server{cfg: cfg, settings: settings, db: db, box: box, pangolin: NewPangolinService(settings.get), routeKick: make(chan struct{}, 1)}
	go s.markOfflineAgents()
	go s.runRouteReconciler()
	go s.runDomainVerifier()
	go s.pollGitDeployments()

	srv := &http.Server{Addr: cfg.ListenAddr, Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("Insta Deploy backend listening on %s", cfg.ListenAddr)
	log.Fatal(srv.ListenAndServe())
}

func (s *Server) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer, quietLogger)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	r.Get("/docs", serveDocs)
	r.Get("/docs/openapi.yaml", serveOpenAPI)

	r.Route("/api", func(r chi.Router) {
		// Dashboard API. Sign-up/sign-in live in the frontend (Better Auth);
		// these routes accept its session cookie or a Bearer session token.
		r.Group(func(r chi.Router) {
			r.Use(s.requireUser)
			r.Get("/me", s.handleMe)
			r.Get("/settings", s.handleGetSettings)
			r.Put("/settings", s.handleSaveSettings)
			r.Post("/settings/pangolin/test", s.handleTestPangolin)
			r.Get("/activity", s.handleActivity)

			r.Get("/projects", s.handleListProjects)
			r.Post("/projects", s.handleCreateProject)
			r.Get("/projects/{id}", s.handleGetProject)
			r.Patch("/projects/{id}", s.handleUpdateProject)
			r.Delete("/projects/{id}", s.handleDeleteProject)

			r.Post("/agents", s.handleCreateAgent)
			r.Get("/agents", s.handleListAgents)
			r.Patch("/agents/{id}", s.handleRenameAgent)
			r.Post("/agents/{id}/revoke", s.handleRevokeAgent)
			r.Post("/agents/{id}/token", s.handleRotateAgentToken)
			r.Delete("/agents/{id}", s.handleDeleteAgent)

			r.Post("/analyze/image", s.handleAnalyzeImage)
			r.Post("/analyze/compose", s.handleAnalyzeCompose)
			r.Post("/analyze/git", s.handleAnalyzeGit)
			r.Post("/convert/docker-run", s.handleConvertDockerRun)
			r.Get("/apps", s.handleListApps)

			r.Get("/github", s.handleGetGitHub)
			r.Put("/github", s.handleSaveGitHub)
			r.Delete("/github", s.handleDeleteGitHub)

			r.Post("/deployments", s.handleCreateDeployment)
			r.Get("/deployments", s.handleListDeployments)
			r.Get("/deployments/{id}", s.handleGetDeployment)
			r.Patch("/deployments/{id}", s.handleUpdateDeployment)
			r.Delete("/deployments/{id}", s.handleDeleteDeployment)
			r.Post("/deployments/{id}/redeploy", s.handleRedeploy)
			r.Post("/deployments/{id}/stop", s.handleLifecycle(TaskStop))
			r.Post("/deployments/{id}/start", s.handleLifecycle(TaskStart))
			r.Post("/deployments/{id}/restart", s.handleLifecycle(TaskRestart))
			r.Post("/deployments/{id}/rollback", s.handleRollback)
			r.Post("/deployments/{id}/routes/retry", s.handleRetryDeploymentRoutes)
			r.Get("/deployments/{id}/revisions", s.handleListRevisions)
			r.Get("/deployments/{id}/logs", s.handleDeploymentLogs)
			r.Get("/deployments/{id}/logs/stream", s.handleDeploymentLogStream)
			r.Patch("/deployments/{id}/services/{service}", s.handleUpdateService)
			r.Get("/deployments/{id}/services/{service}/logs", s.handleContainerLogs)

			r.Get("/variables", s.handleListVariables)
			r.Post("/variables", s.handleCreateVariable)
			r.Patch("/variables/{id}", s.handleUpdateVariable)
			r.Delete("/variables/{id}", s.handleDeleteVariable)

			r.Get("/registries", s.handleListRegistries)
			r.Post("/registries", s.handleCreateRegistry)
			r.Delete("/registries/{id}", s.handleDeleteRegistry)

			r.Get("/domains", s.handleListDomains)
			r.Post("/domains", s.handleCreateDomain)
			r.Patch("/domains/{id}", s.handleUpdateDomain)
			r.Post("/domains/{id}/verify", s.handleVerifyDomain)
			r.Delete("/domains/{id}", s.handleDeleteDomain)
		})

		// GitHub App webhooks, signed with the app's webhook secret.
		r.Post("/github/webhook", s.handleGitHubWebhook)

		// Agent API (agent token).
		r.Route("/agent", func(r chi.Router) {
			r.Use(s.requireAgent)
			r.Post("/register", s.handleAgentRegister)
			r.Post("/heartbeat", s.handleAgentHeartbeat)
			r.Get("/commands", s.handleAgentCommands)
			r.Post("/tasks/{id}/status", s.handleAgentTaskStatus)
			r.Post("/deployments/{id}/logs", s.handleAgentLogs)
		})
	})
	return r
}

// quietLogger logs requests, except the agent's constant polling.
func quietLogger(next http.Handler) http.Handler {
	logged := middleware.Logger(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/agent/commands", "/api/agent/heartbeat", "/healthz":
			next.ServeHTTP(w, r)
		default:
			logged.ServeHTTP(w, r)
		}
	})
}

// migrateLegacyEnv encrypts environment variables carried over from the
// pre-0.2 schema (see migrations/003) and drops the temporary table.
func migrateLegacyEnv(db *sql.DB, box *SecretBox) error {
	var exists bool
	if err := db.QueryRow(`SELECT to_regclass('legacy_env') IS NOT NULL`).Scan(&exists); err != nil || !exists {
		return err
	}
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT deployment_id, project_id, key, value FROM legacy_env`)
	if err != nil {
		return err
	}
	type row struct{ dep, proj, key, value string }
	var list []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.dep, &x.proj, &x.key, &x.value); err != nil {
			rows.Close()
			return err
		}
		list = append(list, x)
	}
	rows.Close()
	for _, x := range list {
		if _, err := tx.Exec(`INSERT INTO variables (project_id, deployment_id, key, value_encrypted) VALUES ($1, $2, $3, $4)
			ON CONFLICT DO NOTHING`, x.proj, x.dep, x.key, box.Encrypt(x.value)); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DROP TABLE legacy_env`); err != nil {
		return err
	}
	log.Printf("encrypted %d environment variable(s) from the old schema", len(list))
	return tx.Commit()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

// internalError logs the details and shows the user a generic message:
// stack traces and SQL errors stay in the server log.
func internalError(w http.ResponseWriter, err error) {
	log.Printf("internal error: %v", err)
	writeError(w, http.StatusInternalServerError, "Something went wrong on the server. Check the backend logs for details.")
}
