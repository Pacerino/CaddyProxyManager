package caddy

import (
	"testing"

	"github.com/Pacerino/CaddyProxyManager/internal/config"
)

func TestGetProvider(t *testing.T) {
	original := config.Configuration.Caddy.Mode
	t.Cleanup(func() { config.Configuration.Caddy.Mode = original })

	tests := []struct {
		mode    string
		want    string
		wantErr bool
	}{
		{"", "*caddy.CaddyfileProvider", false},
		{"caddyfile", "*caddy.CaddyfileProvider", false},
		{"CADDYFILE", "*caddy.CaddyfileProvider", false},
		{"api", "*caddy.APIProvider", false},
		{"API", "*caddy.APIProvider", false},
		{"bogus", "", true},
	}

	for _, tt := range tests {
		config.Configuration.Caddy.Mode = tt.mode
		p, err := GetProvider()
		if tt.wantErr {
			if err == nil {
				t.Errorf("mode %q: expected error, got none", tt.mode)
			}
			continue
		}
		if err != nil {
			t.Errorf("mode %q: unexpected error %v", tt.mode, err)
			continue
		}
		if got := typeName(p); got != tt.want {
			t.Errorf("mode %q: got %s, want %s", tt.mode, got, tt.want)
		}
	}
}

func typeName(v any) string {
	switch v.(type) {
	case *CaddyfileProvider:
		return "*caddy.CaddyfileProvider"
	case *APIProvider:
		return "*caddy.APIProvider"
	default:
		return "unknown"
	}
}
