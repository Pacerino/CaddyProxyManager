package database

import "gorm.io/gorm"

type Host struct {
	gorm.Model
	Domains   string `json:"domains" validate:"required,domains"`
	Matcher   string `json:"matcher"`
	Upstreams []Upstream
}

type Upstream struct {
	gorm.Model
	HostID  uint   `json:"hostId"`
	Backend string `json:"backend" validate:"required,hostname_port"`
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
