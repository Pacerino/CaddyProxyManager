# Installing Caddy plugins from the CPM UI

Date: 2026-10-25
Status: awaiting approval
Builds on: `2026-10-25-dns-provider-plugins-design.md` (generated plugin forms, startup reconcile)

## Intent

CPM is a public project. Its users run the published image (or the release
binary next to a system Caddy) and should be able to add a Caddy plugin, such
as a DNS provider or a rate limiter, from the Plugins page, without building
images, installing Go or running `xcaddy`. Today the only options are the
`-dns` image (25 bundled DNS providers) or a self-built image with
`CADDY_PLUGINS`.

## Goals

- Add or remove one Caddy plugin at a time from the Plugins page; each change
  produces a new Caddy binary and a Caddy restart (flow A, chosen with mockups).
- Any plugin registered with Caddy's build service is installable. Plugins not
  on CPM's tested list carry a visible warning before install.
- Works in the Docker image and in non-Docker installs.
- A failed install never leaves the proxy down: CPM verifies the new binary
  before switching and rolls back automatically if Caddy doesn't come up.
- After a change, the plugin list and generated forms reflect the new binary.

## Non-goals

- Batching several changes into one build (flow B was rejected).
- Upgrading or downgrading Caddy itself; installs keep the running Caddy version.
- Air-gapped installs. Without internet the feature is unavailable; the
  `-dns` image remains the offline option.
- Building binaries locally (no Go toolchain or `xcaddy` in the image).
- Managing plugins of a Caddy that CPM doesn't control (remote admin API).

## Anti-goals

- Silently breaking routing: a change that would leave hosts down, or a
  restart the user didn't confirm.
- Removing a plugin that saved config still uses without the user seeing
  what will stop working.
- Running a downloaded binary as the live Caddy before it passed verification.
- Overwriting a package-managed system binary (e.g. `/usr/bin/caddy` from apt).

## Constraints

- Plugin features require `CPM_CADDY_MODE=api`, like the rest of the plugin UI.
- Plugin install is **on by default**; `CPM_PLUGIN_INSTALL=false` turns it off
  (the UI then shows the installed list read-only with that reason). CPM has no
  roles, so any logged-in user can install while it's on (decided by the
  maintainer).
- Builds come from caddyserver.com:
  - `GET https://caddyserver.com/api/packages` lists ~330 registered packages
    with their module IDs.
  - `GET https://caddyserver.com/api/download?os=<goos>&arch=<goarch>&version=<caddy version>&p=<pkg>&p=<pkg>…`
    returns the binary (`application/octet-stream`); an unregistered package
    returns 400 with `{"status_code":400,"error":{"message":"… is not a registered Caddy module package path"}}`.
    Downloads carry no checksum or signature, so trust rests on HTTPS plus
    verification by running the binary.
  - Builds take from seconds to a few minutes; requests need a generous timeout
    (my call: 10 minutes) and progress feedback.
- Every requested build always includes `github.com/abiosoft/caddy-json-schema`,
  so the generated-forms feature keeps working.
- Restarting Caddy interrupts all hosts for a few seconds; the confirm dialog
  says so.
- Commits authored `Pacerino <public@pa0x43.me>`, no AI attribution.

## Decisions

### 1. What a "build" is

The installed plugin set is the list of Go package paths in the current build.
CPM records it in the database together with the Caddy version, OS/arch and
the binary's SHA-256. On first use, CPM derives the set from the running
binary (`caddy list-modules --packages`, or `list-modules --json` package
URLs), excluding standard Caddy packages. That way the `-dns` image's 25
providers show up as installed, and removing one of them works the same as
removing a plugin installed from the UI (my call).

