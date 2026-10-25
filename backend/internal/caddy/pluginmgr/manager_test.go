package pluginmgr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Pacerino/CaddyProxyManager/internal/caddy"
	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/database"
	"github.com/Pacerino/CaddyProxyManager/internal/logger"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

const (
	duckdns      = "github.com/caddy-dns/duckdns"
	ratelimit    = "github.com/mholt/caddy-ratelimit"
	jsonSchemaV  = "v0.0.0-2022"
	shippedVer   = "v2.11.4"
	latestPkgVer = "v9.9.9"
)

// fakeBin describes a fake Caddy binary rendered as a shell script.
type fakeBin struct {
	Version     string
	Packages    []InstalledPackage
	ValidateErr string
}

func (f fakeBin) script() string {
	mods := []listedModule{{Name: "http", Type: "standard", Version: f.Version, Package: "github.com/caddyserver/caddy/v2"}}
	var deps strings.Builder
	deps.WriteString("path\tcaddy\nmod\tgithub.com/caddyserver/caddy/v2\t" + f.Version + "\th1:x\n")
	for _, p := range f.Packages {
		for _, m := range p.Modules {
			mods = append(mods, listedModule{Name: m, Type: "non-standard", Version: p.Version, Package: p.Path})
		}
		deps.WriteString("dep\t" + p.Path + "\t" + p.Version + "\th1:y\n")
	}
	modsJSON, _ := json.Marshal(mods)
	// Like caddy validate, reject a config file without a JSON object.
	validate := `if [ ! -s "$3" ] || grep -qx 'null' "$3"; then echo "empty config" >&2; exit 1; fi; echo "Valid configuration"`
	if f.ValidateErr != "" {
		validate = fmt.Sprintf("echo %q >&2; exit 1", f.ValidateErr)
	}
	return fmt.Sprintf(`#!/bin/sh
case "$1" in
  version) echo '%s h1:abc=' ;;
  list-modules) cat <<'EOF'
%s
EOF
    ;;
  build-info) cat <<'EOF'
%sEOF
    ;;
  validate) %s ;;
  json-schema)
    if [ "$2" != "--output" ]; then echo "bad args" >&2; exit 2; fi
    echo '{"schema":"%s"}' > "$3"
    ;;
  *) echo "unknown command $1" >&2; exit 2 ;;
esac
`, f.Version, modsJSON, deps.String(), validate, f.Version)
}

// TestMain silences the warnings failed operations log on purpose.
func TestMain(m *testing.M) {
	_ = logger.Configure(&logger.Config{LogThreshold: logger.ErrorLevel, Formatter: "json"})
	os.Exit(m.Run())
}

func moduleFor(pkgPath string) []string {
	if pkgPath == JSONSchemaPackage {
		return nil
	}
	return []string{"x." + path.Base(pkgPath)}
}

// fakeBuilds is an httptest build service that serves fake binaries matching
// the requested packages.
type fakeBuilds struct {
	mu          sync.Mutex
	requests    []url.Values
	status      int    // non-zero: answer with this status and message
	message     string // error message for status
	version     string // overrides the reported Caddy version
	drop        string // package left out of the built binary
	validateErr string
	modules     map[string]string // package path -> module path, default identity
}

func (f *fakeBuilds) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q := r.URL.Query()
	f.requests = append(f.requests, q)
	if f.status != 0 {
		w.WriteHeader(f.status)
		fmt.Fprintf(w, `{"status_code":%d,"error":{"message":%q}}`, f.status, f.message)
		return
	}
	bin := fakeBin{Version: q.Get("version"), ValidateErr: f.validateErr}
	if f.version != "" {
		bin.Version = f.version
	}
	for _, p := range q["p"] {
		pkgPath, ver, ok := strings.Cut(p, "@")
		if !ok {
			ver = latestPkgVer
		}
		if pkgPath == f.drop {
			continue
		}
		mod := pkgPath
		if mp, ok := f.modules[pkgPath]; ok {
			mod = mp
		}
		bin.Packages = append(bin.Packages, InstalledPackage{Path: mod, Version: ver, Modules: moduleFor(pkgPath)})
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write([]byte(bin.script()))
}

