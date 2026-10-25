package handler

import (
	"context"

	"github.com/Pacerino/CaddyProxyManager/internal/caddy"
	"github.com/Pacerino/CaddyProxyManager/internal/caddy/dnsschema"
	"github.com/Pacerino/CaddyProxyManager/internal/database"

	"gorm.io/gorm"
)

// DNSSchemaResolver lists the DNS provider types of the Caddy build with their
// config fields. *dnsschema.Resolver implements it; tests stub it.
type DNSSchemaResolver interface {
	Providers(ctx context.Context) ([]dnsschema.Provider, error)
}

type Handler struct {
	DB *gorm.DB
	// DNSSchemas resolves DNS provider types; nil falls back to
	// dnsschema.NewResolver().
	DNSSchemas DNSSchemaResolver
	// ListModules reports the modules of the Caddy build; nil falls back to
	// caddy.ListModules.
	ListModules func() ([]caddy.Module, error)
}

func NewHandler() *Handler {
	db := database.GetInstance()
	handler := &Handler{
		DB:          db,
		DNSSchemas:  dnsschema.NewResolver(),
		ListModules: caddy.ListModules,
	}
	return handler
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
