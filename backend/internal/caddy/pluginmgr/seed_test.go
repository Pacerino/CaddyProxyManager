package pluginmgr

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// seededEnv is a systemd-style environment: the shipped binary is only known
// by name and found via PATH, and the store has been seeded with a copy.
func seededEnv(t *testing.T) *testEnv {
	t.Helper()
	e := newEnv(t)
	t.Setenv("PATH", e.dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	e.m.ShippedBinary = filepath.Base(e.shipped)
	if err := e.m.EnsureSeeded(); err != nil {
		t.Fatalf("EnsureSeeded: %v", err)
	}
	return e
}

func sameContent(t *testing.T, a, b string) bool {
	t.Helper()
	da, err := os.ReadFile(a)
	if err != nil {
		t.Fatal(err)
	}
	db, err := os.ReadFile(b)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Equal(da, db)
}

func TestEnsureSeeded_SystemdSetupFlow(t *testing.T) {
	e := seededEnv(t)
	cur, ok := e.m.Store.Current()
	if !ok {
		t.Fatal("current must exist after seeding")
	}
	if cur == e.shipped || !sameContent(t, cur, e.shipped) {
		t.Fatalf("current %s must be a copy of the shipped binary", cur)
	}
	if seed, ok := e.m.Store.Seed(); !ok || seed != cur {
		t.Fatalf("seed = %q %v, want %q", seed, ok, cur)
	}

	managed := filepath.Join(e.m.Store.Dir, "current")
	cfg := SetupConfig{ReloadStrategy: "systemd", Service: "caddy", ManagedPath: managed, AdminURL: "http://localhost:2019"}
	u := &fakeUnit{path: "/usr/bin/caddy", args: []string{"run", "--environ", "--config", "/etc/caddy/Caddyfile"}}
	st := DetectSetup(context.Background(), cfg, u)
	if st.State != SetupNeeded || st.DropIn == "" {
		t.Fatalf("before the drop-in: %+v", st)
	}

	// The user installed the drop-in: the unit now runs the managed path,
	// which exists.
	u.path = managed
	if st := DetectSetup(context.Background(), cfg, u); st.State != SetupReady {
		t.Fatalf("after the drop-in: %+v", st)
	}
}

func TestEnsureSeeded_Idempotent(t *testing.T) {
	e := seededEnv(t)
	seed, _ := e.m.Store.Seed()
	if err := e.m.EnsureSeeded(); err != nil {
		t.Fatal(err)
	}
	if got := e.binaries(t); len(got) != 1 {
		t.Fatalf("binaries after second seed: %v", got)
	}

	// A deleted current link is restored from the seed without a new copy.
	if err := os.Remove(filepath.Join(e.m.Store.Dir, "current")); err != nil {
		t.Fatal(err)
	}
	if err := e.m.EnsureSeeded(); err != nil {
		t.Fatal(err)
	}
	if cur, ok := e.m.Store.Current(); !ok || cur != seed {
		t.Fatalf("current = %q %v, want seed %q", cur, ok, seed)
	}
}

func TestEnsureSeeded_KeepsExistingCurrent(t *testing.T) {
	e := newEnv(t)
	cur := promoteFake(t, e, shippedBin())
	if err := e.m.EnsureSeeded(); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.m.Store.Current(); got != cur {
		t.Fatalf("current changed to %s", got)
	}
	if _, ok := e.m.Store.Seed(); ok {
		t.Fatal("no seed expected when a managed binary already runs")
	}
}

func TestEnsureSeeded_MissingBinary(t *testing.T) {
	e := newEnv(t)
	e.m.ShippedBinary = filepath.Join(e.dir, "does-not-exist")
	if err := e.m.EnsureSeeded(); err == nil {
		t.Fatal("expected an error")
	}
	if _, ok := e.m.Store.Current(); ok {
		t.Fatal("no current expected")
	}
}

func TestSwitch_SystemdFirstBuildRollsBackToSeed(t *testing.T) {
	e := seededEnv(t)
	seed, _ := e.m.Store.Seed()
	e.rec.results = map[int][]error{1: {errors.New("sync tls: nope")}}
	op := e.run(t, OpAdd, ratelimit)
	if op.Step != StepRolledBack {
		t.Fatalf("op: %+v", op)
	}
	if cur, ok := e.m.Store.Current(); !ok || cur != seed {
		t.Fatalf("current = %q %v, want the seed %q", cur, ok, seed)
	}
}

func TestSwitch_SystemdLastResortRepromotesSeed(t *testing.T) {
	e := seededEnv(t)
	seed, _ := e.m.Store.Seed()
	if op := e.run(t, OpAdd, ratelimit); op.Step != StepDone {
		t.Fatalf("add: %+v", op)
	}
	if op := e.run(t, OpRemove, duckdns); op.Step != StepDone {
		t.Fatalf("remove: %+v", op)
	}
	// Seed, previous and current are all kept by Prune.
	if _, err := os.Stat(seed); err != nil {
		t.Fatalf("seed pruned: %v", err)
	}

	e.restart.errs = map[int]error{
		3: errors.New("new binary crashed"),
		4: errors.New("previous binary crashed"),
	}
	op := e.run(t, OpAdd, "github.com/x/other")
	if op.Step != StepFailed {
		t.Fatalf("op: %+v", op)
	}
	if cur, ok := e.m.Store.Current(); !ok || cur != seed {
		t.Fatalf("current = %q %v, want the seed %q", cur, ok, seed)
	}
}
