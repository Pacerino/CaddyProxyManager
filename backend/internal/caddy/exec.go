package caddy

import (
	"context"
	"time"

	"github.com/Pacerino/CaddyProxyManager/internal/logger"
	"github.com/coreos/go-systemd/v22/dbus"
)

func ReloadCaddy() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, err := dbus.NewSystemdConnectionContext(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	resChan := make(chan string)
	_, err = conn.ReloadOrRestartUnitContext(ctx, "caddy.service", "replace", resChan)
	if err != nil {
		logger.Error("", err)
		return err
	}
	<-resChan
	return nil
}
