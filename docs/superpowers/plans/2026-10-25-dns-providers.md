# DNS Providers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Generated config forms for every `dns.providers.*` module in the Caddy build, named credentials, per-host provider selection via per-host TLS policies, and a startup reconcile so CPM's config survives Caddy restarts.

**Architecture:** The image generates Caddy's JSON schema at build time (`caddy json-schema`, needs network, so it can't run at runtime). A new `dnsschema` package turns that file (or, per module, the caddyserver.com docs API) into `schema.Field`s. Credentials live in a new `DNSProvider` table; hosts reference one. A new `reconcile` package owns everything that needs the DB *and* the API provider: syncing all CPM TLS policies as one ordered block, the legacy Cloudflare migration, and the startup re-apply.

**Tech Stack:** Go 1.25 (chi, GORM, glebarez/sqlite), React 18 + TypeScript + shadcn/ui, vitest, Docker/xcaddy.

**Spec:** `docs/superpowers/specs/2026-10-25-dns-provider-plugins-design.md`

## Spike result (done while planning)

`caddy json-schema` (github.com/abiosoft/caddy-json-schema, Caddy v2.11.4) **fails offline** (`exit=3`, it fetches `https://caddyserver.com/api/docs/config/` first). Online it writes a ~1 MB schema where each provider is at `definitions["dns.providers.<name>"]`:

```json
{"type":"object","properties":{
  "api_token":{"type":"string","description":"api_token: string\nModule: dns.providers.cloudflare\nAPI token is used for authentication. ...\n"},
  "zone_token":{"type":"string","description":"zone_token: string\nModule: dns.providers.cloudflare\nhttps://pkg.go.dev/github.com/caddy-dns/cloudflare#Provider"}}}
```

Docs API (`GET https://caddyserver.com/api/docs/config/apps/tls/automation/policies/issuers/acme/challenges/dns/provider/<name>`) returns `{"status_code":200,"result":{"structure":{"type":"struct","struct_fields":[{"key":"api_token","value":{"type":"string"}}, ...],"doc":"..."}}}`; unknown providers return HTTP 502.

Consequences, overriding the spec where they differ (my calls):
- The schema is generated **during the image build** and shipped at `/usr/share/cpm/caddy_schema.json`. CPM never runs `caddy json-schema` itself.
- The Dockerfile **always** builds Caddy with xcaddy plus `caddy-json-schema`; the stock-release download path is removed.
- Lookup is per module: schema file → docs API → raw JSON. A custom `CPM_CADDY_BINARY` whose providers aren't in the shipped file still works through the later sources.

## Global Constraints

- Commits authored `Pacerino <public@pa0x43.me>`; no `Co-Authored-By` trailers, no AI attribution anywhere.
- Commit message style: `[Backend] …`, `[Frontend] …`, `[Docker] …`, `[Docs] …`.
- Everything DNS-provider related is **API mode only**; in Caddyfile mode endpoints return 400 with the existing `apiProviderOrError` message.
- TLS policy `@id`: `cpm_tls_host_<hostID>`. Issuer: single `{"module":"acme"}` with the DNS challenge; no ZeroSSL issuer.
- Secret heuristic: key contains `token`, `secret`, `password` or `key` (case-insensitive).
- Generated fields are never `Required`.
- Schema file path: `CPM_CADDY_SCHEMAFILE`, default `/usr/share/cpm/caddy_schema.json`.
- Backend verification: `cd backend && go vet ./... && go test -count=1 ./...`. Frontend: `cd frontend && npm run test && npm run build`.

## Review Focus

1. **A provider module disappears from the Caddy build** (user swaps `CPM_CADDY_BINARY`) while a credential still uses it. Caddy rejects a whole config load that references an unknown module, which would break every host. Expected: policies for unavailable modules are skipped and logged; the UI marks the credential "unavailable". Test lives in Task 4.
2. **Editing a credential's secret-less form** (user changes only the name). Expected: stored secrets are kept; changing the *provider type* drops all old values. Test lives in Task 6.
3. **Caddy not up when CPM starts.** Expected: CPM's HTTP API serves immediately; reconcile retries in the background and applies once Caddy answers. Test lives in Task 8.
4. **Deleting a host that had a provider.** Expected: its policy disappears from Caddy. Test lives in Task 7.
5. **Credential edited while three hosts use it.** Expected: all three policies change in one sync. Test lives in Task 6.

