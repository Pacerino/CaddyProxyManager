export interface Upstream {
  ID?: number;
  hostId?: number;
  backend: string;
}

export interface Host {
  ID: number;
  CreatedAt: string;
  UpdatedAt: string;
  domains: string;
  matcher: string;
  Upstreams: Upstream[];
}

export type AuthMode = "local" | "oidc";

export interface AuthConfig {
  mode: AuthMode;
}