Each package is recorded with its version from
`caddy list-modules --packages --versions` (e.g. `dns.providers.duckdns v0.5.0
github.com/caddy-dns/duckdns`). Adding plugin P requests a build of
`current set (each pinned as pkg@version) + P (latest)`; removing P requests
`current set − P`, still pinned. The build service accepts `p=pkg@version`
(verified: `duckdns@v0.5.0` → 200; a non-existent version → 400 "unable to
fulfill download request"). So a rebuild never silently upgrades other
plugins. The installed list shows versions. The Caddy version is the running
one (`caddy version`); if the build service can't build that exact version the
operation fails with that message (my call; no fallback to another version).
`caddy-json-schema` is always included, hidden from the installed list and
not removable (my call).

### 2. Where binaries live and how the switch works

- Binaries are stored under `<CPM_DATAFOLDER>/caddy-bin/`: `caddy-<sha12>` per
  build, plus `current` and `previous` symlinks. At most the current and
  previous binaries are kept (my call).
- Verification before switching, all against the downloaded file:
  1. it runs: `<new> version` succeeds and reports the expected Caddy version;
  2. `<new> list-modules --json` contains the modules of every requested
     package and of `caddy-json-schema`;
  3. `<new> validate --config <current live config as JSON>` succeeds, so
     the current config (hosts, TLS policies, module configs) loads on it.
     This catches removing a plugin the config still uses.
- Switch: point `current` at the new binary, keep the old one as `previous`,
  restart Caddy (see 3), wait for the admin API, run the existing startup
  reconcile. Success means: the admin API answers within 30 s and the
  reconcile returns no errors (with zero hosts, the admin API answering is
  enough). Any reconcile error counts as failure, so a partially working
  proxy is rolled back too (my call). Roll back: `current` → `previous`
  (for the first managed build, `previous` is the shipped binary), restart,
  reconcile, and report the failure with Caddy's last log lines. If the
  rollback itself fails, CPM falls back to the shipped binary and surfaces a
  persistent error banner (my call).
- After a successful switch, regenerate the schema file with
  `<new> json-schema --output <CPM_DATAFOLDER>/caddy_schema.json` (needs
  internet, which the download already required), and point the resolver at
  it. `CPM_CADDY_SCHEMAFILE` keeps naming the shipped file; the data-folder
  file, when present and matching the current binary's SHA, wins (my call).
- `CPM_CADDY_BINARY` resolution: if `<datafolder>/caddy-bin/current` exists it
  is used for `list-modules`, `json-schema` and (in Docker) for running Caddy;
  otherwise the configured binary, as today.

### 3. Restarting Caddy

- **Docker:** CPM supervises Caddy itself instead of `docker/entrypoint.sh`.
  The entrypoint keeps creating the default `caddy.json` and then execs `cpm`;
  CPM starts `caddy run --resume --config <file>` with the managed or shipped
  binary as a child process (`XDG_CONFIG_HOME` under `/data` so Caddy's
  autosave survives container restarts; `--resume` loads the last applied
  config immediately and the reconcile then corrects any drift, which shortens
  the post-restart gap; my call), forwards its output to the container log, restarts it on
  demand, and stops it on shutdown. If Caddy exits unexpectedly, CPM restarts
  it with backoff and re-runs the reconcile (my call). Enabled by
  `CPM_CADDY_SUPERVISE=true`, set in the image. When it's unset the entrypoint
  keeps today's behaviour (starts Caddy itself), and plugin install is
  unavailable (my call).
- **Non-Docker (systemd):** CPM never writes to the system binary path. The
  Plugins page shows a one-time setup until it detects that the Caddy service
  runs the managed binary: a systemd drop-in that changes the unit's
  `ExecStart` to `<CPM_DATAFOLDER>/caddy-bin/current run --environ --config …`,
  plus making the binary directory readable by the caddy user. CPM writes the
  suggested drop-in text for the user to install; it doesn't install it itself
  (needs root). The drop-in keeps the unit's current arguments: CPM reads
  the service's `ExecStart` over D-Bus and only replaces the executable path
  (my call). Detection: that `ExecStart` points at the managed path. If CPM
  can't read the unit, or the managed binary isn't readable/executable by the
  service's `User=` (e.g. the data folder sits under a protected home), the
  page shows "setup needed" with the specific reason (my call). Restarts use the existing
  systemd D-Bus mechanism (`RestartUnit` instead of reload).
- **Other setups** (non-Docker without systemd, `exec`/`none` reload
  strategy, or Caddy not managed by CPM): install/remove is disabled with an
  explanation.

### 4. CPM image upgrades (newer Caddy than the managed binary)

At startup CPM compares the shipped binary's Caddy version with the managed
`current` binary's. If the shipped one is newer, CPM keeps running the managed
binary and shows a warning on the Plugins page with an **Update** button that
rebuilds the current plugin set (pinned versions) on the shipped Caddy
version, through the same verify/switch/rollback flow. A setting
"Automatically update plugins when CPM ships a newer Caddy" (stored in the DB,
**default off**) makes CPM start that rebuild by itself after startup; if it
fails, the managed binary stays and the warning shows the error (decided by
the maintainer).

### 5. Removing a plugin that's in use

Before removal the UI lists what depends on the plugin's modules: DNS
provider credentials (and their hosts), host plugins and module configs. If
anything does, removal is blocked with that list (my call; the user removes
the usage first). The `validate` step in decision 2 is the backstop for usage
CPM doesn't track (e.g. hand-written `caddy.json`).

### 6. Tested plugins

`backend/internal/caddy/plugins/tested.go` (or similar) lists package paths
known to work with CPM: the 25 DNS providers in the `-dns` image and
`caddy-json-schema`. Every package bundled in an image must be registered with
the build service, otherwise it couldn't survive a rebuild; `caddy-dns/dynv6`
is not registered and is dropped from the `-dns` image for that reason
(decided by the maintainer). Everything else is "untested". The add dialog shows
untested plugins with the warning: the form may only be a raw JSON editor,
the plugin may fail to start, and CPM will roll back automatically if it does.

### 7. UI (flow A)

- **Plugins page:** "Installed plugins" table (package path, modules, tested
  badge, Remove) and an "Add plugin" button. Cloudflare & co. from the `-dns`
  image appear here too.
- **Add dialog:** search over the packages API result (cached in memory for a
  few minutes; my call), each entry with modules, repo link and tested or
  untested badge; untested shows the warning above. Confirm button: "Install &
  restart Caddy". The confirm text says all hosts are briefly interrupted.
- **Progress:** one operation at a time (a second request gets 409). The
  dialog shows the steps (requesting build → downloading → verifying →
  restarting → re-applying hosts → done / rolled back) by polling an
  operation-status endpoint. Closing the dialog doesn't cancel it.
- **Unavailable states:** Caddyfile mode; no internet (packages API
  unreachable); non-Docker without the systemd setup (shows setup
  instructions); unsupported setup. Each state shows a one-line reason.

### 8. API (shape left to the builder, following `/api/caddy/...`)

List installed plugins, search the catalog, start add/remove (returns an
operation ID), get operation status, the Caddy-update status/trigger and
the auto-update setting, and the setup status (supervised Docker
/ systemd ready / setup needed / unsupported, with the drop-in text when
setup is needed).

## Testing expectations

- Unit tests with a fake build service (httptest) and fake binaries (small
  scripts or a test helper binary) for: set computation, verification failure
  at each step, a successful switch, rollback when Caddy doesn't come up,
  pruning of old binaries, and the 409 on concurrent operations.
- Supervisor tests: restart on demand, restart with backoff after an
  unexpected exit, clean stop.
- Handler tests: removal blocked when in use; unavailable states.
- Frontend tests: add dialog with untested warning, progress states,
  blocked-removal message.
- Manual: in the Docker image, add `github.com/mholt/caddy-ratelimit`
  (untested), confirm it appears and hosts keep working; remove it; force a
  failing build (e.g. kill Caddy start via a bogus plugin) and see the
  rollback.

## Left to the builder

- Exact endpoint routes and response shapes.
- How the supervisor plumbs Caddy's stdout/stderr into CPM's logging.
- Polling interval and operation-status persistence (in memory is fine).
- Exact wording of warnings and setup instructions.
