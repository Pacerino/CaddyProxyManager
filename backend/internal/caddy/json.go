package caddy

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Pacerino/CaddyProxyManager/internal/database"
)

// hostRouteID returns the @id used to address a host's route in Caddy's config.
func hostRouteID(hostID uint) string {
	return fmt.Sprintf("cpm_host_%d", hostID)
}

// buildRoute converts a Host into a Caddy JSON route object.
//
// The resulting route matches the host's domains and reverse-proxies matching
// requests to the configured upstreams. It is tagged with an @id so it can be
// addressed individually through the admin API.
func buildRoute(h database.Host) map[string]any {
	hosts := splitDomains(h.Domains)

	upstreams := make([]map[string]any, 0, len(h.Upstreams))
	for _, u := range h.Upstreams {
		upstreams = append(upstreams, map[string]any{"dial": u.Backend})
	}

	proxy := map[string]any{
		"handler":   "reverse_proxy",
		"upstreams": upstreams,
	}

	// Host plugin handlers (e.g. authentication) run before the reverse proxy.
	handle := pluginHandlers(h)
	handle = append(handle, proxy)

	route := map[string]any{
		"@id":      hostRouteID(h.ID),
		"handle":   handle,
		"match":    []map[string]any{{"host": hosts}},
		"terminal": true,
	}

	return route
}

// pluginHandlers renders the enabled host plugin handlers for a host, in a
// stable order. Invalid or empty handler JSON is skipped.
func pluginHandlers(h database.Host) []map[string]any {
	handlers := make([]map[string]any, 0, len(h.Plugins))
	for _, p := range h.Plugins {
		if !p.Enabled || len(p.Handler) == 0 {
			continue
		}
		var handler map[string]any
		if err := json.Unmarshal(p.Handler, &handler); err != nil || len(handler) == 0 {
			continue
		}
		handlers = append(handlers, handler)
	}
	return handlers
}

// splitDomains turns a space separated domain list into a slice.
func splitDomains(domains string) []string {
	fields := strings.Fields(domains)
	if len(fields) == 0 {
		return []string{}
	}
	return fields
}
