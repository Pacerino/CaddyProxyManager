package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Pacerino/CaddyProxyManager/internal/caddy"
	"github.com/Pacerino/CaddyProxyManager/internal/caddy/dnsschema"
	"github.com/Pacerino/CaddyProxyManager/internal/caddy/schema"
	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/database"
)

type stubResolver struct {
	providers []dnsschema.Provider
	err       error
}

func (s stubResolver) Providers(context.Context) ([]dnsschema.Provider, error) {
	return s.providers, s.err
}

const (
	cloudflareID = "dns.providers.cloudflare"
	porkbunID    = "dns.providers.porkbun"
)

func stubProviders() []dnsschema.Provider {
	return []dnsschema.Provider{
		{ModuleID: cloudflareID, Name: "cloudflare", Title: "Cloudflare", Source: dnsschema.SourceBinary, Fields: []schema.Field{
			{Key: "api_token", Label: "API Token", Type: schema.FieldSecret},
			{Key: "zone_id", Label: "Zone ID", Type: schema.FieldString},
		}},
		{ModuleID: porkbunID, Name: "porkbun", Title: "Porkbun", Source: dnsschema.SourceBinary, Fields: []schema.Field{
			{Key: "api_key", Label: "API Key", Type: schema.FieldSecret},
		}},
		{ModuleID: "dns.providers.rawdns", Name: "rawdns", Title: "Rawdns", Source: dnsschema.SourceNone, Fields: []schema.Field{}},
	}
}

func stubModules() ([]caddy.Module, error) {
	return []caddy.Module{
		{ID: cloudflareID, Namespace: "dns.providers", Name: "cloudflare"},
		{ID: porkbunID, Namespace: "dns.providers", Name: "porkbun"},
		{ID: "dns.providers.rawdns", Namespace: "dns.providers", Name: "rawdns"},
	}, nil
}

// newDNSTestHandler wires the stub resolver and module list, a fake Caddy in
// API mode and the job queue.
func newDNSTestHandler(t *testing.T) (Handler, *fakeCaddy) {
	t.Helper()
	h := newTestHandler(t)
	h.DNSSchemas = stubResolver{providers: stubProviders()}
	h.ListModules = stubModules
	srv, fake := fakeCaddyWithState(t)
	useAPIMode(t, srv.URL)
	startJobQueue(t)
	// Let queued syncs finish while the fake Caddy is still up.
	t.Cleanup(func() { drainJobQueue(t) })
	return h, fake
}

type dnsProviderView struct {
	ID         uint           `json:"id"`
	Name       string         `json:"name"`
	ModuleID   string         `json:"moduleId"`
	Values     map[string]any `json:"values"`
	SecretsSet []string       `json:"secretsSet"`
	UsedBy     []struct {
		ID      uint   `json:"id"`
		Domains string `json:"domains"`
	} `json:"usedBy"`
	Available bool `json:"available"`
}

func createDNSProvider(t *testing.T, h Handler, name, moduleID string, values map[string]any) dnsProviderView {
	t.Helper()
	rec := doRequest(h.CreateDNSProvider(), http.MethodPost, "/caddy/dns-providers",
		map[string]any{"name": name, "moduleId": moduleID, "values": values}, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %s: status %d (%s)", name, rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "s3cret") {
		t.Fatalf("create response leaks secret: %s", rec.Body.String())
	}
	var v dnsProviderView
	decodeResult(t, rec, &v)
	return v
}

func putDNSProvider(h Handler, id uint, body map[string]any) (int, string) {
	rec := doRequest(h.UpdateDNSProvider(), http.MethodPut, "/caddy/dns-providers/x", body,
		map[string]string{"id": itoa(id)})
	return rec.Code, rec.Body.String()
}

