package handler

import (
	"net/http"
	"testing"

	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/database"
)

func TestSetModuleConfigWithSchema(t *testing.T) {
	h := newTestHandler(t)
	srv := fakeCaddyServer(t)
	useAPIMode(t, srv.URL)
	startJobQueue(t)

	body := map[string]any{
		"moduleId": "dns.providers.cloudflare",
		"values":   map[string]any{"api_token": "tok"},
	}
	rec := doRequest(h.SetModuleConfig(), http.MethodPut, "/caddy/module-configs", body, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d (%s)", rec.Code, rec.Body.String())
	}
	drainJobQueue(t)

	var configs []database.ModuleConfig
	h.DB.Find(&configs)
	if len(configs) != 1 {
		t.Fatalf("expected 1 module config, got %d", len(configs))
	}
	// Schema default path should be applied.
	if configs[0].Path != "apps/tls/automation/policies" {
		t.Errorf("path = %q", configs[0].Path)
	}
}

func TestSetModuleConfigSchemaValidationFails(t *testing.T) {
	h := newTestHandler(t)
	srv := fakeCaddyServer(t)
	useAPIMode(t, srv.URL)

	body := map[string]any{
		"moduleId": "dns.providers.cloudflare",
		"values":   map[string]any{}, // missing api_token
	}
	rec := doRequest(h.SetModuleConfig(), http.MethodPut, "/caddy/module-configs", body, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestSetModuleConfigRawFallback(t *testing.T) {
	h := newTestHandler(t)
	srv := fakeCaddyServer(t)
	useAPIMode(t, srv.URL)
	startJobQueue(t)

	// Unknown module -> raw JSON fallback, path required.
	body := map[string]any{
		"moduleId": "http.handlers.some_plugin",
		"path":     "apps/http/servers/srv0",
		"config":   map[string]any{"foo": "bar"},
	}
	rec := doRequest(h.SetModuleConfig(), http.MethodPut, "/caddy/module-configs", body, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d (%s)", rec.Code, rec.Body.String())
	}
	drainJobQueue(t)

	var count int64
	h.DB.Model(&database.ModuleConfig{}).Count(&count)
	if count != 1 {
		t.Fatalf("expected 1 config, got %d", count)
	}
}

func TestSetModuleConfigRawRequiresConfig(t *testing.T) {
	h := newTestHandler(t)
	srv := fakeCaddyServer(t)
	useAPIMode(t, srv.URL)

	body := map[string]any{"moduleId": "http.handlers.some_plugin", "path": "x"}
	rec := doRequest(h.SetModuleConfig(), http.MethodPut, "/caddy/module-configs", body, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when raw config missing, got %d", rec.Code)
	}
}

func TestModuleConfigRequiresAPIMode(t *testing.T) {
	h := newTestHandler(t)
	prev := config.Configuration.Caddy.Mode
	config.Configuration.Caddy.Mode = "caddyfile"
	t.Cleanup(func() { config.Configuration.Caddy.Mode = prev })

	body := map[string]any{
		"moduleId": "dns.providers.cloudflare",
		"values":   map[string]any{"api_token": "tok"},
	}
	rec := doRequest(h.SetModuleConfig(), http.MethodPut, "/caddy/module-configs", body, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 in caddyfile mode, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestGetModuleConfigs(t *testing.T) {
	h := newTestHandler(t)
	h.DB.Create(&database.ModuleConfig{ModuleID: "x", Path: "p", Config: database.JSON(`{}`), Enabled: true})

	rec := doRequest(h.GetModuleConfigs(), http.MethodGet, "/caddy/module-configs", nil, nil)
	var configs []database.ModuleConfig
	decodeResult(t, rec, &configs)
	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}
}
