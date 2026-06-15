package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Pacerino/CaddyProxyManager/internal/database"
)

func seedHost(t *testing.T, h Handler) database.Host {
	t.Helper()
	host := database.Host{Domains: "example.com", Upstreams: []database.Upstream{{Backend: "127.0.0.1:8080"}}}
	if err := h.DB.Create(&host).Error; err != nil {
		t.Fatal(err)
	}
	return host
}

func TestSetHostPluginBasicAuth(t *testing.T) {
	h := newTestHandler(t)
	srv := fakeCaddyServer(t)
	useAPIMode(t, srv.URL)
	startJobQueue(t)

	host := seedHost(t, h)

	body := map[string]any{
		"moduleId": "http.handlers.authentication",
		"values":   map[string]any{"username": "bob", "password": "s3cret"},
	}
	rec := doRequest(h.SetHostPlugin(), http.MethodPut, "/hosts/1/plugins", body,
		map[string]string{"hostID": "1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d (%s)", rec.Code, rec.Body.String())
	}
	drainJobQueue(t)

	var plugins []database.HostPlugin
	h.DB.Where("host_id = ?", host.ID).Find(&plugins)
	if len(plugins) != 1 {
		t.Fatalf("expected 1 host plugin, got %d", len(plugins))
	}

	// The stored handler must contain the basic auth provider and a hashed
	// (non-plaintext) password.
	var handler struct {
		Handler   string `json:"handler"`
		Providers struct {
			HTTPBasic struct {
				Accounts []struct {
					Username string `json:"username"`
					Password string `json:"password"`
				} `json:"accounts"`
			} `json:"http_basic"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(plugins[0].Handler, &handler); err != nil {
		t.Fatalf("unmarshal handler: %v", err)
	}
	if handler.Handler != "authentication" {
		t.Errorf("handler type = %q", handler.Handler)
	}
	acc := handler.Providers.HTTPBasic.Accounts
	if len(acc) != 1 || acc[0].Username != "bob" {
		t.Fatalf("accounts = %+v", acc)
	}
	if acc[0].Password == "" || acc[0].Password == "s3cret" {
		t.Error("password should be hashed, not plaintext or empty")
	}
}

func TestSetHostPluginUpsertNoDuplicate(t *testing.T) {
	h := newTestHandler(t)
	srv := fakeCaddyServer(t)
	useAPIMode(t, srv.URL)
	startJobQueue(t)

	seedHost(t, h)

	for i := 0; i < 3; i++ {
		body := map[string]any{
			"moduleId": "http.handlers.authentication",
			"values":   map[string]any{"username": "bob", "password": "pw"},
		}
		rec := doRequest(h.SetHostPlugin(), http.MethodPut, "/hosts/1/plugins", body,
			map[string]string{"hostID": "1"})
		if rec.Code != http.StatusOK {
			t.Fatalf("update %d: status %d (%s)", i, rec.Code, rec.Body.String())
		}
	}
	drainJobQueue(t)

	var count int64
	h.DB.Model(&database.HostPlugin{}).Where("host_id = 1").Count(&count)
	if count != 1 {
		t.Fatalf("expected 1 host plugin after repeated saves, got %d", count)
	}
}

func TestSetHostPluginValidationFails(t *testing.T) {
	h := newTestHandler(t)
	seedHost(t, h)

	// Missing password -> schema validation error.
	body := map[string]any{
		"moduleId": "http.handlers.authentication",
		"values":   map[string]any{"username": "bob"},
	}
	rec := doRequest(h.SetHostPlugin(), http.MethodPut, "/hosts/1/plugins", body,
		map[string]string{"hostID": "1"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestSetHostPluginRejectsNonHostScoped(t *testing.T) {
	h := newTestHandler(t)
	seedHost(t, h)

	// cloudflare is global-scoped, not allowed per host.
	body := map[string]any{
		"moduleId": "dns.providers.cloudflare",
		"values":   map[string]any{"api_token": "x"},
	}
	rec := doRequest(h.SetHostPlugin(), http.MethodPut, "/hosts/1/plugins", body,
		map[string]string{"hostID": "1"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-host plugin, got %d", rec.Code)
	}
}

func TestDeleteHostPlugin(t *testing.T) {
	h := newTestHandler(t)
	srv := fakeCaddyServer(t)
	useAPIMode(t, srv.URL)
	startJobQueue(t)

	host := seedHost(t, h)
	plugin := database.HostPlugin{
		HostID:   host.ID,
		ModuleID: "http.handlers.authentication",
		Handler:  database.JSON(`{"handler":"authentication"}`),
		Enabled:  true,
	}
	if err := h.DB.Create(&plugin).Error; err != nil {
		t.Fatal(err)
	}

	rec := doRequest(h.DeleteHostPlugin(), http.MethodDelete, "/hosts/1/plugins/1", nil,
		map[string]string{"hostID": "1", "pluginID": "1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d (%s)", rec.Code, rec.Body.String())
	}
	drainJobQueue(t)

	var count int64
	h.DB.Model(&database.HostPlugin{}).Count(&count)
	if count != 0 {
		t.Fatalf("expected plugin deleted, count=%d", count)
	}
}
