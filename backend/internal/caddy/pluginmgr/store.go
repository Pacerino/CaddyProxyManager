package pluginmgr

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	currentLink  = "current"
	previousLink = "previous"
	// seedLink marks the copy of the configured Caddy binary that a systemd
	// unit falls back to; it is never pruned.
	seedLink  = "seed"
	tmpPrefix = ".tmp-"
	binPrefix = "caddy-"
)

// Store keeps managed Caddy binaries in a directory: content-addressed files
// named caddy-<sha12> plus "current" and "previous" symlinks and, for
// systemd setups, a "seed" symlink to the copy of the configured binary.
type Store struct {
	Dir string
}

// NewStore returns the store living in <dataFolder>/caddy-bin.
func NewStore(dataFolder string) *Store {
	return &Store{Dir: filepath.Join(dataFolder, "caddy-bin")}
}

// Put writes the binary read from r into the store and returns its path and
// SHA-256 (lowercase hex). The data goes to a temp file first, so a failed
// read never leaves a partial caddy-* file behind.
func (s *Store) Put(r io.Reader) (path, sha256hex string, err error) {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return "", "", err
	}
	var rnd [8]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return "", "", err
	}
	tmp := filepath.Join(s.Dir, tmpPrefix+hex.EncodeToString(rnd[:]))
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", "", err
	}
	cleanup := func() { f.Close(); os.Remove(tmp) }

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), r); err != nil {
		cleanup()
		return "", "", err
	}
	if err := f.Sync(); err != nil {
		cleanup()
		return "", "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return "", "", err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		os.Remove(tmp)
		return "", "", err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	dst := filepath.Join(s.Dir, binPrefix+sum[:12])
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return "", "", err
	}
	return dst, sum, nil
}

// resolve returns the binary the named symlink points at, as a path under Dir,
// if it exists. Only the target's base name is used, so links written with
// absolute targets and relative Dirs are handled alike.
func (s *Store) resolve(name string) (string, bool) {
	link := filepath.Join(s.Dir, name)
	target, err := os.Readlink(link)
	if err != nil {
		return "", false
	}
	target = filepath.Join(s.Dir, filepath.Base(target))
	if _, err := os.Stat(target); err != nil {
		return "", false
	}
	return target, true
}

// Current returns the binary the "current" symlink points at.
func (s *Store) Current() (string, bool) { return s.resolve(currentLink) }

// Previous returns the binary the "previous" symlink points at.
func (s *Store) Previous() (string, bool) { return s.resolve(previousLink) }

// Seed returns the binary the "seed" symlink points at.
func (s *Store) Seed() (string, bool) { return s.resolve(seedLink) }

// SetSeed marks path as the seed binary.
func (s *Store) SetSeed(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return s.setLink(seedLink, path)
}

// setLink atomically points the named symlink at the binary target: it creates
// a temp symlink in the same directory and renames it over the destination.
// The link target is the base name, so it resolves relative to the link's own
// directory whatever Dir looks like.
func (s *Store) setLink(name, target string) error {
	target = filepath.Base(target)
	var rnd [8]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return err
	}
	tmp := filepath.Join(s.Dir, tmpPrefix+"link-"+hex.EncodeToString(rnd[:]))
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(s.Dir, name)); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Promote makes path the current binary; the old current becomes previous.
func (s *Store) Promote(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	if old, ok := s.Current(); ok && old != path {
		if err := s.setLink(previousLink, old); err != nil {
			return err
		}
	}
	return s.setLink(currentLink, path)
}

// RollBack makes the previous binary current again and drops "previous".
func (s *Store) RollBack() error {
	prev, ok := s.Previous()
	if !ok {
		return errors.New("no previous binary to roll back to")
	}
	if err := s.setLink(currentLink, prev); err != nil {
		return err
	}
	return removeIfExists(filepath.Join(s.Dir, previousLink))
}

// Clear removes both symlinks so the shipped binary becomes active again.
func (s *Store) Clear() error {
	for _, name := range []string{currentLink, previousLink} {
		if err := removeIfExists(filepath.Join(s.Dir, name)); err != nil {
			return err
		}
	}
	return nil
}

// Reset returns to the binary Caddy ran before CPM managed it: the seed
// becomes current again when there is one (a systemd unit always runs
// "current", so it must not vanish); otherwise both links are removed and
// the shipped binary is active. "previous" is dropped either way.
func (s *Store) Reset() error {
	seed, ok := s.Seed()
	if !ok {
		return s.Clear()
	}
	if err := s.setLink(currentLink, seed); err != nil {
		return err
	}
	return removeIfExists(filepath.Join(s.Dir, previousLink))
}

// Prune deletes temp files and every caddy-* binary that is neither current,
// previous nor the seed.
func (s *Store) Prune() error {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	keep := map[string]bool{}
	if p, ok := s.Current(); ok {
		keep[filepath.Base(p)] = true
	}
	if p, ok := s.Previous(); ok {
		keep[filepath.Base(p)] = true
	}
	if p, ok := s.Seed(); ok {
		keep[filepath.Base(p)] = true
	}
	for _, e := range entries {
		name := e.Name()
		full := filepath.Join(s.Dir, name)
		switch {
		case strings.HasPrefix(name, tmpPrefix):
		case strings.HasPrefix(name, binPrefix) && !keep[name]:
		default:
			continue
		}
		if err := os.Remove(full); err != nil {
			return fmt.Errorf("pruning %s: %w", name, err)
		}
	}
	return nil
}
