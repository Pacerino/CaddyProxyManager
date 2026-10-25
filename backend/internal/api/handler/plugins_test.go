package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Pacerino/CaddyProxyManager/internal/caddy/pluginmgr"
	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/database"
)

const (
	duckdnsPkg  = "github.com/caddy-dns/duckdns"
	duckdnsMod  = "dns.providers.duckdns"
	customPkg   = "github.com/example/caddy-plug"
	customMod   = "http.handlers.plug"
	customSub   = "github.com/example/caddy-plug/extra"
	notInstPkg  = "github.com/example/other"
	notInstMod  = "http.handlers.other"
	runningOpID = "op-running"
)

// stubPlugins is a PluginManager stub that records started operations.
type stubPlugins struct {
	mu           sync.Mutex
	installed    []pluginmgr.InstalledPackage
	installedErr error
	catalog      []pluginmgr.Package
	catalogErr   error
	busy         bool
	running      *pluginmgr.Operation
	ops          map[string]*pluginmgr.Operation
	lastFinished map[pluginmgr.OpKind]*pluginmgr.Operation
	started      []string // "kind pkg"
	managed      string
	shipped      string
	available    bool
}

func (s *stubPlugins) Installed(context.Context) ([]pluginmgr.InstalledPackage, error) {
	return s.installed, s.installedErr
}

func (s *stubPlugins) Catalog(context.Context) ([]pluginmgr.Package, error) {
	return s.catalog, s.catalogErr
}

func (s *stubPlugins) Start(kind pluginmgr.OpKind, pkg string) (*pluginmgr.Operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return nil, pluginmgr.ErrBusy
	}
	s.started = append(s.started, fmt.Sprintf("%s %s", kind, pkg))
	return &pluginmgr.Operation{ID: "op-new", Kind: kind, Package: pkg, Step: pluginmgr.StepRequesting}, nil
}

func (s *stubPlugins) Get(id string) (*pluginmgr.Operation, bool) {
	op, ok := s.ops[id]
	return op, ok
}

func (s *stubPlugins) Running() (*pluginmgr.Operation, bool) {
	return s.running, s.running != nil
}

func (s *stubPlugins) LastFinished(kind pluginmgr.OpKind) (*pluginmgr.Operation, bool) {
	op, ok := s.lastFinished[kind]
	return op, ok
}

func (s *stubPlugins) UpdateAvailable(context.Context) (string, string, bool, error) {
	return s.managed, s.shipped, s.available, nil
}

func (s *stubPlugins) startedOps() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.started...)
}

func newStubPlugins() *stubPlugins {
	return &stubPlugins{
		installed: []pluginmgr.InstalledPackage{
			{Path: duckdnsPkg, Version: "v0.4.0", Modules: []string{duckdnsMod}},
			{Path: customPkg, Version: "v1.2.3", Modules: []string{customMod}},
			{Path: pluginmgr.JSONSchemaPackage, Version: "v0.0.1"},
		},
		catalog: []pluginmgr.Package{
			{Path: duckdnsPkg, Repo: "https://github.com/caddy-dns/duckdns", Modules: []string{duckdnsMod}},
			{Path: customSub, Repo: "https://github.com/example/caddy-plug", Modules: []string{"http.handlers.extra"}},
			{Path: notInstPkg, Repo: "https://github.com/example/other", Modules: []string{notInstMod}},
			{Path: pluginmgr.JSONSchemaPackage, Repo: "https://github.com/abiosoft/caddy-json-schema"},
		},
		ops: map[string]*pluginmgr.Operation{},
	}
}

// newPluginTestHandler wires a stub plugin manager in API mode with plugin
// install enabled and a supervised Caddy.
func newPluginTestHandler(t *testing.T, stub *stubPlugins) Handler {
	t.Helper()
	h := newTestHandler(t)
	h.Plugins = stub
	h.PluginSetup = func(context.Context) pluginmgr.SetupStatus {
		return pluginmgr.SetupStatus{State: pluginmgr.SetupSupervised}
	}
	useAPIMode(t, "http://127.0.0.1:1")
	prev := config.Configuration.PluginInstall
	config.Configuration.PluginInstall = true
	t.Cleanup(func() { config.Configuration.PluginInstall = prev })
	return h
}