func (f *fakeBuilds) lastRequest(t *testing.T) url.Values {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		t.Fatal("no build request")
	}
	return f.requests[len(f.requests)-1]
}

func (f *fakeBuilds) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

type fakeRestarter struct {
	mu    sync.Mutex
	calls int
	errs  map[int]error // 1-based call number -> error
	block chan struct{} // when set, Restart waits for it
}

func (f *fakeRestarter) Restart(ctx context.Context) error {
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.errs[f.calls]
}

func (f *fakeRestarter) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeReconcile struct {
	mu      sync.Mutex
	calls   int
	results map[int][]error // 1-based call number -> errors
}

func (f *fakeReconcile) Run(ctx context.Context) []error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.results[f.calls]
}

type testEnv struct {
	m        *Manager
	builds   *fakeBuilds
	restart  *fakeRestarter
	rec      *fakeReconcile
	dir      string
	shipped  string
	changed  int
	changeMu sync.Mutex
}

func (e *testEnv) changes() int {
	e.changeMu.Lock()
	defer e.changeMu.Unlock()
	return e.changed
}

func writeBin(t *testing.T, dir, name string, b fakeBin) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(b.script()), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func shippedBin() fakeBin {
	return fakeBin{Version: shippedVer, Packages: []InstalledPackage{
		{Path: JSONSchemaPackage, Version: jsonSchemaV},
		{Path: duckdns, Version: "v0.5.0", Modules: []string{"dns.providers.duckdns"}},
	}}
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	e := &testEnv{
		builds:  &fakeBuilds{},
		restart: &fakeRestarter{},
		rec:     &fakeReconcile{},
		dir:     dir,
	}
	e.shipped = writeBin(t, dir, "caddy-shipped", shippedBin())
	srv := httptest.NewServer(e.builds)
	t.Cleanup(srv.Close)
	e.m = &Manager{
		Builds:        &BuildService{BaseURL: srv.URL, Client: srv.Client()},
		Inspect:       ExecInspector{},
		Store:         NewStore(dir),
		Restart:       e.restart,
		Ping:          func(context.Context) error { return nil },
		LiveConfig:    func(context.Context) ([]byte, error) { return []byte(`{"apps":{}}`), nil },
		Reconcile:     e.rec.Run,
		ShippedBinary: e.shipped,
		DB:            newSettingsDB(t),
		SchemaOut:     filepath.Join(dir, "caddy_schema.json"),
		OS:            "linux",
		Arch:          "amd64",
		OnChanged: func() {
			e.changeMu.Lock()
			e.changed++
			e.changeMu.Unlock()
		},
		pingTimeout:  300 * time.Millisecond,
		pingInterval: 10 * time.Millisecond,
	}
	return e
}

func isTerminal(s OpStep) bool { return s == StepDone || s == StepRolledBack || s == StepFailed }

func (e *testEnv) run(t *testing.T, kind OpKind, pkg string) *Operation {
	t.Helper()
	op, err := e.m.Start(kind, pkg)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return waitOp(t, e.m, op.ID)
}

