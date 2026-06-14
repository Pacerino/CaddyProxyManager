package caddy

import (
	"fmt"
	"strings"

	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/database"
)

// Provider applies host configuration to Caddy. Implementations either write
// per-host Caddyfiles (CaddyfileProvider) or manage Caddy's JSON config through
// its admin API (APIProvider).
type Provider interface {
	// WriteHost creates or updates the configuration for a host.
	WriteHost(h database.Host) error
	// RemoveHost removes the configuration for a host.
	RemoveHost(hostID int) error
}

// GetProvider returns the configured Provider based on CPM_CADDY_MODE.
func GetProvider() (Provider, error) {
	switch strings.ToLower(config.Configuration.Caddy.Mode) {
	case "", "caddyfile":
		return &CaddyfileProvider{}, nil
	case "api":
		return NewAPIProvider(), nil
	default:
		return nil, fmt.Errorf("unknown caddy mode %q (expected \"caddyfile\" or \"api\")", config.Configuration.Caddy.Mode)
	}
}
