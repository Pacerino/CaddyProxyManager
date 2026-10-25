package main

import (
	"context"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Pacerino/CaddyProxyManager/internal/api"
	"github.com/Pacerino/CaddyProxyManager/internal/auth"
	"github.com/Pacerino/CaddyProxyManager/internal/caddy"
	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/database"
	"github.com/Pacerino/CaddyProxyManager/internal/jobqueue"
	"github.com/Pacerino/CaddyProxyManager/internal/logger"
	"github.com/Pacerino/CaddyProxyManager/internal/reconcile"
)

var (
	version = "3.0.0"
	commit  = "abcdefgh"
)

func main() {
	config.Init(&version, &commit)
	if err := auth.EnsureKey(); err != nil {
		logger.Error("KeyBootstrapError", err)
	}
	database.NewDB()
	jobqueue.Start()
	if strings.EqualFold(config.Configuration.Caddy.Mode, "api") {
		startReconcile()
	}

	// HTTP Server
	api.StartServer()

	// Clean Quit
	irqchan := make(chan os.Signal, 1)
	signal.Notify(irqchan, syscall.SIGINT, syscall.SIGTERM)

	for irq := range irqchan {
		if irq == syscall.SIGINT || irq == syscall.SIGTERM {
			logger.Info("Got ", irq, " shutting server down ...")
			break
		}
	}
}

// startReconcile migrates legacy config and re-applies everything to Caddy in
// the background, so the HTTP server doesn't wait for Caddy to come up. The
// reconcile itself runs on the job queue to stay serialized with handler jobs.
func startReconcile() {
	db := database.GetInstance()
	if migrated, err := reconcile.MigrateLegacyCloudflare(db); err != nil {
		logger.Warn("Legacy Cloudflare migration failed: %v", err)
	} else if migrated {
		logger.Info("Migrated legacy Cloudflare config to a DNS provider")
	}

	p := caddy.NewAPIProvider()
	go func() {
		ctx := context.Background()
		if err := reconcile.WaitForCaddy(ctx, p, reconcile.DefaultBackoff); err != nil {
			logger.Warn("Caddy reconcile: waiting for Caddy failed: %v", err)
			return
		}
		err := jobqueue.AddJob(jobqueue.Job{Name: "CaddyReconcile", Action: func() error {
			// Bound the wait so a Caddy outage can't block the serialized
			// worker (and every handler job behind it) forever.
			jobCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			for _, err := range reconcile.Run(jobCtx, db, p, caddy.ListModules, reconcile.DefaultBackoff) {
				logger.Warn("Caddy reconcile: %v", err)
			}
			return nil
		}})
		if err != nil {
			logger.Warn("Caddy reconcile: could not enqueue job: %v", err)
		}
	}()
}
