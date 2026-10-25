package pluginmgr

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/Pacerino/CaddyProxyManager/internal/database"
	"github.com/Pacerino/CaddyProxyManager/internal/logger"

	"gorm.io/gorm"
)

// OpKind is the kind of a plugin operation.
type OpKind string

const (
	OpAdd    OpKind = "add"
	OpRemove OpKind = "remove"
	OpUpdate OpKind = "update"
)

// OpStep is the progress of a plugin operation. Done, RolledBack and Failed
// are terminal.
type OpStep string

const (
	StepRequesting  OpStep = "requesting"
	StepDownloading OpStep = "downloading"
	StepVerifying   OpStep = "verifying"
	StepRestarting  OpStep = "restarting"
	StepReapplying  OpStep = "reapplying"
	StepDone        OpStep = "done"
	StepRolledBack  OpStep = "rolledBack"
	StepFailed      OpStep = "failed"
)

// SettingAutoUpdate is the settings key ("true"/"false") that makes CPM
// rebuild the plugin set on a newer shipped Caddy at startup.
const SettingAutoUpdate = "plugins.autoUpdate"

// SettingRequested is the settings key holding the JSON array of package
// paths requested by the last successful operation.
const SettingRequested = "plugins.requested"

const (
	maxOperations = 20
	maxLogLines   = 20

	defaultPingTimeout      = 30 * time.Second
	defaultPingInterval     = 500 * time.Millisecond
	defaultRestartTimeout   = 2 * time.Minute
	defaultReconcileTimeout = 2 * time.Minute
	defaultSchemaTimeout    = 5 * time.Minute
)

// ErrBusy is returned by Start while another operation runs.
var ErrBusy = errors.New("another plugin operation is running")

// errStoreAborted stops a download whose binary could not be stored.
var errStoreAborted = errors.New("storing the binary failed")

// Operation is the status of one add, remove or update.
type Operation struct {
	ID         string    `json:"id"`
	Kind       OpKind    `json:"kind"`
	Package    string    `json:"package"`
	Step       OpStep    `json:"step"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt,omitzero"`
}

// Manager changes the plugin set of the managed Caddy binary: it downloads a
// new build, verifies it, switches to it and rolls back if Caddy doesn't come
// up. One operation runs at a time.
type Manager struct {
	Builds  *BuildService
	Inspect Inspector
	Store   *Store
	Restart Restarter
	// Ping reports whether Caddy's admin API answers.
	Ping func(ctx context.Context) error
	// LiveConfig returns Caddy's running config as JSON.
	LiveConfig func(ctx context.Context) ([]byte, error)
	// Reconcile re-applies hosts, TLS policies and module configs.
	Reconcile func(ctx context.Context) []error
	// ShippedBinary is the Caddy binary that came with CPM; it runs when no
	// managed binary is current.
	ShippedBinary string
	// SchemaOut is where the new binary's JSON schema is written after a
	// successful switch; a caddy_schema.sha256 sidecar is written next to it.
	SchemaOut string
	OS, Arch  string
	// DB stores the requested package paths (SettingRequested). Without it,
	// installed plugins are re-requested by their Go module path.
	DB *gorm.DB
	// OnChanged, if set, is called when an operation that switched binaries
	// ends (done, rolled back, or failed after the switch).
	OnChanged func()
	// CaddyLogs, if set, returns Caddy's most recent log lines; they are
	// attached to the error of a rolled-back operation.
	CaddyLogs func() []string

	// Zero values mean the defaults; tests shorten them.
	pingTimeout, pingInterval, downloadTimeout, restartTimeout, reconcileTimeout, schemaTimeout time.Duration

	mu    sync.Mutex
	busy  bool
	ops   map[string]*Operation
	order []string // operation IDs, oldest first
}

