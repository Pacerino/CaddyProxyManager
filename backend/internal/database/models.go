package database

import (
	"database/sql/driver"
	"encoding/json"
	"errors"

	"gorm.io/gorm"
)

type Host struct {
	gorm.Model
	Domains   string `json:"domains" validate:"required,domains"`
	Matcher   string `json:"matcher"`
	Upstreams []Upstream
	Plugins   []HostPlugin
}

// HostPlugin is a per-host plugin configuration. Handler holds the rendered
// Caddy route handler JSON (secrets already hashed), which is injected into
// the host's route. ModuleID is the dotted Caddy module name.
type HostPlugin struct {
	gorm.Model
	HostID   uint   `json:"hostId" gorm:"index"`
	ModuleID string `json:"moduleId" validate:"required"`
	// Handler is the rendered route handler object applied to the route.
	Handler JSON `json:"handler"`
	Enabled bool `json:"enabled" gorm:"default:true"`
}

type Upstream struct {
	gorm.Model
	HostID  uint   `json:"hostId"`
	Backend string `json:"backend" validate:"required,hostname_port"`
}

// JSON is a json.RawMessage that persists as TEXT in SQLite via the
// Valuer/Scanner interfaces.
type JSON json.RawMessage

// Value implements driver.Valuer.
func (j JSON) Value() (driver.Value, error) {
	if len(j) == 0 {
		return "null", nil
	}
	return string(j), nil
}

// Scan implements sql.Scanner.
func (j *JSON) Scan(value any) error {
	if value == nil {
		*j = JSON("null")
		return nil
	}
	switch v := value.(type) {
	case []byte:
		*j = append((*j)[0:0], v...)
	case string:
		*j = append((*j)[0:0], v...)
	default:
		return errors.New("unsupported type for JSON column")
	}
	return nil
}

// MarshalJSON makes JSON serialise as raw JSON rather than a byte array.
func (j JSON) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte("null"), nil
	}
	return j, nil
}

// UnmarshalJSON captures raw JSON as-is.
func (j *JSON) UnmarshalJSON(data []byte) error {
	*j = append((*j)[0:0], data...)
	return nil
}

// ModuleConfig stores a configuration fragment for a single Caddy module
// (plugin). Config is applied to the live Caddy config at Path via the admin
// API. ModuleID is the dotted Caddy module name, e.g. "dns.providers.cloudflare".
type ModuleConfig struct {
	gorm.Model
	// ModuleID is the dotted Caddy module name this fragment configures.
	ModuleID string `json:"moduleId" gorm:"uniqueIndex" validate:"required"`
	// Path is the admin API config path the fragment is patched into,
	// e.g. "apps/tls/automation/policies". Relative to /config/.
	Path string `json:"path" validate:"required"`
	// Config is the JSON fragment patched into Path.
	Config JSON `json:"config" validate:"required"`
	// Enabled controls whether the fragment is applied on reconcile.
	Enabled bool `json:"enabled" gorm:"default:true"`
}

type User struct {
	gorm.Model
	Name   string `validate:"required"`
	Email  string `gorm:"unique" validate:"required,email"`
	Secret string `json:"secret,omitempty"`
	// Provider is the auth source for this user: "local" or "oidc".
	Provider string `json:"provider" gorm:"default:local"`
	// Subject is the OIDC subject identifier (sub claim), empty for local users.
	Subject string `json:"subject,omitempty" gorm:"index"`
}
