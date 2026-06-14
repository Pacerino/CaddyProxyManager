package caddy

import (
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

	handler := map[string]any{
		"handler":   "reverse_proxy",
		"upstreams": upstreams,
	}

	route := map[string]any{
		"@id":      hostRouteID(h.ID),
		"handle":   []map[string]any{handler},
		"match":    []map[string]any{{"host": hosts}},
		"terminal": true,
	}

	return route
}

// splitDomains turns a space separated domain list into a slice.
func splitDomains(domains string) []string {
	fields := strings.Fields(domains)
	if len(fields) == 0 {
		return []string{}
	}
	return fields
}
