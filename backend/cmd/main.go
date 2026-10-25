package main

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/Pacerino/CaddyProxyManager/internal/api"
	"github.com/Pacerino/CaddyProxyManager/internal/api/handler"
	"github.com/Pacerino/CaddyProxyManager/internal/auth"
	"github.com/Pacerino/CaddyProxyManager/internal/caddy"
	"github.com/Pacerino/CaddyProxyManager/internal/caddy/pluginmgr"
	"github.com/Pacerino/CaddyProxyManager/internal/caddy/supervisor"
	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/database"
	"github.com/Pacerino/CaddyProxyManager/internal/jobqueue"
	"github.com/Pacerino/CaddyProxyManager/internal/logger"
	"github.com/Pacerino/CaddyProxyManager/internal/reconcile"

	"gorm.io/gorm"
)

var (
	version = "3.0.0"
	commit  = "abcdefgh"
)

// reconcileTimeout bounds one reconcile job so a Caddy outage can't block the
// serialized worker (and every handler job behind it) forever.
const reconcileTimeout = 2 * time.Minute

func main() {
	config.Init(&version, &commit)
	if err := auth.EnsureKey(); err != nil {
		logger.Error("KeyBootstrapError", err)
	}
	database.NewDB()
	jobqueue.Start()

	h := handler.NewHandler()
	var sup *supervisor.Supervisor
	if strings.EqualFold(config.Configuration.Caddy.Mode, "api") {
		db := database.GetInstance()
		p := caddy.NewAPIProvider()
		if config.Configuration.Caddy.Supervise {
			sup = newSupervisor(db, p)
		}
		mgr := newPluginManager(db, p, sup, h)
		if sup != nil {
			// A managed binary that can't run falls back to the shipped one.
			sup.Fallback = mgr.FallBackToShipped
			if err := sup.Start(context.Background()); err != nil {
				logger.Warn("Starting Caddy: %v", err)
			}
		}
		setup := func(ctx context.Context) pluginmgr.SetupStatus { return detectSetup(ctx, mgr) }
		h.Plugins = mgr
		h.PluginSetup = setup
		// Seed the managed binary for a systemd unit before anyone sees the drop-in.
		setup(context.Background())
		startReconcile(db, p, func(ctx context.Context) {
			if !config.Configuration.PluginInstall || !setup(ctx).Switchable() {
				return
			}
			mgr.AutoUpdateOnStartup(ctx, db)
		})
	}

	// HTTP Server
	go api.StartServer(h)

	// Clean Quit
	irqchan := make(chan os.Signal, 1)
	signal.Notify(irqchan, syscall.SIGINT, syscall.SIGTERM)

	for irq := range irqchan {
		if irq == syscall.SIGINT || irq == syscall.SIGTERM {
			logger.Info("Got ", irq, " shutting server down ...")
			break
		}
	}
	if sup != nil {
		if err := sup.Stop(); err != nil {
			logger.Warn("Stopping Caddy: %v", err)
		}
	}
}

// managedBinaryPath is the "current" symlink of the managed Caddy binaries.
func managedBinaryPath() string {
	return filepath.Join(config.Configuration.DataFolder, "caddy-bin", "current")
}

// detectSetup reports whether Caddy can be restarted on a managed binary.
// When a systemd unit needs the drop-in and plugin install is on, the managed
// binary is seeded first (a copy of the configured one); DetectSetup never
// offers the drop-in while that binary is missing.
func detectSetup(ctx context.Context, mgr *pluginmgr.Manager) pluginmgr.SetupStatus {
	c := config.Configuration.Caddy
	cfg := pluginmgr.SetupConfig{
		Supervise:      c.Supervise,
		ReloadStrategy: c.ReloadStrategy,
		Service:        c.Service,
		ManagedPath:    managedBinaryPath(),
		AdminURL:       c.AdminURL,
	}
	if config.Configuration.PluginInstall {
		cfg.Seed = func() error {
			err := mgr.EnsureSeeded()
			if err != nil {
				logger.Warn("Seeding the managed Caddy binary: %v", err)
			}
			return err
		}
	}
	return pluginmgr.DetectSetup(ctx, cfg, pluginmgr.DBusUnit{})
}

