package handler

import (
	"net/http"
	"testing"

	"github.com/Pacerino/CaddyProxyManager/internal/config"
)

func setAuthMode(t *testing.T, mode string) {
	t.Helper()
	prev := config.Configuration.Auth.Mode
	config.Configuration.Auth.Mode = mode
	t.Cleanup(func() { config.Configuration.Auth.Mode = prev })
}

func TestAuthConfigLocal(t *testing.T) {
	h := newTestHandler(t)
	setAuthMode(t, "local")
	rec := doRequest(h.AuthConfig(), http.MethodGet, "/auth/config", nil, nil)
	var resp struct {
		Mode string `json:"mode"`
	}
	decodeResult(t, rec, &resp)
	if resp.Mode != "local" {
		t.Errorf("mode = %q, want local", resp.Mode)
	}
}

func TestAuthConfigOIDC(t *testing.T) {
	h := newTestHandler(t)
	setAuthMode(t, "oidc")
	rec := doRequest(h.AuthConfig(), http.MethodGet, "/auth/config", nil, nil)
	var resp struct {
		Mode string `json:"mode"`
	}
	decodeResult(t, rec, &resp)
	if resp.Mode != "oidc" {
		t.Errorf("mode = %q, want oidc", resp.Mode)
	}
}

func TestUserLoginDisabledUnderOIDC(t *testing.T) {
	h := newTestHandler(t)
	setAuthMode(t, "oidc")
	body := map[string]any{"Email": "a@b.com", "Secret": "x"}
	rec := doRequest(h.UserLogin(), http.MethodPost, "/users/login", body, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when oidc enabled, got %d", rec.Code)
	}
}

func TestUserLoginValidationFails(t *testing.T) {
	h := newTestHandler(t)
	setAuthMode(t, "local")
	// Invalid email + missing secret.
	body := map[string]any{"Email": "notanemail"}
	rec := doRequest(h.UserLogin(), http.MethodPost, "/users/login", body, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestOIDCLoginDisabled(t *testing.T) {
	h := newTestHandler(t)
	setAuthMode(t, "local")
	rec := doRequest(h.OIDCLogin(), http.MethodGet, "/auth/oidc/login", nil, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when oidc disabled, got %d", rec.Code)
	}
}

func TestOIDCCallbackDisabled(t *testing.T) {
	h := newTestHandler(t)
	setAuthMode(t, "local")
	rec := doRequest(h.OIDCCallback(), http.MethodGet, "/auth/oidc/callback", nil, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when oidc disabled, got %d", rec.Code)
	}
}
