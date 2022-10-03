package database

import "gorm.io/gorm"

type Host struct {
	gorm.Model
	Domains   string `json:"domains" validate:"required,fqdn|hostname_port"`
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
	Secret string
}
