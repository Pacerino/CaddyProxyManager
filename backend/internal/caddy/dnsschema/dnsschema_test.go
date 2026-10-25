package dnsschema

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Pacerino/CaddyProxyManager/internal/caddy"
	"github.com/Pacerino/CaddyProxyManager/internal/caddy/schema"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func fieldByKey(t *testing.T, p Provider, key string) schema.Field {
	t.Helper()
	for _, f := range p.Fields {
		if f.Key == key {
			return f
		}
	}
	t.Fatalf("field %q not found in %+v", key, p.Fields)
	return schema.Field{}
}

func TestParseSchemaFile_Cloudflare(t *testing.T) {
	providers, err := ParseSchemaFile(readFixture(t, "caddy_schema_dns.json"))
	if err != nil {
		t.Fatal(err)
	}
	p, ok := providers["dns.providers.cloudflare"]
	if !ok {
		t.Fatal("cloudflare missing")
	}
	if p.Title != "Cloudflare" || p.Name != "cloudflare" || p.Source != "binary" {
		t.Errorf("unexpected provider: %+v", p)
	}
	tok := fieldByKey(t, p, "api_token")
	if tok.Type != schema.FieldSecret || tok.Label != "API Token" {
		t.Errorf("api_token: %+v", tok)
	}
	if !strings.HasPrefix(tok.Description, "API token is used for authentication.") {
		t.Errorf("description: %q", tok.Description)
	}
	zone := fieldByKey(t, p, "zone_token")
	if zone.Type != schema.FieldSecret || zone.Description != "" {
		t.Errorf("zone_token: %+v", zone)
	}
}

func TestParseSchemaFile_TypeMapping(t *testing.T) {
	providers, err := ParseSchemaFile(readFixture(t, "caddy_schema_dns.json"))
	if err != nil {
		t.Fatal(err)
	}
	p := providers["dns.providers.fixture"]
	want := map[string]schema.FieldType{
		"ttl":    schema.FieldInt,
		"debug":  schema.FieldBool,
		"region": schema.FieldString,
		"zones":  schema.FieldStringList,
		"extra":  schema.FieldJSON,
	}
	for key, typ := range want {
		if got := fieldByKey(t, p, key); got.Type != typ {
			t.Errorf("%s: type %q, want %q", key, got.Type, typ)
		}
	}
	for _, f := range p.Fields {
		if f.Required {
			t.Errorf("%s must not be required", f.Key)
		}
	}
}

func TestParseSchemaFile_OnlyDNSProviders(t *testing.T) {
	data := []byte(`{"definitions":{"http.handlers.foo":{"type":"object","properties":{"a":{"type":"string"}}},"dns.providers.x":{"type":"object","properties":{}}}}`)
	providers, err := ParseSchemaFile(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 {
		t.Fatalf("got %d providers: %v", len(providers), providers)
	}
	if _, ok := providers["dns.providers.x"]; !ok {
		t.Error("dns.providers.x missing")
	}
}

func TestParseDocsAPI_DuckDNS(t *testing.T) {
	p, err := ParseDocsAPI("dns.providers.duckdns", readFixture(t, "docsapi_duckdns.json"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Source != "docs" || p.Name != "duckdns" || p.Title != "Duckdns" {
		t.Errorf("unexpected provider: %+v", p)
	}
	if f := fieldByKey(t, p, "api_token"); f.Type != schema.FieldSecret {
		t.Errorf("api_token: %+v", f)
	}
	if f := fieldByKey(t, p, "override_domain"); f.Type != schema.FieldString {
		t.Errorf("override_domain: %+v", f)
	}
}

func TestParseDocsAPI_TypeMapping(t *testing.T) {
	data := []byte(`{"result":{"structure":{"type":"struct","struct_fields":[
		{"key":"a","value":{"type":"bool"}},
		{"key":"b","value":{"type":"int"}},
		{"key":"c","value":{"type":"float"}},
		{"key":"d","value":{"type":"array","elems":{"type":"string"}}},
		{"key":"e","value":{"type":"duration"}},
		{"key":"f","value":{"type":"map"}}]}}}`)
	p, err := ParseDocsAPI("dns.providers.t", data)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]schema.FieldType{
		"a": schema.FieldBool, "b": schema.FieldInt, "c": schema.FieldInt,
		"d": schema.FieldStringList, "e": schema.FieldString, "f": schema.FieldJSON,
	}
	for k, typ := range want {
		if got := fieldByKey(t, p, k); got.Type != typ {
			t.Errorf("%s: %q, want %q", k, got.Type, typ)
		}
	}
	if got := fieldByKey(t, p, "e"); got.Placeholder != "30s" {
		t.Errorf("duration placeholder: %q", got.Placeholder)
	}
}

func noOverride(string) (*schema.Schema, bool) { return nil, false }

func fakeModules() ([]caddy.Module, error) {
	return []caddy.Module{
		{ID: "dns.providers.unknown", Namespace: "dns.providers", Name: "unknown"},
		{ID: "dns.providers.duckdns", Namespace: "dns.providers", Name: "duckdns"},
		{ID: "dns.providers.cloudflare", Namespace: "dns.providers", Name: "cloudflare"},
		{ID: "http.handlers.reverse_proxy", Namespace: "http.handlers", Name: "reverse_proxy"},
	}, nil
}

func TestResolver_FallbackChain(t *testing.T) {
	var duckHits int32
	docs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/duckdns") {
			atomic.AddInt32(&duckHits, 1)
			_, _ = w.Write(readFixture(t, "docsapi_duckdns.json"))
			return
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer docs.Close()

	schemaFile := filepath.Join(t.TempDir(), "schema.json")
	cf := `{"definitions":{"dns.providers.cloudflare":{"description":"cloudflare: object\nModule: dns.providers.cloudflare\nWraps.\n","type":"object","properties":{"api_token":{"type":"string"}}}}}`
	if err := os.WriteFile(schemaFile, []byte(cf), 0o600); err != nil {
		t.Fatal(err)
	}

	r := &Resolver{SchemaFile: schemaFile, DocsBaseURL: docs.URL + "/", Client: docs.Client(), ListModules: fakeModules, override: noOverride}
	for i := 0; i < 2; i++ {
		providers, err := r.Providers(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		var ids []string
		for _, p := range providers {
			got[p.ModuleID] = p.Source
			ids = append(ids, p.ModuleID)
		}
		want := map[string]string{
			"dns.providers.cloudflare": "binary",
			"dns.providers.duckdns":    "docs",
			"dns.providers.unknown":    "none",
		}
		if len(got) != 3 {
			t.Fatalf("got %v", got)
		}
		for id, src := range want {
			if got[id] != src {
				t.Errorf("%s: source %q, want %q", id, got[id], src)
			}
		}
		if ids[0] != "dns.providers.cloudflare" || ids[2] != "dns.providers.unknown" {
			t.Errorf("not sorted: %v", ids)
		}
	}
	if n := atomic.LoadInt32(&duckHits); n != 1 {
		t.Errorf("docs server hit %d times for duckdns, want 1", n)
	}
}

func TestResolver_MissingSchemaFile(t *testing.T) {
	docs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(readFixture(t, "docsapi_duckdns.json"))
	}))
	defer docs.Close()
	r := &Resolver{
		SchemaFile:  filepath.Join(t.TempDir(), "nope.json"),
		DocsBaseURL: docs.URL + "/",
		Client:      docs.Client(),
		override:    noOverride,
		ListModules: func() ([]caddy.Module, error) {
			return []caddy.Module{{ID: "dns.providers.duckdns", Namespace: "dns.providers", Name: "duckdns"}}, nil
		},
	}
	providers, err := r.Providers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 || providers[0].Source != "docs" {
		t.Errorf("got %+v", providers)
	}
}

