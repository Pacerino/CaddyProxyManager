# DNS providers: generated config forms and per-host selection

Date: 2026-10-25
Status: awaiting approval

## Intent

Adding a DNS provider plugin to the Caddy build (the `-dns` image, or a custom
build via `CADDY_PLUGINS`) should be enough to use it from CPM. Today every
plugin needs a hand-written Go schema (`backend/internal/caddy/schema/`) before
it gets a form; only Cloudflare has one, so DuckDNS and Porkbun from the `-dns`
image fall back to raw JSON. And a configured DNS provider applies globally, so
all certificates must go through the same provider account.

Users should be able to:

1. Save one or more **named DNS provider credentials** on the Plugins page,
   using a form generated from the plugin's own config structure.
2. Pick, per host, which credential (if any) issues that host's certificates.

## Goals

- Every `dns.providers.*` module compiled into the managed Caddy binary gets a
  usable form without CPM code changes.
- Multiple named credentials, including several of the same provider type
  (e.g. two Cloudflare accounts).
- Per-host selection in the Add/Edit Host dialog; the choice covers all of the
  host's domains.
- Hosts with no selection behave exactly as today (Caddy's default HTTP/TLS-ALPN
  challenges).
- Everything CPM manages survives a Caddy/container restart (startup
  reconcile, decision 9).

## Non-goals

- Generated forms for other module namespaces (HTTP handlers, matchers,
  storage, logging, …). The basic auth host plugin keeps its hand-written
  schema; the schema registry stays for it.
- Caddyfile mode support (see constraints).
- Different providers for different domains *within one* host.
- Dedicated wildcard-certificate UX. Wildcards become possible as a side effect
  of DNS-01, nothing more is built for them.

## Anti-goals

- Making the UI depend on internet access to function. Online docs may enrich
  forms, never gate them.
- Exposing stored secrets back through CPM's API.
- Breaking hosts that don't use a DNS provider.

## Constraints

- **API mode only.** Plugin config is already API-mode only; Caddyfile syntax
  for DNS plugins is plugin-specific and can't be generated reliably. In
  Caddyfile mode the provider UI is hidden/disabled and the API rejects
  setting a provider. The README must list this as a limitation.
- Existing stack: Go backend (chi, GORM, pure-Go SQLite), React + shadcn/ui
  frontend, schema-driven forms via `SchemaFields.tsx`.
- Commits authored as `Pacerino <public@pa0x43.me>`, no AI attribution.

## Decisions

### 1. Where field definitions come from

Ordered sources, first one that yields a definition wins:

