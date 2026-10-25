package pluginmgr

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// requestedPackages computes the package list of the build for an operation
// from the packages installed in the active binary. Existing packages stay
// pinned at their installed version so a rebuild never upgrades them; an
// added package is requested unpinned (latest). JSONSchemaPackage is always
// part of the request.
func requestedPackages(kind OpKind, pkg string, installed []InstalledPackage) ([]PinnedPackage, error) {
	var out []PinnedPackage
	found, hasSchema := false, false
	for _, p := range installed {
		if p.Path == pkg {
			found = true
			if kind == OpRemove {
				continue
			}
		}
		if p.Path == JSONSchemaPackage {
			hasSchema = true
		}
		out = append(out, PinnedPackage{Path: p.Path, Version: p.Version})
	}
	switch kind {
	case OpAdd:
		if found {
			return nil, fmt.Errorf("%s is already installed", pkg)
		}
		out = append(out, PinnedPackage{Path: pkg})
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
// Caddy: it reports the expected Caddy version, contains every requested
// package (pinned ones at the requested version) and loads the live config.
func verifyBinary(ctx context.Context, insp Inspector, bin, wantVersion string, want []PinnedPackage, liveConfig []byte) error {
	v, err := insp.Version(ctx, bin)
	if err != nil {
		return fmt.Errorf("new binary does not run: %w", err)
	}
	if v != wantVersion {
		return fmt.Errorf("new binary reports Caddy %s, expected %s", v, wantVersion)
	}

	got, err := insp.Installed(ctx, bin)
	if err != nil {
		return fmt.Errorf("listing packages of the new binary: %w", err)
	}
	have := make(map[string]string, len(got))
	for _, p := range got {
		have[p.Path] = p.Version
	}
	for _, p := range want {
		hv, ok := have[p.Path]
		if !ok {
			return fmt.Errorf("new binary lacks package %s", p.Path)
		}
		if p.Version != "" && hv != p.Version {
			return fmt.Errorf("new binary has %s %s, expected %s", p.Path, hv, p.Version)
		}
	}

	if err := insp.Validate(ctx, bin, liveConfig); err != nil {
		return fmt.Errorf("current config does not load on the new binary: %w", err)
	}
	return nil
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
