package caddy

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Pacerino/CaddyProxyManager/internal/config"
)

func TestReloadNone(t *testing.T) {
	prev := config.Configuration.Caddy.ReloadStrategy
	t.Cleanup(func() { config.Configuration.Caddy.ReloadStrategy = prev })

	config.Configuration.Caddy.ReloadStrategy = "none"
	if err := Reload(); err != nil {
		t.Errorf("none strategy should be a no-op, got %v", err)
	}
}

func TestReloadUnknownStrategy(t *testing.T) {
	prev := config.Configuration.Caddy.ReloadStrategy
	t.Cleanup(func() { config.Configuration.Caddy.ReloadStrategy = prev })

	config.Configuration.Caddy.ReloadStrategy = "bogus"
	if err := Reload(); err == nil {
		t.Error("expected error for unknown reload strategy")
	}
}

func TestReloadAPI(t *testing.T) {
	prevCaddy := config.Configuration.Caddy
	prevFile := config.Configuration.CaddyFile
	t.Cleanup(func() {
		config.Configuration.Caddy = prevCaddy
		config.Configuration.CaddyFile = prevFile
	})

	var loaded bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/load" && r.Method == http.MethodPost {
			loaded = true
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	file := filepath.Join(t.TempDir(), "Caddyfile")
	if err := os.WriteFile(file, []byte("example.com {\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	config.Configuration.Caddy.ReloadStrategy = "api"
	config.Configuration.Caddy.AdminURL = srv.URL
	config.Configuration.CaddyFile = file

	if err := Reload(); err != nil {
		t.Fatalf("reload api: %v", err)
	}
	if !loaded {
		t.Error("expected /load to be called")
	}
}

func TestReloadAPIServerError(t *testing.T) {
	prevCaddy := config.Configuration.Caddy
	prevFile := config.Configuration.CaddyFile
	t.Cleanup(func() {
		config.Configuration.Caddy = prevCaddy
		config.Configuration.CaddyFile = prevFile
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	file := filepath.Join(t.TempDir(), "Caddyfile")
	_ = os.WriteFile(file, []byte("x"), 0644)

	config.Configuration.Caddy.ReloadStrategy = "api"
	config.Configuration.Caddy.AdminURL = srv.URL
	config.Configuration.CaddyFile = file

	if err := Reload(); err == nil {
		t.Error("expected error on non-2xx /load response")
	}
}
