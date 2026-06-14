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
| `CPM_CADDYFILE` | `/etc/caddy/Caddyfile` | Main Caddyfile (used by `exec`/`api` reload). |

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
cd backend && go test ./...       # backend (run with -race in CI)
cd frontend && npm run build      # frontend type-check + build
```

CI ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)) runs the backend tests with the race detector, builds the frontend, and publishes the Docker image to GHCR on pushes to `master` and version tags.

## 🐳 Building the image yourself

```bash
docker build -t caddyproxymanager .
```

The multi-stage build compiles the frontend and backend (CGO is required by the SQLite driver) into a small Alpine runtime that bundles Caddy.

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

The Docker image does — its entrypoint launches Caddy with the admin API and then CPM. For bare-metal installs, run Caddy yourself and point CPM at it (`CPM_CADDYFILE` / `CPM_CADDY_ADMINURL`) with the appropriate reload strategy.
</details>

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
