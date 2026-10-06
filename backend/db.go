package main

import (
	"database/sql"
	"embed"
	"fmt"
	"log"
	"sort"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func openDB(url string) (*sql.DB, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	// Postgres may still be starting when we boot under docker compose.
	for i := 0; i < 30; i++ {
		if err = db.Ping(); err == nil {
			return db, nil
		}
		log.Printf("waiting for database: %v", err)
		time.Sleep(time.Second)
	}
	return nil, err
}

// Migrations live in migrations/*.sql and are applied in filename order by
// `backend migrate` (the "migrate" service in docker compose). The server
// itself never changes the schema; it refuses to start if migrations are
// pending.

func migrationNames() ([]string, error) {
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

func ensureMigrationsTable(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		name text PRIMARY KEY,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`)
	return err
}

// pendingMigrations is read-only so the server can call it safely.
func pendingMigrations(db *sql.DB) ([]string, error) {
	names, err := migrationNames()
	if err != nil {
		return nil, err
	}
	var tracked bool
	if err := db.QueryRow(`SELECT to_regclass('schema_migrations') IS NOT NULL`).Scan(&tracked); err != nil {
		return nil, err
	}
	if !tracked {
		return names, nil
	}
	var pending []string
	for _, name := range names {
		var exists bool
		if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name = $1)`, name).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			pending = append(pending, name)
		}
	}
	return pending, nil
}

// migrate applies pending migrations, each in its own transaction.
func migrate(db *sql.DB) error {
	if err := ensureMigrationsTable(db); err != nil {
		return err
	}
	pending, err := pendingMigrations(db)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		log.Println("database is up to date")
	}
	for _, name := range pending {
		body, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (name) VALUES ($1)`, name); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		log.Printf("applied migration %s", name)
	}
	return nil
}