func orDefault(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// activeBinary is the binary Caddy runs: the managed current one if any,
// otherwise the shipped one (mirrors caddy.ActiveBinary).
func (m *Manager) activeBinary() string {
	if p, ok := m.Store.Current(); ok {
		return p
	}
	return m.ShippedBinary
}

// Installed lists the plugins of the active binary, without JSONSchemaPackage.
func (m *Manager) Installed(ctx context.Context) ([]InstalledPackage, error) {
	pkgs, err := m.Inspect.Installed(ctx, m.activeBinary())
	if err != nil {
		return nil, err
	}
	out := make([]InstalledPackage, 0, len(pkgs))
	for _, p := range pkgs {
		if p.Path != JSONSchemaPackage {
			out = append(out, p)
		}
	}
	return out, nil
}

// Start begins an operation in the background. pkg is ignored for updates.
func (m *Manager) Start(kind OpKind, pkg string) (*Operation, error) {
	switch kind {
	case OpAdd, OpRemove:
		if pkg == "" {
			return nil, fmt.Errorf("%s needs a package path", kind)
		}
		if pkg == JSONSchemaPackage {
			return nil, fmt.Errorf("%s is always installed and cannot be changed", JSONSchemaPackage)
		}
	case OpUpdate:
		pkg = ""
	default:
		return nil, fmt.Errorf("unknown operation %q", kind)
	}

	var rnd [8]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return nil, err
	}
	op := &Operation{ID: hex.EncodeToString(rnd[:]), Kind: kind, Package: pkg, Step: StepRequesting, StartedAt: time.Now()}

	m.mu.Lock()
	if m.busy {
		m.mu.Unlock()
		return nil, ErrBusy
	}
	m.busy = true
	if m.ops == nil {
		m.ops = map[string]*Operation{}
	}
	m.ops[op.ID] = op
	m.order = append(m.order, op.ID)
	for len(m.order) > maxOperations {
		delete(m.ops, m.order[0])
		m.order = m.order[1:]
	}
	snapshot := *op
	m.mu.Unlock()

	go m.run(op.ID, kind, pkg)
	return &snapshot, nil
}

// Get returns a copy of a recent operation.
func (m *Manager) Get(id string) (*Operation, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	op, ok := m.ops[id]
	if !ok {
		return nil, false
	}
	c := *op
	return &c, true
}

// Running returns a copy of the operation in progress, if any. Operations
// run one at a time, so while busy it is the most recently started one.
func (m *Manager) Running() (*Operation, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.busy || len(m.order) == 0 {
		return nil, false
	}
	op, ok := m.ops[m.order[len(m.order)-1]]
	if !ok {
		return nil, false
	}
	c := *op
	return &c, true
}

// LastFinished returns a copy of the most recently started operation of kind
// that has finished, with any outcome.
func (m *Manager) LastFinished(kind OpKind) (*Operation, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.order) - 1; i >= 0; i-- {
		op, ok := m.ops[m.order[i]]
		if ok && op.Kind == kind && !op.FinishedAt.IsZero() {
			c := *op
			return &c, true
		}
	}
	return nil, false
}

// Catalog lists the plugins offered by the build service.
func (m *Manager) Catalog(ctx context.Context) ([]Package, error) {
	return m.Builds.Packages(ctx)
}

func (m *Manager) setStep(id string, step OpStep) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if op, ok := m.ops[id]; ok {
		op.Step = step
	}
}

func (m *Manager) run(id string, kind OpKind, pkg string) {
	step, switched, err := m.execute(context.Background(), id, kind, pkg)
	if err != nil {
		logger.Warn("Plugin operation %s %s ended %s: %v", kind, pkg, step, err)
	}
	// Called before the final step is published so pollers that see the
	// result also see its effects.
	if switched && m.OnChanged != nil {
		m.OnChanged()
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if op, ok := m.ops[id]; ok {
		op.Step = step
		if err != nil {
			op.Error = err.Error()
		}
		op.FinishedAt = time.Now()
	}
	m.busy = false
}

// execute runs one operation and returns its terminal step, whether the live
// binary was switched at any point, and the error.
func (m *Manager) execute(ctx context.Context, id string, kind OpKind, pkg string) (OpStep, bool, error) {
	active := m.activeBinary()
	installed, err := m.Inspect.Installed(ctx, active)
	if err != nil {
		return StepFailed, false, fmt.Errorf("inspecting the running Caddy binary: %w", err)
	}
	versionBin := active
	if kind == OpUpdate {
		if _, ok := m.Store.Current(); !ok {
			return StepFailed, false, errors.New("no managed Caddy binary to update")
		}
		versionBin = m.ShippedBinary
	}
	version, err := m.Inspect.Version(ctx, versionBin)
	if err != nil {
		return StepFailed, false, fmt.Errorf("reading the Caddy version: %w", err)
	}
	pkgs, err := requestedPackages(kind, pkg, installed, m.loadRequested())
	if err != nil {
		return StepFailed, false, err
	}

	path, sum, err := m.download(ctx, id, BuildRequest{OS: m.OS, Arch: m.Arch, CaddyVersion: version, Packages: pkgs})
	if err != nil {
		return StepFailed, false, err
	}

	m.setStep(id, StepVerifying)
	live, err := m.LiveConfig(ctx)
	if err == nil {
		err = verifyBinary(ctx, m.Inspect, path, version, pkgs, live)
	} else {
		err = fmt.Errorf("reading the live Caddy config: %w", err)
	}
	if err != nil {
		m.discard(path)
		return StepFailed, false, err
	}

	if cur, ok := m.Store.Current(); ok && cur == path {
		// The build is byte-identical to the running binary.
		m.saveRequested(pkgs)
		return StepDone, false, nil
	}
	step, switched, err := m.switchTo(ctx, id, path, sum)
	if step == StepDone {
		m.saveRequested(pkgs)
	}
	return step, switched, err
}

// loadRequested returns the package paths of the last successful operation.
func (m *Manager) loadRequested() []string {
	if m.DB == nil {
		return nil
	}
	raw := database.GetSetting(m.DB, SettingRequested, "")
	if raw == "" {
		return nil
	}
	var paths []string
	if err := json.Unmarshal([]byte(raw), &paths); err != nil {
		logger.Warn("Reading the requested plugin paths: %v", err)
		return nil
	}
	return paths
}

// saveRequested records the package paths (without versions) of a build.
func (m *Manager) saveRequested(pkgs []PinnedPackage) {
	if m.DB == nil {
		return
	}
	paths := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		paths = append(paths, p.Path)
	}
	data, err := json.Marshal(paths)
	if err == nil {
		err = database.SetSetting(m.DB, SettingRequested, string(data))
	}
	if err != nil {
		logger.Warn("Saving the requested plugin paths: %v", err)
	}
}

