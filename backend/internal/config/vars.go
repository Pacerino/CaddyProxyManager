package config

import "github.com/Pacerino/CaddyProxyManager/internal/logger"

// IsSetup defines whether we have an admin user or not
var IsSetup bool

var logLevel logger.Level

type log struct {
	Level  string `json:"level" envconfig:"optional,default=debug"`
	Format string `json:"format" envconfig:"optional,default=nice"`
}

// caddy holds the configuration for how CPM talks to Caddy.
type caddy struct {
	// Mode selects the provider used to apply host configuration.
	// Valid values: "caddyfile" (render per-host Caddyfiles) or "api"
	// (manage Caddy's JSON config via the admin API).
	Mode string `json:"mode" envconfig:"optional,default=caddyfile"`
	// AdminURL is the base URL of the Caddy admin API (used by the api
	// provider and the "api" reload strategy).
	AdminURL string `json:"admin_url" envconfig:"optional,default=http://localhost:2019"`
	// ServerName is the name of the http server in Caddy's JSON config
	// that the api provider manages routes under.
	ServerName string `json:"server_name" envconfig:"optional,default=srv0"`
	// Listen are the addresses the bootstrapped server listens on when the
	// api provider has to create it. Defaults to the standard HTTP/HTTPS
	// ports; use e.g. :8080 for local non-root testing.
	Listen []string `json:"listen" envconfig:"optional,default=:80;:443"`
	// ReloadStrategy controls how the caddyfile provider triggers a reload.
	// Valid values: "systemd", "exec", "api" or "none".
	ReloadStrategy string `json:"reload_strategy" envconfig:"optional,default=systemd"`
	// Binary is the caddy executable used by the "exec" reload strategy.
	Binary string `json:"binary" envconfig:"optional,default=caddy"`
	// Service is the systemd unit reloaded by the "systemd" strategy.
	Service string `json:"service" envconfig:"optional,default=caddy.service"`
}

// oidc holds the OpenID Connect / OAuth2 configuration used when the auth
// mode is set to "oidc".
type oidc struct {
	// Issuer is the OIDC issuer URL (used for provider discovery).
	Issuer string `json:"issuer" envconfig:"optional"`
	// ClientID and ClientSecret identify CPM to the provider.
	ClientID     string `json:"client_id" envconfig:"optional"`
	ClientSecret string `json:"client_secret" envconfig:"optional"`
	// RedirectURL is the CPM callback URL registered with the provider,
	// e.g. https://cpm.example.com/api/auth/oidc/callback.
	RedirectURL string `json:"redirect_url" envconfig:"optional"`
	// Scopes requested from the provider.
	Scopes []string `json:"scopes" envconfig:"optional,default=openid;profile;email"`
	// AllowedDomains optionally restricts JIT provisioning to email domains.
	// Empty means any successfully authenticated user is allowed.
	AllowedDomains []string `json:"allowed_domains" envconfig:"optional"`
}

// auth holds the authentication configuration.
type auth struct {
	// Mode selects the authentication method: "local" (email + password)
	// or "oidc" (external identity provider).
	Mode string `json:"mode" envconfig:"optional,default=local"`
	OIDC oidc   `json:"oidc"`
}

// admin holds the seed credentials for the default local admin user, created
// on first startup when the database is empty.
type admin struct {
	Email    string `json:"email" envconfig:"optional,default=admin@example.com"`
	Password string `json:"password" envconfig:"optional,default=changeme"`
}

// Configuration is the main configuration object
var Configuration struct {
	DataFolder  string `json:"data_folder" envconfig:"optional,default=/etc/caddy/"`
	LogFolder   string `json:"log_folder" envconfig:"optional,default=/var/log/caddy"`
	CaddyFile   string `json:"caddy_file" envconfig:"optional,default=/etc/caddy/Caddyfile"`
	FrontendDir string `json:"frontend_dir" envconfig:"optional"`
	Caddy       caddy  `json:"caddy"`
	Auth        auth   `json:"auth"`
	Admin       admin  `json:"admin"`
	Log         log    `json:"log"`
}
