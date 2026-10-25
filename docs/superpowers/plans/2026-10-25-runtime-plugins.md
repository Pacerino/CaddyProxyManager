# Runtime Plugin Install Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Users add or remove any Caddy plugin from the Plugins page; CPM fetches a matching Caddy binary from caddyserver.com, verifies it, switches to it with automatic rollback, in Docker and with systemd.

**Architecture:** A new `pluginmgr` package owns the build-service client, binary inspection, an on-disk binary store and the operation manager (verify → switch → reconcile → rollback). Restarting Caddy is behind a `Restarter` interface with two implementations: a supervisor that runs Caddy as CPM's child process (Docker) and a systemd D-Bus restarter. HTTP handlers expose installed plugins, the catalog, operations and status; the React Plugins page gets an installed list, an add dialog and progress.

**Tech Stack:** Go 1.25 (chi, GORM, glebarez/sqlite, go-systemd dbus), React 18 + TypeScript + shadcn/ui, vitest, Docker.

**Spec:** `docs/superpowers/specs/2026-10-25-runtime-plugin-install-design.md`

Branch: `feature/runtime-plugins` (stacked on `feature/dns-providers`).

## Facts established while planning

- `caddy list-modules --json` returns, per module, `{"module_name":"dns.providers.duckdns","module_type":"non-standard","version":"v0.5.0","package_url":"github.com/caddy-dns/duckdns"}`; standard modules have `module_type":"standard"` and `package_url":"github.com/caddyserver/caddy/v2"`. The installed set = non-standard modules grouped by `package_url`, with that version.
- Build service: `GET https://caddyserver.com/api/packages` → `{"status_code":200,"result":[{"path","repo","modules":[{"name",...}],"downloads","published"},...]}` (~330 entries). `GET https://caddyserver.com/api/download?os=linux&arch=amd64&version=v2.11.4&p=github.com/caddy-dns/duckdns@v0.5.0&p=...` → 200 `application/octet-stream`; unknown package → 400 `{"status_code":400,"error":{"message":"... is not a registered Caddy module package path"}}`; unknown version → 400 `"unable to fulfill download request"`.
- `internal/caddy/reload.go` `reloadSystemd` calls `logger.Error`, which calls `os.Exit(1)`. Never use `logger.Error` for non-fatal paths (use `logger.Warn`).
- A local image with 25+ plugins exists for fixture capture: `cpm-local:dns` (if missing, build per Task 9 Step 3).

## Global Constraints

- Commits authored `Pacerino <public@pa0x43.me>`; no `Co-Authored-By` trailers, no AI attribution anywhere. Message prefixes `[Backend]`, `[Frontend]`, `[Docker]`, `[CI]`, `[Docs]`.
- Plugin features require `CPM_CADDY_MODE=api`.
- Plugin install on by default; `CPM_PLUGIN_INSTALL=false` disables it (installed list read-only with the reason).
- `github.com/abiosoft/caddy-json-schema` is always part of every build request, hidden from the installed list, not removable.
- Existing plugins are always requested pinned (`path@version`); the newly added one unpinned (latest). Caddy version = the running binary's version; no fallback.
- Build download timeout 10 minutes; Caddy must answer the admin API within 30 s after a switch.
- Success after a switch = admin API answers and the reconcile returns no errors (zero hosts: admin API answering suffices). Any reconcile error → rollback.
- Binaries live in `<CPM_DATAFOLDER>/caddy-bin/` as `caddy-<sha12>` plus `current` / `previous` symlinks; keep only those two.
- One operation at a time; a concurrent request gets HTTP 409.
- Never write to the system Caddy binary path.
- Backend verification: `cd backend && go vet ./... && go test -count=1 ./...`. Frontend: `cd frontend && npm run test && npm run build`.

## Review Focus