func TestResolver_RegisteredSchemaOverrides(t *testing.T) {
	r := &Resolver{
		ListModules: fakeModules,
		override: func(id string) (*schema.Schema, bool) {
			if id == "dns.providers.cloudflare" {
				return &schema.Schema{ModuleID: id, Title: "Registered", Fields: []schema.Field{{Key: "api_token", Type: schema.FieldSecret}}}, true
			}
			return nil, false
		},
	}
	providers, err := r.Providers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range providers {
		if p.ModuleID == "dns.providers.cloudflare" {
			found = true
			if p.Source != SourceOverride {
				t.Errorf("cloudflare source %q, want override", p.Source)
			}
		}
	}
	if !found {
		t.Fatal("cloudflare missing from providers")
	}
}

func duckResolver(docsURL string, client *http.Client) *Resolver {
	return &Resolver{
		DocsBaseURL: docsURL,
		Client:      client,
		override:    noOverride,
		ListModules: func() ([]caddy.Module, error) {
			return []caddy.Module{{ID: "dns.providers.duckdns", Namespace: "dns.providers", Name: "duckdns"}}, nil
		},
	}
}

type failingTransport struct{ fail atomic.Bool }

func (f *failingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if f.fail.Load() {
		return nil, errors.New("network down")
	}
	return http.DefaultTransport.RoundTrip(req)
}

func TestResolver_TransportFailureNotCached(t *testing.T) {
	docs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(readFixture(t, "docsapi_duckdns.json"))
	}))
	defer docs.Close()
	tr := &failingTransport{}
	tr.fail.Store(true)
	r := duckResolver(docs.URL+"/", &http.Client{Transport: tr})

	first, err := r.Providers(context.Background())
	if err != nil || first[0].Source != SourceNone {
		t.Fatalf("first: %+v %v", first, err)
	}
	tr.fail.Store(false)
	second, err := r.Providers(context.Background())
	if err != nil || second[0].Source != SourceDocs {
		t.Fatalf("second: %+v %v", second, err)
	}
}

func TestResolver_CancelledContextNotCached(t *testing.T) {
	docs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(readFixture(t, "docsapi_duckdns.json"))
	}))
	defer docs.Close()
	r := duckResolver(docs.URL+"/", docs.Client())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	first, err := r.Providers(ctx)
	if err != nil || first[0].Source != SourceNone {
		t.Fatalf("first: %+v %v", first, err)
	}
	second, err := r.Providers(context.Background())
	if err != nil || second[0].Source != SourceDocs {
		t.Fatalf("second: %+v %v", second, err)
	}
}

func TestSchemaFileFor(t *testing.T) {
	dir := t.TempDir()
	const shipped = "/usr/share/cpm/caddy_schema.json"
	schemaPath := filepath.Join(dir, "caddy_schema.json")
	sidecar := filepath.Join(dir, "caddy_schema.sha256")

	if got := SchemaFileFor(dir, "abc", shipped); got != shipped {
		t.Fatalf("missing files: got %q", got)
	}
	if err := os.WriteFile(schemaPath, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := SchemaFileFor(dir, "abc", shipped); got != shipped {
		t.Fatalf("missing sidecar: got %q", got)
	}
	if err := os.WriteFile(sidecar, []byte("abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := SchemaFileFor(dir, "abc", shipped); got != schemaPath {
		t.Fatalf("matching sidecar: got %q", got)
	}
	if got := SchemaFileFor(dir, "def", shipped); got != shipped {
		t.Fatalf("mismatch: got %q", got)
	}
	if got := SchemaFileFor(dir, "", shipped); got != shipped {
		t.Fatalf("empty sha: got %q", got)
	}
}
