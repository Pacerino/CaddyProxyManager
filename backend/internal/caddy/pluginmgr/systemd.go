package pluginmgr

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/coreos/go-systemd/v22/dbus"
)

// SystemdUnit abstracts the systemd D-Bus calls so the setup detection can be
// tested without systemd.
type SystemdUnit interface {
	// ExecStart returns the unit's program, its arguments (without argv[0])
	// and the user the service runs as (empty means root).
	ExecStart(ctx context.Context, unit string) (path string, args []string, user string, err error)
	Restart(ctx context.Context, unit string) error
}

// SetupState describes how Caddy is run relative to CPM's managed binary.
type SetupState string

const (
	SetupSupervised  SetupState = "supervised"
	SetupReady       SetupState = "systemd-ready"
	SetupNeeded      SetupState = "setup-needed"
	SetupUnsupported SetupState = "unsupported"
)

// SetupStatus is the result of DetectSetup. DropIn is only set for SetupNeeded.
type SetupStatus struct {
	State  SetupState `json:"state"`
	Reason string     `json:"reason,omitempty"`
	DropIn string     `json:"dropIn,omitempty"`
}

// SetupConfig carries the settings DetectSetup needs.
type SetupConfig struct {
	Supervise      bool
	ReloadStrategy string
	Service        string
	ManagedPath    string
}

// DetectSetup reports whether Caddy can be switched to the managed binary and,
// when the systemd unit needs a change, the drop-in the user has to install.
func DetectSetup(ctx context.Context, cfg SetupConfig, unit SystemdUnit) SetupStatus {
	if cfg.Supervise {
		return SetupStatus{State: SetupSupervised}
	}
	if strings.ToLower(cfg.ReloadStrategy) != "systemd" {
		return SetupStatus{State: SetupUnsupported, Reason: fmt.Sprintf("reload strategy %q is not systemd", cfg.ReloadStrategy)}
	}
	path, args, svcUser, err := unit.ExecStart(ctx, cfg.Service)
	if err != nil {
		return SetupStatus{State: SetupUnsupported, Reason: fmt.Sprintf("cannot query systemd: %v", err)}
	}

	reason := ""
	if path != cfg.ManagedPath {
		reason = fmt.Sprintf("service %s runs %s instead of the managed binary", cfg.Service, path)
	} else if err := readableBy(cfg.ManagedPath, svcUser); err != nil {
		reason = fmt.Sprintf("managed binary is not readable by the service user: %v", err)
	}
	if reason == "" {
		return SetupStatus{State: SetupReady}
	}

	dropIn := "[Service]\nExecStart=\nExecStart=" + cfg.ManagedPath
	if len(args) > 0 {
		dropIn += " " + strings.Join(args, " ")
	}
	return SetupStatus{State: SetupNeeded, Reason: reason, DropIn: dropIn + "\n"}
}

// readableBy checks that userName can read and execute path and search every
// parent directory. An empty userName means root, which can always access it.
func readableBy(path, userName string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	if userName == "" || userName == "root" {
		return nil
	}
	u, err := user.Lookup(userName)
	if err != nil {
		return err
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return err
	}
	gids := map[uint32]bool{}
	if g, err := strconv.ParseUint(u.Gid, 10, 32); err == nil {
		gids[uint32(g)] = true
	}
	if groups, err := u.GroupIds(); err == nil {
		for _, s := range groups {
			if g, err := strconv.ParseUint(s, 10, 32); err == nil {
				gids[uint32(g)] = true
			}
		}
	}

	check := func(p string, need os.FileMode) error {
		fi, err := os.Stat(p)
		if err != nil {
			return err
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("cannot read ownership of %s", p)
		}
		mode := fi.Mode().Perm()
		var have os.FileMode
		switch {
		case uint64(st.Uid) == uid:
			have = (mode >> 6) & 7
		case gids[st.Gid]:
			have = (mode >> 3) & 7
		default:
			have = mode & 7
		}
		if have&need != need {
			return fmt.Errorf("%s has insufficient permissions for user %s", p, userName)
		}
		return nil
	}

	if err := check(path, 5); err != nil { // r-x
		return err
	}
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		if err := check(dir, 1); err != nil { // --x
			return err
		}
		if dir == filepath.Dir(dir) {
			break
		}
	}
	return nil
}

// SystemdRestarter restarts the Caddy service through systemd.
type SystemdRestarter struct {
	Unit    SystemdUnit
	Service string
}

func (r *SystemdRestarter) Restart(ctx context.Context) error {
	return r.Unit.Restart(ctx, r.Service)
}

// DBusUnit is the D-Bus backed SystemdUnit.
type DBusUnit struct{}

func (DBusUnit) ExecStart(ctx context.Context, unit string) (string, []string, string, error) {
	conn, err := dbus.NewSystemdConnectionContext(ctx)
	if err != nil {
		return "", nil, "", err
	}
	defer conn.Close()
	props, err := conn.GetUnitTypePropertiesContext(ctx, unit, "Service")
	if err != nil {
		return "", nil, "", err
	}
	path, args, err := parseExecStart(props["ExecStart"])
	if err != nil {
		return "", nil, "", err
	}
	svcUser, _ := props["User"].(string)
	return path, args, svcUser, nil
}

// parseExecStart decodes the D-Bus a(sasbttttuii) value of ExecStart. The
// first command's path and argv[1:] are returned. go-systemd delivers each
// entry as []interface{}{path string, argv []string, ...}.
func parseExecStart(v interface{}) (string, []string, error) {
	entries, ok := v.([][]interface{})
	if !ok || len(entries) == 0 || len(entries[0]) < 2 {
		return "", nil, fmt.Errorf("unexpected ExecStart property %T", v)
	}
	path, ok := entries[0][0].(string)
	if !ok {
		return "", nil, fmt.Errorf("unexpected ExecStart path %T", entries[0][0])
	}
	argv, _ := entries[0][1].([]string)
	if len(argv) > 0 {
		argv = argv[1:] // argv[0] is the program name
	}
	return path, argv, nil
}

func (DBusUnit) Restart(ctx context.Context, unit string) error {
	conn, err := dbus.NewSystemdConnectionContext(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	res := make(chan string, 1)
	if _, err := conn.RestartUnitContext(ctx, unit, "replace", res); err != nil {
		return err
	}
	select {
	case r := <-res:
		if r != "done" {
			return fmt.Errorf("restart of %s finished with result %q", unit, r)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