func waitOp(t *testing.T, m *Manager, id string) *Operation {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		op, ok := m.Get(id)
		if !ok {
			t.Fatalf("operation %s not found", id)
		}
		if isTerminal(op.Step) {
			if op.FinishedAt.IsZero() {
				t.Fatalf("terminal op without FinishedAt: %+v", op)
			}
			return op
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("operation %s did not finish", id)
	return nil
}

// binaries lists the caddy-* files and temp files in the store directory.
func (e *testEnv) binaries(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(e.m.Store.Dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	var out []string
	for _, en := range entries {
		if strings.HasPrefix(en.Name(), binPrefix) || strings.HasPrefix(en.Name(), tmpPrefix) {
			out = append(out, en.Name())
		}
	}
	sort.Strings(out)
	return out
}

func (e *testEnv) current(t *testing.T) string {
	t.Helper()
	p, _ := e.m.Store.Current()
	return p
}

func TestAdd_Success(t *testing.T) {
	e := newEnv(t)
	op := e.run(t, OpAdd, ratelimit)
	if op.Step != StepDone || op.Error != "" {
		t.Fatalf("op: %+v", op)
	}
	if op.Kind != OpAdd || op.Package != ratelimit {
		t.Fatalf("op fields: %+v", op)
	}

	q := e.builds.lastRequest(t)
	if q.Get("os") != "linux" || q.Get("arch") != "amd64" || q.Get("version") != shippedVer {
		t.Fatalf("request: %v", q)
	}
	want := []string{JSONSchemaPackage + "@" + jsonSchemaV, duckdns + "@v0.5.0", ratelimit}
	got := append([]string(nil), q["p"]...)
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("packages: got %v want %v", got, want)
	}

	cur, ok := e.m.Store.Current()
	if !ok {
		t.Fatal("no current binary after add")
	}
	if e.restart.count() != 1 || e.rec.calls != 1 {
		t.Fatalf("restarts=%d reconciles=%d", e.restart.count(), e.rec.calls)
	}

	schema, err := os.ReadFile(e.m.SchemaOut)
	if err != nil || !strings.Contains(string(schema), shippedVer) {
		t.Fatalf("schema not regenerated: %q %v", schema, err)
	}
	sidecar, err := os.ReadFile(filepath.Join(e.dir, "caddy_schema.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	sum := strings.TrimSpace(string(sidecar))
	if len(sum) != 64 || filepath.Base(cur) != binPrefix+sum[:12] {
		t.Fatalf("sidecar %q does not match current %s", sum, cur)
	}
	if e.changes() != 1 {
		t.Fatalf("OnChanged calls: %d", e.changes())
	}

	installed, err := e.m.Installed(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, p := range installed {
		paths = append(paths, p.Path)
	}
	if strings.Join(paths, ",") != duckdns+","+ratelimit {
		t.Fatalf("installed after add: %v", paths)
	}
}

func TestRemove_Success(t *testing.T) {
	e := newEnv(t)
	op := e.run(t, OpRemove, duckdns)
	if op.Step != StepDone {
		t.Fatalf("op: %+v", op)
	}
	q := e.builds.lastRequest(t)
	if len(q["p"]) != 1 || q["p"][0] != JSONSchemaPackage+"@"+jsonSchemaV {
		t.Fatalf("packages: %v", q["p"])
	}
	installed, err := e.m.Installed(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 0 {
		t.Fatalf("installed after remove: %+v", installed)
	}
}

func TestUpdate_UsesShippedVersion(t *testing.T) {
	e := newEnv(t)
	// The managed binary runs an older Caddy than the shipped one.
	old := shippedBin()
	old.Version = "v2.10.0"
	promoteFake(t, e, old)

	op := e.run(t, OpUpdate, "")
	if op.Step != StepDone {
		t.Fatalf("op: %+v", op)
	}
	q := e.builds.lastRequest(t)
	if q.Get("version") != shippedVer {
		t.Fatalf("version: %v", q.Get("version"))
	}
	got := append([]string(nil), q["p"]...)
	sort.Strings(got)
	if strings.Join(got, ",") != JSONSchemaPackage+"@"+jsonSchemaV+","+duckdns+"@v0.5.0" {
		t.Fatalf("packages: %v", got)
	}
}

// promoteFake installs b as the managed current binary.
func promoteFake(t *testing.T, e *testEnv, b fakeBin) string {
	t.Helper()
	p, _, err := e.m.Store.Put(strings.NewReader(b.script()))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.m.Store.Promote(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func assertVerifyFailed(t *testing.T, e *testEnv, op *Operation, msg string) {
	t.Helper()
	if op.Step != StepFailed || !strings.Contains(op.Error, msg) {
		t.Fatalf("want failed with %q, got %+v", msg, op)
	}
	if _, ok := e.m.Store.Current(); ok {
		t.Fatal("current must stay unchanged")
	}
	if b := e.binaries(t); len(b) != 0 {
		t.Fatalf("downloaded file not removed: %v", b)
	}
	if e.restart.count() != 0 || e.changes() != 0 {
		t.Fatalf("nothing may be switched: restarts=%d changes=%d", e.restart.count(), e.changes())
	}
}

func TestVerify_WrongVersion(t *testing.T) {
	e := newEnv(t)
	e.builds.version = "v2.9.0"
	op := e.run(t, OpAdd, ratelimit)
	assertVerifyFailed(t, e, op, "v2.9.0")
}

func TestVerify_MissingPackage(t *testing.T) {
	e := newEnv(t)
	e.builds.drop = ratelimit
	op := e.run(t, OpAdd, ratelimit)
	assertVerifyFailed(t, e, op, ratelimit)
}

func TestVerify_PinnedVersionMismatch(t *testing.T) {
	e := newEnv(t)
	// Serve duckdns at a different version than requested.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		bin := fakeBin{Version: q.Get("version"), Packages: []InstalledPackage{
			{Path: JSONSchemaPackage, Version: jsonSchemaV},
			{Path: duckdns, Version: "v0.6.0", Modules: []string{"dns.providers.duckdns"}},
			{Path: ratelimit, Version: latestPkgVer, Modules: moduleFor(ratelimit)},
		}}
		_, _ = w.Write([]byte(bin.script()))
	}))
	defer srv.Close()
	e.m.Builds.BaseURL = srv.URL

	op := e.run(t, OpAdd, ratelimit)
	assertVerifyFailed(t, e, op, "v0.6.0")
}

func TestVerify_ValidateFails(t *testing.T) {
	e := newEnv(t)
	e.builds.validateErr = "unknown module: dns.providers.duckdns"
	op := e.run(t, OpRemove, duckdns)
	assertVerifyFailed(t, e, op, "unknown module: dns.providers.duckdns")
}

func TestVerify_LiveConfigFails(t *testing.T) {
	e := newEnv(t)
	e.m.LiveConfig = func(context.Context) ([]byte, error) { return nil, errors.New("admin api down") }
	op := e.run(t, OpAdd, ratelimit)
	assertVerifyFailed(t, e, op, "admin api down")
}

func TestVerify_FailureKeepsExistingBinaries(t *testing.T) {
	e := newEnv(t)
	cur := promoteFake(t, e, shippedBin())
	// The build service returns exactly the current binary, which then fails
	// validation: the shared file must not be deleted.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(shippedBin().script()))
	}))
	defer srv.Close()
	e.m.Builds.BaseURL = srv.URL
	e.m.LiveConfig = func(context.Context) ([]byte, error) { return nil, errors.New("no config") }

	op := e.run(t, OpUpdate, "")
	if op.Step != StepFailed {
		t.Fatalf("op: %+v", op)
	}
	if _, err := os.Stat(cur); err != nil {
		t.Fatalf("current binary deleted: %v", err)
	}
}

