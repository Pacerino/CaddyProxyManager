package pluginmgr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// inspectTimeout bounds every call into a Caddy binary. It is a variable so
// tests can shorten it.
var inspectTimeout = 30 * time.Second

// InstalledPackage is a non-standard Caddy package compiled into a binary.
type InstalledPackage struct {
	Path    string
	Version string
	Modules []string
}

// Inspector looks into a Caddy binary without running it as a server.
type Inspector interface {
	Version(ctx context.Context, bin string) (string, error)
	Installed(ctx context.Context, bin string) ([]InstalledPackage, error)
	Validate(ctx context.Context, bin string, configJSON []byte) error
}

type listedModule struct {
	Name    string `json:"module_name"`
	Type    string `json:"module_type"`
	Version string `json:"version"`
	Package string `json:"package_url"`
}

// ParseInstalled groups the non-standard modules of `caddy list-modules --json`
// by package, sorted by path. JSONSchemaPackage is included; callers hide it.
func ParseInstalled(listModulesJSON []byte) []InstalledPackage {
	var mods []listedModule
	if err := json.Unmarshal(listModulesJSON, &mods); err != nil {
		return []InstalledPackage{}
	}
	byPath := map[string]*InstalledPackage{}
	for _, m := range mods {
		if m.Type != "non-standard" || m.Package == "" {
			continue
		}
		p, ok := byPath[m.Package]
		if !ok {
			p = &InstalledPackage{Path: m.Package, Version: m.Version}
			byPath[m.Package] = p
		}
		p.Modules = append(p.Modules, m.Name)
	}
	out := make([]InstalledPackage, 0, len(byPath))
	for _, p := range byPath {
		sort.Strings(p.Modules)
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// ExecInspector implements Inspector by executing the binary.
type ExecInspector struct{}

func run(ctx context.Context, bin string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, inspectTimeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Don't wait forever on pipes held open by grandchildren after a kill.
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return nil, fmt.Errorf("%s %s: %w: %s", filepath.Base(bin), strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// Version returns the semantic version of the binary, e.g. "v2.11.4".
func (ExecInspector) Version(ctx context.Context, bin string) (string, error) {
	out, err := run(ctx, bin, "version")
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", fmt.Errorf("%s version: empty output", filepath.Base(bin))
	}
	return fields[0], nil
}

// Installed lists the non-standard packages compiled into the binary.
func (ExecInspector) Installed(ctx context.Context, bin string) ([]InstalledPackage, error) {
	out, err := run(ctx, bin, "list-modules", "--json")
	if err != nil {
		return nil, err
	}
	var probe []listedModule
	if err := json.Unmarshal(out, &probe); err != nil {
		return nil, fmt.Errorf("parse list-modules output: %w", err)
	}
	pkgs := ParseInstalled(out)

	// Command-only plugins (e.g. caddy-json-schema) register no module, so
	// they only show up in build-info.
	bi, err := run(ctx, bin, "build-info")
	if err != nil {
		return nil, err
	}
	if v, ok := ParseBuildInfoDeps(bi)[JSONSchemaPackage]; ok {
		found := false
		for _, p := range pkgs {
			if p.Path == JSONSchemaPackage {
				found = true
				break
			}
		}
		if !found {
			pkgs = append(pkgs, InstalledPackage{Path: JSONSchemaPackage, Version: v})
			sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Path < pkgs[j].Path })
		}
	}
	return pkgs, nil
}

// ParseBuildInfoDeps returns path -> version for the `dep` lines of
// `caddy build-info` (tab separated). Replacement lines carry extra fields;
// the version field is used as-is.
func ParseBuildInfoDeps(out []byte) map[string]string {
	deps := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(f) >= 3 && f[0] == "dep" {
			deps[f[1]] = f[2]
		}
	}
	return deps
}

// Validate loads and provisions configJSON with the binary without starting it.
func (ExecInspector) Validate(ctx context.Context, bin string, configJSON []byte) error {
	dir, err := os.MkdirTemp("", "cpm-validate-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, configJSON, 0o600); err != nil {
		return err
	}
	_, err = run(ctx, bin, "validate", "--config", path, "--adapter", "")
	return err
}
