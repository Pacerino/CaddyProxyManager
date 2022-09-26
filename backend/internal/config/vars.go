package config

import "github.com/Pacerino/CaddyProxyManager/internal/logger"

// IsSetup defines whether we have an admin user or not
var IsSetup bool

var logLevel logger.Level

type log struct {
	Level  string `json:"level" envconfig:"optional,default=debug"`
	Format string `json:"format" envconfig:"optional,default=nice"`
}

// Configuration is the main configuration object
var Configuration struct {
	DataFolder string `json:"data_folder" envconfig:"optional,default=/etc/caddy/"`
	LogFolder  string `json:"log_folder" envconfig:"optional,default=/var/log/caddy"`
	CaddyFile  string `json:"caddy_file" envconfig:"optional,default=/etc/caddy/Caddyfile"`
	Log        log    `json:"log"`
}
