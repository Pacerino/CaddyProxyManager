export interface Upstream {
  ID?: number;
  hostId?: number;
  backend: string;
}

export interface HostPlugin {
  ID: number;
  hostId: number;
  moduleId: string;
  handler: unknown;
  enabled: boolean;
}

export interface Host {
  ID: number;
  CreatedAt: string;
  UpdatedAt: string;
  domains: string;
  matcher: string;
  Upstreams: Upstream[];
  Plugins?: HostPlugin[];
  dnsProviderId: number | null;
}

export type AuthMode = "local" | "oidc";

export interface AuthConfig {
  mode: AuthMode;
}

// CaddyModule is a single module reported by `caddy list-modules`.
export interface CaddyModule {
  id: string;
  namespace: string;
  name: string;
  standard: boolean;
  version?: string;
  package?: string;
}

export interface CaddyModulesResponse {
  modules: CaddyModule[];
  plugins: CaddyModule[];
}

export type SchemaFieldType = "string" | "int" | "bool" | "secret" | "stringlist" | "json";

export interface SchemaField {
  key: string;
  label: string;
  description?: string;
  type: SchemaFieldType;
  required?: boolean;
  default?: unknown;
  placeholder?: string;
}

export type PluginScope = "global" | "host";

// PluginSchema is a typed configuration descriptor for a known plugin.
export interface PluginSchema {
  moduleId: string;
  title: string;
  description?: string;
  scopes: PluginScope[];
  path?: string;
  fields: SchemaField[];
}

// ModuleConfig is a stored configuration fragment for a Caddy module.
export interface ModuleConfig {
  ID: number;
  CreatedAt: string;
  UpdatedAt: string;
  moduleId: string;
  path: string;
  config: unknown;
  enabled: boolean;
}

// DNSProviderType mirrors dnsschema.Provider: a DNS provider module of the
// Caddy build with its config fields. source "none" means no schema is known
// and the values are edited as raw JSON.
export interface DNSProviderType {
  moduleId: string;
  name: string;
  title: string;
  description?: string;
  fields: SchemaField[];
  source: "binary" | "docs" | "none" | string;
}

export interface DNSProviderHost {
  id: number;
  domains: string;
}

// DNSProvider is a stored DNS credential. Secret values are never returned;
// secretsSet names the secret keys that have a stored value.
export interface DNSProvider {
  id: number;
  name: string;
  moduleId: string;
  values: Record<string, unknown>;
  secretsSet: string[];
  usedBy: DNSProviderHost[];
  available: boolean;
}

// "fallback" records that CPM switched back to the shipped Caddy because the
// managed binary could not run.
export type PluginOpKind = "add" | "remove" | "update" | "fallback";
export type PluginOpStep =
  | "requesting"
  | "downloading"
  | "verifying"
  | "restarting"
  | "reapplying"
  | "done"
  | "rolledBack"
  | "failed";

// A runtime plugin install/remove/update operation.
export interface PluginOperation {
  id: string;
  kind: PluginOpKind;
  package: string;
  step: PluginOpStep;
  error?: string;
  // The Caddy version of the new binary and of the one it replaced.
  caddyVersion?: string;
  previousCaddyVersion?: string;
  startedAt: string;
  finishedAt?: string;
}

export interface InstalledPlugin {
  path: string;
  version: string;
  modules: string[];
  tested: boolean;
}

export type PluginSetupState =
  | "supervised"
  | "systemd-ready"
  | "setup-needed"
  | "unsupported";

export interface PluginsState {
  installed: InstalledPlugin[];
  installEnabled: boolean;
  disabledReason?: string;
  setup: { state: PluginSetupState; reason?: string; dropIn?: string };
  update: { available: boolean; managed: string; shipped: string };
  autoUpdate: boolean;
  operation?: PluginOperation;
  lastUpdate?: PluginOperation;
  // The most recent finished change if it failed, until one succeeds.
  lastFailed?: PluginOperation;
}

export interface CatalogPlugin {
  path: string;
  repo: string;
  modules: string[];
  tested: boolean;
  installed: boolean;
}

export interface PluginUsage {
  kind: string;
  id: number;
  name: string;
}
