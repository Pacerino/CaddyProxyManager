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

// TestListModulesTextFallback simulates an older Caddy that rejects --json but
// supports the plain-text flags, exercising the text-parsing fallback.
func TestListModulesTextFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell binary not supported on windows")
	}
	prev := config.Configuration.Caddy.Binary
	t.Cleanup(func() { config.Configuration.Caddy.Binary = prev })

	script := filepath.Join(t.TempDir(), "fakecaddy")
	body := `#!/bin/sh
# Reject --json like older caddy does.
for a in "$@"; do
  if [ "$a" = "--json" ]; then
    echo "Error: unknown flag: --json" >&2
    exit 1
  fi
done
# --skip-standard returns only the plugin(s).
case "$*" in
  *--skip-standard*)
    echo "dns.providers.cloudflare github.com/caddy-dns/cloudflare"
    ;;
  *)
    echo "http.handlers.reverse_proxy github.com/caddyserver/caddy/v2"
    echo "dns.providers.cloudflare github.com/caddy-dns/cloudflare"
    echo ""
    echo "  Standard modules: 1"
    ;;
esac
`
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	config.Configuration.Caddy.Binary = script

	mods, err := ListModules()
	if err != nil {
		t.Fatalf("ListModules text fallback: %v", err)
	}
	if len(mods) != 2 {
		t.Fatalf("expected 2 modules, got %d: %+v", len(mods), mods)
	}

	byID := map[string]Module{}
	for _, m := range mods {
		byID[m.ID] = m
	}
	cf, ok := byID["dns.providers.cloudflare"]
	if !ok || cf.Standard {
		t.Errorf("cloudflare should be present and non-standard: %+v", cf)
	}
	if cf.Package != "github.com/caddy-dns/cloudflare" {
		t.Errorf("cloudflare package = %q", cf.Package)
	}
	rp, ok := byID["http.handlers.reverse_proxy"]
	if !ok || !rp.Standard {
		t.Errorf("reverse_proxy should be present and standard: %+v", rp)
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
