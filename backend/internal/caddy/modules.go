package caddy

import (
	"bytes"
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

// ListModules returns the module list reported by the configured Caddy binary,
// sorted by ID. It is the canonical way to discover which plugins a
// user-supplied Caddy build provides.
//
// It prefers `caddy list-modules --json`, but older Caddy versions don't
// support the --json flag; in that case it falls back to parsing the plain
// text output.
func ListModules() ([]Module, error) {
	out, jsonErr := runListModules("--json")
	if jsonErr == nil {
		return parseModules(out)
	}
	// Older Caddy without --json support: fall back to text parsing.
	return listModulesText()
}

// runListModules runs `caddy list-modules <args...>` and returns stdout,
// surfacing stderr in the error message.
func runListModules(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, config.Configuration.Caddy.Binary, append([]string{"list-modules"}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("running %q list-modules: %s", config.Configuration.Caddy.Binary, msg)
	}
	return out, nil
}

// listModulesText builds the module list from Caddy's plain-text output, used
// when the binary doesn't support --json. It runs once for the full list
// (with package paths) and once skipping standard modules to classify which
// modules are non-standard plugins.
func listModulesText() ([]Module, error) {
	full, err := runListModules("--packages")
	if err != nil {
		return nil, err
	}
	// --skip-standard yields only the non-standard (plugin) module IDs.
	skipStd, err := runListModules("--skip-standard", "--packages")
	if err != nil {
		// Without classification, treat everything as standard rather than fail.
		skipStd = nil
	}

	nonStandard := map[string]bool{}
	for _, line := range parseTextModuleLines(skipStd) {
		nonStandard[line.id] = true
	}

	lines := parseTextModuleLines(full)
	modules := make([]Module, 0, len(lines))
	for _, l := range lines {
		namespace, name := splitModuleID(l.id)
		modules = append(modules, Module{
			ID:        l.id,
			Namespace: namespace,
			Name:      name,
			Standard:  !nonStandard[l.id],
			Package:   l.pkg,
		})
	}

	sort.Slice(modules, func(i, j int) bool { return modules[i].ID < modules[j].ID })
	return modules, nil
}

type textModuleLine struct {
	id  string
	pkg string
}

// parseTextModuleLines parses lines of `caddy list-modules --packages` output.
// Each module line looks like "http.handlers.reverse_proxy github.com/...".
// Summary/blank lines (e.g. "Standard modules: 132") are skipped.
func parseTextModuleLines(data []byte) []textModuleLine {
	var out []textModuleLine
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		id := fields[0]
		// A module ID is a dotted token; summary lines like "Standard
		// modules: 132" start with a word and a colon, so skip non-module ids.
		if !strings.Contains(id, ".") || strings.HasSuffix(id, ":") {
			continue
		}
		pkg := ""
		if len(fields) > 1 {
			pkg = fields[len(fields)-1]
		}
		out = append(out, textModuleLine{id: id, pkg: pkg})
	}
	return out
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
