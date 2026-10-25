package caddy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/Pacerino/CaddyProxyManager/internal/database"
)

const (
	tlsPolicyIDPrefix = "cpm_tls_host_"
	tlsPoliciesPath   = "/config/apps/tls/automation/policies"
)

// HostPolicyID returns the @id of the TLS automation policy for a host.
func HostPolicyID(hostID uint) string {
	return fmt.Sprintf("%s%d", tlsPolicyIDPrefix, hostID)
}

// BuildTLSPolicies renders one ACME DNS-challenge policy per host that
// references a usable DNS provider. Hosts without a provider are skipped
// silently; hosts whose provider is missing or whose module is not in
// available are skipped with a warning. Policies without wildcard subjects come
// first so Caddy's first-match selection picks the most specific policy, then
// wildcard ones; within each group policies are ordered by host ID.
func BuildTLSPolicies(hosts []database.Host, providers map[uint]database.DNSProvider, available map[string]bool) ([]map[string]any, []string) {
	type entry struct {
		id       uint
		wildcard bool
		policy   map[string]any
	}
	var entries []entry
	var warnings []string

	for _, h := range hosts {
		if h.DNSProviderID == nil {
			continue
		}
		prov, ok := providers[*h.DNSProviderID]
		if !ok {
			warnings = append(warnings, fmt.Sprintf("host %d: DNS provider %d not found, skipping TLS policy", h.ID, *h.DNSProviderID))
			continue
		}
		if !available[prov.ModuleID] {
			warnings = append(warnings, fmt.Sprintf("host %d: DNS provider module %s is not available in this Caddy build, skipping TLS policy", h.ID, prov.ModuleID))
			continue
		}
		subjects := splitDomains(h.Domains)
		if len(subjects) == 0 {
			warnings = append(warnings, fmt.Sprintf("host %d: no domains, skipping TLS policy", h.ID))
			continue
		}
		provider, err := providerObject(prov)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("host %d: invalid config for DNS provider %q: %v", h.ID, prov.Name, err))
			continue
		}

		wildcard := false
		for _, s := range subjects {
			if strings.Contains(s, "*") {
				wildcard = true
				break
			}
		}
		entries = append(entries, entry{
			id:       h.ID,
			wildcard: wildcard,
			policy: map[string]any{
				"@id":      HostPolicyID(h.ID),
				"subjects": subjects,
				"issuers": []any{map[string]any{
					"module": "acme",
					"challenges": map[string]any{
						"dns": map[string]any{"provider": provider},
					},
				}},
			},
		})
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].wildcard != entries[j].wildcard {
			return !entries[i].wildcard
		}
		return entries[i].id < entries[j].id
	})

	policies := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		policies = append(policies, e.policy)
	}
	return policies, warnings
}

// providerObject builds the Caddy DNS provider object: the module's short name
// merged with the stored config, leaving out empty strings and nulls.
func providerObject(prov database.DNSProvider) (map[string]any, error) {
	obj := map[string]any{}
	if len(prov.Config) > 0 {
		var cfg map[string]any
		if err := json.Unmarshal(prov.Config, &cfg); err != nil {
			return nil, err
		}
		for k, v := range cfg {
			if v == nil || v == "" {
				continue
			}
			obj[k] = v
		}
	}
	obj["name"] = database.ShortName(prov.ModuleID)
	return obj, nil
}

// SyncTLSPolicies writes policies into Caddy ahead of every policy CPM does not
// own. CPM-owned policies (per-host @id and the legacy Cloudflare policy of
// earlier versions) are replaced; all others are preserved in order.
func (p *APIProvider) SyncTLSPolicies(policies []map[string]any) error {
	// Probe top-down: Caddy answers 200 "null" only when the last path segment
	// is missing; a missing intermediate segment is a 400.
	tlsExists, err := p.configExists("/config/apps/tls")
	if err != nil {
		return err
	}
	autoExists := false
	if tlsExists {
		if autoExists, err = p.configExists("/config/apps/tls/automation"); err != nil {
			return err
		}
	}
	var existing []map[string]any
	if autoExists {
		if err := p.GetConfig(tlsPoliciesPath, &existing); err != nil {
			return fmt.Errorf("read tls policies: %w", err)
		}
	}

	merged := make([]map[string]any, 0, len(policies)+len(existing))
	merged = append(merged, policies...)
	removed := 0
	for _, e := range existing {
		if isCPMPolicy(e) {
			removed++
			continue
		}
		merged = append(merged, e)
	}
	if len(merged) == 0 && removed == 0 {
		return nil
	}

	// A present policies array (even empty) means PATCH can replace it.
	if existing != nil {
		return p.do(http.MethodPatch, tlsPoliciesPath, merged, nil)
	}
	if !tlsExists {
		return p.do(http.MethodPut, "/config/apps/tls", map[string]any{
			"automation": map[string]any{"policies": merged},
		}, nil)
	}
	if !autoExists {
		return p.do(http.MethodPut, "/config/apps/tls/automation", map[string]any{"policies": merged}, nil)
	}
	// Automation exists but has no policies key yet: PATCH would fail on the
	// absent key, so create it.
	return p.do(http.MethodPut, tlsPoliciesPath, merged, nil)
}

// isCPMPolicy reports whether a policy is owned by CPM: a per-host policy or a
// legacy subject-less Cloudflare DNS-challenge policy.
func isCPMPolicy(policy map[string]any) bool {
	if id, _ := policy["@id"].(string); strings.HasPrefix(id, tlsPolicyIDPrefix) {
		return true
	}
	if _, hasSubjects := policy["subjects"]; hasSubjects {
		return false
	}
	issuers, _ := policy["issuers"].([]any)
	for _, i := range issuers {
		issuer, _ := i.(map[string]any)
		challenges, _ := issuer["challenges"].(map[string]any)
		dns, _ := challenges["dns"].(map[string]any)
		provider, _ := dns["provider"].(map[string]any)
		if name, _ := provider["name"].(string); name == "cloudflare" {
			return true
		}
	}
	return false
}

// Ping checks that Caddy's admin API is reachable.
func (p *APIProvider) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/config/", nil)
	if err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("caddy admin GET /config/ returned %d", resp.StatusCode)
	}
	return nil
}
