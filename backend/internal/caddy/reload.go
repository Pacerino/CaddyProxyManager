package caddy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/logger"
	"github.com/coreos/go-systemd/v22/dbus"
)

// Reload triggers Caddy to pick up configuration changes using the strategy
// configured via CPM_CADDY_RELOADSTRATEGY.
func Reload() error {
	switch strings.ToLower(config.Configuration.Caddy.ReloadStrategy) {
	case "", "systemd":
		return reloadSystemd()
	case "exec":
		return reloadExec()
	case "api":
		return reloadAPI()
	case "none":
		return nil
	default:
		return fmt.Errorf("unknown reload strategy %q (expected systemd, exec, api or none)", config.Configuration.Caddy.ReloadStrategy)
	}
}

// reloadSystemd reloads the caddy unit over the systemd D-Bus interface.
func reloadSystemd() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := dbus.NewSystemdConnectionContext(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	resChan := make(chan string)
	_, err = conn.ReloadOrRestartUnitContext(ctx, config.Configuration.Caddy.Service, "replace", resChan)
	if err != nil {
		logger.Error("CaddyReloadError", err)
		return err
	}
	<-resChan
	return nil
}

// reloadExec runs `caddy reload --config <Caddyfile>` to apply changes.
func reloadExec() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx,
		config.Configuration.Caddy.Binary,
		"reload",
		"--config", config.Configuration.CaddyFile,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		logger.Error("CaddyReloadError", err)
		return err
	}
	return nil
}

// reloadAPI loads the on-disk Caddyfile through the admin API's /load endpoint.
func reloadAPI() error {
	data, err := os.ReadFile(config.Configuration.CaddyFile)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	url := strings.TrimRight(config.Configuration.Caddy.AdminURL, "/") + "/load"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/caddyfile")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Error("CaddyReloadError", err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("caddy /load returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
