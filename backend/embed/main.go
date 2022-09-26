package embed

import "embed"

// Frontend Files served by this app
//
//go:embed all:assets/**
var Assets embed.FS

// CaddyFiles hold all template for caddy
//
//go:embed caddy
var CaddyFiles embed.FS
