package caddy

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/Pacerino/CaddyProxyManager/internal/config"
)

// Module describes a single Caddy module (plugin) reported by
// `caddy list-modules --json`.
type Module struct {
	// ID is the full dotted module name, e.g. "http.handlers.reverse_proxy"
	// or "dns.providers.cloudflare".
	ID string `json:"id"`
	// Namespace is the module's parent namespace, derived from ID. For
	// "dns.providers.cloudflare" it is "dns.providers".
	Namespace string `json:"namespace"`
	// Name is the leaf segment of the ID, e.g. "cloudflare".
	Name string `json:"name"`
	// Standard reports whether the module ships with stock Caddy. Anything
	// that is not "standard" is treated as a user-supplied plugin.
	Standard bool `json:"standard"`
	// Version and Package are informational, taken from the build info.
	Version string `json:"version,omitempty"`
	Package string `json:"package,omitempty"`
}

// rawModule mirrors the JSON object emitted by `caddy list-modules --json`.
type rawModule struct {
	ModuleName string `json:"module_name"`
	ModuleType string `json:"module_type"`
	Version    string `json:"version"`
	PackageURL string `json:"package_url"`
}

// ListModules runs `caddy list-modules --json` against the configured binary
// and returns the parsed module list sorted by ID. It is the canonical way to
// discover which plugins a user-supplied Caddy build provides.
func ListModules() ([]Module, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, config.Configuration.Caddy.Binary, "list-modules", "--json")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("running %q list-modules: %w", config.Configuration.Caddy.Binary, err)
	}

	return parseModules(out)
}

// parseModules decodes the JSON output of `caddy list-modules --json`.
func parseModules(data []byte) ([]Module, error) {
	var raw []rawModule
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing list-modules output: %w", err)
	}

	modules := make([]Module, 0, len(raw))
	for _, rm := range raw {
		id := strings.TrimSpace(rm.ModuleName)
		if id == "" {
			continue
		}
		namespace, name := splitModuleID(id)
		modules = append(modules, Module{
			ID:        id,
			Namespace: namespace,
			Name:      name,
			Standard:  rm.ModuleType == "standard",
			Version:   rm.Version,
			Package:   rm.PackageURL,
		})
	}

	sort.Slice(modules, func(i, j int) bool { return modules[i].ID < modules[j].ID })
	return modules, nil
}

// splitModuleID splits a dotted module ID into its namespace and leaf name.
// For "dns.providers.cloudflare" it returns ("dns.providers", "cloudflare").
// For a top-level id like "http" it returns ("", "http").
func splitModuleID(id string) (namespace, name string) {
	idx := strings.LastIndex(id, ".")
	if idx < 0 {
		return "", id
	}
	return id[:idx], id[idx+1:]
}