1. **The Caddy binary**, via the
   [`caddy-json-schema`](https://github.com/abiosoft/caddy-json-schema) plugin
   (`caddy json-schema --output <file>`). It describes every module compiled
   into the binary, third-party ones included. Field *structure* comes from the
   binary; *descriptions* come from caddyserver.com and only exist for plugins
   registered there.
2. **caddyserver.com docs API** for binaries without that plugin (custom
   builds), e.g.
   `https://caddyserver.com/api/docs/config/apps/tls/automation/policies/issuers/acme/challenges/dns/provider/<name>`,
   which returns `struct_fields` with types and docs. Only works for registered
   plugins and with internet access.
3. **Raw JSON object editor** for the provider config as the last resort.

CPM runs source 1 against the binary it manages (`CPM_CADDY_BINARY`) and caches
the result in the data folder, keyed so it is regenerated when the binary
changes (my call: key on `caddy version` output plus the module list).

**Must be verified first (spike):** whether `caddy json-schema` works with no
network access (structure-only output vs. failure) and what its output looks
like for `dns.providers.*` modules. If it cannot produce structure offline, the
cache must be generated at image build time instead of at runtime. That
fallback is acceptable; the anti-goal above is about the UI, not the build.

### 2. Image builds

Both image flavors get `github.com/abiosoft/caddy-json-schema` compiled in. The
default flavor therefore switches from downloading the stock Caddy release to
an `xcaddy` build with just that plugin. The Dockerfile keeps the stock-release
download path for `CADDY_PLUGINS=""` (my call: CI passes the json-schema plugin
explicitly for both flavors, so the Dockerfile stays generic).

### 3. Schema → form fields

A generic converter turns a provider's field definitions into the existing
`schema.Field` shape:

| Source type | Form field |
| --- | --- |
| string | `string`, or `secret` when the key matches `token`, `secret`, `password`, `key` (case-insensitive, substring) |
| integer / number | `int` |
| boolean | `bool` |
| duration (Caddy `caddy.Duration`) | `string` with a placeholder like `30s` |
| array of strings | new `stringlist` field type |
| nested object / anything else | per-field JSON editor |

All string and secret fields accept Caddy placeholders such as `{env.CF_TOKEN}`,
so secrets can stay out of CPM's database. Required-ness: none of the sources
mark fields as required reliably, so generated fields are optional (my call).

Hand-written schemas for a `dns.providers.*` module, if present, override the
generated one. The existing Cloudflare schema is deleted once generation covers
it.

### 4. Data model

- New table **`DNSProvider`**: `ID`, `Name` (unique, user-chosen),
  `ModuleID` (e.g. `dns.providers.cloudflare`), `Config` (JSON of the form
  values), timestamps.
- **`Host.DNSProviderID`**: nullable foreign key. Null means "Caddy default".
- Deleting a credential still referenced by a host is refused (HTTP 409) with
  the list of hosts using it.

### 5. Applying to Caddy

For each host with a provider, CPM maintains one TLS automation policy:

```json
{
  "@id": "cpm_tls_host_<hostID>",
  "subjects": ["<the host's domains>"],
  "issuers": [{
    "module": "acme",
    "challenges": { "dns": { "provider": { "name": "<module short name>", "...": "config values" } } }
  }]
}
```

- CPM's policies sit at the **front** of `apps/tls/automation/policies` so
  they win over any catch-all policy; the path is created if missing. Among
  CPM's policies, ones with only exact subjects come before ones containing
  wildcard subjects, so `foo.example.com` on one host beats `*.example.com` on
  another (my call).
- Saving a host is rejected (400) when one of its domains is literally also
  listed by another host and the two hosts would get different TLS policies
  (my call; overlapping domains with the same provider, or both default, stay
  allowed as today).
- The issuer is a single `acme` issuer (Let's Encrypt) carrying the DNS
  challenge. Caddy's default ZeroSSL fallback is not mirrored; the README notes
  this (my call).
- Written alongside the host's route (`WriteHost`), updated via `/id/…`,
  removed when the host is deleted or its provider is cleared.
- Editing a credential re-applies the policy of every host using it.

### 6. Migrating the existing global Cloudflare config

An existing `ModuleConfig` row for `dns.providers.cloudflare` is converted on
startup into a `DNSProvider` named "Cloudflare" (its `api_token` taken from the
stored policy JSON) and the row is deleted. To keep current behaviour (the
global policy covered every certificate), all existing hosts are assigned the
migrated credential, but only if the row was `Enabled` (my call).

Removing the old policy from the live config: CPM scans
`apps/tls/automation/policies` and deletes policies that have no `subjects`
and whose issuers use a `cloudflare` DNS challenge, leaving any other policies
untouched (my call). This is a database-only migration if Caddy is
unreachable; the live config is corrected by the next successful apply. The `ModuleConfig` mechanism
itself stays for any non-DNS global module configs.

### 7. Secrets in the API

Responses never return values of `secret` fields; they come back empty with a
"set" indicator. On update, an empty secret field means "keep the stored
value". Values are stored as-is in SQLite, same as today. (Caddy's admin API,
which listens on localhost only, necessarily contains them.)

### 8. UI

- **Plugins page:** a "DNS providers" section listing saved credentials (name,
  provider type, used-by count) with add/edit/delete. "Add" offers only the
  `dns.providers.*` modules present in the Caddy build.
- **Host dialog:** a "DNS provider" select with "Default (HTTP challenge)" plus
  every saved credential. Hidden in Caddyfile mode.
- Switching `CPM_CADDY_MODE` from api to caddyfile keeps stored selections but
  ignores them; `CaddyfileProvider` doesn't render them. The Hosts page shows
  a small warning on hosts with an ignored provider (my call).
- Form for a credential: a name field, then the generated fields rendered by
  `SchemaFields`, with descriptions shown when available.

### 9. Startup reconcile (API mode)

Today nothing re-applies CPM's state after Caddy restarts, and the Docker
image starts Caddy from `/data/caddy.json`, which admin API changes never
update. Routes, and the TLS policies added here, are lost on restart.

On startup in API mode, CPM:

1. Waits for the admin API with retry/backoff (it may start before Caddy is
   ready; the entrypoint only sleeps 1s). It keeps retrying in the background
   rather than blocking CPM's own API from serving.
2. Ensures the HTTP server exists, then re-applies every host route, every
   host TLS policy and every enabled `ModuleConfig`, all idempotently
   (create or update via `@id`).
3. Runs the Cloudflare migration's live-config cleanup (decision 6) as part of
   this pass.

Failures are logged per item and don't stop the rest. A manual trigger
(e.g. `POST /api/caddy/reconcile`) is the builder's call. Detecting a Caddy
restart while CPM keeps running is out of scope.

### 10. Docs

README: DNS provider section (saving credentials, per-host selection, `{env.*}`
placeholders, which image flavor bundles which providers) and a limitations
note that DNS providers and plugins only work in API mode.

## Testing expectations

- Converter unit tests against captured `caddy json-schema` and docs-API
  fixtures for cloudflare, duckdns and porkbun.
- Policy builder and apply/remove tests (insert-at-front, `@id` update, removal
  on host delete / provider cleared).
- Handler tests: credential CRUD, 409 on delete-in-use, secret masking and
  keep-on-empty, Caddyfile-mode rejection.
- Migration test for the Cloudflare `ModuleConfig` row.
- Reconcile tests against a fake admin API: waits for availability, re-applies
  routes/policies/module configs idempotently, continues past item failures.
- Frontend tests for the credential dialog and host dialog selector.

## Left to the builder

- Exact parsing of the `caddy json-schema` output and the docs API response.
- Cache file location/format and invalidation details.
- Visual layout of the Plugins page section and dialogs (follow existing
  shadcn/ui patterns).
- API route shapes (follow existing `/api/caddy/…` conventions).
