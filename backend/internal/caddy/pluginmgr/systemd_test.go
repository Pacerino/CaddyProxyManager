package pluginmgr

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeUnit struct {
	path      string
	args      []string
	user      string
	err       error
	restarted string
}

func (f *fakeUnit) ExecStart(ctx context.Context, unit string) (string, []string, string, error) {
	return f.path, f.args, f.user, f.err
}

func (f *fakeUnit) Restart(ctx context.Context, unit string) error {
	f.restarted = unit
	return f.err
}

func managedBinary(t *testing.T, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "current")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDetectSetup_Supervised(t *testing.T) {
	st := DetectSetup(context.Background(), SetupConfig{Supervise: true, ReloadStrategy: "systemd"}, &fakeUnit{})
	if st.State != SetupSupervised {
		t.Fatalf("state = %q", st.State)
	}
}

func TestDetectSetup_UnsupportedStrategy(t *testing.T) {
	st := DetectSetup(context.Background(), SetupConfig{ReloadStrategy: "exec"}, &fakeUnit{})
	if st.State != SetupUnsupported || st.Reason == "" {
		t.Fatalf("status = %+v", st)
	}
}

func TestDetectSetup_DBusError(t *testing.T) {
	st := DetectSetup(context.Background(), SetupConfig{ReloadStrategy: "systemd", Service: "caddy"}, &fakeUnit{err: errors.New("no bus")})
	if st.State != SetupUnsupported || !strings.Contains(st.Reason, "no bus") {
		t.Fatalf("status = %+v", st)
	}
}

func TestDetectSetup_Ready(t *testing.T) {
	managed := managedBinary(t, 0o755)
	u := &fakeUnit{path: managed, args: []string{"run"}, user: ""}
	st := DetectSetup(context.Background(), SetupConfig{ReloadStrategy: "systemd", Service: "caddy", ManagedPath: managed}, u)
	if st.State != SetupReady {
		t.Fatalf("status = %+v", st)
	}
}

func TestDetectSetup_SetupNeededDropIn(t *testing.T) {
	managed := managedBinary(t, 0o755)
	u := &fakeUnit{path: "/usr/bin/caddy", args: []string{"run", "--environ", "--config", "/etc/caddy/Caddyfile"}}
	st := DetectSetup(context.Background(), SetupConfig{ReloadStrategy: "systemd", Service: "caddy", ManagedPath: managed}, u)
	if st.State != SetupNeeded || st.Reason == "" {
		t.Fatalf("status = %+v", st)
	}
	want := "[Service]\nExecStart=\nExecStart=" + managed + " run --environ --config /etc/caddy/Caddyfile\n"
	if st.DropIn != want {
		t.Fatalf("dropIn = %q, want %q", st.DropIn, want)
	}
}

func TestDetectSetup_NotReadableByUnitUser(t *testing.T) {
	managed := managedBinary(t, 0o700)
	u := &fakeUnit{path: managed, args: []string{"run"}, user: "nobody"}
	st := DetectSetup(context.Background(), SetupConfig{ReloadStrategy: "systemd", Service: "caddy", ManagedPath: managed}, u)
	if st.State != SetupNeeded || !strings.Contains(st.Reason, "readable") {
		t.Fatalf("status = %+v", st)
	}
}

func TestSystemdRestarter(t *testing.T) {
	u := &fakeUnit{}
	if err := (&SystemdRestarter{Unit: u, Service: "caddy.service"}).Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if u.restarted != "caddy.service" {
		t.Fatalf("restarted = %q", u.restarted)
	}
	var _ Restarter = &SystemdRestarter{}
}

func TestSetupStatus_Switchable(t *testing.T) {
	for state, want := range map[SetupState]bool{
		SetupSupervised:  true,
		SetupReady:       true,
		SetupNeeded:      false,
		SetupUnsupported: false,
	} {
		if got := (SetupStatus{State: state}).Switchable(); got != want {
			t.Errorf("%s: Switchable = %v, want %v", state, got, want)
		}
	}
}

