package main

import (
	"database/sql"
	"errors"
	"net/http"
)

// Registration, login, logout and password changes are handled by Better
// Auth in the frontend (/api/auth/*). This only reports who is signed in.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	var email, name string
	err := s.db.QueryRowContext(r.Context(), `SELECT email, name FROM "user" WHERE id = $1`, userID(r)).Scan(&email, &name)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "account no longer exists")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	st := s.settings.get()
	writeJSON(w, http.StatusOK, map[string]any{
		"id":               userID(r),
		"email":            email,
		"name":             name,
		"is_admin":         s.isAdmin(r),
		"setup_complete":   st.SetupComplete,
		"apps_domain":      st.AppsDomain,
		"api_url":          st.PublicAPIURL,
		"pangolin_enabled": st.PangolinEnabled(),
	})
}