// newSupervisor prepares running Caddy as a child process. After a
// crash-restart the config is re-applied; intentional restarts don't trigger
// that (the plugin manager reconciles itself).
func newSupervisor(db *gorm.DB, p *caddy.APIProvider) *supervisor.Supervisor {
	return &supervisor.Supervisor{
		Binary:     caddy.ActiveBinary,
		ConfigFile: config.CaddyConfigFile(),
		OnRestart: func() {
			go func() {
				for _, err := range runReconcile(context.Background(), db, p) {
					logger.Warn("Caddy reconcile after restart: %v", err)
				}
			}()
		},
	}
}

// newPluginManager builds the plugin manager. A supervised Caddy is restarted
// by the supervisor, otherwise through systemd; the endpoints refuse changes
// while the setup doesn't allow them.
func newPluginManager(db *gorm.DB, p *caddy.APIProvider, sup *supervisor.Supervisor, h *handler.Handler) *pluginmgr.Manager {
	var restarter pluginmgr.Restarter = &pluginmgr.SystemdRestarter{Unit: pluginmgr.DBusUnit{}, Service: config.Configuration.Caddy.Service}
	var caddyLogs func() []string
	if sup != nil {
		restarter = sup
		// A rolled-back operation reports what the failed Caddy printed.
		caddyLogs = sup.RecentLogs
	}
	return &pluginmgr.Manager{
		Builds:  pluginmgr.NewBuildService(),
		Inspect: pluginmgr.ExecInspector{},
		Store:   pluginmgr.NewStore(config.Configuration.DataFolder),
		Restart: restarter,
		Ping:    p.Ping,
		LiveConfig: func(ctx context.Context) ([]byte, error) {
			var raw json.RawMessage
			if err := p.GetConfig("", &raw); err != nil {
				return nil, err
			}
			return raw, nil
		},
		Reconcile: func(ctx context.Context) []error {
			return runReconcile(ctx, db, p)
		},
		ShippedBinary: config.Configuration.Caddy.Binary,
		SchemaOut:     filepath.Join(config.Configuration.DataFolder, "caddy_schema.json"),
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		DB:            db,
		// The DNS provider forms follow the binary that now runs.
		OnChanged: h.ReloadDNSSchemas,
		CaddyLogs: caddyLogs,
	}
}

// runReconcile re-applies everything to Caddy on the job queue, to stay
// serialized with handler jobs, and waits for the result.
func runReconcile(ctx context.Context, db *gorm.DB, p *caddy.APIProvider) []error {
	res := make(chan []error, 1)
	err := jobqueue.AddJob(jobqueue.Job{Name: "CaddyReconcile", Action: func() error {
		jobCtx, cancel := context.WithTimeout(ctx, reconcileTimeout)
		defer cancel()
		res <- reconcile.Run(jobCtx, db, p, caddy.ListModules, reconcile.DefaultBackoff)
		return nil
	}})
	if err != nil {
		return []error{err}
	}
	select {
	case errs := <-res:
		return errs
	case <-ctx.Done():
		return []error{ctx.Err()}
	}
}

// startReconcile migrates legacy config and re-applies everything to Caddy in
// the background, so the HTTP server doesn't wait for Caddy to come up. After
// the first reconcile, afterFirst runs.
func startReconcile(db *gorm.DB, p *caddy.APIProvider, afterFirst func(ctx context.Context)) {
	if migrated, err := reconcile.MigrateLegacyCloudflare(db); err != nil {
		logger.Warn("Legacy Cloudflare migration failed: %v", err)
	} else if migrated {
		logger.Info("Migrated legacy Cloudflare config to a DNS provider")
	}

	go func() {
		ctx := context.Background()
		if err := reconcile.WaitForCaddy(ctx, p, reconcile.DefaultBackoff); err != nil {
			logger.Warn("Caddy reconcile: waiting for Caddy failed: %v", err)
			return
		}
		for _, err := range runReconcile(ctx, db, p) {
			logger.Warn("Caddy reconcile: %v", err)
		}
		afterFirst(ctx)
	}()
}
