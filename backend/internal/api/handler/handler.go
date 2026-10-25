package handler

import (
	"context"
	"sync/atomic"

	"github.com/Pacerino/CaddyProxyManager/internal/caddy"
	"github.com/Pacerino/CaddyProxyManager/internal/caddy/dnsschema"
	"github.com/Pacerino/CaddyProxyManager/internal/caddy/pluginmgr"
	"github.com/Pacerino/CaddyProxyManager/internal/database"

	"gorm.io/gorm"
)

// DNSSchemaResolver lists the DNS provider types of the Caddy build with their
// config fields. *dnsschema.Resolver implements it; tests stub it.
type DNSSchemaResolver interface {
	Providers(ctx context.Context) ([]dnsschema.Provider, error)
}

// SwappableResolver is a DNSSchemaResolver whose target can be replaced while
// requests use it, so the DNS provider forms follow a new Caddy binary.
type SwappableResolver struct {
	cur atomic.Pointer[resolverBox]
}

type resolverBox struct{ r DNSSchemaResolver }

// NewSwappableResolver returns a SwappableResolver delegating to r.
func NewSwappableResolver(r DNSSchemaResolver) *SwappableResolver {
	s := &SwappableResolver{}
	s.Set(r)
	return s
}

// Set replaces the resolver used by later calls.
func (s *SwappableResolver) Set(r DNSSchemaResolver) {
	s.cur.Store(&resolverBox{r: r})
}

func (s *SwappableResolver) Providers(ctx context.Context) ([]dnsschema.Provider, error) {
	return s.cur.Load().r.Providers(ctx)
}

type Handler struct {
	DB *gorm.DB
	// DNSSchemas resolves DNS provider types; nil falls back to
	// dnsschema.NewResolver().
	DNSSchemas DNSSchemaResolver
	// ListModules reports the modules of the Caddy build; nil falls back to
	// caddy.ListModules.
	ListModules func() ([]caddy.Module, error)
	// Plugins installs and removes Caddy plugins; nil outside API mode.
	Plugins PluginManager
	// PluginSetup reports whether Caddy can be switched to a managed binary.
	PluginSetup func(ctx context.Context) pluginmgr.SetupStatus
}

func NewHandler() *Handler {
	db := database.GetInstance()
	handler := &Handler{
		DB:          db,
		DNSSchemas:  NewSwappableResolver(dnsschema.NewResolver()),
		ListModules: caddy.ListModules,
	}
	return handler
}

// ReloadDNSSchemas rebuilds the DNS schema resolver for the active Caddy
// binary. It is a no-op unless DNSSchemas is a SwappableResolver.
func (s Handler) ReloadDNSSchemas() {
	if sw, ok := s.DNSSchemas.(*SwappableResolver); ok {
		sw.Set(dnsschema.NewResolver())
	}
}

// dnsSchemas returns the configured resolver or the default one.
func (s Handler) dnsSchemas() DNSSchemaResolver {
	if s.DNSSchemas != nil {
		return s.DNSSchemas
	}
	return dnsschema.NewResolver()
}

// listModules returns the configured module lister or caddy.ListModules.
func (s Handler) listModules() func() ([]caddy.Module, error) {
	if s.ListModules != nil {
		return s.ListModules
	}
	return caddy.ListModules
}