// discard deletes a downloaded binary unless the store still links it.
func (m *Manager) discard(path string) {
	if p, ok := m.Store.Current(); ok && p == path {
		return
	}
	if p, ok := m.Store.Previous(); ok && p == path {
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		logger.Warn("Removing rejected Caddy binary %s: %v", path, err)
	}
}

// download streams the build into the store. A failed or interrupted
// download leaves no file behind.
func (m *Manager) download(ctx context.Context, id string, req BuildRequest) (path, sum string, err error) {
	ctx, cancel := context.WithTimeout(ctx, orDefault(m.downloadTimeout, downloadTimeout))
	defer cancel()

	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		w := &firstWriteHook{w: pw, hook: func() { m.setStep(id, StepDownloading) }}
		err := m.Builds.Download(ctx, req, w)
		pw.CloseWithError(err)
		done <- err
	}()
	path, sum, putErr := m.Store.Put(pr)
	// Unblock the downloader if Put stopped reading early.
	pr.CloseWithError(errStoreAborted)
	if dlErr := <-done; dlErr != nil && !errors.Is(dlErr, errStoreAborted) {
		if putErr == nil {
			m.discard(path)
		}
		return "", "", fmt.Errorf("downloading the Caddy build: %w", dlErr)
	}
	if putErr != nil {
		return "", "", fmt.Errorf("storing the Caddy build: %w", putErr)
	}
	return path, sum, nil
}

// firstWriteHook calls hook once, before the first write.
type firstWriteHook struct {
	w     io.Writer
	hook  func()
	fired bool
}

func (f *firstWriteHook) Write(p []byte) (int, error) {
	if !f.fired {
		f.fired = true
		f.hook()
	}
	return f.w.Write(p)
}

// UpdateAvailable compares the Caddy version of the managed current binary
// with the shipped one. Without a managed binary no update is available.
func (m *Manager) UpdateAvailable(ctx context.Context) (managed, shipped string, available bool, err error) {
	cur, ok := m.Store.Current()
	if !ok {
		return "", "", false, nil
	}
	if managed, err = m.Inspect.Version(ctx, cur); err != nil {
		return "", "", false, err
	}
	if shipped, err = m.Inspect.Version(ctx, m.ShippedBinary); err != nil {
		return managed, "", false, err
	}
	c, err := compareSemver(managed, shipped)
	if err != nil {
		return managed, shipped, false, err
	}
	return managed, shipped, c < 0, nil
}

// AutoUpdateOnStartup starts an update when the auto-update setting is on and
// the shipped Caddy is newer than the managed one.
func (m *Manager) AutoUpdateOnStartup(ctx context.Context, db *gorm.DB) {
	if database.GetSetting(db, SettingAutoUpdate, "false") != "true" {
		return
	}
	managed, shipped, available, err := m.UpdateAvailable(ctx)
	if err != nil {
		logger.Warn("Checking for a Caddy update of the managed binary: %v", err)
		return
	}
	if !available {
		return
	}
	logger.Info("Updating the managed Caddy binary from %s to %s", managed, shipped)
	if _, err := m.Start(OpUpdate, ""); err != nil {
		logger.Warn("Starting the Caddy update: %v", err)
	}
}
