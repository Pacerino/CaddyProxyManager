// Package reconcile re-applies CPM's stored state to Caddy and keeps the TLS
// automation policies in sync with the hosts' DNS providers.
package reconcile

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Pacerino/CaddyProxyManager/internal/caddy"
	"github.com/Pacerino/CaddyProxyManager/internal/database"
	"github.com/Pacerino/CaddyProxyManager/internal/logger"

	"gorm.io/gorm"
)

const dnsProviderPrefix = "dns.providers."

// DefaultBackoff waits 1s after the first failed ping, doubling up to 30s.
func DefaultBackoff(attempt int) time.Duration {
	d := time.Second
	for i := 0; i < attempt && d < 30*time.Second; i++ {
		d *= 2
	}
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	return d
}

// SyncTLS rebuilds the per-host TLS policies from the database and pushes them
// to Caddy. Hosts whose provider is missing or not compiled into Caddy are
// skipped with a warning.
func SyncTLS(db *gorm.DB, p *caddy.APIProvider, listModules func() ([]caddy.Module, error)) error {
	var hosts []database.Host
	if err := db.Where("dns_provider_id IS NOT NULL").Find(&hosts).Error; err != nil {
		return fmt.Errorf("load hosts: %w", err)
	}
	var list []database.DNSProvider
	if err := db.Find(&list).Error; err != nil {
		return fmt.Errorf("load dns providers: %w", err)
	}
	providers := make(map[uint]database.DNSProvider, len(list))
	for _, d := range list {
		providers[d.ID] = d
	}
	modules, err := listModules()
	if err != nil {
		return fmt.Errorf("list caddy modules: %w", err)
	}
	available := map[string]bool{}
	for _, m := range modules {
		if strings.HasPrefix(m.ID, dnsProviderPrefix) {
			available[m.ID] = true
		}
	}

	policies, warnings := caddy.BuildTLSPolicies(hosts, providers, available)
	for _, w := range warnings {
		logger.Warn("TLS sync: %s", w)
	}
	return p.SyncTLSPolicies(policies)
}

// WaitForCaddy pings Caddy's admin API until it answers, sleeping
// backoff(attempt) between tries. It returns ctx's error if ctx ends first.
func WaitForCaddy(ctx context.Context, p *caddy.APIProvider, backoff func(attempt int) time.Duration) error {
	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := p.Ping(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff(attempt)):
		}
	}
}

// Run waits for Caddy and then re-applies every host route, the TLS policies
// and all enabled module configs. It continues past per-item failures and
// returns all of them.
func Run(ctx context.Context, db *gorm.DB, p *caddy.APIProvider, listModules func() ([]caddy.Module, error), backoff func(attempt int) time.Duration) []error {
	if err := WaitForCaddy(ctx, p, backoff); err != nil {
		return []error{fmt.Errorf("waiting for caddy: %w", err)}
	}

	var errs []error
	if err := p.EnsureServer(); err != nil {
		errs = append(errs, fmt.Errorf("ensure server: %w", err))
	}

	var hosts []database.Host
	if err := db.Preload("Upstreams").Preload("Plugins").Find(&hosts).Error; err != nil {
		errs = append(errs, fmt.Errorf("load hosts: %w", err))
	}
	for _, h := range hosts {
		if err := p.WriteHost(h); err != nil {
			errs = append(errs, fmt.Errorf("write host %d: %w", h.ID, err))
		}
	}

	if err := SyncTLS(db, p, listModules); err != nil {
		errs = append(errs, fmt.Errorf("sync tls: %w", err))
	}

	var configs []database.ModuleConfig
	if err := db.Where("enabled = ?", true).Find(&configs).Error; err != nil {
		errs = append(errs, fmt.Errorf("load module configs: %w", err))
	}
	for _, mc := range configs {
		if err := p.ApplyModuleConfig(mc); err != nil {
			errs = append(errs, fmt.Errorf("apply module config %s: %w", mc.ModuleID, err))
		}
	}
	return errs
}