1. **Download succeeds but the binary is broken or lacks a requested plugin** (wrong arch, truncated file, build service returns an older build). Expected: verification fails, nothing switches, the error names the failed check. Test in Task 6.
2. **Caddy restarts on the new binary but some hosts fail to re-apply** (a removed plugin still used by a hand-written config). Expected: rollback to previous, hosts back, error shown. Test in Task 6.
3. **CPM process restarted mid-operation** (container killed while downloading). Expected: on start, CPM runs whatever `current` points to; no half-written file is ever `current`; stale temp files are cleaned. Test in Task 3.
4. **Caddy crashes on its own** (e.g. the proxy-loop crash seen in testing). Expected: the supervisor restarts it with backoff and re-runs the reconcile instead of leaving the proxy dead. Test in Task 4.
5. **Build service unreachable or slow** (no internet, 5xx). Expected: catalog shows "unavailable" with the reason, installs fail fast with a clear message, the UI never hangs. Test in Task 1 and Task 7.

---

### Task 1: Build-service client and tested list

**Files:**
- Create: `backend/internal/caddy/pluginmgr/buildservice.go`, `buildservice_test.go`
- Create: `backend/internal/caddy/pluginmgr/tested.go`, `tested_test.go`

**Interfaces:**
- Produces:
  - `type Package struct { Path string \`json:"path"\`; Repo string \`json:"repo"\`; Modules []string \`json:"modules"\` }`
  - `type PinnedPackage struct { Path, Version string }` (`Version` empty = latest); `func (p PinnedPackage) Param() string` → `path` or `path@version`.
  - `type BuildRequest struct { OS, Arch, CaddyVersion string; Packages []PinnedPackage }`
  - `type BuildService struct { BaseURL string; Client *http.Client }`; `func NewBuildService() *BuildService` (BaseURL `https://caddyserver.com`, client timeout 10 min).
  - `func (s *BuildService) Packages(ctx context.Context) ([]Package, error)`: caches the result for 5 minutes in memory (mutex-guarded); errors are never cached.
  - `func (s *BuildService) Download(ctx context.Context, req BuildRequest, w io.Writer) error`: on non-200, returns `*BuildError{Status int, Message string}` with the API's `error.message`.
  - `const JSONSchemaPackage = "github.com/abiosoft/caddy-json-schema"`; `var TestedPackages []string` (the 25 `github.com/caddy-dns/...` paths from `.github/workflows/ci.yml` plus `JSONSchemaPackage`); `func IsTested(path string) bool`.

- [ ] **Step 1: Failing tests** (httptest server):
  - `TestPackages_Parses`: fixture with 2 packages → paths, repos and module names mapped; second call within 5 min doesn't hit the server.
  - `TestPackages_ErrorNotCached`: first call 503 → error; second call 200 → result.
  - `TestDownload_BuildsQuery`: request URL has `os=linux&arch=arm64&version=v2.11.4` and `p` values `github.com/a/b@v1.2.3` and `github.com/c/d` in order; body is streamed to the writer.
  - `TestDownload_BuildError`: 400 with `{"status_code":400,"error":{"message":"x is not a registered Caddy module package path"}}` → `*BuildError` with that message.
  - `TestDownload_ContextCancelled`: cancelled ctx → error, nothing written.
  - `TestTestedPackages_MatchCI`: parses `../../../../.github/workflows/ci.yml` (relative to the package dir) with `gopkg.in/yaml.v3` (add as test-only dependency) or a simple line scan for `github.com/caddy-dns/` lines inside the `dns` flavor; the set equals `TestedPackages` minus `JSONSchemaPackage`.
- [ ] **Step 2: Run** `cd backend && go test ./internal/caddy/pluginmgr/` → FAIL.
- [ ] **Step 3: Implement.** **Step 4: Run** the full backend verification → PASS.
- [ ] **Step 5: Commit** `[Backend] Add Caddy build-service client and tested plugin list`

### Task 2: Binary inspection

**Files:**
- Create: `backend/internal/caddy/pluginmgr/inspect.go`, `inspect_test.go`
- Create: `backend/internal/caddy/pluginmgr/testdata/list-modules.json` (captured: `docker run --rm --entrypoint caddy cpm-local:dns list-modules --json > testdata/list-modules.json`)

