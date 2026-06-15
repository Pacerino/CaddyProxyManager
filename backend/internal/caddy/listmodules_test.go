package caddy

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Pacerino/CaddyProxyManager/internal/config"
)

// TestListModules points the configured Caddy binary at a fake script that
// emits a known list-modules JSON payload, exercising the exec + parse path.
func TestListModules(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell binary not supported on windows")
	}
	prev := config.Configuration.Caddy.Binary
	t.Cleanup(func() { config.Configuration.Caddy.Binary = prev })

	script := filepath.Join(t.TempDir(), "fakecaddy")
	body := `#!/bin/sh
cat <<'EOF'
[
  {"module_name":"http.handlers.reverse_proxy","module_type":"standard","version":"v2","package_url":"caddy"},
  {"module_name":"dns.providers.cloudflare","module_type":"non-standard","version":"","package_url":"caddy-dns/cloudflare"}
]
EOF
`
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	config.Configuration.Caddy.Binary = script

	mods, err := ListModules()
	if err != nil {
		t.Fatalf("ListModules: %v", err)
	}
	if len(mods) != 2 {
		t.Fatalf("expected 2 modules, got %d", len(mods))
	}
	// Sorted: dns.providers.cloudflare first.
	if mods[0].ID != "dns.providers.cloudflare" || mods[0].Standard {
		t.Errorf("unexpected first module: %+v", mods[0])
	}
}

func TestListModulesBinaryMissing(t *testing.T) {
	prev := config.Configuration.Caddy.Binary
	t.Cleanup(func() { config.Configuration.Caddy.Binary = prev })

	config.Configuration.Caddy.Binary = "/nonexistent/definitely/not/caddy"
	if _, err := ListModules(); err == nil {
		t.Error("expected error when binary is missing")
	}
}
