package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Pacerino/CaddyProxyManager/internal/caddy/schema"
	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/database"
)

func TestGetCaddySchemas(t *testing.T) {
	h := newTestHandler(t)
	rec := doRequest(h.GetCaddySchemas(), http.MethodGet, "/caddy/schemas", nil, nil)
	var schemas []schema.Schema
	decodeResult(t, rec, &schemas)
	// Only global-scope schemas (cloudflare) should appear.
	for _, s := range schemas {
		found := false
		for _, sc := range s.Scopes {
			if sc == schema.ScopeGlobal {
				found = true
			}
		}
		if !found {
			t.Errorf("schema %s is not global-scoped", s.ModuleID)
		}
	}
}

func TestGetHostScopedSchemas(t *testing.T) {
	h := newTestHandler(t)
	rec := doRequest(h.GetHostScopedSchemas(), http.MethodGet, "/caddy/host-schemas", nil, nil)
	var schemas []schema.Schema
	decodeResult(t, rec, &schemas)
	if len(schemas) == 0 {
		t.Fatal("expected at least one host-scoped schema")
	}
	for _, s := range schemas {
		ok := false
		for _, sc := range s.Scopes {
			if sc == schema.ScopeHost {
				ok = true
			}
		}
		if !ok {
			t.Errorf("schema %s is not host-scoped", s.ModuleID)
		}
	}
}

func TestGetCaddyConfig(t *testing.T) {
	h := newTestHandler(t)
	srv := fakeCaddyServer(t) // GET returns "null"
	useAPIMode(t, srv.URL)

	rec := doRequest(h.GetCaddyConfig(), http.MethodGet, "/caddy/config", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestGetCaddyConfigRequiresAPIMode(t *testing.T) {
	h := newTestHandler(t)
	prev := config.Configuration.Caddy.Mode
	config.Configuration.Caddy.Mode = "caddyfile"
	t.Cleanup(func() { config.Configuration.Caddy.Mode = prev })

	rec := doRequest(h.GetCaddyConfig(), http.MethodGet, "/caddy/config", nil, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 in caddyfile mode, got %d", rec.Code)
	}
}

func TestGetCaddyModules(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell binary not supported on windows")
	}
	h := newTestHandler(t)

	script := filepath.Join(t.TempDir(), "fakecaddy")
	body := `#!/bin/sh
cat <<'EOF'
[
  {"module_name":"http.handlers.reverse_proxy","module_type":"standard"},
  {"module_name":"dns.providers.cloudflare","module_type":"non-standard"}
]
EOF
`
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	prev := config.Configuration.Caddy.Binary
	config.Configuration.Caddy.Binary = script
	t.Cleanup(func() { config.Configuration.Caddy.Binary = prev })

	rec := doRequest(h.GetCaddyModules(), http.MethodGet, "/caddy/modules", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Modules []schema.Schema `json:"modules"`
		Plugins []struct {
			ID string `json:"id"`
		} `json:"plugins"`
	}
	decodeResult(t, rec, &resp)
	if len(resp.Plugins) != 1 || resp.Plugins[0].ID != "dns.providers.cloudflare" {
		t.Errorf("expected cloudflare as the only plugin, got %+v", resp.Plugins)
	}
}

func TestGetCaddyModulesBinaryError(t *testing.T) {
	h := newTestHandler(t)
	prev := config.Configuration.Caddy.Binary
	config.Configuration.Caddy.Binary = "/nonexistent/caddy"
	t.Cleanup(func() { config.Configuration.Caddy.Binary = prev })

	rec := doRequest(h.GetCaddyModules(), http.MethodGet, "/caddy/modules", nil, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on binary error, got %d", rec.Code)
	}
}

func TestDeleteModuleConfig(t *testing.T) {
	h := newTestHandler(t)
	srv := fakeCaddyServer(t)
	useAPIMode(t, srv.URL)
	startJobQueue(t)

	mc := database.ModuleConfig{ModuleID: "x", Path: "apps/http", Config: database.JSON(`{}`), Enabled: true}
	if err := h.DB.Create(&mc).Error; err != nil {
		t.Fatal(err)
	}

	rec := doRequest(h.DeleteModuleConfig(), http.MethodDelete, "/caddy/module-configs/1", nil,
		map[string]string{"id": "1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d (%s)", rec.Code, rec.Body.String())
	}
	drainJobQueue(t)

	var count int64
	h.DB.Model(&database.ModuleConfig{}).Count(&count)
	if count != 0 {
		t.Fatalf("expected config deleted, count=%d", count)
	}
}

func TestDeleteModuleConfigRequiresAPIMode(t *testing.T) {
	h := newTestHandler(t)
	prev := config.Configuration.Caddy.Mode
	config.Configuration.Caddy.Mode = "caddyfile"
	t.Cleanup(func() { config.Configuration.Caddy.Mode = prev })

	rec := doRequest(h.DeleteModuleConfig(), http.MethodDelete, "/caddy/module-configs/1", nil,
		map[string]string{"id": "1"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
