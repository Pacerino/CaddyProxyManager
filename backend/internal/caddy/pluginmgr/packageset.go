package pluginmgr

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
)

// underModule reports whether the package path lies in the Go module
// modPath: it is the module root or a subdirectory of it.
func underModule(path, modPath string) bool {
	return path == modPath || strings.HasPrefix(path, modPath+"/")
}

// owningModule returns the installed module that contains the package path
// (the longest matching module path).
func owningModule(path string, installed []InstalledPackage) (InstalledPackage, bool) {
	var best InstalledPackage
	found := false
	for _, m := range installed {
		if underModule(path, m.Path) && (!found || len(m.Path) > len(best.Path)) {
			best, found = m, true
		}
	}
	return best, found
}

// currentPackages maps the installed modules to the package paths to request
// for them. Installed lists Go module paths, while a registered package may
// live in a module subdirectory (e.g. github.com/x/plug/http), so every
// previously requested path inside an installed module is requested again,
// pinned at that module's version. A module without a requested path falls
// back to the module path, as for the plugins of the shipped binary.
func currentPackages(installed []InstalledPackage, requested []string) []PinnedPackage {
	byModule := map[string][]string{}
	seen := map[string]bool{}
	for _, r := range requested {
		if seen[r] {
			continue
		}
		seen[r] = true
		if m, ok := owningModule(r, installed); ok {
			byModule[m.Path] = append(byModule[m.Path], r)
		}
	}
	var out []PinnedPackage
	for _, m := range installed {
		paths := byModule[m.Path]
		if len(paths) == 0 {
			paths = []string{m.Path}
		}
		for _, p := range paths {
			out = append(out, PinnedPackage{Path: p, Version: m.Version})
		}
	}
	return out
}

// requestedPackages computes the package list of the build for an operation
// from the modules installed in the active binary and the package paths
// requested by the last successful operation. Existing packages stay pinned
// at their installed version so a rebuild never upgrades them; an added
// package is requested unpinned (latest), or pinned at its module's version
// when that module is already installed. Removing a path drops every
// requested path under it. JSONSchemaPackage is always part of the request.
func requestedPackages(kind OpKind, pkg string, installed []InstalledPackage, requested []string) ([]PinnedPackage, error) {
	current := currentPackages(installed, requested)
	var out []PinnedPackage
	found, hasSchema := false, false
	for _, p := range current {
		if p.Path == pkg {
			found = true
		}
		if kind == OpRemove && underModule(p.Path, pkg) {
			found = true
			continue
		}
		if p.Path == JSONSchemaPackage {
			hasSchema = true
		}
		out = append(out, p)
	}
	switch kind {
	case OpAdd:
		if found {
			return nil, fmt.Errorf("%s is already installed", pkg)
		}
		added := PinnedPackage{Path: pkg}
		if m, ok := owningModule(pkg, installed); ok {
			// One Go module can only be built at one version.
			added.Version = m.Version
		}
		out = append(out, added)
	case OpRemove:
		if !found {
			return nil, fmt.Errorf("%s is not installed", pkg)
		}
	}
	if !hasSchema {
		out = append(out, PinnedPackage{Path: JSONSchemaPackage})
	}
	return out, nil
}

// verifyBinary checks a downloaded binary before it may run as the live
// Caddy: it reports a Caddy version no older than the running one (the build
// service always builds its latest Caddy), contains the module of every
// requested package (pinned ones at the requested version) and loads the
// live config.
func verifyBinary(ctx context.Context, insp Inspector, bin, runningVersion string, want []PinnedPackage, liveConfig []byte) (string, error) {
	v, err := insp.Version(ctx, bin)
	if err != nil {
		return "", fmt.Errorf("new binary does not run: %w", err)
	}
	cmp, err := compareSemver(v, runningVersion)
	if err != nil {
		return v, fmt.Errorf("comparing Caddy versions: %w", err)
	}
	if cmp < 0 {
		return v, fmt.Errorf("new binary reports Caddy %s, older than the running %s", v, runningVersion)
	}

	got, err := insp.Installed(ctx, bin)
	if err != nil {
		return v, fmt.Errorf("listing packages of the new binary: %w", err)
	}
	for _, p := range want {
		m, ok := owningModule(p.Path, got)
		if !ok {
			return v, fmt.Errorf("new binary lacks package %s", p.Path)
		}
		if p.Version != "" && m.Version != p.Version {
			return v, fmt.Errorf("new binary has %s %s, expected %s", m.Path, m.Version, p.Version)
		}
	}

	if err := insp.Validate(ctx, bin, normalizeConfig(liveConfig)); err != nil {
		return v, fmt.Errorf("current config does not load on the new binary: %w", err)
	}
	return v, nil
}

// normalizeConfig turns the empty config of a fresh Caddy (no body or a
// literal null) into an empty JSON object that validate accepts.
func normalizeConfig(cfg []byte) []byte {
	if t := bytes.TrimSpace(cfg); len(t) == 0 || string(t) == "null" {
		return []byte("{}")
	}
	return cfg
}

// compareSemver compares two semantic versions with an optional "v" prefix
// and returns -1, 0 or 1. Build metadata is ignored.
func compareSemver(a, b string) (int, error) {
	ac, ap, err := parseSemver(a)
	if err != nil {
		return 0, err
	}
	bc, bp, err := parseSemver(b)
	if err != nil {
		return 0, err
	}
	for i := range ac {
		if c := cmpInt(ac[i], bc[i]); c != 0 {
			return c, nil
		}
	}
	// A release ranks above any of its pre-releases.
	switch {
	case len(ap) == 0 && len(bp) == 0:
		return 0, nil
	case len(ap) == 0:
		return 1, nil
	case len(bp) == 0:
		return -1, nil
	}
	for i := 0; i < len(ap) && i < len(bp); i++ {
		if c := cmpPrerelease(ap[i], bp[i]); c != 0 {
			return c, nil
		}
	}
	return cmpInt(len(ap), len(bp)), nil
}

func parseSemver(s string) (core [3]int, pre []string, err error) {
	v := strings.TrimPrefix(s, "v")
	v, _, _ = strings.Cut(v, "+")
	v, p, hasPre := strings.Cut(v, "-")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return core, nil, fmt.Errorf("not a semantic version: %q", s)
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return core, nil, fmt.Errorf("not a semantic version: %q", s)
		}
		core[i] = n
	}
	if hasPre {
		pre = strings.Split(p, ".")
	}
	return core, pre, nil
}

// cmpPrerelease compares pre-release identifiers: numeric ones numerically
// and below alphanumeric ones, alphanumeric ones lexically.
func cmpPrerelease(a, b string) int {
	an, aErr := strconv.Atoi(a)
	bn, bErr := strconv.Atoi(b)
	switch {
	case aErr == nil && bErr == nil:
		return cmpInt(an, bn)
	case aErr == nil:
		return -1
	case bErr == nil:
		return 1
	}
	return strings.Compare(a, b)
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