func itoa(n uint) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func storedConfig(t *testing.T, h Handler, id uint) map[string]any {
	t.Helper()
	var row database.DNSProvider
	if err := h.DB.First(&row, id).Error; err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(row.Config, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestDNSProviderTypes(t *testing.T) {
	h, _ := newDNSTestHandler(t)
	rec := doRequest(h.GetDNSProviderTypes(), http.MethodGet, "/caddy/dns-provider-types", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d (%s)", rec.Code, rec.Body.String())
	}
	var types []dnsschema.Provider
	decodeResult(t, rec, &types)
	if len(types) != 3 || types[0].ModuleID != cloudflareID {
		t.Errorf("types = %+v", types)
	}
}

func TestDNSProviderTypes_EmptyIsArray(t *testing.T) {
	h, _ := newDNSTestHandler(t)
	h.DNSSchemas = stubResolver{} // nil slice
	rec := doRequest(h.GetDNSProviderTypes(), http.MethodGet, "/caddy/dns-provider-types", nil, nil)
	if !strings.Contains(rec.Body.String(), `"result":[]`) {
		t.Errorf("want empty array result, got %s", rec.Body.String())
	}
}

func TestDNSProviders_CreateListMasksSecrets(t *testing.T) {
	h, _ := newDNSTestHandler(t)
	created := createDNSProvider(t, h, "prod", cloudflareID, map[string]any{"api_token": "s3cret", "zone_id": "z1"})
	if strings.Contains(strings.Join(created.SecretsSet, ","), "s3cret") || created.Values["api_token"] != nil {
		t.Errorf("create response leaks secret: %+v", created)
	}

	rec := doRequest(h.GetDNSProviders(), http.MethodGet, "/caddy/dns-providers", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "s3cret") {
		t.Fatalf("list leaks secret: %s", rec.Body.String())
	}
	var list []dnsProviderView
	decodeResult(t, rec, &list)
	if len(list) != 1 {
		t.Fatalf("list = %+v", list)
	}
	got := list[0]
	if len(got.SecretsSet) != 1 || got.SecretsSet[0] != "api_token" {
		t.Errorf("secretsSet = %v", got.SecretsSet)
	}
	if got.Values["zone_id"] != "z1" || !got.Available || got.Name != "prod" || got.ModuleID != cloudflareID {
		t.Errorf("view = %+v", got)
	}
	if got.UsedBy == nil || len(got.UsedBy) != 0 {
		t.Errorf("usedBy should be an empty array, got %v", got.UsedBy)
	}
}

func TestDNSProviders_ListEmptyIsArray(t *testing.T) {
	h, _ := newDNSTestHandler(t)
	rec := doRequest(h.GetDNSProviders(), http.MethodGet, "/caddy/dns-providers", nil, nil)
	if !strings.Contains(rec.Body.String(), `"result":[]`) {
		t.Errorf("want empty array result, got %s", rec.Body.String())
	}
}

func TestDNSProviders_UnavailableModule(t *testing.T) {
	h, _ := newDNSTestHandler(t)
	// Stored credential whose module is no longer in the build.
	row := database.DNSProvider{Name: "old", ModuleID: "dns.providers.gone", Config: database.JSON(`{"api_token":"hush","region":"eu"}`)}
	if err := h.DB.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	rec := doRequest(h.GetDNSProviders(), http.MethodGet, "/caddy/dns-providers", nil, nil)
	if strings.Contains(rec.Body.String(), "hush") {
		t.Fatalf("unknown module leaks secret: %s", rec.Body.String())
	}
	var list []dnsProviderView
	decodeResult(t, rec, &list)
	if len(list) != 1 || list[0].Available || len(list[0].SecretsSet) != 1 || list[0].Values["region"] != "eu" {
		t.Errorf("view = %+v", list)
	}
}

func TestDNSProviders_UpdateKeepsSecret(t *testing.T) {
	h, _ := newDNSTestHandler(t)
	p := createDNSProvider(t, h, "prod", cloudflareID, map[string]any{"api_token": "s3cret"})

	code, body := putDNSProvider(h, p.ID, map[string]any{
		"name": "renamed", "moduleId": cloudflareID, "values": map[string]any{"api_token": "", "zone_id": "z2"},
	})
	if code != http.StatusOK {
		t.Fatalf("status %d (%s)", code, body)
	}
	if strings.Contains(body, "s3cret") {
		t.Errorf("update response leaks secret: %s", body)
	}
	cfg := storedConfig(t, h, p.ID)
	if cfg["api_token"] != "s3cret" || cfg["zone_id"] != "z2" {
		t.Errorf("stored config = %v", cfg)
	}
	var row database.DNSProvider
	h.DB.First(&row, p.ID)
	if row.Name != "renamed" {
		t.Errorf("name = %q", row.Name)
	}

	// An absent secret keeps the stored value too; a new one replaces it.
	putDNSProvider(h, p.ID, map[string]any{"name": "renamed", "moduleId": cloudflareID, "values": map[string]any{}})
	if cfg := storedConfig(t, h, p.ID); cfg["api_token"] != "s3cret" {
		t.Errorf("absent secret dropped: %v", cfg)
	}
	_, body = putDNSProvider(h, p.ID, map[string]any{"name": "renamed", "moduleId": cloudflareID, "values": map[string]any{"api_token": "n3w"}})
	if strings.Contains(body, "n3w") || strings.Contains(body, "s3cret") {
		t.Errorf("update response leaks secret: %s", body)
	}
	if cfg := storedConfig(t, h, p.ID); cfg["api_token"] != "n3w" {
		t.Errorf("secret not replaced: %v", cfg)
	}
}

func TestDNSProviders_UpdateTypeChangeDropsValues(t *testing.T) {
	h, _ := newDNSTestHandler(t)
	p := createDNSProvider(t, h, "prod", cloudflareID, map[string]any{"api_token": "s3cret", "zone_id": "z1"})

	code, body := putDNSProvider(h, p.ID, map[string]any{
		"name": "prod", "moduleId": porkbunID, "values": map[string]any{"api_key": "k"},
	})
	if code != http.StatusOK {
		t.Fatalf("status %d (%s)", code, body)
	}
	cfg := storedConfig(t, h, p.ID)
	if len(cfg) != 1 || cfg["api_key"] != "k" {
		t.Errorf("stored config = %v, want exactly {api_key:k}", cfg)
	}
}

func TestDNSProviders_UpdateNotFound(t *testing.T) {
	h, _ := newDNSTestHandler(t)
	code, _ := putDNSProvider(h, 99, map[string]any{"name": "x", "moduleId": cloudflareID, "values": map[string]any{}})
	if code != http.StatusNotFound {
		t.Errorf("status %d, want 404", code)
	}
}

func TestDNSProviders_DuplicateName400(t *testing.T) {
	h, _ := newDNSTestHandler(t)
	createDNSProvider(t, h, "prod", cloudflareID, map[string]any{"api_token": "a"})
	other := createDNSProvider(t, h, "other", cloudflareID, map[string]any{"api_token": "b"})

	rec := doRequest(h.CreateDNSProvider(), http.MethodPost, "/caddy/dns-providers",
		map[string]any{"name": "prod", "moduleId": cloudflareID, "values": map[string]any{"api_token": "c"}}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("create duplicate: status %d", rec.Code)
	}
	if code, _ := putDNSProvider(h, other.ID, map[string]any{"name": "prod", "moduleId": cloudflareID, "values": map[string]any{}}); code != http.StatusBadRequest {
		t.Errorf("rename to duplicate: status %d", code)
	}
	// Keeping its own name is fine.
	if code, body := putDNSProvider(h, other.ID, map[string]any{"name": "other", "moduleId": cloudflareID, "values": map[string]any{}}); code != http.StatusOK {
		t.Errorf("same name update: status %d (%s)", code, body)
	}
}

func TestDNSProviders_UnknownModule400(t *testing.T) {
	h, _ := newDNSTestHandler(t)
	rec := doRequest(h.CreateDNSProvider(), http.MethodPost, "/caddy/dns-providers",
		map[string]any{"name": "x", "moduleId": "dns.providers.nope", "values": map[string]any{}}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestDNSProviders_Validation400(t *testing.T) {
	h, _ := newDNSTestHandler(t)
	post := func(body map[string]any) int {
		return doRequest(h.CreateDNSProvider(), http.MethodPost, "/caddy/dns-providers", body, nil).Code
	}
	if c := post(map[string]any{"name": "  ", "moduleId": cloudflareID, "values": map[string]any{}}); c != http.StatusBadRequest {
		t.Errorf("empty name: %d", c)
	}
	if c := post(map[string]any{"name": "x", "moduleId": cloudflareID, "values": map[string]any{"api_token": 5}}); c != http.StatusBadRequest {
		t.Errorf("wrong field type: %d", c)
	}
	if c := post(map[string]any{"name": "x", "moduleId": "dns.providers.rawdns", "values": []string{"a"}}); c != http.StatusBadRequest {
		t.Errorf("non-object values: %d", c)
	}
	if c := post(map[string]any{"name": "raw", "moduleId": "dns.providers.rawdns", "values": map[string]any{"anything": 1}}); c != http.StatusCreated {
		t.Errorf("raw object values: %d", c)
	}
}

func TestDNSProviders_DeleteInUse409(t *testing.T) {
	h, _ := newDNSTestHandler(t)
	p := createDNSProvider(t, h, "prod", cloudflareID, map[string]any{"api_token": "a"})
	host := database.Host{Domains: "a.example.com", DNSProviderID: &p.ID}
	if err := h.DB.Create(&host).Error; err != nil {
		t.Fatal(err)
	}

	rec := doRequest(h.DeleteDNSProvider(), http.MethodDelete, "/caddy/dns-providers/1", nil, map[string]string{"id": itoa(p.ID)})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d (%s)", rec.Code, rec.Body.String())
	}
	var env struct {
		Error struct {
			Data []struct {
				ID      uint   `json:"id"`
				Domains string `json:"domains"`
			} `json:"invalid"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Error.Data) != 1 || env.Error.Data[0].ID != host.ID || env.Error.Data[0].Domains != "a.example.com" {
		t.Errorf("error data = %s", rec.Body.String())
	}
	var n int64
	h.DB.Model(&database.DNSProvider{}).Count(&n)
	if n != 1 {
		t.Errorf("provider was deleted")
	}

	// The list reports the host under usedBy.
	list := doRequest(h.GetDNSProviders(), http.MethodGet, "/caddy/dns-providers", nil, nil)
	var views []dnsProviderView
	decodeResult(t, list, &views)
	if len(views) != 1 || len(views[0].UsedBy) != 1 || views[0].UsedBy[0].ID != host.ID {
		t.Errorf("usedBy = %+v", views)
	}
}

func TestDNSProviders_DeleteAndNameReuse(t *testing.T) {
	h, _ := newDNSTestHandler(t)
	p := createDNSProvider(t, h, "prod", cloudflareID, map[string]any{"api_token": "a"})

	rec := doRequest(h.DeleteDNSProvider(), http.MethodDelete, "/caddy/dns-providers/1", nil, map[string]string{"id": itoa(p.ID)})
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status %d (%s)", rec.Code, rec.Body.String())
	}
	createDNSProvider(t, h, "prod", cloudflareID, map[string]any{"api_token": "b"})

	rec = doRequest(h.DeleteDNSProvider(), http.MethodDelete, "/caddy/dns-providers/99", nil, map[string]string{"id": "99"})
	if rec.Code != http.StatusNotFound {
		t.Errorf("delete missing: status %d", rec.Code)
	}
}

func TestDNSProviders_UpdateResyncsAllHosts(t *testing.T) {
	h, fake := newDNSTestHandler(t)
	p := createDNSProvider(t, h, "prod", cloudflareID, map[string]any{"api_token": "old"})
	for _, d := range []string{"a.example.com", "b.example.com", "c.example.com"} {
		if err := h.DB.Create(&database.Host{Domains: d, DNSProviderID: &p.ID}).Error; err != nil {
			t.Fatal(err)
		}
	}

	code, body := putDNSProvider(h, p.ID, map[string]any{
		"name": "prod", "moduleId": cloudflareID, "values": map[string]any{"api_token": "fresh"},
	})
	if code != http.StatusOK {
		t.Fatalf("status %d (%s)", code, body)
	}
	drainJobQueue(t)

	policies := fake.Policies()
	if len(policies) != 3 {
		t.Fatalf("want 3 CPM policies, got %d: %v", len(policies), policies)
	}
	for _, pol := range policies {
		id, _ := pol["@id"].(string)
		if !strings.HasPrefix(id, "cpm_tls_host_") {
			t.Errorf("policy id %q", id)
		}
		issuers := pol["issuers"].([]any)
		prov := issuers[0].(map[string]any)["challenges"].(map[string]any)["dns"].(map[string]any)["provider"].(map[string]any)
		if prov["api_token"] != "fresh" || prov["name"] != "cloudflare" {
			t.Errorf("provider = %v", prov)
		}
	}
}

func TestDNSProviders_CaddyfileMode400(t *testing.T) {
	h := newTestHandler(t)
	prev := config.Configuration.Caddy.Mode
	config.Configuration.Caddy.Mode = "caddyfile"
	t.Cleanup(func() { config.Configuration.Caddy.Mode = prev })

	calls := map[string]*struct{ fn http.HandlerFunc }{
		"types":  {h.GetDNSProviderTypes()},
		"list":   {h.GetDNSProviders()},
		"create": {h.CreateDNSProvider()},
		"update": {h.UpdateDNSProvider()},
		"delete": {h.DeleteDNSProvider()},
	}
	for name, c := range calls {
		rec := doRequest(c.fn, http.MethodPost, "/x", map[string]any{}, map[string]string{"id": "1"})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, rec.Code)
		}
	}
}