func TestSwitch_ReconcileErrorRollsBack(t *testing.T) {
	e := newEnv(t)
	if op := e.run(t, OpAdd, ratelimit); op.Step != StepDone {
		t.Fatalf("first add: %+v", op)
	}
	first := e.current(t)

	e.rec.results = map[int][]error{2: {errors.New("write host 1: boom")}}
	restartsBefore := e.restart.count()
	op := e.run(t, OpRemove, duckdns)
	if op.Step != StepRolledBack || !strings.Contains(op.Error, "write host 1: boom") {
		t.Fatalf("op: %+v", op)
	}
	if got := e.current(t); got != first {
		t.Fatalf("current %s, want %s", got, first)
	}
	if n := e.restart.count() - restartsBefore; n != 2 {
		t.Fatalf("restarts during op: %d", n)
	}
	if e.rec.calls != 3 {
		t.Fatalf("reconciles: %d", e.rec.calls)
	}
	if e.changes() != 2 {
		t.Fatalf("OnChanged calls: %d", e.changes())
	}
}

func TestSwitch_FirstBuildRollsBackToShipped(t *testing.T) {
	e := newEnv(t)
	oldCfg := config.Configuration
	t.Cleanup(func() { config.Configuration = oldCfg })
	config.Configuration.DataFolder = e.dir
	config.Configuration.Caddy.Binary = e.shipped

	e.rec.results = map[int][]error{1: {errors.New("sync tls: nope")}}
	op := e.run(t, OpAdd, ratelimit)
	if op.Step != StepRolledBack {
		t.Fatalf("op: %+v", op)
	}
	if _, ok := e.m.Store.Current(); ok {
		t.Fatal("current link must be cleared")
	}
	if got := caddy.ActiveBinary(); got != e.shipped {
		t.Fatalf("ActiveBinary = %s, want shipped %s", got, e.shipped)
	}
	if e.restart.count() != 2 {
		t.Fatalf("restarts: %d", e.restart.count())
	}
}

