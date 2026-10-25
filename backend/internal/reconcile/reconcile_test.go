package reconcile

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Pacerino/CaddyProxyManager/internal/caddy"
	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/database"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

const (
	tlsPath  = "/config/apps/tls"
	autoPath = "/config/apps/tls/automation"
	polPath  = "/config/apps/tls/automation/policies"
)

// fakeCaddy is a small stateful Caddy admin API. Like the real one, a GET whose
// last path segment is missing answers 200 "null" and one under a missing
// intermediate segment answers 400.
type fakeCaddy struct {
	mu sync.Mutex

	pings, downPings int // GET /config/ calls; the first downPings answer 503
	serverExists     bool
	failFirstRoute   bool

	routeWrites     int
	pingsAtFirstRun int // pings seen when the first route was written
	tlsWrites       int
	tlsExists, auto bool
	policies        []any
	moduleApplies   []string
}

func (f *fakeCaddy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := r.URL.Path
	body, _ := io.ReadAll(r.Body)
	reply := func(status int, out string) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(out))
	}
	null := func(present bool, out string) {
		if present {
			reply(200, out)
		} else {
			reply(200, "null")
		}
	}

	switch {
	case r.Method == http.MethodGet && path == "/config/":
		f.pings++
		if f.pings <= f.downPings {
			reply(http.StatusServiceUnavailable, "down")
			return
		}
		reply(200, "{}")
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/config/apps/http/servers/"):
		null(f.serverExists, `{"routes":[]}`)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/id/"):
		reply(200, "null")
	case r.Method == http.MethodPatch && path == "/config/apps/http":
		f.serverExists = true
		reply(200, "")
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/routes"):
		if f.routeWrites == 0 {
			f.pingsAtFirstRun = f.pings
		}
		f.routeWrites++
		if f.failFirstRoute && f.routeWrites == 1 {
			reply(500, "boom")
			return
		}
		reply(200, "")
	case r.Method == http.MethodGet && path == tlsPath:
		null(f.tlsExists, "{}")
	case r.Method == http.MethodGet && path == autoPath:
		if !f.tlsExists {
			reply(400, `{"error":"invalid traversal path at: apps/tls"}`)
			return
		}
		null(f.auto, "{}")
	case r.Method == http.MethodGet && path == polPath:
		if !f.auto {
			reply(400, `{"error":"invalid traversal path at: apps/tls/automation"}`)
			return
		}
		if f.policies == nil {
			reply(200, "null")
			return
		}
		b, _ := json.Marshal(f.policies)
		reply(200, string(b))
	case r.Method == http.MethodPut && path == tlsPath:
		var v struct {
			Automation struct {
				Policies []any `json:"policies"`
			} `json:"automation"`
		}
		_ = json.Unmarshal(body, &v)
		f.tlsExists, f.auto, f.policies = true, true, v.Automation.Policies
		f.tlsWrites++
		reply(200, "")
	case (r.Method == http.MethodPatch || r.Method == http.MethodPut) && path == polPath:
		var v []any
		_ = json.Unmarshal(body, &v)
		f.policies = v
		f.tlsWrites++
		reply(200, "")
	case r.Method == http.MethodPatch && strings.HasPrefix(path, "/config/"):
		f.moduleApplies = append(f.moduleApplies, path)
		reply(200, "")
	default:
		reply(400, "unexpected "+r.Method+" "+path)
	}
}

// setup returns an in-memory DB and an APIProvider pointed at a fresh fake.
func setup(t *testing.T) (*gorm.DB, *caddy.APIProvider, *fakeCaddy) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&database.Host{}, &database.Upstream{}, &database.HostPlugin{}, &database.ModuleConfig{}, &database.DNSProvider{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	fake := &fakeCaddy{}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	prev := config.Configuration.Caddy
	config.Configuration.Caddy.AdminURL = srv.URL
	config.Configuration.Caddy.ServerName = "srv0"
	config.Configuration.Caddy.Listen = []string{":80"}
	t.Cleanup(func() { config.Configuration.Caddy = prev })
	return db, caddy.NewAPIProvider(), fake
}

