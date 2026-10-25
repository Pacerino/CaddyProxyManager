<div align="center">

# Caddy Proxy Manager

**A modern web interface for managing [Caddy](https://caddyserver.com/) reverse proxy hosts.**

Add, edit and delete proxy hosts from a clean UI — let Caddy handle TLS, HTTP/2, HSTS and automatic certificates for you.

[![CI](https://github.com/Pacerino/CaddyProxyManager/actions/workflows/ci.yml/badge.svg)](https://github.com/Pacerino/CaddyProxyManager/actions/workflows/ci.yml)
![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go&logoColor=white)
![React](https://img.shields.io/badge/React-18-61DAFB?logo=react&logoColor=black)
![Caddy](https://img.shields.io/badge/Caddy-2-1F88C0?logo=caddy&logoColor=white)
![License](https://img.shields.io/badge/license-MIT-green)

</div>

---

> [!WARNING]
> This project is under active development and not yet considered stable. Use at your own risk and review the configuration before exposing it publicly.

## ✨ Features

- **Host management** — create, edit and delete hosts with multiple domains and upstream backends.
- **Two ways to drive Caddy**, switchable at runtime:
  - 📝 **Caddyfile mode** *(default)* — renders a per-host Caddyfile snippet and reloads Caddy.
  - 🔌 **API mode** — manages Caddy's JSON config through the admin API, one route per host, no reload required.
- **Flexible authentication** — local email/password or external **OIDC/OAuth** (Authelia, Keycloak, Authentik, …) with just-in-time user provisioning.
- **Single binary** — a modern SPA (Vite · React · TypeScript · Tailwind · shadcn/ui) embedded directly in the Go binary, or served from an external directory.
- **Container-ready** — multi-stage Docker image that bundles Caddy and starts everything for you.

## 🚀 Quick start (Docker)

```bash
docker run -d --name cpm \
  -p 3001:3001 -p 80:80 -p 443:443 \
  -v cpm-data:/data \
  ghcr.io/pacerino/caddyproxymanager
```

Then open <http://localhost:3001> and sign in with the default credentials:

| Email | Password |
| --- | --- |
| `admin@example.com` | `changeme` |

> [!IMPORTANT]
> Change the defaults via `CPM_ADMIN_EMAIL` / `CPM_ADMIN_PASSWORD` on first run.

The container starts Caddy (with its admin API) and CPM in `api` mode. Mount your own Caddy config at `/data/caddy.json` to customise it.

### Image tags

Images are published for `linux/amd64` and `linux/arm64` in two flavors:

| Tag | Contents |
| --- | --- |
| `latest`, `<version>`, `<major>.<minor>` | Newest stable release with the stock Caddy build. |
| `latest-dns`, `<version>-dns`, … | Same, with Caddy built with 25 DNS providers for ACME DNS-01 challenges (see [DNS providers](#-dns-providers)). |

> [!NOTE]
> Images are only published for version tags. Pre-releases (e.g. `v3.2.0-rc1`) get their exact version tag but never move `latest`.

## 🏗️ How it works

```
┌──────────────┐      ┌──────────────────────┐      ┌─────────────────┐
│   Browser    │ ───► │  CPM (Go + embedded  │ ───► │      Caddy      │
│  (React SPA) │      │   React frontend)    │      │ (reverse proxy) │
└──────────────┘      └──────────┬───────────┘      └─────────────────┘
                                 │
                  Caddyfile mode │ writes host_<id>.conf + reload
                       API mode  │ PUT/PATCH/DELETE via admin API
```

CPM stores hosts in a local SQLite database and applies each change to Caddy using the selected provider.

## ⚙️ Configuration

All settings are environment variables prefixed with `CPM_`.

### Caddy

| Variable | Default | Description |
| --- | --- | --- |
| `CPM_CADDY_MODE` | `caddyfile` | Provider to use: `caddyfile` or `api`. |
| `CPM_CADDY_ADMINURL` | `http://localhost:2019` | Caddy admin API base URL (API mode + `api` reload). |
| `CPM_CADDY_SERVERNAME` | `srv0` | HTTP server name routes are managed under (API mode). |
| `CPM_CADDY_LISTEN` | `:80,:443` | Listen addresses for the bootstrapped server (API mode); use e.g. `:8080` for local non-root testing. |
| `CPM_CADDY_RELOADSTRATEGY` | `systemd` | Reload strategy (Caddyfile mode): `systemd`, `exec`, `api` or `none`. |
| `CPM_CADDY_BINARY` | `caddy` | Caddy executable for the `exec` reload strategy. |
| `CPM_CADDY_SERVICE` | `caddy.service` | systemd unit for the `systemd` reload strategy. |
| `CPM_CADDY_SUPERVISE` | `false` (`true` in the Docker image) | CPM starts and supervises the Caddy process itself (API mode); required for installing plugins in Docker. |
| `CPM_CADDY_CONFIGFILE` | `<CPM_DATAFOLDER>/caddy.json` | JSON config the supervised Caddy starts with. |
| `CPM_CADDYFILE` | `/etc/caddy/Caddyfile` | Main Caddyfile (used by `exec`/`api` reload). |
| `CPM_CADDY_SCHEMAFILE` | `/usr/share/cpm/caddy_schema.json` | Caddy JSON schema used to describe DNS provider fields (API mode). |

### Authentication

| Variable | Default | Description |
| --- | --- | --- |
| `CPM_AUTH_MODE` | `local` | `local` or `oidc`. |
| `CPM_AUTH_OIDC_ISSUER` | — | OIDC issuer URL (e.g. Keycloak/Authelia). |
| `CPM_AUTH_OIDC_CLIENTID` | — | OIDC client ID. |
| `CPM_AUTH_OIDC_CLIENTSECRET` | — | OIDC client secret. |
| `CPM_AUTH_OIDC_REDIRECTURL` | — | Callback URL, e.g. `https://cpm.example.com/api/auth/oidc/callback`. |
| `CPM_AUTH_OIDC_SCOPES` | `openid,profile,email` | Requested scopes. |
| `CPM_AUTH_OIDC_ALLOWEDDOMAINS` | — | Optional email-domain allowlist for JIT provisioning. |

### General

| Variable | Default | Description |
| --- | --- | --- |
| `CPM_DATAFOLDER` | `/etc/caddy/` | Data + per-host config folder. |
| `CPM_LOGFOLDER` | `/var/log/caddy` | Per-host log folder. |
| `CPM_PLUGIN_INSTALL` | `true` | Allow installing and removing Caddy plugins from the UI. Set to `false` to make the installed list read-only. |
| `CPM_FRONTENDDIR` | — | Serve the frontend from this directory instead of the embedded assets. |
| `CPM_ADMIN_EMAIL` | `admin@example.com` | Seed admin email (first run, local mode). |
| `CPM_ADMIN_PASSWORD` | `changeme` | Seed admin password (first run, local mode). |

## 🧑‍💻 Development

**Prerequisites:** Go 1.24+, Node 22+, and a local [Caddy](https://caddyserver.com/docs/install) for end-to-end testing.

Run the backend and frontend in two terminals:

```bash
# Terminal 1 — backend on :3001
cd backend
CPM_DATAFOLDER=./data CPM_LOGFOLDER=./data CPM_CADDY_RELOADSTRATEGY=none \
  go run ./cmd/main.go

# Terminal 2 — frontend dev server on :5173 (proxies /api → :3001)
cd frontend
npm install
npm run dev
```

Open <http://localhost:5173> and log in with `admin@example.com` / `changeme`.

### Building a single binary

```bash
cd frontend && npm run build      # emits into backend/embed/assets
cd ../backend && go build ./cmd/main.go
```

### Testing

```bash
cd backend && go test ./...       # backend
cd frontend && npm run test       # frontend unit tests
cd frontend && npm run build      # frontend type-check + build
```

CI ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)) runs on GitHub-hosted runners. It runs the backend and frontend tests, and builds the multi-arch Docker images (both flavors) on every PR and push to `master`. Nothing is published automatically: only pushing a version tag (`v*`) publishes the images to GHCR and creates a GitHub release with cross-compiled binaries.

## 🐳 Building the image yourself

```bash
docker build -t caddyproxymanager .
```

The multi-stage build compiles the frontend and backend (a pure-Go build, no C toolchain required) into a small Alpine runtime that bundles Caddy. Caddy is always built with [`xcaddy`](https://github.com/caddyserver/xcaddy), including [`caddy-json-schema`](https://github.com/abiosoft/caddy-json-schema), and the image ships the generated schema at `/usr/share/cpm/caddy_schema.json`. The build needs internet access (the schema generation fetches the Caddy docs).

To bundle Caddy plugins, pass their Go module paths (optionally pinned with `@version`) and they are added to the xcaddy build on top of `caddy-json-schema`:

```bash
docker build -t caddyproxymanager:custom \
  --build-arg CADDY_PLUGINS="github.com/caddy-dns/cloudflare github.com/mholt/caddy-ratelimit" .
```

`CADDY_VERSION` (default `2.11.4`) selects the Caddy release.

## ❓ FAQ

<details>
<summary><strong>Should I use Caddyfile mode or API mode?</strong></summary>

Use **Caddyfile mode** (the default) for the simplest setup — CPM writes snippets that Caddy imports, and you keep all of Caddy's conveniences (automatic HTTPS, HSTS, HTTP/2). Use **API mode** when you want CPM to manage Caddy's live JSON config directly through the admin API, with no reloads.
</details>

<details>
<summary><strong>Can I use external SSO (Authelia, Keycloak, Authentik …)?</strong></summary>

Yes. Set `CPM_AUTH_MODE=oidc` and configure the `CPM_AUTH_OIDC_*` variables. On first successful login CPM provisions the matching user automatically (JIT). Restrict access with `CPM_AUTH_OIDC_ALLOWEDDOMAINS`.
</details>

<details>
<summary><strong>Does CPM run Caddy for me?</strong></summary>

The Docker image does — CPM starts and supervises Caddy itself (restarting it if it crashes) and keeps its data in `/data`. For bare-metal installs, run Caddy yourself and point CPM at it (`CPM_CADDYFILE` / `CPM_CADDY_ADMINURL`) with the appropriate reload strategy.
</details>

## 🔐 DNS providers

CPM can obtain certificates through ACME DNS-01 challenges, e.g. for wildcard domains or hosts that are not reachable from the internet.

1. Open the **Plugins** page and save credentials for a DNS provider (Cloudflare, DuckDNS, Porkbun, or any other DNS module built into your Caddy).
2. Pick one of the saved providers when creating or editing a host.

Use Caddy `{env.VAR}` placeholders for secrets (e.g. `api_token = {env.DUCKDNS_TOKEN}`) and pass the variable to the container (`-e DUCKDNS_TOKEN=...`). The placeholder is stored in the database, not the secret itself, and API responses never contain secret values.

The `-dns` image flavor bundles these providers: [`cloudflare`](https://github.com/caddy-dns/cloudflare), [`duckdns`](https://github.com/caddy-dns/duckdns), [`porkbun`](https://github.com/caddy-dns/porkbun), [`route53`](https://github.com/caddy-dns/route53), [`digitalocean`](https://github.com/caddy-dns/digitalocean), [`hetzner`](https://github.com/caddy-dns/hetzner), [`ovh`](https://github.com/caddy-dns/ovh), [`namecheap`](https://github.com/caddy-dns/namecheap), [`gandi`](https://github.com/caddy-dns/gandi), [`desec`](https://github.com/caddy-dns/desec), [`netcup`](https://github.com/caddy-dns/netcup), [`ionos`](https://github.com/caddy-dns/ionos), [`linode`](https://github.com/caddy-dns/linode), [`godaddy`](https://github.com/caddy-dns/godaddy), [`rfc2136`](https://github.com/caddy-dns/rfc2136), [`acmedns`](https://github.com/caddy-dns/acmedns), [`bunny`](https://github.com/caddy-dns/bunny), [`inwx`](https://github.com/caddy-dns/inwx), [`azure`](https://github.com/caddy-dns/azure), [`googleclouddns`](https://github.com/caddy-dns/googleclouddns), [`namesilo`](https://github.com/caddy-dns/namesilo), [`powerdns`](https://github.com/caddy-dns/powerdns), [`transip`](https://github.com/caddy-dns/transip), [`alidns`](https://github.com/caddy-dns/alidns), [`he`](https://github.com/caddy-dns/he). For other providers, build your own image with `CADDY_PLUGINS` (see [Building the image yourself](#-building-the-image-yourself)).

> [!NOTE]
> Hosts using a DNS provider get a dedicated TLS policy that uses Let's Encrypt only; there is no ZeroSSL fallback for DNS-challenge hosts.

## 🧩 Installing plugins

On the **Plugins** page you can add or remove Caddy plugins without rebuilding the image. CPM asks the [Caddy download service](https://caddyserver.com/download) for a build containing the plugin, swaps Caddy over to it and rolls back automatically if Caddy does not come up healthy.

- **Any registered plugin works.** Search the catalog or enter a Go module path (e.g. `github.com/mholt/caddy-ratelimit`). Plugins from the tested list (the ones bundled in the `-dns` image) are marked as tested; everything else is marked untested, so use it at your own risk.
- **Restart note.** Switching binaries restarts Caddy, so there is a short interruption while the new build starts. Existing plugins stay pinned to their installed version; the new one is added at its latest version.
- **Rollback.** CPM keeps the previous binary. If the new build fails to start, or applying your hosts to it reports errors, CPM switches back automatically and shows the error.
- **Updates.** When a newer version of an installed plugin is available, the Plugins page shows an update banner and offers to update (it applies the same safe switch with rollback). A failed update is reported on the page.
- **Disabling.** Set `CPM_PLUGIN_INSTALL=false` to turn installing off; the installed list stays visible but read-only.
- **Internet access** is required to download builds. For offline setups use the `-dns` image flavor (or build your own with `CADDY_PLUGINS`) and keep plugin install disabled.
- **Requirements.** API mode (`CPM_CADDY_MODE=api`) is required. The Docker image works out of the box because CPM supervises Caddy itself (`CPM_CADDY_SUPERVISE=true`); Caddy's certificates and autosave are stored in the `/data` volume. Managed binaries live in `<CPM_DATAFOLDER>/caddy-bin/`.

### systemd setup

Without Docker, Caddy runs under systemd and CPM can only restart the service, so the unit must start CPM's managed binary. The Plugins page detects this and shows the exact drop-in to install; it looks like this (adjust the path to your `CPM_DATAFOLDER`, keep your original arguments):

```ini
# systemctl edit caddy.service
[Service]
ExecStart=
ExecStart=/etc/caddy/caddy-bin/current run --environ --config /etc/caddy/Caddyfile
```

Then run `systemctl daemon-reload && systemctl restart caddy`. The service user must be able to read the managed binary and CPM needs permission to restart the unit. Run CPM with `CPM_CADDY_RELOADSTRATEGY=systemd` (the default).

## ⚠️ Limitations

- Plugins and DNS providers require `CPM_CADDY_MODE=api`. In Caddyfile mode the plugin and DNS provider endpoints return `400`, and creating or updating a host with a DNS provider is rejected with `400` ("DNS providers are only available when CPM_CADDY_MODE=api"). Provider choices saved earlier in API mode are kept but ignored, and the Hosts page flags them.

## 🗺️ Roadmap

- [ ] Log viewer
- [ ] Plugin management
- [ ] Per-host advanced directives

## 🙌 Acknowledgements

Inspired by the excellent [Nginx Proxy Manager](https://github.com/NginxProxyManager/nginx-proxy-manager) by [jc21](https://github.com/jc21) — go check it out. CPM brings the same idea to Caddy.

## 🤝 Contributing

Pull requests and issues are welcome! Please open an issue to discuss larger changes first.

## 📄 License

[MIT](LICENSE)
