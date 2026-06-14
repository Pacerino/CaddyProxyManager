package caddy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Pacerino/CaddyProxyManager/internal/database"
)

// fakeCaddy is a minimal in-memory stand-in for the Caddy admin API that
// mimics the quirks the real API exhibits (200 + "null" body for absent paths).
type fakeCaddy struct {
	serverExists bool
	routes       map[string]bool // route id -> exists
	calls        []string        // method+path log for assertions
}

func newFakeCaddy() *fakeCaddy {
	return &fakeCaddy{routes: map[string]bool{}}
}

func (f *fakeCaddy) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/config/apps/http/servers/"):
			// Caddy returns 200 + "null" when the server is absent.
			if f.serverExists {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"listen":[":8080"],"routes":[]}`))
			} else {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("null"))
			}
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/id/"):
			id := strings.TrimPrefix(path, "/id/")
			w.WriteHeader(http.StatusOK)
			if f.routes[id] {
				w.Write([]byte(`{"@id":"` + id + `"}`))
			} else {
				w.Write([]byte("null"))
			}
		case r.Method == http.MethodPatch && path == "/config/apps/http":
			f.serverExists = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/routes"):
			// New route appended; mark generic existence is handled by id GET.
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPatch && strings.HasPrefix(path, "/id/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/id/"):
			id := strings.TrimPrefix(path, "/id/")
			delete(f.routes, id)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	})
}

func newTestProvider(url string) *APIProvider {
	return &APIProvider{
		baseURL:    strings.TrimRight(url, "/"),
		serverName: "srv0",
		listen:     []string{":8080"},
		client:     &http.Client{Timeout: 2 * time.Second},
	}
}

func sampleHost() database.Host {
	h := database.Host{
		Domains:   "example.com",
		Upstreams: []database.Upstream{{Backend: "127.0.0.1:8080"}},
	}
	h.ID = 1
	return h
}

func TestAPIProviderBootstrapsServerOnFirstWrite(t *testing.T) {
	fake := newFakeCaddy()
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	p := newTestProvider(srv.URL)
	if err := p.WriteHost(sampleHost()); err != nil {
		t.Fatalf("WriteHost: %v", err)
	}

	if !fake.serverExists {
		t.Fatal("expected server to be bootstrapped via PATCH")
	}
	assertCalled(t, fake, "PATCH /config/apps/http")
	assertCalled(t, fake, "POST /config/apps/http/servers/srv0/routes")
}

func TestAPIProviderUpdatesExistingRoute(t *testing.T) {
	fake := newFakeCaddy()
	fake.serverExists = true
	fake.routes["cpm_host_1"] = true
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	p := newTestProvider(srv.URL)
	if err := p.WriteHost(sampleHost()); err != nil {
		t.Fatalf("WriteHost: %v", err)
	}

	// Existing route -> PATCH /id/, not a POST.
	assertCalled(t, fake, "PATCH /id/cpm_host_1")
	for _, c := range fake.calls {
		if strings.HasPrefix(c, "POST ") {
			t.Errorf("did not expect POST on update, got %q", c)
		}
	}
}

func TestAPIProviderRemoveHost(t *testing.T) {
	fake := newFakeCaddy()
	fake.serverExists = true
	fake.routes["cpm_host_1"] = true
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	p := newTestProvider(srv.URL)
	if err := p.RemoveHost(1); err != nil {
		t.Fatalf("RemoveHost: %v", err)
	}
	assertCalled(t, fake, "DELETE /id/cpm_host_1")
	if fake.routes["cpm_host_1"] {
		t.Error("route should have been deleted")
	}
}

func TestAPIProviderRemoveMissingHostIsNoop(t *testing.T) {
	fake := newFakeCaddy()
	fake.serverExists = true
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	p := newTestProvider(srv.URL)
	if err := p.RemoveHost(99); err != nil {
		t.Fatalf("RemoveHost on missing should be noop, got %v", err)
	}
	for _, c := range fake.calls {
		if strings.HasPrefix(c, "DELETE ") {
			t.Errorf("expected no DELETE for missing host, got %q", c)
		}
	}
}

func assertCalled(t *testing.T, f *fakeCaddy, want string) {
	t.Helper()
	for _, c := range f.calls {
		if c == want {
			return
		}
	}
	t.Errorf("expected call %q, calls were: %v", want, f.calls)
}
