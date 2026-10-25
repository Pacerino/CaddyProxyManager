package caddy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/Pacerino/CaddyProxyManager/internal/database"
)

func tlsHost(id uint, domains string, providerID *uint) database.Host {
	h := database.Host{Domains: domains, DNSProviderID: providerID}
	h.ID = id
	return h
}

func uptr(v uint) *uint { return &v }

func cfProvider() database.DNSProvider {
	return database.DNSProvider{
		Name:     "cf",
		ModuleID: "dns.providers.cloudflare",
		Config:   database.JSON(`{"api_token":"t","zone_token":"","other":null}`),
	}
}

var cfAvailable = map[string]bool{"dns.providers.cloudflare": true}

func TestBuildTLSPolicies_Shape(t *testing.T) {
	policies, warnings := BuildTLSPolicies(
		[]database.Host{tlsHost(1, "a.com b.com", uptr(5))},
		map[uint]database.DNSProvider{5: cfProvider()},
		cfAvailable,
	)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	gotJSON, _ := json.Marshal(policies)
	var got, want any
	json.Unmarshal(gotJSON, &got)
	json.Unmarshal([]byte(`[{"@id":"cpm_tls_host_1","subjects":["a.com","b.com"],"issuers":[{"module":"acme","challenges":{"dns":{"provider":{"name":"cloudflare","api_token":"t"}}}}]}]`), &want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %s", gotJSON)
	}
}

func TestBuildTLSPolicies_WildcardsLast(t *testing.T) {
	policies, _ := BuildTLSPolicies(
		[]database.Host{
			tlsHost(1, "*.example.com", uptr(5)),
			tlsHost(2, "foo.example.com", uptr(5)),
		},
		map[uint]database.DNSProvider{5: cfProvider()},
		cfAvailable,
	)
	if len(policies) != 2 {
		t.Fatalf("expected 2 policies, got %d", len(policies))
	}
	if policies[0]["@id"] != "cpm_tls_host_2" || policies[1]["@id"] != "cpm_tls_host_1" {
		t.Errorf("wrong order: %v, %v", policies[0]["@id"], policies[1]["@id"])
	}
}

func TestBuildTLSPolicies_SkipsUnavailableModule(t *testing.T) {
	policies, warnings := BuildTLSPolicies(
		[]database.Host{
			tlsHost(1, "a.com", uptr(5)),
			tlsHost(2, "b.com", nil),
			tlsHost(3, "c.com", uptr(99)),
		},
		map[uint]database.DNSProvider{5: cfProvider()},
		map[string]bool{},
	)
	if len(policies) != 0 {
		t.Errorf("expected no policies, got %v", policies)
	}
	if len(warnings) < 1 || !strings.Contains(warnings[0], "dns.providers.cloudflare") {
		t.Errorf("expected warning naming the module, got %v", warnings)
	}
	if len(warnings) != 2 {
		t.Errorf("expected warnings for the unavailable module and the missing provider only, got %v", warnings)
	}
}

func legacyCloudflarePolicy() map[string]any {
	return map[string]any{
		"issuers": []any{map[string]any{
			"module": "acme",
			"challenges": map[string]any{"dns": map[string]any{
				"provider": map[string]any{"name": "cloudflare", "api_token": "old"},
			}},
		}},
	}
}

func TestSyncTLSPolicies_KeepsForeignDropsLegacy(t *testing.T) {
	fake := newFakeCaddy()
	fake.tlsExists, fake.automationExists = true, true
	fake.tlsPolicies = []any{
		map[string]any{"subjects": []any{"x.org"}},
		map[string]any{"@id": "cpm_tls_host_9"},
		legacyCloudflarePolicy(),
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	p1 := map[string]any{"@id": "cpm_tls_host_1", "subjects": []any{"a.com"}}
	if err := newTestProvider(srv.URL).SyncTLSPolicies([]map[string]any{p1}); err != nil {
		t.Fatalf("SyncTLSPolicies: %v", err)
	}
	want := []any{p1, map[string]any{"subjects": []any{"x.org"}}}
	if !reflect.DeepEqual(fake.tlsPolicies, want) {
		t.Errorf("got %v, want %v", fake.tlsPolicies, want)
	}
	assertCalled(t, fake, "PATCH /config/apps/tls/automation/policies")
}

func TestSyncTLSPolicies_CreatesMissingTLSApp(t *testing.T) {
	fake := newFakeCaddy()
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	p1 := map[string]any{"@id": "cpm_tls_host_1"}
	if err := newTestProvider(srv.URL).SyncTLSPolicies([]map[string]any{p1}); err != nil {
		t.Fatalf("SyncTLSPolicies: %v", err)
	}
	assertCalled(t, fake, "PUT /config/apps/tls")
	if len(fake.tlsPolicies) != 1 {
		t.Errorf("expected 1 stored policy, got %v", fake.tlsPolicies)
	}
}

func TestSyncTLSPolicies_CreatesMissingAutomation(t *testing.T) {
	fake := newFakeCaddy()
	fake.tlsExists = true
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	if err := newTestProvider(srv.URL).SyncTLSPolicies([]map[string]any{{"@id": "cpm_tls_host_1"}}); err != nil {
		t.Fatalf("SyncTLSPolicies: %v", err)
	}
	assertCalled(t, fake, "PUT /config/apps/tls/automation")
}

func TestSyncTLSPolicies_NoWriteWhenNothingToDo(t *testing.T) {
	fake := newFakeCaddy()
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	if err := newTestProvider(srv.URL).SyncTLSPolicies(nil); err != nil {
		t.Fatalf("SyncTLSPolicies: %v", err)
	}
	for _, c := range fake.calls {
		if !strings.HasPrefix(c, "GET ") {
			t.Errorf("expected no writes, got %q", c)
		}
	}
}

func TestSyncTLSPolicies_RemovesLastPolicy(t *testing.T) {
	fake := newFakeCaddy()
	fake.tlsExists, fake.automationExists = true, true
	fake.tlsPolicies = []any{map[string]any{"@id": "cpm_tls_host_9"}}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	if err := newTestProvider(srv.URL).SyncTLSPolicies(nil); err != nil {
		t.Fatalf("SyncTLSPolicies: %v", err)
	}
	if len(fake.tlsPolicies) != 0 {
		t.Errorf("expected empty policies, got %v", fake.tlsPolicies)
	}
}

func TestPing(t *testing.T) {
	fake := newFakeCaddy()
	srv := httptest.NewServer(fake.handler())
	if err := newTestProvider(srv.URL).Ping(t.Context()); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	srv.Close()
	if err := newTestProvider(srv.URL).Ping(t.Context()); err == nil {
		t.Error("expected error for refused connection")
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()
	if err := newTestProvider(bad.URL).Ping(t.Context()); err == nil {
		t.Error("expected error for 500")
	}
}
