package caddy

import "testing"

const sampleListModules = `[
  {"module_name":"http.handlers.reverse_proxy","module_type":"standard","version":"v2.11.4","package_url":"github.com/caddyserver/caddy/v2"},
  {"module_name":"dns.providers.cloudflare","module_type":"non-standard","version":"","package_url":"github.com/caddy-dns/cloudflare"},
  {"module_name":"http","module_type":"standard","version":"v2.11.4","package_url":"github.com/caddyserver/caddy/v2"},
  {"module_name":"  ","module_type":"standard"}
]`

func TestParseModules(t *testing.T) {
	mods, err := parseModules([]byte(sampleListModules))
	if err != nil {
		t.Fatalf("parseModules: %v", err)
	}

	// The blank module_name entry must be dropped.
	if len(mods) != 3 {
		t.Fatalf("expected 3 modules, got %d: %+v", len(mods), mods)
	}

	// Sorted by ID: dns.providers.cloudflare, http, http.handlers.reverse_proxy
	if mods[0].ID != "dns.providers.cloudflare" {
		t.Errorf("expected sorted first id dns.providers.cloudflare, got %q", mods[0].ID)
	}

	cf := mods[0]
	if cf.Namespace != "dns.providers" || cf.Name != "cloudflare" {
		t.Errorf("split wrong: namespace=%q name=%q", cf.Namespace, cf.Name)
	}
	if cf.Standard {
		t.Error("cloudflare should be flagged non-standard (plugin)")
	}
	if cf.Package != "github.com/caddy-dns/cloudflare" {
		t.Errorf("package not carried through: %q", cf.Package)
	}

	// Top-level module without a dot.
	if mods[1].ID != "http" || mods[1].Namespace != "" || mods[1].Name != "http" {
		t.Errorf("top-level module split wrong: %+v", mods[1])
	}
	if !mods[2].Standard {
		t.Error("reverse_proxy should be standard")
	}
}

func TestSplitModuleID(t *testing.T) {
	cases := map[string][2]string{
		"dns.providers.cloudflare":    {"dns.providers", "cloudflare"},
		"http.handlers.reverse_proxy": {"http.handlers", "reverse_proxy"},
		"http":                        {"", "http"},
	}
	for id, want := range cases {
		ns, name := splitModuleID(id)
		if ns != want[0] || name != want[1] {
			t.Errorf("splitModuleID(%q) = (%q,%q), want (%q,%q)", id, ns, name, want[0], want[1])
		}
	}
}
