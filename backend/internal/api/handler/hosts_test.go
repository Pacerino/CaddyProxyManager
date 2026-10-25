package handler

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/Pacerino/CaddyProxyManager/internal/config"
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

func createHostBody(domains string, providerID *uint) map[string]any {
	body := map[string]any{
		"domains":   domains,
		"Upstreams": []map[string]any{{"backend": "127.0.0.1:8080"}},
	}
	if providerID != nil {
		body["dnsProviderId"] = *providerID
	}
	return body
}

func TestCreateHost_WithProviderWritesPolicy(t *testing.T) {
	h, fake := newDNSTestHandler(t)
	p := createDNSProvider(t, h, "prod", cloudflareID, map[string]any{"api_token": "s3cret"})

	rec := doRequest(h.CreateHost(), http.MethodPost, "/hosts", createHostBody("a.example.com b.example.com", &p.ID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d (%s)", rec.Code, rec.Body.String())
	}
	drainJobQueue(t)

	var host database.Host
	decodeResult(t, rec, &host)
	policies := fake.Policies()
	if len(policies) != 1 || policies[0]["@id"] != fmt.Sprintf("cpm_tls_host_%d", host.ID) {
		t.Fatalf("policies = %v", policies)
	}
}

func TestDeleteHost_RemovesPolicy(t *testing.T) {
	h, fake := newDNSTestHandler(t)
	p := createDNSProvider(t, h, "prod", cloudflareID, map[string]any{"api_token": "s3cret"})
	rec := doRequest(h.CreateHost(), http.MethodPost, "/hosts", createHostBody("a.example.com", &p.ID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: status %d (%s)", rec.Code, rec.Body.String())
	}
	var host database.Host
	decodeResult(t, rec, &host)
	drainJobQueue(t)
	if len(fake.Policies()) != 1 {
		t.Fatalf("want 1 policy before delete, got %v", fake.Policies())
	}

	id := fmt.Sprint(host.ID)
	rec = doRequest(h.DeleteHost(), http.MethodDelete, "/hosts/"+id, nil, map[string]string{"hostID": id})
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: status %d (%s)", rec.Code, rec.Body.String())
	}
	drainJobQueue(t)

	for _, pol := range fake.Policies() {
		if pol["@id"] == "cpm_tls_host_"+id {
			t.Fatalf("policy for deleted host still present: %v", fake.Policies())
		}
	}
}

func TestHost_ConflictDifferentProvider400(t *testing.T) {
	h, _ := newDNSTestHandler(t)
	p1 := createDNSProvider(t, h, "one", cloudflareID, map[string]any{"api_token": "s3cret"})
	p2 := createDNSProvider(t, h, "two", porkbunID, map[string]any{"api_key": "s3cret"})
	first := database.Host{Domains: "a.example.com shared.example.com", DNSProviderID: &p1.ID}
	if err := h.DB.Create(&first).Error; err != nil {
		t.Fatal(err)
	}

	// Different provider, and nil vs. value, both conflict on create.
	for _, pid := range []*uint{&p2.ID, nil} {
		rec := doRequest(h.CreateHost(), http.MethodPost, "/hosts", createHostBody("shared.example.com", pid), nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("create: want 400, got %d (%s)", rec.Code, rec.Body.String())
		}
		if msg := rec.Body.String(); !strings.Contains(msg, "shared.example.com") || !strings.Contains(msg, fmt.Sprintf("host %d", first.ID)) {
			t.Errorf("message lacks domain/host id: %s", msg)
		}
	}

	// Update of another host onto the domain conflicts too.
	second := database.Host{Domains: "b.example.com", DNSProviderID: &p2.ID}
	if err := h.DB.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	body := createHostBody("shared.example.com", &p2.ID)
	body["ID"] = second.ID
	rec := doRequest(h.UpdateHost(), http.MethodPut, "/hosts", body, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("update: want 400, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestHost_SameDomainSameProviderAllowed(t *testing.T) {
	h, _ := newDNSTestHandler(t)
	p := createDNSProvider(t, h, "prod", cloudflareID, map[string]any{"api_token": "s3cret"})
	first := database.Host{Domains: "shared.example.com", DNSProviderID: &p.ID}
	if err := h.DB.Create(&first).Error; err != nil {
		t.Fatal(err)
	}

	rec := doRequest(h.CreateHost(), http.MethodPost, "/hosts", createHostBody("shared.example.com", &p.ID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d (%s)", rec.Code, rec.Body.String())
	}
	// Updating a host onto its own domains never conflicts with itself.
	body := createHostBody("shared.example.com", &p.ID)
	body["ID"] = first.ID
	rec = doRequest(h.UpdateHost(), http.MethodPut, "/hosts", body, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: status %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestHost_ProviderInCaddyfileMode400(t *testing.T) {
	h := newTestHandler(t)
	prev := config.Configuration.Caddy.Mode
	config.Configuration.Caddy.Mode = "caddyfile"
	t.Cleanup(func() { config.Configuration.Caddy.Mode = prev })

	id := uint(1)
	rec := doRequest(h.CreateHost(), http.MethodPost, "/hosts", createHostBody("a.example.com", &id), nil)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "DNS providers are only available when CPM_CADDY_MODE=api") {
		t.Fatalf("status %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestHost_UnknownProvider400(t *testing.T) {
	h, _ := newDNSTestHandler(t)
	id := uint(999)
	rec := doRequest(h.CreateHost(), http.MethodPost, "/hosts", createHostBody("a.example.com", &id), nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d (%s)", rec.Code, rec.Body.String())
	}
	var n int64
	h.DB.Model(&database.Host{}).Count(&n)
	if n != 0 {
		t.Errorf("host persisted despite 400")
	}
}

func TestHost_WildcardDomainWritesPolicy(t *testing.T) {
	h, fake := newDNSTestHandler(t)
	p := createDNSProvider(t, h, "prod", cloudflareID, map[string]any{"api_token": "s3cret"})

	rec := doRequest(h.CreateHost(), http.MethodPost, "/hosts", createHostBody("*.example.com", &p.ID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d (%s)", rec.Code, rec.Body.String())
	}
	drainJobQueue(t)

	policies := fake.Policies()
	if len(policies) != 1 {
		t.Fatalf("policies = %v", policies)
	}
	subjects, _ := policies[0]["subjects"].([]any)
	if len(subjects) != 1 || subjects[0] != "*.example.com" {
		t.Errorf("subjects = %v", subjects)
	}
}

func useCaddyfileMode(t *testing.T) {
	t.Helper()
	prev := config.Configuration.Caddy.Mode
	config.Configuration.Caddy.Mode = "caddyfile"
	t.Cleanup(func() { config.Configuration.Caddy.Mode = prev })
}

func TestHost_CaddyfileModeKeepsStoredProvider(t *testing.T) {
	h := newTestHandler(t)
	p := database.DNSProvider{Name: "prod", ModuleID: "dns.providers.cloudflare", Config: database.JSON(`{"api_token":"x"}`)}
	if err := h.DB.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	host := database.Host{Domains: "a.example.com", DNSProviderID: &p.ID, Upstreams: []database.Upstream{{Backend: "127.0.0.1:8080"}}}
	if err := h.DB.Create(&host).Error; err != nil {
		t.Fatal(err)
	}
	useCaddyfileMode(t)
	startJobQueue(t)

	// Unchanged stored provider: the edit goes through and keeps the value.
	body := createHostBody("b.example.com", &p.ID)
	body["ID"] = host.ID
	rec := doRequest(h.UpdateHost(), http.MethodPut, "/hosts", body, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("unchanged: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	drainJobQueue(t)
	var got database.Host
	if err := h.DB.First(&got, host.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.DNSProviderID == nil || *got.DNSProviderID != p.ID || got.Domains != "b.example.com" {
		t.Errorf("stored host = %+v", got)
	}

	// Changing or clearing it is rejected.
	other := p.ID + 1
	for _, pid := range []*uint{&other, nil} {
		body := createHostBody("b.example.com", pid)
		body["ID"] = host.ID
		rec := doRequest(h.UpdateHost(), http.MethodPut, "/hosts", body, nil)
		if pid != nil && rec.Code != http.StatusBadRequest {
			t.Errorf("change: want 400, got %d (%s)", rec.Code, rec.Body.String())
		}
		if pid == nil && rec.Code != http.StatusBadRequest {
			t.Errorf("clear: want 400, got %d (%s)", rec.Code, rec.Body.String())
		}
	}
}

func TestHost_ClearingProviderRemovesPolicy(t *testing.T) {
	h, fake := newDNSTestHandler(t)
	p := createDNSProvider(t, h, "prod", cloudflareID, map[string]any{"api_token": "s3cret"})
	rec := doRequest(h.CreateHost(), http.MethodPost, "/hosts", createHostBody("a.example.com", &p.ID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: status %d (%s)", rec.Code, rec.Body.String())
	}
	var host database.Host
	decodeResult(t, rec, &host)
	drainJobQueue(t)
	if len(fake.Policies()) != 1 {
		t.Fatalf("want 1 policy before clearing, got %v", fake.Policies())
	}

	body := createHostBody("a.example.com", nil)
	body["ID"] = host.ID
	rec = doRequest(h.UpdateHost(), http.MethodPut, "/hosts", body, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: status %d (%s)", rec.Code, rec.Body.String())
	}
	drainJobQueue(t)

	var got database.Host
	if err := h.DB.First(&got, host.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.DNSProviderID != nil {
		t.Errorf("provider not cleared in DB: %v", *got.DNSProviderID)
	}
	for _, pol := range fake.Policies() {
		if pol["@id"] == fmt.Sprintf("cpm_tls_host_%d", host.ID) {
			t.Fatalf("policy still present: %v", fake.Policies())
		}
	}
}
