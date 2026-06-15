package caddy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Pacerino/CaddyProxyManager/internal/database"
)

// recordingCaddy records requests and serves configurable GET responses.
type recordingCaddy struct {
	getBody  string // body returned for GET requests
	requests []recordedRequest
}

type recordedRequest struct {
	method string
	path   string
	body   string
}

func (c *recordingCaddy) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		c.requests = append(c.requests, recordedRequest{r.Method, r.URL.Path, string(raw)})
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			body := c.getBody
			if body == "" {
				body = "null"
			}
			_, _ = w.Write([]byte(body))
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}

func (c *recordingCaddy) find(method, path string) *recordedRequest {
	for i := range c.requests {
		if c.requests[i].method == method && c.requests[i].path == path {
			return &c.requests[i]
		}
	}
	return nil
}

func TestGetConfig(t *testing.T) {
	fake := &recordingCaddy{getBody: `{"listen":[":80"]}`}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	p := newTestProvider(srv.URL)

	var out map[string]any
	if err := p.GetConfig("apps/http", &out); err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if fake.find(http.MethodGet, "/config/apps/http") == nil {
		t.Fatalf("expected GET /config/apps/http, got %+v", fake.requests)
	}
	if _, ok := out["listen"]; !ok {
		t.Errorf("decoded config missing listen: %v", out)
	}
}

func TestGetConfigNormalisesLeadingPaths(t *testing.T) {
	fake := &recordingCaddy{getBody: "null"}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	p := newTestProvider(srv.URL)

	// A path that already includes /config/ and a leading slash should not
	// double up.
	if err := p.GetConfig("/config/apps", nil); err != nil {
		t.Fatal(err)
	}
	if fake.find(http.MethodGet, "/config/apps") == nil {
		t.Errorf("path not normalised: %+v", fake.requests)
	}
}

func TestPatchConfig(t *testing.T) {
	fake := &recordingCaddy{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	p := newTestProvider(srv.URL)

	if err := p.PatchConfig("apps/tls", map[string]any{"x": 1}); err != nil {
		t.Fatalf("PatchConfig: %v", err)
	}
	req := fake.find(http.MethodPatch, "/config/apps/tls")
	if req == nil {
		t.Fatalf("expected PATCH /config/apps/tls, got %+v", fake.requests)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(req.body), &sent); err != nil {
		t.Fatalf("body not json: %v", err)
	}
	if sent["x"].(float64) != 1 {
		t.Errorf("patched body = %v", sent)
	}
}

func TestApplyModuleConfigEnabled(t *testing.T) {
	fake := &recordingCaddy{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	p := newTestProvider(srv.URL)

	mc := database.ModuleConfig{
		ModuleID: "dns.providers.cloudflare",
		Path:     "apps/tls/automation/policies",
		Config:   database.JSON(`{"issuers":[]}`),
		Enabled:  true,
	}
	if err := p.ApplyModuleConfig(mc); err != nil {
		t.Fatalf("ApplyModuleConfig: %v", err)
	}
	if fake.find(http.MethodPatch, "/config/apps/tls/automation/policies") == nil {
		t.Errorf("expected PATCH to module path, got %+v", fake.requests)
	}
}

func TestApplyModuleConfigDisabledRemoves(t *testing.T) {
	// getBody non-null so configExists reports present and a DELETE follows.
	fake := &recordingCaddy{getBody: `{"present":true}`}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	p := newTestProvider(srv.URL)

	mc := database.ModuleConfig{
		ModuleID: "dns.providers.cloudflare",
		Path:     "apps/tls/automation/policies",
		Config:   database.JSON(`{"issuers":[]}`),
		Enabled:  false,
	}
	if err := p.ApplyModuleConfig(mc); err != nil {
		t.Fatalf("ApplyModuleConfig disabled: %v", err)
	}
	if fake.find(http.MethodDelete, "/config/apps/tls/automation/policies") == nil {
		t.Errorf("expected DELETE for disabled config, got %+v", fake.requests)
	}
}

func TestRemoveModuleConfigMissingIsNoop(t *testing.T) {
	fake := &recordingCaddy{getBody: "null"} // not present
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	p := newTestProvider(srv.URL)

	mc := database.ModuleConfig{Path: "apps/tls/automation/policies"}
	if err := p.RemoveModuleConfig(mc); err != nil {
		t.Fatalf("RemoveModuleConfig: %v", err)
	}
	for _, r := range fake.requests {
		if r.method == http.MethodDelete {
			t.Errorf("expected no DELETE for missing config, got %+v", fake.requests)
		}
	}
}

func TestTrimConfigPrefix(t *testing.T) {
	cases := map[string]string{
		"apps/http":         "apps/http",
		"/apps/http":        "apps/http",
		"///apps/http":      "apps/http",
		"config/apps/http":  "apps/http",
		"/config/apps/http": "apps/http",
	}
	for in, want := range cases {
		if got := trimConfigPrefix(in); got != want {
			t.Errorf("trimConfigPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}