func TestDetectSetup_RemoteAdminAPI(t *testing.T) {
	managed := managedBinary(t, 0o755)
	for _, admin := range []string{"http://caddy:2019", "http://10.0.0.5:2019", "https://caddy.example.com"} {
		for _, cfg := range []SetupConfig{
			{Supervise: true, AdminURL: admin},
			{ReloadStrategy: "systemd", Service: "caddy", ManagedPath: managed, AdminURL: admin},
		} {
			st := DetectSetup(context.Background(), cfg, &fakeUnit{path: managed})
			if st.State != SetupUnsupported || !strings.Contains(st.Reason, "CPM_CADDY_ADMINURL") {
				t.Errorf("%s supervise=%v: %+v", admin, cfg.Supervise, st)
			}
		}
	}
}

func TestDetectSetup_LocalAdminAPI(t *testing.T) {
	for _, admin := range []string{"", "http://localhost:2019", "http://127.0.0.1:2019", "http://[::1]:2019", "http://LOCALHOST:2019"} {
		st := DetectSetup(context.Background(), SetupConfig{Supervise: true, AdminURL: admin}, &fakeUnit{})
		if st.State != SetupSupervised {
			t.Errorf("%q: %+v", admin, st)
		}
	}
}

func missingManaged(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "caddy-bin", "current")
}

func TestDetectSetup_NoDropInWithoutManagedBinaryWhenDisabled(t *testing.T) {
	u := &fakeUnit{path: "/usr/bin/caddy", args: []string{"run"}}
	st := DetectSetup(context.Background(), SetupConfig{ReloadStrategy: "systemd", Service: "caddy", ManagedPath: missingManaged(t)}, u)
	if st.State != SetupUnsupported || st.DropIn != "" || st.Reason != "plugin install is disabled (CPM_PLUGIN_INSTALL=false)" {
		t.Fatalf("status = %+v", st)
	}
}

func TestDetectSetup_NoDropInWhenSeedingFails(t *testing.T) {
	u := &fakeUnit{path: "/usr/bin/caddy", args: []string{"run"}}
	cfg := SetupConfig{ReloadStrategy: "systemd", Service: "caddy", ManagedPath: missingManaged(t),
		Seed: func() error { return errors.New("finding the Caddy binary: not found") }}
	st := DetectSetup(context.Background(), cfg, u)
	if st.State != SetupUnsupported || st.DropIn != "" || st.Reason != "could not prepare a managed Caddy copy: finding the Caddy binary: not found" {
		t.Fatalf("status = %+v", st)
	}
}

func TestDetectSetup_SeedThatLeavesNoBinaryOffersNoDropIn(t *testing.T) {
	u := &fakeUnit{path: "/usr/bin/caddy", args: []string{"run"}}
	cfg := SetupConfig{ReloadStrategy: "systemd", Service: "caddy", ManagedPath: missingManaged(t), Seed: func() error { return nil }}
	st := DetectSetup(context.Background(), cfg, u)
	if st.State != SetupUnsupported || st.DropIn != "" {
		t.Fatalf("status = %+v", st)
	}
}

func TestDetectSetup_SeedsThenOffersDropIn(t *testing.T) {
	managed := missingManaged(t)
	u := &fakeUnit{path: "/usr/bin/caddy", args: []string{"run"}}
	cfg := SetupConfig{ReloadStrategy: "systemd", Service: "caddy", ManagedPath: managed, Seed: func() error {
		if err := os.MkdirAll(filepath.Dir(managed), 0o755); err != nil {
			return err
		}
		return os.WriteFile(managed, []byte("x"), 0o755)
	}}
	st := DetectSetup(context.Background(), cfg, u)
	if st.State != SetupNeeded || st.DropIn == "" {
		t.Fatalf("status = %+v", st)
	}
}
