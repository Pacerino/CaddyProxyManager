package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/database"
	"github.com/Pacerino/CaddyProxyManager/internal/jobqueue"

	"github.com/glebarez/sqlite"
	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"
)

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

	if err := db.AutoMigrate(&database.Host{}, &database.Upstream{}, &database.HostPlugin{}, &database.User{}, &database.ModuleConfig{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return Handler{DB: db}
}

// fakeCaddyServer starts an httptest server that accepts the admin API calls
// the APIProvider makes, so WriteHost/Apply succeed without a real Caddy.
func fakeCaddyServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GET traversal endpoints return "null" (absent) so the provider
		// bootstraps; everything else returns 200.
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("null"))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv
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