**Interfaces:**
- Consumes: `PinnedPackage`, `JSONSchemaPackage` (Task 1).
- Produces:
  - `type InstalledPackage struct { Path, Version string; Modules []string }`
  - `func ParseInstalled(listModulesJSON []byte) []InstalledPackage`: non-standard modules grouped by `package_url`, sorted by path; includes `JSONSchemaPackage` (callers hide it).
  - `type Inspector interface { Version(ctx context.Context, bin string) (string, error); Installed(ctx context.Context, bin string) ([]InstalledPackage, error); Validate(ctx context.Context, bin string, configJSON []byte) error }`
  - `type ExecInspector struct{}` implementing it: `<bin> version` (first field, e.g. `v2.11.4`), `<bin> list-modules --json`, `<bin> validate --config <tmpfile> --adapter ""` with the config written to a temp `.json` file. Each call has a 30 s timeout; errors include trimmed stderr.

- [ ] **Step 1: Failing tests:** `TestParseInstalled_Fixture` (the captured fixture yields `github.com/caddy-dns/duckdns` at its version with module `dns.providers.duckdns`, no `github.com/caddyserver/caddy/v2` entry, and `JSONSchemaPackage` present); `TestExecInspector_*` using a fake binary: write a small shell script into `t.TempDir()` that answers `version`, `list-modules --json` and `validate` (exit 1 with stderr when the config contains `"bad"`), and assert parsing, error propagation with stderr, and timeout handling.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** full backend verification → PASS.
- [ ] **Step 5: Commit** `[Backend] Inspect Caddy binaries (version, plugins, config validation)`

### Task 3: Binary store and active-binary resolution

**Files:**
- Create: `backend/internal/caddy/pluginmgr/store.go`, `store_test.go`
- Modify: `backend/internal/config/vars.go` (new fields), `backend/internal/caddy/modules.go` (use the active binary), `backend/internal/caddy/dnsschema/dnsschema.go` (schema file resolution)
- Create: `backend/internal/caddy/binary.go`, `binary_test.go`

**Interfaces:**
- Produces:
  - Config (`caddy` struct): `Supervise bool` (`CPM_CADDY_SUPERVISE`, default false); `PluginInstall bool` (`CPM_PLUGIN_INSTALL`, default true). Check how envconfig handles bool defaults in this repo and follow it.
  - `type Store struct { Dir string }`; `func NewStore(dataFolder string) *Store` (Dir = `<dataFolder>/caddy-bin`).
  - `func (s *Store) Put(r io.Reader) (path, sha256hex string, err error)`: writes to `Dir/.tmp-<random>`, fsyncs, chmod 0755, renames to `caddy-<sha12>`; never leaves a non-temp partial file.
  - `func (s *Store) Current() (string, bool)`, `func (s *Store) Previous() (string, bool)` (resolved symlink targets).
  - `func (s *Store) Promote(path string) error`: `previous` ← old `current` target, `current` ← path; symlinks replaced atomically (create temp symlink + rename).
  - `func (s *Store) RollBack() error`: `current` ← `previous` target, `previous` removed.
  - `func (s *Store) Clear() error`: removes the `current` and `previous` symlinks (the shipped binary becomes active again).
  - `func (s *Store) Prune() error`: deletes `caddy-*` files not targeted by `current`/`previous` and any `.tmp-*`.
  - package caddy: `func ActiveBinary() string` → `<datafolder>/caddy-bin/current` if it exists, else `config.Configuration.Caddy.Binary`; `ListModules` uses it (replace direct `config.Configuration.Caddy.Binary` use in `runListModules`).
  - dnsschema: `NewResolver` uses `<datafolder>/caddy_schema.json` when it exists and its sidecar `<datafolder>/caddy_schema.sha256` equals the active binary's SHA-256; otherwise `config.Caddy.SchemaFile`. Export `func SchemaFileFor(dataFolder, activeSHA, shipped string) string` for testing.

- [ ] **Step 1: Failing tests:**
  - `TestStore_PutPromoteRollBack`: put A, promote A; put B, promote B → current=B, previous=A; RollBack → current=A, no previous.
  - `TestStore_PutAtomic` (Review Focus 3): a reader that errors midway → no `caddy-*` file created, temp removed.
  - `TestStore_Prune`: leftover `.tmp-x` and an orphan `caddy-old` are removed; current/previous kept.
  - `TestActiveBinary_PrefersManaged` / `_FallsBackToConfig`.
  - `TestSchemaFileFor`: matching sidecar → data-folder file; mismatch or missing → shipped path.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** full backend verification → PASS.