---

### Task 1: Image builds Caddy with json-schema and ships the schema

**Files:**
- Modify: `Dockerfile` (caddy stage + runtime stage)
- Modify: `README.md` ("Building the image yourself")

**Interfaces:**
- Produces: `/usr/share/cpm/caddy_schema.json` in every image; `ENV CPM_CADDY_SCHEMAFILE=/usr/share/cpm/caddy_schema.json`.

- [ ] **Step 1: Caddy stage always uses xcaddy.** Add `ARG CADDY_JSON_SCHEMA=github.com/abiosoft/caddy-json-schema`, prepend it to the `--with` list, delete the stock-release `wget` branch. `CADDY_PLUGINS` keeps its meaning (extra plugins).
- [ ] **Step 2: Generate the schema in the runtime stage**, right after `RUN caddy version`: `RUN mkdir -p /usr/share/cpm && caddy json-schema --output /usr/share/cpm/caddy_schema.json`, and add `CPM_CADDY_SCHEMAFILE` to the `ENV` block. (Runs under QEMU for arm64; it's a short run.)
- [ ] **Step 3: Verify both flavors locally**

Run:
```bash
docker buildx build --load -t cpm-t:default . && \
docker run --rm --entrypoint sh cpm-t:default -c 'caddy list-modules | grep -c json; test -s /usr/share/cpm/caddy_schema.json && echo schema-ok'
docker buildx build --load -t cpm-t:dns --build-arg CADDY_PLUGINS="github.com/caddy-dns/cloudflare github.com/caddy-dns/duckdns github.com/caddy-dns/porkbun" . && \
docker run --rm --entrypoint sh cpm-t:dns -c "apk add -q jq && jq -r '.definitions|keys[]' /usr/share/cpm/caddy_schema.json | grep dns.providers"
```
Expected: `schema-ok`; the second prints `dns.providers.cloudflare`, `duckdns`, `porkbun`.

- [ ] **Step 4: README.** In "Building the image yourself": Caddy is always built with xcaddy (+ `caddy-json-schema`), the build needs internet access, and `CADDY_PLUGINS` adds extra plugins.
- [ ] **Step 5: Commit** `[Docker] Always build Caddy with xcaddy and ship its JSON schema`

### Task 2: `dnsschema` package: fields from schema file and docs API

**Files:**
- Modify: `backend/internal/caddy/schema/schema.go` (new field types; `checkType` handles them)
- Create: `backend/internal/caddy/dnsschema/dnsschema.go`, `dnsschema_test.go`
- Modify: `backend/internal/caddy/dnsschema/testdata/caddy_schema_dns.json` (already committed: the spike's trimmed `definitions` for cloudflare/duckdns/porkbun; add one hand-written provider `dns.providers.fixture` with properties of type `number`, `boolean`, `["string","null"]`, `array` of strings, and `object`)
- Use: `backend/internal/caddy/dnsschema/testdata/docsapi_duckdns.json` (already committed)
- Modify: `backend/internal/config/vars.go` (or wherever `Caddy` config lives): `SchemaFile string` with env `CPM_CADDY_SCHEMAFILE`, default `/usr/share/cpm/caddy_schema.json`

**Interfaces:**
- Consumes: `caddy.ListModules() ([]caddy.Module, error)`, `schema.Field`, `schema.Get`.
- Produces:
  - `schema.FieldStringList FieldType = "stringlist"` (value `[]any` of strings), `schema.FieldJSON FieldType = "json"` (any JSON value).
  - `type dnsschema.Provider struct { ModuleID, Name, Title, Description string; Fields []schema.Field; Source string }` (JSON tags camelCase). `Source` is `"binary"`, `"docs"`, `"override"` or `"none"`; `"none"` means raw JSON editing.
  - `func ParseSchemaFile(data []byte) (map[string]Provider, error)`: keyed by module ID, only `dns.providers.*`.
  - `func ParseDocsAPI(moduleID string, data []byte) (Provider, error)`
  - `type Resolver struct { SchemaFile string; DocsBaseURL string; Client *http.Client; ListModules func() ([]caddy.Module, error) }`
  - `func NewResolver() *Resolver`: config-backed defaults, `DocsBaseURL = "https://caddyserver.com/api/docs/config/apps/tls/automation/policies/issuers/acme/challenges/dns/provider/"`, client timeout 5s.
  - `func (r *Resolver) Providers(ctx context.Context) ([]Provider, error)`: one entry per `dns.providers.*` module in the build, sorted by ID.

- [ ] **Step 1: Write failing tests** in `dnsschema_test.go`:
  - `TestParseSchemaFile_Cloudflare`: `api_token` → `Type: secret`, `Label: "API Token"`, `Description` starts with `"API token is used for authentication."` (the `key: type` and `Module:` header lines and bare URL lines are stripped); `zone_token` → `secret` with empty description. `Title == "Cloudflare"`, `Name == "cloudflare"`, `Source == "binary"`.
  - `TestParseSchemaFile_TypeMapping` (fixture provider): `number`→`int`, `boolean`→`bool`, `["string","null"]`→`string`, array of strings→`stringlist`, `object`→`json`. No field is `Required`.
  - `TestParseSchemaFile_OnlyDNSProviders`: non-`dns.providers` definitions are ignored.
  - `TestParseDocsAPI_DuckDNS`: fields `api_token` (secret), `override_domain` (string), `Source == "docs"`. Docs types map `string`→string, `bool`→bool, `int`/`float`→int, `array` of string→stringlist, `duration`→string with `Placeholder: "30s"`, anything else→json.
  - `TestResolver_FallbackChain` (httptest docs server + fake `ListModules` returning cloudflare, duckdns, `dns.providers.unknown`; schema file containing only cloudflare): cloudflare `binary`, duckdns `docs`, unknown `none` (docs server answers 502), and a second `Providers` call doesn't hit the docs server for duckdns again (in-memory cache).
  - `TestResolver_MissingSchemaFile`: a nonexistent path is not an error; it falls through to docs.
- [ ] **Step 2: Run** `cd backend && go test ./internal/caddy/dnsschema/` → FAIL (package missing).
- [ ] **Step 3: Implement.** Label = key split on `_`, each word capitalized, with `api`→`API`, `id`→`ID`, `url`→`URL`, `dns`→`DNS`, `ttl`→`TTL`. Title = `Name` with that same rule. A registered `schema.Get(moduleID)` overrides any source (`Source: "override"`). Cache docs results (including misses) in a mutex-guarded map on the Resolver.
- [ ] **Step 4: Run** the package tests → PASS. Then the full backend verification.
- [ ] **Step 5: Commit** `[Backend] Generate DNS provider fields from Caddy's JSON schema or docs API`

### Task 3: Data model

**Files:**
- Modify: `backend/internal/database/models.go`, `backend/internal/database/sqlite.go` (AutoMigrate), `backend/internal/api/handler/testmain_test.go` (AutoMigrate list)
- Test: `backend/internal/database/models_test.go`

**Interfaces:**
- Produces:
  - `type DNSProvider struct { gorm.Model; Name string \`json:"name" gorm:"uniqueIndex" validate:"required"\`; ModuleID string \`json:"moduleId" validate:"required"\`; Config JSON \`json:"config"\` }`
  - `Host.DNSProviderID *uint \`json:"dnsProviderId"\``
  - `func ShortName(moduleID string) string` (`"dns.providers.cloudflare"` → `"cloudflare"`)

- [ ] **Step 1: Failing tests:** `TestDNSProvider_NameUnique` (second insert with the same name errors), `TestHost_DNSProviderIDNullable` (save a host with nil, then with a value, then nil again; it reads back nil), `TestShortName`.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** the full backend verification → PASS.
- [ ] **Step 5: Commit** `[Backend] Add DNSProvider model and host provider reference`

### Task 4: TLS policy building and sync

**Files:**
- Create: `backend/internal/caddy/tls.go`, `backend/internal/caddy/tls_test.go`

**Interfaces:**
- Consumes: `database.Host`, `database.DNSProvider`, `database.ShortName`, `splitDomains`.
- Produces:
  - `func HostPolicyID(hostID uint) string` → `"cpm_tls_host_%d"`
  - `func BuildTLSPolicies(hosts []database.Host, providers map[uint]database.DNSProvider, available map[string]bool) ([]map[string]any, []string)`. Returns the policies plus warnings. It skips hosts with a nil `DNSProviderID`, a missing provider, or a provider whose `ModuleID` isn't in `available` (warning names the host and module). Order: policies whose subjects contain no `*` first, then wildcard ones; within each group by host ID. Provider object = `{"name": ShortName(moduleID)}` merged with the config values, skipping empty strings and nulls.
  - `func (p *APIProvider) SyncTLSPolicies(policies []map[string]any) error`. GETs `/config/apps/tls/automation/policies`. Keeps every existing policy except those whose `@id` starts with `cpm_tls_host_`, and except **legacy CPM Cloudflare policies** (no `subjects`, and an issuer whose `challenges.dns.provider.name == "cloudflare"`). Writes `policies ++ kept`, creating missing parents: `/config/apps/tls` null → `PUT /config/apps/tls {"automation":{"policies":…}}`; `automation` null → `PUT /config/apps/tls/automation {"policies":…}`; otherwise `PATCH /config/apps/tls/automation/policies`. When the result is empty and there is nothing to remove, it makes no write.
  - `func (p *APIProvider) Ping(ctx context.Context) error`: `GET /config/` must return 2xx.

- [ ] **Step 1: Failing tests** (extend `fakeCaddy` in `api_test.go` with a stored `tlsPolicies []any` and handlers for the GET/PUT/PATCH paths above):
  - `TestBuildTLSPolicies_Shape`: a host `a.com b.com` with a cloudflare credential `{"api_token":"t","zone_token":""}` gives exactly `{"@id":"cpm_tls_host_1","subjects":["a.com","b.com"],"issuers":[{"module":"acme","challenges":{"dns":{"provider":{"name":"cloudflare","api_token":"t"}}}}]}`.
  - `TestBuildTLSPolicies_WildcardsLast`: host 1 `*.example.com` and host 2 `foo.example.com` → host 2's policy comes first.
  - `TestBuildTLSPolicies_SkipsUnavailableModule` (Review Focus 1): no policy and one warning containing the module ID.
  - `TestSyncTLSPolicies_KeepsForeignDropsLegacy`: existing `[{"subjects":["x.org"]}, {"@id":"cpm_tls_host_9"}, {legacy cloudflare}]` + new `[P1]` → `[P1, {"subjects":["x.org"]}]`.
  - `TestSyncTLSPolicies_CreatesMissingTLSApp`: the fake has no tls app, so one `PUT /config/apps/tls` is made.
  - `TestPing`: 200 → nil; connection refused → error.
- [ ] **Step 2: Run** `go test ./internal/caddy/ -run 'TLS|Ping'` → FAIL. **Step 3: Implement.** **Step 4: Run** the full backend verification → PASS.
- [ ] **Step 5: Commit** `[Backend] Build and sync per-host TLS policies for DNS challenges`

### Task 5: `reconcile` package: TLS sync, legacy migration, startup re-apply

**Files:**
- Create: `backend/internal/reconcile/reconcile.go`, `migrate.go`, `reconcile_test.go`, `migrate_test.go`
- Modify: `backend/cmd/main.go`
- Modify: `backend/internal/caddy/api.go` (export `EnsureServer`, keeping `ensureServer` callers working)

**Interfaces:**
- Consumes: Task 3 models, Task 4 `BuildTLSPolicies`/`SyncTLSPolicies`/`Ping`, `caddy.ListModules`, `APIProvider.WriteHost`, `APIProvider.ApplyModuleConfig`, `jobqueue.AddJob`.
- Produces:
  - `func SyncTLS(db *gorm.DB, p *caddy.APIProvider, listModules func() ([]caddy.Module, error)) error`: loads hosts with non-nil `DNSProviderID` and all providers, builds the `available` set from `listModules` (`dns.providers.*` IDs), logs warnings, syncs.
  - `func MigrateLegacyCloudflare(db *gorm.DB) (bool, error)`: if a `ModuleConfig` with `module_id = "dns.providers.cloudflare"` exists, read `api_token` from its stored policy (`issuers[0].challenges.dns.provider.api_token`; tolerate a top-level array or object), create `DNSProvider{Name: "Cloudflare", ModuleID: "dns.providers.cloudflare", Config: {"api_token": …}}` (name `"Cloudflare (migrated)"` if `Cloudflare` is taken), set every host's `DNSProviderID` to it **only if the row was Enabled**, and delete the row, all in one transaction.
  - `func Run(ctx context.Context, db *gorm.DB, p *caddy.APIProvider, listModules func() ([]caddy.Module, error), backoff func(attempt int) time.Duration) []error`: calls `Ping` until success or ctx is done (sleeping `backoff(attempt)`; the default is 1s doubling, capped at 30s). Then `EnsureServer`, `WriteHost` for every host (preload `Upstreams`, `Plugins`), `SyncTLS`, and `ApplyModuleConfig` for each enabled `ModuleConfig`. It returns every per-item error and continues past them.
  - `main.go`, API mode only: after `jobqueue.Start()`, call `MigrateLegacyCloudflare` synchronously (log errors), then `go func(){ wait for Ping; jobqueue.AddJob(Job{Name: "CaddyReconcile", Action: …Run…}) }()` so reconcile is serialized with handler jobs. `api.StartServer()` must not wait on it.

- [ ] **Step 1: Failing tests** (write a small stateful fake admin API in `reconcile_test.go`: counts pings, records route writes and module-config PATCHes, and stores the TLS policies array for the GET/PUT/PATCH paths from Task 4):
  - `TestRun_WaitsForCaddy` (Review Focus 3): the fake returns 503 for `GET /config/` twice and then 200; with zero backoff, `Run` writes host routes after the third ping and returns no errors.
  - `TestRun_ReappliesEverything`: 2 hosts (one with a provider), 1 enabled and 1 disabled `ModuleConfig` → 2 route writes, 1 TLS policy write containing `cpm_tls_host_<id>`, exactly 1 module config apply.
  - `TestRun_ContinuesPastFailures`: the fake fails the first host's route write; the second host is still written and the error is returned.
  - `TestRun_CancelledContext`: Caddy is never up; `Run` returns once ctx is cancelled.
  - `TestMigrateLegacyCloudflare_Enabled`: the row is converted, both hosts are assigned, and the row is gone.
  - `TestMigrateLegacyCloudflare_Disabled`: the credential is created, hosts stay nil.
  - `TestMigrateLegacyCloudflare_NoRow`: returns `false, nil`, no changes.
- [ ] **Step 2: Run** `go test ./internal/reconcile/` → FAIL. **Step 3: Implement.** **Step 4: Run** the full backend verification → PASS.
- [ ] **Step 5: Commit** `[Backend] Reconcile Caddy config on startup and migrate legacy Cloudflare config`

### Task 6: DNS provider API

**Files:**
- Create: `backend/internal/api/handler/dnsproviders.go`, `dnsproviders_test.go`
- Modify: `backend/internal/api/router.go` (inside the existing `/caddy` route group)
- Modify: `backend/internal/api/handler/handler.go` (a `DNSSchemas` field holding a resolver interface so tests can stub it)
- Delete: `backend/internal/caddy/schema/cloudflare.go` and the Cloudflare cases in `schema_test.go` / handler tests that depend on it

**Interfaces:**
- Consumes: Task 2 `Resolver.Providers`, Task 3 models, Task 5 `reconcile.SyncTLS`, `apiProviderOrError`, `schema.Schema{Fields}.Validate`.
- Produces (all API mode only, all behind `middleware.Enforce()`):
  - `GET /api/caddy/dns-provider-types` → `[]dnsschema.Provider`
  - `GET /api/caddy/dns-providers` → `[]{id, name, moduleId, values, secretsSet: []string, usedBy: [{id, domains}], available: bool}`. `values` has secret fields removed; `secretsSet` lists secret keys that have a stored non-empty value.
  - `POST /api/caddy/dns-providers` body `{name, moduleId, values}` → 201 with the same shape.
  - `PUT /api/caddy/dns-providers/{id:[0-9]+}`, same body. An empty or absent secret keeps the stored value, unless `moduleId` changed, in which case the old values are discarded entirely.
  - `DELETE /api/caddy/dns-providers/{id:[0-9]+}` → 409 with `usedBy` as error data when hosts reference it.
  - Validation: name non-empty and unique (400); `moduleId` must be among the resolver's types (400); values are type-checked through the fields when `Source != "none"`; otherwise `values` must be a JSON object.
  - After create/update/delete, enqueue `jobqueue.Job{Name: "CaddySyncTLS", Action: reconcile.SyncTLS…}`.

- [ ] **Step 1: Failing tests** (stub resolver with cloudflare `api_token` secret + porkbun; `ListModules` is stubbed via the same seam to report both as available; extend `fakeCaddyServer` in `testmain_test.go` to store the TLS policies array like the Task 4 fake, since Tasks 6 and 7 assert on it):
  - `TestDNSProviders_CreateListMasksSecrets`: create cloudflare `{"api_token":"s3cret"}`; the list response doesn't contain `s3cret` anywhere and has `secretsSet == ["api_token"]`.
  - `TestDNSProviders_UpdateKeepsSecret` (Review Focus 2): PUT with a new name and `api_token: ""` stores the old `s3cret`.
  - `TestDNSProviders_UpdateTypeChangeDropsValues` (Review Focus 2): PUT changing to porkbun with `{"api_key":"k"}` stores exactly `{"api_key":"k"}`.
  - `TestDNSProviders_DuplicateName400`, `TestDNSProviders_UnknownModule400`.
  - `TestDNSProviders_DeleteInUse409`: the response data lists the host.
  - `TestDNSProviders_UpdateResyncsAllHosts` (Review Focus 5): 3 hosts use the credential; after PUT, the fake Caddy's policies array holds 3 CPM policies with the new token.
  - `TestDNSProviders_CaddyfileMode400`.
- [ ] **Step 2: Run** `go test ./internal/api/handler/ -run DNSProviders` → FAIL. **Step 3: Implement.** **Step 4: Run** the full backend verification → PASS.
- [ ] **Step 5: Commit** `[Backend] Add DNS provider credential API`

### Task 7: Hosts accept a provider; domain conflict check

**Files:**
- Modify: `backend/internal/api/handler/hosts.go`
- Test: `backend/internal/api/handler/hosts_test.go`

**Interfaces:**
- Consumes: `Host.DNSProviderID`, `reconcile.SyncTLS`, `config.Configuration.Caddy.Mode`.
- Produces: create/update/delete host behaviour:
  - A non-nil `dnsProviderId` in Caddyfile mode → 400 `"DNS providers are only available when CPM_CADDY_MODE=api"`. A nonexistent provider ID → 400.
  - Conflict check (on create and update): split domains with `strings.Fields`, same as routes. If another host lists the same literal domain **and** its `DNSProviderID` differs (nil ≠ value; values compared by number) → 400 naming the domain and the other host's ID.
  - In API mode, after the route job, enqueue `CaddySyncTLS` on create, update and delete.

- [ ] **Step 1: Failing tests:** `TestCreateHost_WithProviderWritesPolicy`, `TestDeleteHost_RemovesPolicy` (Review Focus 4: the fake's policies have no `cpm_tls_host_<id>` afterwards), `TestHost_ConflictDifferentProvider400`, `TestHost_SameDomainSameProviderAllowed`, `TestHost_ProviderInCaddyfileMode400`, `TestHost_UnknownProvider400`.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** the full backend verification → PASS.
- [ ] **Step 5: Commit** `[Backend] Per-host DNS provider selection with domain conflict check`

### Task 8: Frontend: field types, DNS providers section, host selector

**Files:**
- Modify: `frontend/src/lib/types.ts`, `frontend/src/components/SchemaFields.tsx` (+ `SchemaFields.test.tsx`)
- Create: `frontend/src/components/DNSProvidersSection.tsx`, `DNSProviderDialog.tsx`, `DNSProviderDialog.test.tsx`
- Modify: `frontend/src/pages/Plugins.tsx` (render the section above the module list; hide `dns.providers.*` from the generic module list)
- Modify: `frontend/src/components/HostDialog.tsx` (+ `HostDialog.test.tsx`), `frontend/src/pages/Hosts.tsx`

**Interfaces:**
- Consumes: Task 6 endpoints; `Host.dnsProviderId`.
- Produces:
  - Types `SchemaFieldType` gains `"stringlist" | "json"`; `DNSProviderType` (mirrors `dnsschema.Provider`); `DNSProvider` (mirrors the list response); `Host.dnsProviderId: number | null`; `HostPayload.dnsProviderId: number | null`.
  - `SchemaFields`: `stringlist` renders as one input taking comma-separated values and emits a `string[]`; `json` renders as a textarea, emits the raw text, and the dialog parses it on submit. `FieldValues` widens to `string | boolean | string[]`.
  - `DNSProviderDialog` props: `{open, onOpenChange, types: DNSProviderType[], provider: DNSProvider | null, submitting, onSubmit(payload: {name, moduleId, values})}`. Secret inputs show placeholder `"•••••• (unchanged)"` when the key is in `secretsSet`. `Source === "none"` renders a single JSON textarea for all values.
  - `DNSProvidersSection`: a table (name, provider title, used-by count, an "unavailable" badge when `!available`), add/edit/delete. A 409 on delete shows a toast listing the host domains. When the list request returns 400 (Caddyfile mode), it shows a one-line notice instead.
  - `HostDialog` gets a `dnsProviders: DNSProvider[] | null` prop. `null` hides the select (Caddyfile mode). The select options are "Default (HTTP challenge)" (→ `null`) plus each provider by name.
  - `Hosts.tsx`: fetches `/caddy/dns-providers` once; on 400 passes `null`, and hosts with a non-null `dnsProviderId` show a small warning icon titled "DNS provider ignored in Caddyfile mode".

- [ ] **Step 1: Failing tests:**
  - `SchemaFields.test.tsx`: `stringlist` input `"a, b"` → `onChange` with `["a","b"]`.
  - `DNSProviderDialog.test.tsx`: picking the Cloudflare type renders the `API Token` password input; submitting gives `{name:"CF", moduleId:"dns.providers.cloudflare", values:{api_token:"t"}}`; when editing with `secretsSet:["api_token"]` and the field untouched, it submits `api_token: ""`.
  - `HostDialog.test.tsx`: the existing payload expectation gains `dnsProviderId: null`; a new test selects a provider and expects `dnsProviderId: 2`; with `dnsProviders={null}` the select is absent.
- [ ] **Step 2: Run** `cd frontend && npm run test` → FAIL. **Step 3: Implement.** **Step 4: Run** the frontend verification → PASS.
- [ ] **Step 5: Commit** `[Frontend] DNS provider credentials and per-host selection`

### Task 9: Docs and end-to-end manual check

**Files:**
- Modify: `README.md`

- [ ] **Step 1: README.**
  - New "🔐 DNS providers" section: save credentials on the Plugins page, pick one per host, `{env.VAR}` placeholders keep secrets out of the DB, the `-dns` image bundles cloudflare/duckdns/porkbun, custom builds via `CADDY_PLUGINS`, Let's Encrypt only (no ZeroSSL fallback) for DNS-challenge hosts.
  - New "⚠️ Limitations" section: plugins and DNS providers require `CPM_CADDY_MODE=api`; provider choices are ignored in Caddyfile mode.
  - Remove the Roadmap item "Plugin management" only if you judge it delivered; otherwise leave the roadmap untouched.
- [ ] **Step 2: Manual check with the `dns` image.** Run it with `-e CPM_CADDY_LISTEN=":80"`, log in, add a DuckDNS credential with `api_token = {env.DUCKDNS_TOKEN}`, create a host using it, then:
  - `curl localhost:2019/config/apps/tls/automation/policies` from inside the container shows `cpm_tls_host_1` with `"name":"duckdns"`
  - `docker restart` the container; the route and the policy are back within ~30s.
- [ ] **Step 3: Commit** `[Docs] Document DNS providers and API-mode limitation`
