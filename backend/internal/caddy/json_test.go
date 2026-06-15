package caddy

import (
	"reflect"
	"testing"

	"github.com/Pacerino/CaddyProxyManager/internal/database"
)

func TestHostRouteID(t *testing.T) {
	if got := hostRouteID(42); got != "cpm_host_42" {
		t.Fatalf("hostRouteID(42) = %q, want cpm_host_42", got)
	}
}

func TestSplitDomains(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", []string{}},
		{"   ", []string{}},
		{"example.com", []string{"example.com"}},
		{"a.com b.com", []string{"a.com", "b.com"}},
		{"  a.com   b.com  ", []string{"a.com", "b.com"}},
	}
	for _, tt := range tests {
		if got := splitDomains(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("splitDomains(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestBuildRoute(t *testing.T) {
	h := database.Host{
		Domains: "example.com www.example.com",
		Upstreams: []database.Upstream{
			{Backend: "127.0.0.1:8080"},
			{Backend: "127.0.0.1:8081"},
		},
	}
	h.ID = 7

	route := buildRoute(h)

	if route["@id"] != "cpm_host_7" {
		t.Errorf("@id = %v, want cpm_host_7", route["@id"])
	}
	if route["terminal"] != true {
		t.Errorf("terminal = %v, want true", route["terminal"])
	}

	match := route["match"].([]map[string]any)
	hosts := match[0]["host"].([]string)
	if !reflect.DeepEqual(hosts, []string{"example.com", "www.example.com"}) {
		t.Errorf("hosts = %v", hosts)
	}

	handle := route["handle"].([]map[string]any)
	if handle[0]["handler"] != "reverse_proxy" {
		t.Errorf("handler = %v, want reverse_proxy", handle[0]["handler"])
	}
	ups := handle[0]["upstreams"].([]map[string]any)
	if len(ups) != 2 || ups[0]["dial"] != "127.0.0.1:8080" || ups[1]["dial"] != "127.0.0.1:8081" {
		t.Errorf("upstreams = %v", ups)
	}
}

func TestBuildRouteInjectsHostPlugins(t *testing.T) {
	h := database.Host{
		Domains:   "example.com",
		Upstreams: []database.Upstream{{Backend: "127.0.0.1:8080"}},
		Plugins: []database.HostPlugin{
			{
				ModuleID: "http.handlers.authentication",
				Enabled:  true,
				Handler:  database.JSON(`{"handler":"authentication"}`),
			},
			{
				ModuleID: "http.handlers.authentication",
				Enabled:  false, // disabled -> skipped
				Handler:  database.JSON(`{"handler":"ignored"}`),
			},
		},
	}
	h.ID = 1

	route := buildRoute(h)
	handle := route["handle"].([]map[string]any)

	// Plugin handler must come first, reverse_proxy last.
	if len(handle) != 2 {
		t.Fatalf("expected 2 handlers, got %d: %v", len(handle), handle)
	}
	if handle[0]["handler"] != "authentication" {
		t.Errorf("first handler = %v, want authentication", handle[0]["handler"])
	}
	if handle[len(handle)-1]["handler"] != "reverse_proxy" {
		t.Errorf("last handler = %v, want reverse_proxy", handle[len(handle)-1]["handler"])
	}
}
