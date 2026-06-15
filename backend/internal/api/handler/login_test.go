package handler

import (
	"net/http"
	"testing"

	"github.com/Pacerino/CaddyProxyManager/internal/auth"
	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/database"
)

// TestUserLoginHappyPath exercises the full local login flow: it relies on the
// global database instance and a generated JWT signing key.
func TestUserLoginHappyPath(t *testing.T) {
	dir := t.TempDir()
	prevData := config.Configuration.DataFolder
	prevMode := config.Configuration.Auth.Mode
	prevAdmin := config.Configuration.Admin
	t.Cleanup(func() {
		config.Configuration.DataFolder = prevData
		config.Configuration.Auth.Mode = prevMode
		config.Configuration.Admin = prevAdmin
	})

	config.Configuration.DataFolder = dir
	config.Configuration.Auth.Mode = "local"
	config.Configuration.Admin.Email = "admin@example.com"
	config.Configuration.Admin.Password = "supersecret"

	// Create the JWT signing key and the database (which seeds the admin).
	if err := auth.EnsureKey(); err != nil {
		t.Fatalf("EnsureKey: %v", err)
	}
	database.NewDB()

	h := Handler{DB: database.GetInstance()}

	body := map[string]any{"Email": "admin@example.com", "Secret": "supersecret"}
	rec := doRequest(h.UserLogin(), http.MethodPost, "/users/login", body, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Token   string `json:"token"`
		Expires int64  `json:"expires"`
	}
	decodeResult(t, rec, &resp)
	if resp.Token == "" {
		t.Error("expected a token on successful login")
	}

	// Wrong password -> unauthorized.
	bad := map[string]any{"Email": "admin@example.com", "Secret": "wrong"}
	rec = doRequest(h.UserLogin(), http.MethodPost, "/users/login", bad, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong password, got %d", rec.Code)
	}

	// Unknown user -> unauthorized (record not found).
	missing := map[string]any{"Email": "ghost@example.com", "Secret": "x"}
	rec = doRequest(h.UserLogin(), http.MethodPost, "/users/login", missing, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unknown user, got %d", rec.Code)
	}
}
