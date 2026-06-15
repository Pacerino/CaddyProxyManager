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

export type SchemaFieldType = "string" | "int" | "bool" | "secret";

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