- [ ] **Step 5: Commit** `[Backend] Store managed Caddy binaries and resolve the active one`

### Task 4: Caddy supervisor (Docker)

**Files:**
- Create: `backend/internal/caddy/supervisor/supervisor.go`, `supervisor_test.go`

**Interfaces:**
- Produces:
  - `type Restarter interface { Restart(ctx context.Context) error }` — declare it in `pluginmgr` (Task 6 consumes it); the supervisor satisfies it.
  - `type Supervisor struct { Binary func() string; ConfigFile string; Env []string; OnRestart func(); Backoff func(attempt int) time.Duration }`
  - `func (s *Supervisor) Start(ctx context.Context) error`: runs `Binary() run --resume --config ConfigFile`, stdout/stderr copied to CPM's stdout/stderr; returns once started.
  - `func (s *Supervisor) Restart(ctx context.Context) error`: SIGTERM, wait up to 10 s (then SIGKILL), start again with the current `Binary()`.
  - `func (s *Supervisor) Stop() error`.
  - Unexpected exit: restart with `Backoff(attempt)` (default 1s doubling, capped 30s; attempt resets after 60 s of uptime), then call `OnRestart` (main wires it to enqueue the reconcile job).

- [ ] **Step 1: Failing tests** using a fake Caddy script (`sleep`-based, writes its PID and args to a file):
  - `TestSupervisor_StartPassesArgs`: args are `run --resume --config <file>`.
  - `TestSupervisor_RestartUsesCurrentBinary`: `Binary()` switched between calls → the second process is the new script.
  - `TestSupervisor_RestartsAfterCrash` (Review Focus 4): the fake exits by itself → restarted with zero backoff, `OnRestart` called once.
  - `TestSupervisor_StopNoRestart`: after `Stop`, no restart happens.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** full backend verification → PASS.
- [ ] **Step 5: Commit** `[Backend] Supervise Caddy as a child process`

### Task 5: systemd restarter and setup detection

**Files:**
- Create: `backend/internal/caddy/pluginmgr/systemd.go`, `systemd_test.go`
- Modify: `backend/internal/caddy/reload.go` (replace `logger.Error` with `logger.Warn` in `reloadSystemd`, `reloadExec`, `reloadAPI`)

**Interfaces:**
- Produces:
  - `type SystemdUnit interface { ExecStart(ctx context.Context, unit string) (path string, args []string, user string, err error); Restart(ctx context.Context, unit string) error }` with a D-Bus implementation (`dbus.NewSystemdConnectionContext`, `GetUnitTypePropertiesContext(ctx, unit, "Service")` reading `ExecStart` and `User`, `RestartUnitContext` waiting for the job result).
  - `type SetupState string` with `SetupSupervised = "supervised"`, `SetupReady = "systemd-ready"`, `SetupNeeded = "setup-needed"`, `SetupUnsupported = "unsupported"`.
  - `type SetupStatus struct { State SetupState \`json:"state"\`; Reason string \`json:"reason,omitempty"\`; DropIn string \`json:"dropIn,omitempty"\` }`
  - `func DetectSetup(ctx context.Context, cfg SetupConfig, unit SystemdUnit) SetupStatus` where `SetupConfig{ Supervise bool; ReloadStrategy, Service, ManagedPath string }`: supervise → supervised; strategy not `systemd` → unsupported (reason); D-Bus error → unsupported (reason); ExecStart path == ManagedPath and managed path readable+executable by the unit's `User` (owner/group/other bits via `os.Stat` + `user.Lookup`) → ready; otherwise setup-needed with the drop-in:
    ```
    [Service]
    ExecStart=
    ExecStart=<ManagedPath> <original args joined by spaces>
    ```
    and a reason (which check failed).
  - `type SystemdRestarter struct { Unit SystemdUnit; Service string }` satisfying `Restarter`.

