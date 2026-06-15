package caddy

import (
	"os"
	"strings"
	"testing"

	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/database"
)

// useCaddyfileTempDirs points the config at temp folders and disables reloads
// so the CaddyfileProvider can write files without side effects.
func useCaddyfileTempDirs(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev := config.Configuration
	config.Configuration.DataFolder = dir
	config.Configuration.LogFolder = dir
	config.Configuration.Caddy.ReloadStrategy = "none"
	t.Cleanup(func() { config.Configuration = prev })
	return dir
}

func TestCaddyfileWriteHost(t *testing.T) {
	dir := useCaddyfileTempDirs(t)
	p := &CaddyfileProvider{}

	host := database.Host{
		Domains:   "example.com",
		Matcher:   "/api/*",
		Upstreams: []database.Upstream{{Backend: "127.0.0.1:8080"}},
	}
	host.ID = 5

	if err := p.WriteHost(host); err != nil {
		t.Fatalf("WriteHost: %v", err)
	}

	data, err := os.ReadFile(hostConfigPath(5))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	out := string(data)
	if !strings.Contains(out, "example.com") {
		t.Errorf("missing domain in output: %s", out)
	}
	if !strings.Contains(out, "reverse_proxy") || !strings.Contains(out, "127.0.0.1:8080") {
		t.Errorf("missing reverse_proxy/upstream: %s", out)
	}
	_ = dir
}

func TestCaddyfileWriteHostWithBasicAuth(t *testing.T) {
	useCaddyfileTempDirs(t)
	p := &CaddyfileProvider{}

	host := database.Host{
		Domains:   "secure.example.com",
		Upstreams: []database.Upstream{{Backend: "127.0.0.1:9000"}},
		Plugins: []database.HostPlugin{
			{
				ModuleID: "http.handlers.authentication",
				Enabled:  true,
				Handler:  database.JSON(`{"handler":"authentication","providers":{"http_basic":{"accounts":[{"username":"bob","password":"hashed=="}]}}}`),
			},
		},
	}
	host.ID = 6

	if err := p.WriteHost(host); err != nil {
		t.Fatalf("WriteHost: %v", err)
	}
	data, _ := os.ReadFile(hostConfigPath(6))
	out := string(data)
	if !strings.Contains(out, "basic_auth") {
		t.Errorf("expected basic_auth block: %s", out)
	}
	if !strings.Contains(out, "bob") || !strings.Contains(out, "hashed==") {
		t.Errorf("expected account in output: %s", out)
	}
}

func TestCaddyfileRemoveHost(t *testing.T) {
	useCaddyfileTempDirs(t)
	p := &CaddyfileProvider{}

	host := database.Host{Domains: "a.com", Upstreams: []database.Upstream{{Backend: "127.0.0.1:1"}}}
	host.ID = 7
	if err := p.WriteHost(host); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(hostConfigPath(7)); err != nil {
		t.Fatalf("config should exist: %v", err)
	}

	if err := p.RemoveHost(7); err != nil {
		t.Fatalf("RemoveHost: %v", err)
	}
	if _, err := os.Stat(hostConfigPath(7)); !os.IsNotExist(err) {
		t.Errorf("config should be removed, stat err = %v", err)
	}

	// Removing a missing host is a no-op (file already gone).
	if err := p.RemoveHost(7); err != nil {
		t.Errorf("RemoveHost on missing should be noop, got %v", err)
	}
}

func TestBasicAuthAccounts(t *testing.T) {
	host := database.Host{
		Plugins: []database.HostPlugin{
			{
				ModuleID: "http.handlers.authentication",
				Enabled:  true,
				Handler:  database.JSON(`{"providers":{"http_basic":{"accounts":[{"username":"u1","password":"p1"}]}}}`),
			},
			{
				ModuleID: "http.handlers.authentication",
				Enabled:  false, // skipped
				Handler:  database.JSON(`{"providers":{"http_basic":{"accounts":[{"username":"skip","password":"x"}]}}}`),
			},
			{
				ModuleID: "other.module", // skipped
				Enabled:  true,
				Handler:  database.JSON(`{}`),
			},
		},
	}

	accounts := basicAuthAccounts(host)
	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d: %+v", len(accounts), accounts)
	}
	if accounts[0].User != "u1" || accounts[0].Hash != "p1" {
		t.Errorf("account = %+v", accounts[0])
	}
}

func TestHostConfigPath(t *testing.T) {
	useCaddyfileTempDirs(t)
	got := hostConfigPath(42)
	if !strings.HasSuffix(got, "/host_42.conf") {
		t.Errorf("hostConfigPath = %q", got)
	}
}