type pluginsView struct {
	Installed []struct {
		Path    string   `json:"path"`
		Version string   `json:"version"`
		Modules []string `json:"modules"`
		Tested  bool     `json:"tested"`
	} `json:"installed"`
	InstallEnabled bool                  `json:"installEnabled"`
	DisabledReason string                `json:"disabledReason"`
	Setup          pluginmgr.SetupStatus `json:"setup"`
	Update         map[string]any        `json:"update"`
	AutoUpdate     bool                  `json:"autoUpdate"`
	Operation      *pluginmgr.Operation  `json:"operation"`
	LastUpdate     *pluginmgr.Operation  `json:"lastUpdate"`
}

func getPlugins(t *testing.T, h Handler) pluginsView {
	t.Helper()
	rec := doRequest(h.GetPlugins(), http.MethodGet, "/caddy/plugins", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status %d body %s", rec.Code, rec.Body.String())
	}
	var v pluginsView
	decodeResult(t, rec, &v)
	return v
}

// errorBody decodes the { error } envelope.
func errorBody(t *testing.T, body []byte) (string, json.RawMessage) {
	t.Helper()
	var env struct {
		Error struct {
			Message string          `json:"message"`
			Invalid json.RawMessage `json:"invalid"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode error: %v (%s)", err, body)
	}
	return env.Error.Message, env.Error.Invalid
}

func TestGetPlugins_Shape(t *testing.T) {
	stub := newStubPlugins()
	stub.managed, stub.shipped, stub.available = "v2.10.0", "v2.11.0", true
	stub.running = &pluginmgr.Operation{ID: runningOpID, Kind: pluginmgr.OpAdd, Package: notInstPkg, Step: pluginmgr.StepDownloading}
	h := newPluginTestHandler(t, stub)
	if err := database.SetSetting(h.DB, pluginmgr.SettingAutoUpdate, "true"); err != nil {
		t.Fatal(err)
	}

	v := getPlugins(t, h)
	if len(v.Installed) != 2 {
		t.Fatalf("installed = %+v, want duckdns and custom (json-schema hidden)", v.Installed)
	}
	if v.Installed[0].Path != duckdnsPkg || !v.Installed[0].Tested || v.Installed[0].Version != "v0.4.0" || v.Installed[0].Modules[0] != duckdnsMod {
		t.Errorf("duckdns = %+v", v.Installed[0])
	}
	if v.Installed[1].Path != customPkg || v.Installed[1].Tested {
		t.Errorf("custom = %+v", v.Installed[1])
	}
	if !v.InstallEnabled || v.DisabledReason != "" || v.Setup.State != pluginmgr.SetupSupervised {
		t.Errorf("install state = %v %q %+v", v.InstallEnabled, v.DisabledReason, v.Setup)
	}
	if v.Update["available"] != true || v.Update["managed"] != "v2.10.0" || v.Update["shipped"] != "v2.11.0" {
		t.Errorf("update = %+v", v.Update)
	}
	if !v.AutoUpdate {
		t.Error("autoUpdate = false, want true")
	}
	if v.Operation == nil || v.Operation.ID != runningOpID {
		t.Errorf("operation = %+v", v.Operation)
	}
}

func TestGetPlugins_InstallDisabled(t *testing.T) {
	h := newPluginTestHandler(t, newStubPlugins())
	config.Configuration.PluginInstall = false
	v := getPlugins(t, h)
	if v.InstallEnabled || v.DisabledReason == "" {
		t.Errorf("installEnabled=%v reason=%q, want disabled with a reason", v.InstallEnabled, v.DisabledReason)
	}
	if len(v.Installed) != 2 {
		t.Errorf("installed list should stay readable: %+v", v.Installed)
	}
	if v.Operation != nil {
		t.Errorf("operation = %+v, want none", v.Operation)
	}

	rec := doRequest(h.AddPlugin(), http.MethodPost, "/caddy/plugins", map[string]string{"path": notInstPkg}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("add while disabled: status %d", rec.Code)
	}
}

func TestGetPlugins_SetupNeeded(t *testing.T) {
	h := newPluginTestHandler(t, newStubPlugins())
	h.PluginSetup = func(context.Context) pluginmgr.SetupStatus {
		return pluginmgr.SetupStatus{State: pluginmgr.SetupNeeded, Reason: "service runs /usr/bin/caddy", DropIn: "[Service]\n"}
	}
	v := getPlugins(t, h)
	if v.InstallEnabled || v.DisabledReason != "service runs /usr/bin/caddy" || v.Setup.DropIn == "" {
		t.Errorf("view = %+v", v)
	}
	rec := doRequest(h.AddPlugin(), http.MethodPost, "/caddy/plugins", map[string]string{"path": notInstPkg}, nil)
	if msg, _ := errorBody(t, rec.Body.Bytes()); rec.Code != http.StatusBadRequest || msg != "service runs /usr/bin/caddy" {
		t.Errorf("add: status %d message %q", rec.Code, msg)
	}
}

func TestPlugins_CaddyfileMode(t *testing.T) {
	h := newPluginTestHandler(t, newStubPlugins())
	config.Configuration.Caddy.Mode = "caddyfile"
	for name, fn := range map[string]http.HandlerFunc{
		"list":    h.GetPlugins(),
		"catalog": h.GetPluginCatalog(),
		"add":     h.AddPlugin(),
		"remove":  h.RemovePlugin(),
		"update":  h.UpdatePlugins(),
		"op":      h.GetPluginOperation(),
		"setting": h.SetPluginSettings(),
	} {
		rec := doRequest(fn, http.MethodGet, "/caddy/plugins", nil, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, rec.Code)
		}
	}
}

type catalogEntry struct {
	Path      string   `json:"path"`
	Repo      string   `json:"repo"`
	Modules   []string `json:"modules"`
	Tested    bool     `json:"tested"`
	Installed bool     `json:"installed"`
}

func getCatalog(t *testing.T, h Handler, q string) []catalogEntry {
	t.Helper()
	rec := doRequest(h.GetPluginCatalog(), http.MethodGet, "/caddy/plugins/catalog?q="+q, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog: status %d body %s", rec.Code, rec.Body.String())
	}
	var out []catalogEntry
	decodeResult(t, rec, &out)
	return out
}

func TestGetPluginCatalog(t *testing.T) {
	h := newPluginTestHandler(t, newStubPlugins())

	all := getCatalog(t, h, "")
	if len(all) != 3 {
		t.Fatalf("catalog = %+v, want 3 entries without json-schema", all)
	}
	byPath := map[string]catalogEntry{}
	for _, e := range all {
		byPath[e.Path] = e
	}
	if e := byPath[duckdnsPkg]; !e.Installed || !e.Tested || e.Repo == "" {
		t.Errorf("duckdns = %+v", e)
	}
	if e := byPath[customSub]; !e.Installed {
		t.Errorf("subdirectory of an installed module should be installed: %+v", e)
	}
	if e := byPath[notInstPkg]; e.Installed || e.Tested {
		t.Errorf("other = %+v", e)
	}

	// Case-insensitive match on the path ...
	if got := getCatalog(t, h, "DuckDNS"); len(got) != 1 || got[0].Path != duckdnsPkg {
		t.Errorf("q=DuckDNS: %+v", got)
	}
	// ... and on module names.
	if got := getCatalog(t, h, "HANDLERS.OTHER"); len(got) != 1 || got[0].Path != notInstPkg {
		t.Errorf("q=HANDLERS.OTHER: %+v", got)
	}
}

func TestGetPluginCatalog_Max50(t *testing.T) {
	stub := newStubPlugins()
	stub.catalog = nil
	for i := 0; i < 80; i++ {
		stub.catalog = append(stub.catalog, pluginmgr.Package{Path: fmt.Sprintf("github.com/x/p%d", i)})
	}
	h := newPluginTestHandler(t, stub)
	if got := getCatalog(t, h, "github"); len(got) != 50 {
		t.Errorf("got %d entries, want 50", len(got))
	}
}

func TestGetPluginCatalog_BuildServiceDown(t *testing.T) {
	stub := newStubPlugins()
	stub.catalogErr = errors.New("caddy build service returned 502: Bad Gateway")
	h := newPluginTestHandler(t, stub)
	rec := doRequest(h.GetPluginCatalog(), http.MethodGet, "/caddy/plugins/catalog", nil, nil)
	if msg, _ := errorBody(t, rec.Body.Bytes()); rec.Code != http.StatusServiceUnavailable || msg != stub.catalogErr.Error() {
		t.Errorf("status %d message %q", rec.Code, msg)
	}
}

func TestAddPlugin(t *testing.T) {
	stub := newStubPlugins()
	h := newPluginTestHandler(t, stub)

	rec := doRequest(h.AddPlugin(), http.MethodPost, "/caddy/plugins", map[string]string{"path": notInstPkg}, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("add: status %d body %s", rec.Code, rec.Body.String())
	}
	var op pluginmgr.Operation
	decodeResult(t, rec, &op)
	if op.ID != "op-new" || op.Kind != pluginmgr.OpAdd || op.Package != notInstPkg {
		t.Errorf("op = %+v", op)
	}
	if got := stub.startedOps(); len(got) != 1 || got[0] != "add "+notInstPkg {
		t.Errorf("started = %v", got)
	}

	for name, path := range map[string]string{
		"empty":           "",
		"json-schema":     pluginmgr.JSONSchemaPackage,
		"installed":       duckdnsPkg,
		"installed-subpk": customSub,
	} {
		rec := doRequest(h.AddPlugin(), http.MethodPost, "/caddy/plugins", map[string]string{"path": path}, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, rec.Code)
		}
	}
	if got := stub.startedOps(); len(got) != 1 {
		t.Errorf("rejected requests started operations: %v", got)
	}

	stub.busy = true
	rec = doRequest(h.AddPlugin(), http.MethodPost, "/caddy/plugins", map[string]string{"path": notInstPkg}, nil)
	if rec.Code != http.StatusConflict {
		t.Errorf("busy: status %d, want 409", rec.Code)
	}
}

func TestRemovePlugin(t *testing.T) {
	stub := newStubPlugins()
	h := newPluginTestHandler(t, stub)

	rec := doRequest(h.RemovePlugin(), http.MethodDelete, "/caddy/plugins?path="+customPkg, nil, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("remove: status %d body %s", rec.Code, rec.Body.String())
	}
	if got := stub.startedOps(); len(got) != 1 || got[0] != "remove "+customPkg {
		t.Errorf("started = %v", got)
	}

	for name, path := range map[string]string{
		"empty":         "",
		"json-schema":   pluginmgr.JSONSchemaPackage,
		"not installed": notInstPkg,
	} {
		rec := doRequest(h.RemovePlugin(), http.MethodDelete, "/caddy/plugins?path="+path, nil, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, rec.Code)
		}
	}

	stub.busy = true
	rec = doRequest(h.RemovePlugin(), http.MethodDelete, "/caddy/plugins?path="+customPkg, nil, nil)
	if rec.Code != http.StatusConflict {
		t.Errorf("busy: status %d, want 409", rec.Code)
	}
}

func TestRemovePlugin_InUse(t *testing.T) {
	stub := newStubPlugins()
	h := newPluginTestHandler(t, stub)
	prov := database.DNSProvider{Name: "home", ModuleID: duckdnsMod}
	if err := h.DB.Create(&prov).Error; err != nil {
		t.Fatal(err)
	}
	host := database.Host{Domains: "a.example.com"}
	if err := h.DB.Create(&host).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.DB.Create(&database.HostPlugin{HostID: host.ID, ModuleID: customMod, Handler: database.JSON(`{}`)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.DB.Create(&database.ModuleConfig{ModuleID: customMod, Path: "apps/x", Config: database.JSON(`{}`)}).Error; err != nil {
		t.Fatal(err)
	}

	rec := doRequest(h.RemovePlugin(), http.MethodDelete, "/caddy/plugins?path="+duckdnsPkg, nil, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	_, raw := errorBody(t, rec.Body.Bytes())
	var invalid []struct {
		Kind string `json:"kind"`
		ID   uint   `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &invalid); err != nil {
		t.Fatalf("decode invalid: %v (%s)", err, raw)
	}
	if len(invalid) != 1 || invalid[0].Kind != "dnsProvider" || invalid[0].ID != prov.ID || invalid[0].Name != "home" {
		t.Errorf("invalid = %+v", invalid)
	}

	rec = doRequest(h.RemovePlugin(), http.MethodDelete, "/caddy/plugins?path="+customPkg, nil, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("custom: status %d, want 409", rec.Code)
	}
	_, raw = errorBody(t, rec.Body.Bytes())
	invalid = nil
	_ = json.Unmarshal(raw, &invalid)
	kinds := map[string]string{}
	for _, i := range invalid {
		kinds[i.Kind] = i.Name
	}
	if len(invalid) != 2 || kinds["hostPlugin"] != "a.example.com" || kinds["moduleConfig"] != customMod {
		t.Errorf("invalid = %+v", invalid)
	}
	if got := stub.startedOps(); len(got) != 0 {
		t.Errorf("blocked removes started operations: %v", got)
	}
}

func TestUpdatePlugins(t *testing.T) {
	stub := newStubPlugins()
	h := newPluginTestHandler(t, stub)

	rec := doRequest(h.UpdatePlugins(), http.MethodPost, "/caddy/plugins/update", nil, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("no update: status %d, want 400", rec.Code)
	}

	stub.managed, stub.shipped, stub.available = "v2.10.0", "v2.11.0", true
	rec = doRequest(h.UpdatePlugins(), http.MethodPost, "/caddy/plugins/update", nil, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("update: status %d body %s", rec.Code, rec.Body.String())
	}
	if got := stub.startedOps(); len(got) != 1 || got[0] != "update " {
		t.Errorf("started = %v", got)
	}

	stub.busy = true
	rec = doRequest(h.UpdatePlugins(), http.MethodPost, "/caddy/plugins/update", nil, nil)
	if rec.Code != http.StatusConflict {
		t.Errorf("busy: status %d, want 409", rec.Code)
	}
}

func TestGetPluginOperation(t *testing.T) {
	stub := newStubPlugins()
	stub.ops["abc"] = &pluginmgr.Operation{ID: "abc", Kind: pluginmgr.OpRemove, Step: pluginmgr.StepDone}
	h := newPluginTestHandler(t, stub)

	rec := doRequest(h.GetPluginOperation(), http.MethodGet, "/caddy/plugins/operations/abc", nil, map[string]string{"id": "abc"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var op pluginmgr.Operation
	decodeResult(t, rec, &op)
	if op.ID != "abc" || op.Step != pluginmgr.StepDone {
		t.Errorf("op = %+v", op)
	}

	rec = doRequest(h.GetPluginOperation(), http.MethodGet, "/caddy/plugins/operations/nope", nil, map[string]string{"id": "nope"})
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown: status %d, want 404", rec.Code)
	}
}

func TestSetPluginSettings(t *testing.T) {
	h := newPluginTestHandler(t, newStubPlugins())

	rec := doRequest(h.SetPluginSettings(), http.MethodPut, "/caddy/plugins/settings", map[string]bool{"autoUpdate": true}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if got := database.GetSetting(h.DB, pluginmgr.SettingAutoUpdate, ""); got != "true" {
		t.Errorf("setting = %q, want true", got)
	}
	if !getPlugins(t, h).AutoUpdate {
		t.Error("list autoUpdate = false after enabling")
	}

	rec = doRequest(h.SetPluginSettings(), http.MethodPut, "/caddy/plugins/settings", map[string]bool{"autoUpdate": false}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if got := database.GetSetting(h.DB, pluginmgr.SettingAutoUpdate, ""); got != "false" {
		t.Errorf("setting = %q, want false", got)
	}

	rec = doRequest(h.SetPluginSettings(), http.MethodPut, "/caddy/plugins/settings", nil, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty body: status %d, want 400", rec.Code)
	}
}

func TestSwappableResolver(t *testing.T) {
	sw := NewSwappableResolver(stubResolver{err: errors.New("old")})
	if _, err := sw.Providers(context.Background()); err == nil || err.Error() != "old" {
		t.Fatalf("err = %v", err)
	}
	sw.Set(stubResolver{err: errors.New("new")})
	if _, err := sw.Providers(context.Background()); err == nil || err.Error() != "new" {
		t.Fatalf("err after Set = %v", err)
	}
}

func TestGetPlugins_LastUpdate(t *testing.T) {
	stub := newStubPlugins()
	h := newPluginTestHandler(t, stub)
	if v := getPlugins(t, h); v.LastUpdate != nil {
		t.Errorf("lastUpdate = %+v, want none", v.LastUpdate)
	}
	rec := doRequest(h.GetPlugins(), http.MethodGet, "/caddy/plugins", nil, nil)
	if strings.Contains(rec.Body.String(), "lastUpdate") {
		t.Errorf("lastUpdate should be omitted: %s", rec.Body.String())
	}

	finished := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	stub.lastFinished = map[pluginmgr.OpKind]*pluginmgr.Operation{
		pluginmgr.OpUpdate: {ID: "u1", Kind: pluginmgr.OpUpdate, Step: pluginmgr.StepRolledBack, Error: "caddy did not answer", FinishedAt: finished},
	}
	v := getPlugins(t, h)
	if v.LastUpdate == nil || v.LastUpdate.Step != pluginmgr.StepRolledBack || v.LastUpdate.Error != "caddy did not answer" || !v.LastUpdate.FinishedAt.Equal(finished) {
		t.Errorf("failed lastUpdate = %+v", v.LastUpdate)
	}

	stub.lastFinished[pluginmgr.OpUpdate] = &pluginmgr.Operation{ID: "u2", Kind: pluginmgr.OpUpdate, Step: pluginmgr.StepDone, FinishedAt: finished}
	v = getPlugins(t, h)
	if v.LastUpdate == nil || v.LastUpdate.Step != pluginmgr.StepDone || v.LastUpdate.Error != "" {
		t.Errorf("done lastUpdate = %+v", v.LastUpdate)
	}
}
