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
