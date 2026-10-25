package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Pacerino/CaddyProxyManager/internal/caddy/schema"
	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/database"
	"github.com/Pacerino/CaddyProxyManager/internal/jobqueue"

	"github.com/glebarez/sqlite"
	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"
)

func init() {
	// A global-scope schema fixture for the module-config tests.
	schema.Register(&schema.Schema{
		ModuleID: "test.global",
		Title:    "Test global",
		Scopes:   []schema.Scope{schema.ScopeGlobal},
		Path:     "apps/tls/automation/policies",
		Fields:   []schema.Field{{Key: "api_token", Label: "API Token", Type: schema.FieldSecret, Required: true}},
	})
}

// newTestHandler returns a Handler backed by a fresh in-memory SQLite database
// with the schema migrated. Each test gets an isolated database (private
// in-memory connection, single connection so it persists for the test).
func newTestHandler(t *testing.T) Handler {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?_pragma=foreign_keys(1)"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// A private in-memory DB lives only as long as its single connection, so
	// pin the pool to one connection for the test's lifetime.
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := db.AutoMigrate(&database.Host{}, &database.Upstream{}, &database.HostPlugin{}, &database.User{}, &database.ModuleConfig{}, &database.DNSProvider{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return Handler{DB: db}
}

const (
	fakeTLSPath      = "/config/apps/tls"
	fakeAutoPath     = "/config/apps/tls/automation"
	fakePoliciesPath = "/config/apps/tls/automation/policies"
)

// fakeCaddy is the state behind fakeCaddyServer. It models Caddy v2.11.7's
// admin API for the TLS policies array: a GET whose last path segment is
// missing answers 200 "null", one under a missing intermediate segment answers
// 400; PUT creates (the parent must exist) and PATCH replaces. Every other
// GET answers "null" and every other method 200.
type fakeCaddy struct {
	mu       sync.Mutex
	tls      bool
	auto     bool
	policies []any
}

// Policies returns a copy of the stored TLS policies array (nil when absent).
func (f *fakeCaddy) Policies() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []map[string]any
	for _, p := range f.policies {
		m, _ := p.(map[string]any)
		out = append(out, m)
	}
	return out
}

func (f *fakeCaddy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	reply := func(status int, out string) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(out))
	}
	path := r.URL.Path
	body, _ := io.ReadAll(r.Body)

	switch r.Method {
	case http.MethodGet:
		switch path {
		case fakeTLSPath:
			if f.tls {
				reply(http.StatusOK, "{}")
				return
			}
		case fakeAutoPath:
			if !f.tls {
				reply(http.StatusBadRequest, `{"error":"invalid traversal path at: apps/tls"}`)
				return
			}
			if f.auto {
				reply(http.StatusOK, "{}")
				return
			}
		case fakePoliciesPath:
			if !f.auto {
				reply(http.StatusBadRequest, `{"error":"invalid traversal path at: apps/tls/automation"}`)
				return
			}
			if f.policies != nil {
				b, _ := json.Marshal(f.policies)
				reply(http.StatusOK, string(b))
				return
			}
		}
		reply(http.StatusOK, "null")
	case http.MethodPut, http.MethodPatch:
		switch path {
		case fakeTLSPath:
			var v struct {
				Automation struct {
					Policies []any `json:"policies"`
				} `json:"automation"`
			}
			_ = json.Unmarshal(body, &v)
			f.tls, f.auto, f.policies = true, true, v.Automation.Policies
		case fakeAutoPath:
			var v struct {
				Policies []any `json:"policies"`
			}
			_ = json.Unmarshal(body, &v)
			f.auto, f.policies = true, v.Policies
		case fakePoliciesPath:
			var v []any
			_ = json.Unmarshal(body, &v)
			f.policies = v
		}
		reply(http.StatusOK, "")
	default:
		reply(http.StatusOK, "")
	}
}

// fakeCaddyServer starts an httptest server that accepts the admin API calls
// the APIProvider makes, so WriteHost/Apply succeed without a real Caddy.
func fakeCaddyServer(t *testing.T) *httptest.Server {
	srv, _ := fakeCaddyWithState(t)
	return srv
}

// fakeCaddyWithState is fakeCaddyServer plus access to the stored TLS policies.
func fakeCaddyWithState(t *testing.T) (*httptest.Server, *fakeCaddy) {
	t.Helper()
	f := &fakeCaddy{}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return srv, f
}

// useAPIMode points the global config at the fake Caddy and uses API mode with
// no reload side effects. It restores the previous config on cleanup.
func useAPIMode(t *testing.T, adminURL string) {
	t.Helper()
	prev := config.Configuration.Caddy
	config.Configuration.Caddy.Mode = "api"
	config.Configuration.Caddy.AdminURL = adminURL
	config.Configuration.Caddy.ServerName = "srv0"
	config.Configuration.Caddy.Listen = []string{":80"}
	config.Configuration.Caddy.ReloadStrategy = "none"
	t.Cleanup(func() { config.Configuration.Caddy = prev })
}

// startJobQueue starts the async job worker for tests that enqueue jobs and
// stops it on cleanup. Jobs run on a single worker goroutine.
func startJobQueue(t *testing.T) {
	t.Helper()
	jobqueue.Start()
	t.Cleanup(func() { _ = jobqueue.Shutdown() })
}

// drainJobQueue blocks until all previously enqueued jobs have run, by
// enqueueing a sentinel job and waiting for it to execute. The worker is a
// single goroutine processing jobs in order, so once the sentinel runs every
// earlier job has completed.
func drainJobQueue(t *testing.T) {
	t.Helper()
	done := make(chan struct{})
	if err := jobqueue.AddJob(jobqueue.Job{
		Name:   "TestSentinel",
		Action: func() error { close(done); return nil },
	}); err != nil {
		t.Fatalf("enqueue sentinel: %v", err)
	}
	<-done
}

// doRequest invokes a handler func with an optional JSON body and chi URL
// params, returning the recorded response.
func doRequest(handler http.HandlerFunc, method, target string, body any, urlParams map[string]string) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body != nil {
		buf, _ := json.Marshal(body)
		reader = bytes.NewReader(buf)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, target, reader)
	if len(urlParams) > 0 {
		rctx := chi.NewRouteContext()
		for k, v := range urlParams {
			rctx.URLParams.Add(k, v)
		}
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

// decodeResult unmarshals the { result } envelope into out.
func decodeResult(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	var env struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v (body=%s)", err, rec.Body.String())
	}
	if out != nil && len(env.Result) > 0 {
		if err := json.Unmarshal(env.Result, out); err != nil {
			t.Fatalf("decode result: %v (result=%s)", err, env.Result)
		}
	}
}