func TestSwitch_RestartErrorRollsBack(t *testing.T) {
	e := newEnv(t)
	e.restart.errs = map[int]error{1: errors.New("caddy exited: bad plugin")}
	op := e.run(t, OpAdd, ratelimit)
	if op.Step != StepRolledBack || !strings.Contains(op.Error, "bad plugin") {
		t.Fatalf("op: %+v", op)
	}
	if e.restart.count() != 2 {
		t.Fatalf("restarts: %d", e.restart.count())
	}
}

func TestSwitch_CaddyNeverAnswers(t *testing.T) {
	e := newEnv(t)
	// Caddy only answers again after the rollback restart.
	e.m.Ping = func(context.Context) error {
		if e.restart.count() < 2 {
			return errors.New("connection refused")
		}
		return nil
	}
	start := time.Now()
	op := e.run(t, OpAdd, ratelimit)
	if op.Step != StepRolledBack || !strings.Contains(op.Error, "connection refused") {
		t.Fatalf("op: %+v", op)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("ping timeout not honoured: %v", time.Since(start))
	}
	if e.rec.calls != 1 {
		t.Fatalf("reconcile must only run after the rollback, calls=%d", e.rec.calls)
	}
}

func TestSwitch_RollbackFailsFallsBackToShipped(t *testing.T) {
	e := newEnv(t)
	if op := e.run(t, OpAdd, ratelimit); op.Step != StepDone {
		t.Fatalf("first add: %+v", op)
	}
	e.restart.errs = map[int]error{
		2: errors.New("new binary crashed"),
		3: errors.New("previous binary crashed"),
	}
	op := e.run(t, OpRemove, duckdns)
	if op.Step != StepFailed {
		t.Fatalf("op: %+v", op)
	}
	if !strings.Contains(op.Error, "new binary crashed") || !strings.Contains(op.Error, "previous binary crashed") {
		t.Fatalf("both errors expected: %q", op.Error)
	}
	if _, ok := e.m.Store.Current(); ok {
		t.Fatal("store must be cleared so the shipped binary runs")
	}
	if e.restart.count() != 4 {
		t.Fatalf("restarts: %d", e.restart.count())
	}
	if e.changes() != 2 {
		t.Fatalf("OnChanged calls: %d", e.changes())
	}
}

func TestSwitch_IncludesCaddyLogs(t *testing.T) {
	e := newEnv(t)
	e.m.CaddyLogs = func() []string { return []string{"line a", "panic: plugin init"} }
	e.restart.errs = map[int]error{1: errors.New("exited")}
	op := e.run(t, OpAdd, ratelimit)
	if op.Step != StepRolledBack || !strings.Contains(op.Error, "panic: plugin init") {
		t.Fatalf("op: %+v", op)
	}
}