func modules() ([]caddy.Module, error) {
	return []caddy.Module{{ID: "dns.providers.cloudflare", Namespace: "dns.providers"}}, nil
}

func noBackoff(int) time.Duration { return 0 }

func TestRun_WaitsForCaddy(t *testing.T) {
	db, p, fake := setup(t)
	fake.downPings = 2
	db.Create(&database.Host{Domains: "a.example.com"})

	if errs := Run(context.Background(), db, p, modules, noBackoff); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if fake.routeWrites != 1 {
		t.Fatalf("route writes = %d, want 1", fake.routeWrites)
	}
	if fake.pingsAtFirstRun != 3 {
		t.Errorf("route written after %d pings, want 3", fake.pingsAtFirstRun)
	}
}

func TestRun_ReappliesEverything(t *testing.T) {
	db, p, fake := setup(t)
	prov := database.DNSProvider{Name: "CF", ModuleID: "dns.providers.cloudflare", Config: database.JSON(`{"api_token":"t"}`)}
	db.Create(&prov)
	h1 := database.Host{Domains: "a.example.com", DNSProviderID: &prov.ID}
	h2 := database.Host{Domains: "b.example.com"}
	db.Create(&h1)
	db.Create(&h2)
	db.Create(&database.ModuleConfig{ModuleID: "on", Path: "apps/foo", Config: database.JSON(`{"x":1}`), Enabled: true})
	// gorm skips the zero value on create because of the column default.
	off := database.ModuleConfig{ModuleID: "off", Path: "apps/bar", Config: database.JSON(`{}`), Enabled: true}
	db.Create(&off)
	db.Model(&off).Update("enabled", false)

	if errs := Run(context.Background(), db, p, modules, noBackoff); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if fake.routeWrites != 2 {
		t.Errorf("route writes = %d, want 2", fake.routeWrites)
	}
	if fake.tlsWrites != 1 {
		t.Errorf("tls writes = %d, want 1", fake.tlsWrites)
	}
	b, _ := json.Marshal(fake.policies)
	if !strings.Contains(string(b), caddy.HostPolicyID(h1.ID)) || len(fake.policies) != 1 {
		t.Errorf("policies = %s, want exactly %s", b, caddy.HostPolicyID(h1.ID))
	}
	if len(fake.moduleApplies) != 1 || fake.moduleApplies[0] != "/config/apps/foo" {
		t.Errorf("module applies = %v, want [/config/apps/foo]", fake.moduleApplies)
	}
}

func TestRun_ContinuesPastFailures(t *testing.T) {
	db, p, fake := setup(t)
	fake.failFirstRoute = true
	db.Create(&database.Host{Domains: "a.example.com"})
	db.Create(&database.Host{Domains: "b.example.com"})

	errs := Run(context.Background(), db, p, modules, noBackoff)
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want exactly 1", errs)
	}
	if fake.routeWrites != 2 {
		t.Errorf("route writes = %d, want 2 (second host still written)", fake.routeWrites)
	}
}

func TestRun_CancelledContext(t *testing.T) {
	db, p, fake := setup(t)
	fake.downPings = 1 << 30
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	done := make(chan []error, 1)
	go func() {
		done <- Run(ctx, db, p, modules, func(int) time.Duration { return 5 * time.Millisecond })
	}()
	select {
	case errs := <-done:
		if len(errs) != 1 {
			t.Errorf("errors = %v, want 1 (context cancelled)", errs)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if fake.routeWrites != 0 {
		t.Errorf("route writes = %d, want 0", fake.routeWrites)
	}
}

func TestDefaultBackoff(t *testing.T) {
	for attempt, want := range map[int]time.Duration{0: time.Second, 1: 2 * time.Second, 4: 16 * time.Second, 5: 30 * time.Second, 50: 30 * time.Second} {
		if got := DefaultBackoff(attempt); got != want {
			t.Errorf("DefaultBackoff(%d) = %v, want %v", attempt, got, want)
		}
	}
}
