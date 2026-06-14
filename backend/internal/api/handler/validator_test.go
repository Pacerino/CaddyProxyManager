package handler

import (
	"testing"

	"github.com/Pacerino/CaddyProxyManager/internal/database"
)

func TestDomainsValidator(t *testing.T) {
	tests := []struct {
		name    string
		domains string
		wantErr bool
	}{
		{"single fqdn", "example.com", false},
		{"multiple space separated", "example.com www.example.com", false},
		{"comma separated", "example.com,www.example.com", false},
		{"host:port", "example.com:8080", false},
		{"empty", "", true},
		{"invalid domain", "not a domain!", true},
		{"one invalid among valid", "example.com bad domain", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := database.Host{
				Domains:   tt.domains,
				Upstreams: []database.Upstream{{Backend: "127.0.0.1:8080"}},
			}
			err := validate.Struct(h)
			if tt.wantErr && err == nil {
				t.Errorf("domains %q: expected validation error, got none", tt.domains)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("domains %q: unexpected error %v", tt.domains, err)
			}
		})
	}
}