func TestBusy(t *testing.T) {
	e := newEnv(t)
	e.restart.block = make(chan struct{})
	op, err := e.m.Start(OpAdd, ratelimit)
	if err != nil {
		t.Fatal(err)
	}
	// Wait until the first operation reached the restart.
	deadline := time.Now().Add(10 * time.Second)
	for {
		cur, _ := e.m.Get(op.ID)
		if cur.Step == StepRestarting {
			break
		}
		if isTerminal(cur.Step) || time.Now().After(deadline) {
			t.Fatalf("op did not reach restarting: %+v", cur)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := e.m.Start(OpRemove, duckdns); !errors.Is(err, ErrBusy) {
		t.Fatalf("want ErrBusy, got %v", err)
	}
	close(e.restart.block)
	if done := waitOp(t, e.m, op.ID); done.Step != StepDone {
		t.Fatalf("op: %+v", done)
	}
	// Free again once the first finished.
	if op2, err := e.m.Start(OpRemove, ratelimit); err != nil {
		t.Fatalf("Start after finish: %v", err)
	} else {
		waitOp(t, e.m, op2.ID)
	}
}

func TestStart_InvalidRequests(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		kind OpKind
		pkg  string
	}{
		{OpAdd, ""},
		{OpRemove, ""},
		{OpRemove, JSONSchemaPackage},
		{OpAdd, JSONSchemaPackage},
		{"explode", ratelimit},
	}
	for _, c := range cases {
		if _, err := e.m.Start(c.kind, c.pkg); err == nil || errors.Is(err, ErrBusy) {
			t.Errorf("Start(%q, %q): want validation error, got %v", c.kind, c.pkg, err)
		}
	}
}

func TestAdd_AlreadyInstalled(t *testing.T) {
	e := newEnv(t)
	op := e.run(t, OpAdd, duckdns)
	if op.Step != StepFailed || !strings.Contains(op.Error, "already installed") {
		t.Fatalf("op: %+v", op)
	}
	if e.builds.count() != 0 {
		t.Fatal("no build expected")
	}
}

func TestRemove_NotInstalled(t *testing.T) {
	e := newEnv(t)
	op := e.run(t, OpRemove, ratelimit)
	if op.Step != StepFailed || !strings.Contains(op.Error, "not installed") {
		t.Fatalf("op: %+v", op)
	}
}

func TestBuildServiceDown(t *testing.T) {
	e := newEnv(t)
	e.builds.status = http.StatusServiceUnavailable
	e.builds.message = "build workers are busy"
	start := time.Now()
	op := e.run(t, OpAdd, ratelimit)
	if op.Step != StepFailed || !strings.Contains(op.Error, "build workers are busy") {
		t.Fatalf("op: %+v", op)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("not quick: %v", time.Since(start))
	}
	if b := e.binaries(t); len(b) != 0 {
		t.Fatalf("leftover files: %v", b)
	}
	if e.restart.count() != 0 || e.changes() != 0 {
		t.Fatal("nothing may be switched")
	}
}

func TestDownloadInterrupted(t *testing.T) {
	e := newEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100000")
		_, _ = w.Write([]byte("#!/bin/sh\npartial"))
	}))
	defer srv.Close()
	e.m.Builds.BaseURL = srv.URL
	op := e.run(t, OpAdd, ratelimit)
	if op.Step != StepFailed {
		t.Fatalf("op: %+v", op)
	}
	if b := e.binaries(t); len(b) != 0 {
		t.Fatalf("partial download kept: %v", b)
	}
}

func TestGet_KeepsLast20(t *testing.T) {
	e := newEnv(t)
	var first string
	for i := 0; i < 21; i++ {
		op := e.run(t, OpRemove, ratelimit) // fails fast: not installed
		if i == 0 {
			first = op.ID
		}
	}
	if _, ok := e.m.Get(first); ok {
		t.Fatal("oldest operation should be evicted")
	}
	if len(e.m.ops) != 20 {
		t.Fatalf("kept %d operations", len(e.m.ops))
	}
}

func TestUpdateAvailable(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	if _, _, avail, err := e.m.UpdateAvailable(ctx); err != nil || avail {
		t.Fatalf("no managed binary: avail=%v err=%v", avail, err)
	}

	old := shippedBin()
	old.Version = "v2.10.2"
	promoteFake(t, e, old)
	managed, shipped, avail, err := e.m.UpdateAvailable(ctx)
	if err != nil || !avail || managed != "v2.10.2" || shipped != shippedVer {
		t.Fatalf("managed=%s shipped=%s avail=%v err=%v", managed, shipped, avail, err)
	}

	promoteFake(t, e, shippedBin())
	if _, _, avail, err := e.m.UpdateAvailable(ctx); err != nil || avail {
		t.Fatalf("same version: avail=%v err=%v", avail, err)
	}

	newer := shippedBin()
	newer.Version = "v2.12.0-beta.1"
	promoteFake(t, e, newer)
	if _, _, avail, err := e.m.UpdateAvailable(ctx); err != nil || avail {
		t.Fatalf("managed newer: avail=%v err=%v", avail, err)
	}
}

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v2.11.4", "v2.11.4", 0},
		{"v2.10.0", "v2.11.4", -1},
		{"v2.11.10", "v2.11.4", 1},
		{"2.11.4", "v2.11.4", 0},
		{"v2.11.0-beta.1", "v2.11.0", -1},
		{"v2.11.0-beta.2", "v2.11.0-beta.10", -1},
		{"v2.11.0-rc.1", "v2.11.0-beta.1", 1},
		{"v3.0.0", "v2.99.99", 1},
		{"v2.11.4+meta", "v2.11.4", 0},
	}
	for _, c := range cases {
		got, err := compareSemver(c.a, c.b)
		if err != nil || got != c.want {
			t.Errorf("compareSemver(%s, %s) = %d, %v; want %d", c.a, c.b, got, err, c.want)
		}
	}
	if _, err := compareSemver("devel", "v2.11.4"); err == nil {
		t.Error("expected error for non-semver")
	}
}

func newSettingsDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&database.Setting{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestAutoUpdateOnStartup(t *testing.T) {
	for _, on := range []bool{false, true} {
		t.Run(fmt.Sprintf("enabled=%v", on), func(t *testing.T) {
			e := newEnv(t)
			old := shippedBin()
			old.Version = "v2.10.0"
			promoteFake(t, e, old)
			db := newSettingsDB(t)
			if on {
				if err := database.SetSetting(db, SettingAutoUpdate, "true"); err != nil {
					t.Fatal(err)
				}
			}

			e.m.AutoUpdateOnStartup(context.Background(), db)

			e.m.mu.Lock()
			ids := append([]string(nil), e.m.order...)
			e.m.mu.Unlock()
			if !on {
				if len(ids) != 0 || e.builds.count() != 0 {
					t.Fatalf("no update expected: ops=%v builds=%d", ids, e.builds.count())
				}
				return
			}
			if len(ids) != 1 {
				t.Fatalf("one update expected, got %v", ids)
			}
			op := waitOp(t, e.m, ids[0])
			if op.Kind != OpUpdate || op.Step != StepDone {
				t.Fatalf("op: %+v", op)
			}
			if v := e.builds.lastRequest(t).Get("version"); v != shippedVer {
				t.Fatalf("version: %s", v)
			}
		})
	}
}

func TestAutoUpdateOnStartup_NothingToUpdate(t *testing.T) {
	e := newEnv(t)
	db := newSettingsDB(t)
	if err := database.SetSetting(db, SettingAutoUpdate, "true"); err != nil {
		t.Fatal(err)
	}
	promoteFake(t, e, shippedBin())
	e.m.AutoUpdateOnStartup(context.Background(), db)
	if len(e.m.order) != 0 {
		t.Fatalf("no update expected: %v", e.m.order)
	}
}

func TestOperation_JSON(t *testing.T) {
	op := Operation{ID: "abc", Kind: OpAdd, Package: ratelimit, Step: StepDone, StartedAt: time.Unix(0, 0).UTC()}
	data, err := json.Marshal(op)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, k := range []string{`"id":"abc"`, `"kind":"add"`, `"package":`, `"step":"done"`, `"startedAt":`} {
		if !strings.Contains(s, k) {
			t.Errorf("missing %s in %s", k, s)
		}
	}
	if strings.Contains(s, "finishedAt") {
		t.Errorf("zero finishedAt should be omitted: %s", s)
	}
}

const (
	subPkg    = "github.com/x/plug/http"
	subModule = "github.com/x/plug"
)

func requestedSetting(t *testing.T, e *testEnv) []string {
	t.Helper()
	var paths []string
	if err := json.Unmarshal([]byte(database.GetSetting(e.m.DB, SettingRequested, "[]")), &paths); err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	return paths
}

func sortedParams(q url.Values) string {
	p := append([]string(nil), q["p"]...)
	sort.Strings(p)
	return strings.Join(p, ",")
}

func TestSubdirectoryPackage_AddRepinRemove(t *testing.T) {
	e := newEnv(t)
	e.builds.modules = map[string]string{subPkg: subModule}

	// Installing a package that lives in a module subdirectory verifies
	// against its module and records the requested path.
	if op := e.run(t, OpAdd, subPkg); op.Step != StepDone {
		t.Fatalf("add subdirectory package: %+v", op)
	}
	if got := strings.Join(requestedSetting(t, e), ","); got != JSONSchemaPackage+","+duckdns+","+subPkg {
		t.Fatalf("requested setting: %s", got)
	}

	// The next build re-pins the subdirectory path, not the module root.
	if op := e.run(t, OpAdd, ratelimit); op.Step != StepDone {
		t.Fatalf("second add: %+v", op)
	}
	want := strings.Join([]string{duckdns + "@v0.5.0", JSONSchemaPackage + "@" + jsonSchemaV, ratelimit, subPkg + "@" + latestPkgVer}, ",")
	if got := sortedParams(e.builds.lastRequest(t)); got != sortedSlice(want) {
		t.Fatalf("packages: got %s want %s", got, sortedSlice(want))
	}

	// Removing the module (as the installed list shows it) drops the path.
	if op := e.run(t, OpRemove, subModule); op.Step != StepDone {
		t.Fatalf("remove: %+v", op)
	}
	if got := sortedParams(e.builds.lastRequest(t)); strings.Contains(got, subModule) {
		t.Fatalf("removed package still requested: %s", got)
	}
	for _, p := range requestedSetting(t, e) {
		if strings.HasPrefix(p, subModule) {
			t.Fatalf("removed package still recorded: %v", requestedSetting(t, e))
		}
	}
}

func sortedSlice(csv string) string {
	p := strings.Split(csv, ",")
	sort.Strings(p)
	return strings.Join(p, ",")
}

func TestRequestedPackages_Subdirectories(t *testing.T) {
	installed := []InstalledPackage{
		{Path: JSONSchemaPackage, Version: jsonSchemaV},
		{Path: duckdns, Version: "v0.5.0"},
		{Path: subModule, Version: "v1.2.0"},
	}
	requested := []string{JSONSchemaPackage, subPkg, subModule + "/admin", "github.com/gone/plugin"}

	got, err := requestedPackages(OpUpdate, "", installed, requested)
	if err != nil {
		t.Fatal(err)
	}
	var params []string
	for _, p := range got {
		params = append(params, p.Param())
	}
	sort.Strings(params)
	want := []string{duckdns + "@v0.5.0", JSONSchemaPackage + "@" + jsonSchemaV, subModule + "/admin@v1.2.0", subPkg + "@v1.2.0"}
	sort.Strings(want)
	if strings.Join(params, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", params, want)
	}

	// Removing one subdirectory path keeps its sibling.
	got, err = requestedPackages(OpRemove, subPkg, installed, requested)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range got {
		if p.Path == subPkg {
			t.Fatalf("not removed: %v", got)
		}
	}
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}

	// Adding a sibling of an installed module pins it to the module version.
	got, err = requestedPackages(OpAdd, subModule+"/extra", installed, requested)
	if err != nil {
		t.Fatal(err)
	}
	if last := got[len(got)-1]; last.Param() != subModule+"/extra@v1.2.0" {
		t.Fatalf("added: %v", last)
	}
	if _, err := requestedPackages(OpAdd, subPkg, installed, requested); err == nil {
		t.Fatal("adding an installed subdirectory path must fail")
	}
}

func TestVerify_SubdirectoryPrefixBoundary(t *testing.T) {
	e := newEnv(t)
	// The module github.com/x/plugin must not satisfy github.com/x/plug/http.
	e.builds.modules = map[string]string{subPkg: "github.com/x/plugin"}
	op := e.run(t, OpAdd, subPkg)
	assertVerifyFailed(t, e, op, subPkg)
}

func TestVerify_EmptyLiveConfig(t *testing.T) {
	for _, cfg := range []string{"", "null", " null\n"} {
		t.Run(fmt.Sprintf("%q", cfg), func(t *testing.T) {
			e := newEnv(t)
			e.m.LiveConfig = func(context.Context) ([]byte, error) { return []byte(cfg), nil }
			if op := e.run(t, OpAdd, ratelimit); op.Step != StepDone {
				t.Fatalf("op: %+v", op)
			}
		})
	}
}
