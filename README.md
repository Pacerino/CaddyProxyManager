# Caddy Proxy Manager - CPM

## Shoutout
Much was copied from the [original Nginx Proxy Manager](https://github.com/NginxProxyManager/nginx-proxy-manager) and implemented for Caddy. The complete basic idea therefore goes back to the repo of [jc21](https://github.com/jc21) and so the honor goes to him! So please have a look at his repo and his Nginx Proxy Manager too!

<br>

## Idea
This project tries to implement the basic idea of the Nginx Proxy Manager for Caddy and thus provide a web interface for Caddy.

Currently the version is completely unstable and untidy.

Caddy is installed normally on the system and integrates further Caddyfiles via Caddyfile. So a hotreload and caddyfiles per host is possible.



## Current features
- Adding, editing and deleting hosts with multiple domains/upstreams
- Two ways to apply config to Caddy, selectable at runtime:
  - **Caddyfile mode** (`CPM_CADDY_MODE=caddyfile`, default): renders a per-host
    Caddyfile snippet and triggers a reload.
  - **API mode** (`CPM_CADDY_MODE=api`): manages Caddy's JSON config through the
    admin API, maintaining one route per host (no reload needed).
- Configurable authentication: local email/password or external OIDC/OAuth
  (`CPM_AUTH_MODE=local|oidc`) with JIT user provisioning.
- Modern SPA frontend (Vite + React + TypeScript + Tailwind + shadcn/ui),
  embedded in the binary by default or served from an external directory.

## Configuration

All options are environment variables prefixed with `CPM_`:

| Variable | Default | Description |
| --- | --- | --- |
| `CPM_CADDY_MODE` | `caddyfile` | `caddyfile` or `api` |
| `CPM_CADDY_ADMINURL` | `http://localhost:2019` | Caddy admin API base URL (api mode + api reload) |
| `CPM_CADDY_SERVERNAME` | `srv0` | http server name routes are managed under (api mode) |
| `CPM_CADDY_LISTEN` | `:80,:443` | listen addresses for the bootstrapped server (api mode); use e.g. `:8080` for local non-root testing |
| `CPM_CADDY_RELOADSTRATEGY` | `systemd` | `systemd`, `exec`, `api` or `none` (caddyfile mode) |
| `CPM_CADDY_BINARY` | `caddy` | caddy executable for the `exec` reload strategy |
| `CPM_CADDY_SERVICE` | `caddy.service` | systemd unit for the `systemd` reload strategy |
| `CPM_DATAFOLDER` | `/etc/caddy/` | data + per-host config folder |
| `CPM_LOGFOLDER` | `/var/log/caddy` | per-host log folder |
| `CPM_CADDYFILE` | `/etc/caddy/Caddyfile` | main Caddyfile (used by `exec`/`api` reload) |
| `CPM_FRONTENDDIR` | _(empty)_ | serve the frontend from this directory instead of the embedded assets |
| `CPM_AUTH_MODE` | `local` | `local` or `oidc` |
| `CPM_AUTH_OIDC_ISSUER` | _(empty)_ | OIDC issuer URL (e.g. Keycloak/Authelia) |
| `CPM_AUTH_OIDC_CLIENTID` | _(empty)_ | OIDC client id |
| `CPM_AUTH_OIDC_CLIENTSECRET` | _(empty)_ | OIDC client secret |
| `CPM_AUTH_OIDC_REDIRECTURL` | _(empty)_ | callback URL, e.g. `https://cpm.example.com/api/auth/oidc/callback` |
| `CPM_AUTH_OIDC_SCOPES` | `openid,profile,email` | requested scopes |
| `CPM_AUTH_OIDC_ALLOWEDDOMAINS` | _(empty)_ | optional email-domain allowlist for JIT provisioning |

## Planned features
- Logview
- Manage Plugins

## FAQ

> Can I use the Caddy admin API instead of writing a Caddyfile?

Yes. Set `CPM_CADDY_MODE=api` and CPM will manage Caddy's JSON config through the
admin API, keeping one route per host. The Caddyfile mode remains the default and
is the simplest setup, since features like HSTS, HTTP/2, SSL etc. come for free.

> Can I use external SSO (Authelia, Keycloak, etc.)?

Yes. Set `CPM_AUTH_MODE=oidc` and configure the `CPM_AUTH_OIDC_*` variables. On
first successful login CPM creates the matching user automatically (JIT). Use
`CPM_AUTH_OIDC_ALLOWEDDOMAINS` to restrict which email domains may sign in.

> How can I use CPM?

You have to compile CPM yourself. The frontend (Vite + React + TypeScript) is built from the ``frontend`` directory with ``npm install && npm run build``, which outputs straight into ``backend/embed/assets``. The backend is written in Go (1.24+) and can be compiled with ``go build ./cmd/main.go`` from the ``backend`` directory, producing a single binary with the frontend embedded. During development run ``npm run dev`` (Vite proxies ``/api`` to the Go backend on port 3001).

## Docker

A multi-stage `Dockerfile` builds the frontend, compiles the backend (CGO is
required by the SQLite driver) and produces a small Alpine runtime image that
bundles Caddy. The container entrypoint starts Caddy with its admin API and then
CPM in `api` mode.

```bash
docker build -t caddyproxymanager .
docker run -d --name cpm \
  -p 3001:3001 -p 80:80 -p 443:443 \
  -v cpm-data:/data \
  caddyproxymanager
```

Published images are available at `ghcr.io/pacerino/caddyproxymanager`.

## Development & testing

```bash
# Backend tests
cd backend && go test ./...

# Frontend type-check + build
cd frontend && npm run build
```

CI (GitHub Actions, `.github/workflows/ci.yml`) runs the backend tests with the
race detector, builds the frontend, and on pushes to `master` / version tags
builds and publishes the Docker image to GHCR.

## Contribution
If you want to help with the development pull requests etc. are welcome!