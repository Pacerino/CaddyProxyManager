package pluginmgr

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"

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

// Switchable reports whether CPM can restart Caddy on a managed binary.
func (s SetupStatus) Switchable() bool {
	return s.State == SetupSupervised || s.State == SetupReady
}

// SetupConfig carries the settings DetectSetup needs.
type SetupConfig struct {
	Supervise      bool
	ReloadStrategy string
	Service        string
	ManagedPath    string
	// AdminURL is CPM_CADDY_ADMINURL; empty means the local default.
	AdminURL string
}

// remoteAdminReason explains why plugins can't be installed for a Caddy whose
// admin API is not on this machine.
const remoteAdminReason = "Caddy's admin API is not local (CPM_CADDY_ADMINURL); plugins can only be installed for a Caddy that CPM runs"

// isLocalAdmin reports whether the admin API URL points at this machine
// (localhost or a loopback address).
func isLocalAdmin(adminURL string) bool {
	if adminURL == "" {
		return true
	}
	u, err := url.Parse(adminURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// DetectSetup reports whether Caddy can be switched to the managed binary and,
// when the systemd unit needs a change, the drop-in the user has to install.
func DetectSetup(ctx context.Context, cfg SetupConfig, unit SystemdUnit) SetupStatus {
	// CPM can only swap the binary of a Caddy it runs or restarts locally;
	// a remote admin API belongs to some other Caddy.
	if !isLocalAdmin(cfg.AdminURL) {
		return SetupStatus{State: SetupUnsupported, Reason: remoteAdminReason}
	}
	if cfg.Supervise {
		return SetupStatus{State: SetupSupervised}
	}
	if reason := platformUnsupported(); reason != "" {
		return SetupStatus{State: SetupUnsupported, Reason: reason}
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
