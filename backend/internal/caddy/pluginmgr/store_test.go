package pluginmgr

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type failingReader struct{ sent bool }

func (f *failingReader) Read(p []byte) (int, error) {
	if !f.sent {
		f.sent = true
		return copy(p, "partial"), nil
	}
	return 0, errors.New("boom")
}

func put(t *testing.T, s *Store, content string) string {
	t.Helper()
	p, sum, err := s.Put(strings.NewReader(content))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if len(sum) != 64 || filepath.Base(p) != "caddy-"+sum[:12] {
		t.Fatalf("unexpected path/sum: %s %s", p, sum)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v", fi.Mode())
	}
	return p
}

func TestStore_PutPromoteRollBack(t *testing.T) {
	s := NewStore(t.TempDir())
	a := put(t, s, "A")
	if err := s.Promote(a); err != nil {
		t.Fatal(err)
	}
	if cur, ok := s.Current(); !ok || cur != a {
		t.Fatalf("current = %q %v", cur, ok)
	}
	if _, ok := s.Previous(); ok {
		t.Fatal("unexpected previous")
	}
	b := put(t, s, "B")
	if err := s.Promote(b); err != nil {
		t.Fatal(err)
	}
	cur, _ := s.Current()
	prev, ok := s.Previous()
	if cur != b || !ok || prev != a {
		t.Fatalf("current=%q previous=%q", cur, prev)
	}
	if err := s.RollBack(); err != nil {
		t.Fatal(err)
	}
	cur, _ = s.Current()
	if cur != a {
		t.Fatalf("current after rollback = %q", cur)
	}
	if _, ok := s.Previous(); ok {
		t.Fatal("previous should be removed")
	}
	if err := s.RollBack(); err == nil {
		t.Fatal("rollback without previous should fail")
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Current(); ok {
		t.Fatal("current should be cleared")
	}
	if err := s.Clear(); err != nil {
		t.Fatalf("Clear on empty store: %v", err)
	}
}

func TestStore_PutAtomic(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, _, err := s.Put(&failingReader{}); err == nil {
		t.Fatal("expected error")
	}
	entries, _ := os.ReadDir(s.Dir)
	if len(entries) != 0 {
		t.Fatalf("leftover files: %v", entries)
	}
}

func TestStore_Prune(t *testing.T) {
	s := NewStore(t.TempDir())
	a := put(t, s, "A")
	b := put(t, s, "B")
	_ = s.Promote(a)
	_ = s.Promote(b)
	orphan := filepath.Join(s.Dir, "caddy-old")
	tmp := filepath.Join(s.Dir, ".tmp-x")
	for _, p := range []string{orphan, tmp} {
		if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Prune(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{orphan, tmp} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s should be gone", p)
		}
	}
	for _, p := range []string{a, b} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s should be kept: %v", p, err)
		}
	}
}

func TestStore_RelativeDir(t *testing.T) {
	wd, _ := os.Getwd()
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(wd) })

	s := NewStore("data")
	a := put(t, s, "A")
	if err := s.Promote(a); err != nil {
		t.Fatal(err)
	}
	cur, ok := s.Current()
	if !ok {
		t.Fatal("current dangles with a relative data folder")
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "current")); err != nil {
		t.Fatalf("current link does not resolve: %v", err)
	}
	if cur != a {
		t.Fatalf("current = %q, want %q", cur, a)
	}
}

func TestStore_PruneSpellingIndependent(t *testing.T) {
	abs := t.TempDir()
	wd, _ := os.Getwd()
	if err := os.Chdir(filepath.Dir(abs)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(wd) })

	absStore := &Store{Dir: abs}
	a := put(t, absStore, "A")
	b := put(t, absStore, "B")
	// Links written with absolute targets.
	if err := os.Symlink(a, filepath.Join(abs, "current")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(b, filepath.Join(abs, "previous")); err != nil {
		t.Fatal(err)
	}
	rel := &Store{Dir: filepath.Base(abs)}
	if err := rel.Prune(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{a, b} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s was pruned: %v", p, err)
		}
	}
}

func TestStore_ResetWithoutSeedClears(t *testing.T) {
	s := NewStore(t.TempDir())
	a, b := put(t, s, "A"), put(t, s, "B")
	if err := s.Promote(a); err != nil {
		t.Fatal(err)
	}
	if err := s.Promote(b); err != nil {
		t.Fatal(err)
	}
	if err := s.Reset(); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Current(); ok {
		t.Fatal("current must be gone")
	}
	if _, ok := s.Previous(); ok {
		t.Fatal("previous must be gone")
	}
}

func TestStore_ResetRepromotesSeed(t *testing.T) {
	s := NewStore(t.TempDir())
	seed, b, c := put(t, s, "seed"), put(t, s, "B"), put(t, s, "C")
	if err := s.SetSeed(seed); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{seed, b, c} {
		if err := s.Promote(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Prune(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(seed); err != nil {
		t.Fatalf("Prune removed the seed: %v", err)
	}
	if err := s.Reset(); err != nil {
		t.Fatal(err)
	}
	if cur, ok := s.Current(); !ok || cur != seed {
		t.Fatalf("current = %q %v, want seed", cur, ok)
	}
	if _, ok := s.Previous(); ok {
		t.Fatal("previous must be gone")
	}
}
