package handler

import (
	"net/http"
	"testing"

	"github.com/Pacerino/CaddyProxyManager/internal/database"
)

func TestCreateHost(t *testing.T) {
	h := newTestHandler(t)
	srv := fakeCaddyServer(t)
	useAPIMode(t, srv.URL)
	startJobQueue(t)

	body := map[string]any{
		"domains": "example.com",
		"matcher": "",
		"Upstreams": []map[string]any{
			{"backend": "127.0.0.1:8080"},
		},
	}
	rec := doRequest(h.CreateHost(), http.MethodPost, "/hosts", body, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	drainJobQueue(t)

	var hosts []database.Host
	h.DB.Preload("Upstreams").Find(&hosts)
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	if len(hosts[0].Upstreams) != 1 {
		t.Fatalf("expected 1 upstream, got %d", len(hosts[0].Upstreams))
	}
}

func TestCreateHostValidationFails(t *testing.T) {
	h := newTestHandler(t)
	// Missing domains -> validation error, no Caddy needed.
	body := map[string]any{
		"domains":   "",
		"Upstreams": []map[string]any{{"backend": "127.0.0.1:8080"}},
	}
	rec := doRequest(h.CreateHost(), http.MethodPost, "/hosts", body, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestGetAndListHost(t *testing.T) {
	h := newTestHandler(t)
	host := database.Host{Domains: "a.com", Upstreams: []database.Upstream{{Backend: "127.0.0.1:1"}}}
	if err := h.DB.Create(&host).Error; err != nil {
		t.Fatal(err)
	}

	list := doRequest(h.GetHosts(), http.MethodGet, "/hosts", nil, nil)
	var hosts []database.Host
	decodeResult(t, list, &hosts)
	if len(hosts) != 1 || hosts[0].Domains != "a.com" {
		t.Fatalf("list returned %+v", hosts)
	}

	get := doRequest(h.GetHost(), http.MethodGet, "/hosts/1", nil, map[string]string{"hostID": "1"})
	var got database.Host
	decodeResult(t, get, &got)
	if got.ID != host.ID || len(got.Upstreams) != 1 {
		t.Fatalf("get returned %+v", got)
	}
}

// TestUpdateHostDoesNotDuplicateUpstreams is the regression test for the bug
// where repeated edits accumulated duplicate upstream rows.
func TestUpdateHostDoesNotDuplicateUpstreams(t *testing.T) {
	h := newTestHandler(t)
	srv := fakeCaddyServer(t)
	useAPIMode(t, srv.URL)
	startJobQueue(t)

	host := database.Host{
		Domains:   "example.com",
		Upstreams: []database.Upstream{{Backend: "127.0.0.1:8080"}},
	}
	if err := h.DB.Create(&host).Error; err != nil {
		t.Fatal(err)
	}

	// Simulate the frontend: send upstreams WITHOUT ids, several times.
	for i := 0; i < 3; i++ {
		body := map[string]any{
			"ID":      host.ID,
			"domains": "example.com",
			"matcher": "",
			"Upstreams": []map[string]any{
				{"backend": "127.0.0.1:8080"},
				{"backend": "127.0.0.1:8081"},
			},
		}
		rec := doRequest(h.UpdateHost(), http.MethodPut, "/hosts", body, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("update %d: status %d (%s)", i, rec.Code, rec.Body.String())
		}
	}
	drainJobQueue(t)

	var upstreams []database.Upstream
	h.DB.Where("host_id = ?", host.ID).Find(&upstreams)
	if len(upstreams) != 2 {
		t.Fatalf("expected 2 upstreams after repeated edits, got %d: %+v", len(upstreams), upstreams)
	}
}

func TestUpdateHostReducesUpstreams(t *testing.T) {
	h := newTestHandler(t)
	srv := fakeCaddyServer(t)
	useAPIMode(t, srv.URL)
	startJobQueue(t)

	host := database.Host{
		Domains: "example.com",
		Upstreams: []database.Upstream{
			{Backend: "127.0.0.1:8080"},
			{Backend: "127.0.0.1:8081"},
		},
	}
	if err := h.DB.Create(&host).Error; err != nil {
		t.Fatal(err)
	}

	body := map[string]any{
		"ID":        host.ID,
		"domains":   "example.com",
		"Upstreams": []map[string]any{{"backend": "127.0.0.1:9090"}},
	}
	rec := doRequest(h.UpdateHost(), http.MethodPut, "/hosts", body, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d (%s)", rec.Code, rec.Body.String())
	}
	drainJobQueue(t)

	var upstreams []database.Upstream
	h.DB.Where("host_id = ?", host.ID).Find(&upstreams)
	if len(upstreams) != 1 || upstreams[0].Backend != "127.0.0.1:9090" {
		t.Fatalf("expected single replaced upstream, got %+v", upstreams)
	}
}

func TestDeleteHost(t *testing.T) {
	h := newTestHandler(t)
	srv := fakeCaddyServer(t)
	useAPIMode(t, srv.URL)
	startJobQueue(t)

	host := database.Host{Domains: "a.com", Upstreams: []database.Upstream{{Backend: "127.0.0.1:1"}}}
	if err := h.DB.Create(&host).Error; err != nil {
		t.Fatal(err)
	}

	rec := doRequest(h.DeleteHost(), http.MethodDelete, "/hosts/1", nil, map[string]string{"hostID": "1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d (%s)", rec.Code, rec.Body.String())
	}
	drainJobQueue(t)

	var count int64
	h.DB.Model(&database.Host{}).Count(&count)
	if count != 0 {
		t.Fatalf("expected host deleted, count=%d", count)
	}
}
