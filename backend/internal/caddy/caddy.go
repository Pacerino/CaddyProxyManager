package caddy

import (
	"fmt"
	"os"

	"github.com/Pacerino/CaddyProxyManager/embed"
	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/database"
	"github.com/Pacerino/CaddyProxyManager/internal/logger"

	"github.com/aymerick/raymond"
)

// CaddyfileProvider applies host configuration by rendering a per-host
// Caddyfile snippet into the data folder and reloading Caddy.
type CaddyfileProvider struct{}

type hostEntity struct {
	Host    database.Host
	LogPath string
}

func hostConfigPath(hostID uint) string {
	return fmt.Sprintf("%s/host_%d.conf", config.Configuration.DataFolder, hostID)
}

// WriteHost renders the host template and writes it to the config folder.
func (p *CaddyfileProvider) WriteHost(h database.Host) error {
	data := &hostEntity{
		Host:    h,
		LogPath: fmt.Sprintf("%s/host_%d.log", config.Configuration.LogFolder, h.ID),
	}
	filename := hostConfigPath(h.ID)
	// Read Template from Embed FS
	template, err := embed.CaddyFiles.ReadFile("caddy/host.hbs")
	if err != nil {
		logger.Error(err.Error(), err)
		return err
	}

	// Parse Data into Template
	tmplOutput, err := raymond.Render(string(template), data)
	if err != nil {
		logger.Error(err.Error(), err)
		return err
	}
	// Write filled out template to the config folder
	if err := os.WriteFile(filename, []byte(tmplOutput), 0644); err != nil {
		return err
	}

	return Reload()
}

// RemoveHost deletes the per-host config file and reloads Caddy.
func (p *CaddyfileProvider) RemoveHost(hostID int) error {
	filename := hostConfigPath(uint(hostID))
	if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
		return err
	}
	return Reload()
}