- [ ] **Step 1: Failing tests** with a fake `SystemdUnit`: supervised; unsupported strategy; D-Bus error; ready; setup-needed with the exact drop-in text for `/usr/bin/caddy run --environ --config /etc/caddy/Caddyfile`; setup-needed when the binary isn't readable by the unit user; restarter calls `Restart` with the service name.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** full backend verification → PASS.
- [ ] **Step 5: Commit** `[Backend] Restart Caddy via systemd and detect plugin-install setup`

### Task 6: Operation manager (add / remove / update, verify, switch, rollback)

**Files:**
- Create: `backend/internal/caddy/pluginmgr/manager.go`, `manager_test.go`
- Modify: `backend/internal/database/models.go` (+ `sqlite.go` AutoMigrate, `handler/testmain_test.go` AutoMigrate)

**Interfaces:**
- Consumes: Tasks 1–5 (`BuildService`, `Inspector`, `Store`, `Restarter`), `reconcile.Run` signature from `internal/reconcile`.
- Produces:
  - Models: `Setting { Key string \`gorm:"primaryKey"\`; Value string }` with helpers `database.GetSetting(db, key, def string) string` / `SetSetting(db, key, value string) error`; key `"plugins.autoUpdate"` ("true"/"false").
  - `type OpKind string` (`"add"`, `"remove"`, `"update"`); `type OpStep string` (`"requesting"`, `"downloading"`, `"verifying"`, `"restarting"`, `"reapplying"`, `"done"`, `"rolledBack"`, `"failed"`).
  - `type Operation struct { ID string; Kind OpKind; Package string; Step OpStep; Error string; StartedAt, FinishedAt time.Time }` (JSON camelCase).
  - `type Manager struct { Builds *BuildService; Inspect Inspector; Store *Store; Restart Restarter; Ping func(ctx context.Context) error; LiveConfig func(ctx context.Context) ([]byte, error); Reconcile func(ctx context.Context) []error; ShippedBinary string; SchemaOut string; OS, Arch string }`
  - `func (m *Manager) Installed(ctx context.Context) ([]InstalledPackage, error)` (active binary, `JSONSchemaPackage` filtered out).
  - `func (m *Manager) Start(kind OpKind, pkg string) (*Operation, error)`: returns `ErrBusy` when an operation runs; runs asynchronously; `func (m *Manager) Get(id string) (*Operation, bool)` (keep the last 20 in memory).
  - `func (m *Manager) UpdateAvailable(ctx context.Context) (managed, shipped string, available bool, err error)`: compares `Inspect.Version` of the managed current vs `ShippedBinary` (semver compare, `v` prefix).
  - Flow of an operation (one function, steps set as it goes):
    1. compute the package set from `Installed` of the active binary: add → set + pkg (unpinned); remove → set − pkg; update → same set; always + `JSONSchemaPackage` pinned at its installed version; Caddy version = running (`update`: shipped binary's version).
    2. download to `Store.Put`;
    3. verify (Review Focus 1): `Version` equals the expected Caddy version; `Installed` contains every requested package path (and, for pinned ones, the same version); `Validate` against `LiveConfig(ctx)` (main wires it to `APIProvider.GetConfig("", &raw)`) succeeds. On failure: delete the file, step `failed` with the check's message, nothing switched.
    4. `Store.Promote`, `Restart`, `Ping` within 30 s, `Reconcile`: any error → `Store.RollBack` (first managed build: `Store.Clear()` so `ActiveBinary` falls back to shipped), `Restart`, `Reconcile`, step `rolledBack` with the error and up to 20 last Caddy log lines if available (left to the builder how to capture; fine to omit if not supervised). If the rollback restart fails too → `Store.Clear()`, restart on shipped, step `failed` with both errors.
    5. on success: `<bin> json-schema --output SchemaOut` + write the sha sidecar (failure here only logs a warning), `Store.Prune`, step `done`.
  - `func (m *Manager) AutoUpdateOnStartup(ctx context.Context, db *gorm.DB)`: if setting `plugins.autoUpdate` is "true" and an update is available, `Start(update)`.

- [ ] **Step 1: Failing tests** with fakes for every dependency (fake build service httptest serving fake-binary scripts from Task 2's helper, fake inspector, fake restarter, fake reconcile):
  - `TestAdd_Success`: request params contain existing packages pinned, the new one unpinned and `JSONSchemaPackage`; current promoted; schema regenerated; step `done`.
  - `TestRemove_Success`: removed package absent from the request.
  - `TestVerify_WrongVersion` / `_MissingPackage` / `_ValidateFails` (Review Focus 1): step `failed`, current unchanged, downloaded file gone.
  - `TestSwitch_ReconcileErrorRollsBack` (Review Focus 2): reconcile returns one error after the switch → current back to previous, restarter called twice, step `rolledBack`.
  - `TestSwitch_FirstBuildRollsBackToShipped`: no previous → `Store.Clear()` called, `ActiveBinary` is the shipped binary.
  - `TestSwitch_CaddyNeverAnswers`: ping fails for 30 s (inject a short timeout in the test) → rollback.
  - `TestBusy`: second `Start` while the first blocks → `ErrBusy`.
  - `TestBuildServiceDown` (Review Focus 5): 503 → step `failed` quickly with the message.
  - `TestUpdateAvailable` and `TestAutoUpdateOnStartup` (setting on/off).
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** full backend verification → PASS.
- [ ] **Step 5: Commit** `[Backend] Add plugin operation manager with verify, switch and rollback`

### Task 7: HTTP API and startup wiring

**Files:**
- Create: `backend/internal/api/handler/plugins.go`, `plugins_test.go`
- Modify: `backend/internal/api/handler/handler.go` (Handler gets `Plugins *pluginmgr.Manager` and `PluginSetup func(ctx) pluginmgr.SetupStatus`), `backend/internal/api/router.go`, `backend/cmd/main.go`

**Interfaces:**
- Consumes: Task 6 `Manager`, Task 5 `DetectSetup`, Task 4 `Supervisor`, `reconcile.Run`/`WaitForCaddy`, `jobqueue`.
- Produces (all under `/api/caddy/plugins`, behind `middleware.Enforce()`, API mode only via `apiProviderOrError`):
  - `GET /` → `{ installed: [{path, version, modules, tested}], installEnabled: bool, disabledReason?: string, setup: SetupStatus, update: {available, managed, shipped}, autoUpdate: bool, operation?: Operation (running one) }`. `installEnabled` = `PluginInstall` && setup ∈ {supervised, systemd-ready}.
  - `GET /catalog?q=` → `[{path, repo, modules, tested, installed}]` filtered by case-insensitive substring on path or module names, max 50; build-service failure → 503 with the message (Review Focus 5).
  - `POST /` body `{path}` → 202 `Operation`; 409 when busy; 400 when install disabled (reason), when `path` is `JSONSchemaPackage`, or already installed.
  - `DELETE /?path=` → 202; 409 busy; 400 not installed / json-schema; **409 with `invalid: [{kind, id, name}]`** listing usages when any module of the package is used by a `DNSProvider.ModuleID`, `HostPlugin.ModuleID` or `ModuleConfig.ModuleID`.
  - `POST /update` → 202 (400 when no update available).
  - `GET /operations/{id}` → `Operation` or 404.
  - `PUT /settings` body `{autoUpdate: bool}` → 200.
  - `main.go`: when `Supervise` is set (API mode), create the `Supervisor` (binary = `caddy.ActiveBinary`, config `/data/caddy.json` via a new `CPM_CADDY_CONFIGFILE` default `<datafolder>/caddy.json`), `Start` it before `startReconcile`, `OnRestart` enqueues the reconcile job; stop it on shutdown. Build the `Manager` with the supervisor or a `SystemdRestarter` (per setup), `Reconcile` wired to `reconcile.Run` via the job queue (wait for the job's result), then `AutoUpdateOnStartup` in a goroutine after the first reconcile.

- [ ] **Step 1: Failing tests** with a stub manager seam (define a small interface in the handler for the manager methods used): list shape incl. `tested` and hidden json-schema; catalog filter + 503; add 202/409/400 cases; remove blocked with usages (DNS provider using `dns.providers.duckdns`); install disabled by `PluginInstall=false`; Caddyfile mode 400; operations 404; settings persisted.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** full backend verification → PASS.
- [ ] **Step 5: Commit** `[Backend] Plugin install API and startup wiring`

### Task 8: Frontend

**Files:**
- Create: `frontend/src/components/InstalledPluginsSection.tsx`, `AddPluginDialog.tsx`, `PluginOperationProgress.tsx` (+ tests)
- Modify: `frontend/src/pages/Plugins.tsx`, `frontend/src/lib/types.ts`

**Interfaces:**
- Consumes: Task 7 endpoints.
- Produces:
  - `InstalledPluginsSection` on the Plugins page above the DNS providers section: table (path with repo link, version, modules, ✔ tested / ⚠ untested badge, Remove), "Add plugin" button; when `installEnabled` is false the buttons are hidden and `disabledReason` or the setup state is shown; for `setup-needed` show the reason and the drop-in text in a copyable `<pre>`. Update banner when `update.available`: "Caddy <shipped> is available (running <managed>)" with an **Update** button and an "Update automatically" checkbox bound to `PUT /settings`.
  - `AddPluginDialog`: search input (debounced 300 ms) → catalog; each row path, modules, tested badge; selecting an untested one shows the warning: "This plugin hasn't been tested with CPM. Its settings may only be editable as raw JSON, and it might fail to start. If Caddy doesn't come up with it, CPM switches back to your previous Caddy automatically." Confirm button "Install & restart Caddy" with the note "All hosts are briefly unavailable while Caddy restarts."
  - Remove confirm dialog with the same restart note; a 409 with usages lists them ("Used by DNS provider 'X'" …).
  - `PluginOperationProgress`: polls `GET /operations/{id}` every 1 s, shows the steps in order with the current one highlighted, final `done` / `rolledBack` / `failed` with the error; on finish refreshes the page data (installed list, DNS provider types).
- [ ] **Step 1: Failing tests:** untested warning shown only for untested; install posts `{path}` and opens progress; remove 409 renders usages; progress renders `rolledBack` with the error; setup-needed renders the drop-in; disabled state hides buttons.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** frontend verification → PASS.
- [ ] **Step 5: Commit** `[Frontend] Install and remove Caddy plugins from the Plugins page`

### Task 9: Docker image, docs and end-to-end check

**Files:**
- Modify: `docker/entrypoint.sh`, `Dockerfile`, `README.md`

- [ ] **Step 1: Entrypoint:** keep creating the default config; if `CPM_CADDY_SUPERVISE` is `true`, `exec /usr/local/bin/cpm` without starting Caddy; otherwise keep today's behaviour. Dockerfile `ENV`: `CPM_CADDY_SUPERVISE=true`, `XDG_CONFIG_HOME=/data/.config`, `XDG_DATA_HOME=/data/.local/share` (Caddy's autosave and **certificates** then persist in the volume; today certificates are lost on every container recreate — my call to fix here), `CPM_CADDY_CONFIGFILE=/data/caddy.json`.
- [ ] **Step 2: README:** "Installing plugins" section (any registered plugin, tested vs untested, restart note, rollback, `CPM_PLUGIN_INSTALL=false`, update banner + auto-update, systemd setup with the drop-in, internet requirement, `-dns` image as the offline option); env table rows for `CPM_PLUGIN_INSTALL`, `CPM_CADDY_SUPERVISE`, `CPM_CADDY_CONFIGFILE`.
- [ ] **Step 3: Manual check** (record outputs in the report): build `docker buildx build --load -t cpm-rt:test .` (default flavor); run with a volume; via the API add `github.com/mholt/caddy-ratelimit` (untested) → operation reaches `done`, `caddy list-modules` inside the container shows `http.handlers.rate_limit`, an existing host still proxies (whoami on a shared network as in earlier testing); `docker restart` → still on the managed binary; remove it → `done`; add a DNS provider plugin and see its form appear in `GET /api/caddy/dns-provider-types`; kill the Caddy child (`docker exec … kill <caddy pid>`) → supervisor restarts it and hosts come back. Clean up.
- [ ] **Step 4: Commit** `[Docker] Let CPM supervise Caddy and persist Caddy data` and `[Docs] Document installing plugins from the UI`
