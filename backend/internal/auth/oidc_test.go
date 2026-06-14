package auth

import (
	"testing"

	"github.com/Pacerino/CaddyProxyManager/internal/config"
)

func TestIsAllowedDomain(t *testing.T) {
	original := config.Configuration.Auth.OIDC.AllowedDomains
	t.Cleanup(func() { config.Configuration.Auth.OIDC.AllowedDomains = original })

	tests := []struct {
		name    string
		allowed []string
		email   string
		want    bool
	}{
		{"empty allowlist permits all", nil, "user@anything.com", true},
		{"exact match", []string{"example.com"}, "user@example.com", true},
		{"case insensitive", []string{"Example.COM"}, "user@example.com", true},
		{"whitespace trimmed", []string{" example.com "}, "user@example.com", true},
		{"not allowed", []string{"example.com"}, "user@evil.com", false},
		{"no at sign", []string{"example.com"}, "notanemail", false},
		{"one of many", []string{"a.com", "b.com"}, "user@b.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config.Configuration.Auth.OIDC.AllowedDomains = tt.allowed
			if got := isAllowedDomain(tt.email); got != tt.want {
				t.Errorf("isAllowedDomain(%q) with %v = %v, want %v", tt.email, tt.allowed, got, tt.want)
			}
		})
	}
}

func TestIsOIDCEnabled(t *testing.T) {
	original := config.Configuration.Auth.Mode
	t.Cleanup(func() { config.Configuration.Auth.Mode = original })

	config.Configuration.Auth.Mode = "oidc"
	if !IsOIDCEnabled() {
		t.Error("expected oidc enabled")
	}
	config.Configuration.Auth.Mode = "OIDC"
	if !IsOIDCEnabled() {
		t.Error("expected oidc enabled (case-insensitive)")
	}
	config.Configuration.Auth.Mode = "local"
	if IsOIDCEnabled() {
		t.Error("expected oidc disabled")
	}
}
