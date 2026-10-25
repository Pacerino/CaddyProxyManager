package handler

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	h "github.com/Pacerino/CaddyProxyManager/internal/api/http"
	"github.com/Pacerino/CaddyProxyManager/internal/caddy"
	"github.com/Pacerino/CaddyProxyManager/internal/caddy/dnsschema"
	"github.com/Pacerino/CaddyProxyManager/internal/caddy/schema"
	"github.com/Pacerino/CaddyProxyManager/internal/database"
	"github.com/Pacerino/CaddyProxyManager/internal/jobqueue"
	"github.com/Pacerino/CaddyProxyManager/internal/logger"
	"github.com/Pacerino/CaddyProxyManager/internal/reconcile"

	"github.com/go-chi/chi/v5"
)

// dnsProviderHost is a host referencing a DNS provider credential.
type dnsProviderHost struct {
	ID      uint   `json:"id"`
	Domains string `json:"domains"`
}

// dnsProviderResponse is the view of a stored credential. Secret values are
// never included; SecretsSet names the secret keys that have a stored value.
type dnsProviderResponse struct {
	ID         uint              `json:"id"`
	Name       string            `json:"name"`
	ModuleID   string            `json:"moduleId"`
	Values     map[string]any    `json:"values"`
	SecretsSet []string          `json:"secretsSet"`
	UsedBy     []dnsProviderHost `json:"usedBy"`
	Available  bool              `json:"available"`
}

// dnsProviderRequest is the body of create and update.
type dnsProviderRequest struct {
	Name     string          `json:"name"`
	ModuleID string          `json:"moduleId"`
	Values   json.RawMessage `json:"values"`
}

// GetDNSProviderTypes lists the DNS provider modules of the Caddy build with
// their config fields.
// Route: GET /caddy/dns-provider-types
func (s Handler) GetDNSProviderTypes() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := apiProviderOrError(w, r); !ok {
			return
		}
		types, err := s.dnsSchemas().Providers(r.Context())
		if err != nil {
			h.ResultErrorJSON(w, r, http.StatusInternalServerError, err.Error(), nil)
			return
		}
		if types == nil {
			types = []dnsschema.Provider{}
		}
		h.ResultResponseJSON(w, r, http.StatusOK, types)
	}
}

// GetDNSProviders lists the stored DNS provider credentials.
// Route: GET /caddy/dns-providers
func (s Handler) GetDNSProviders() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := apiProviderOrError(w, r); !ok {
			return
		}
		var rows []database.DNSProvider
		if err := s.DB.Order("id").Find(&rows).Error; err != nil {
			h.ResultErrorJSON(w, r, http.StatusInternalServerError, err.Error(), nil)
			return
		}
		usage, err := s.dnsProviderUsage()
		if err != nil {
			h.ResultErrorJSON(w, r, http.StatusInternalServerError, err.Error(), nil)
			return
		}
		available := s.availableModules()
		fields := s.providerFields(r)

		out := make([]dnsProviderResponse, 0, len(rows))
		for _, row := range rows {
			out = append(out, s.dnsProviderView(row, fields[row.ModuleID], available, usage[row.ID]))
		}
		h.ResultResponseJSON(w, r, http.StatusOK, out)
	}
}

// CreateDNSProvider stores a new credential and re-syncs the TLS policies.
// Route: POST /caddy/dns-providers
func (s Handler) CreateDNSProvider() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		provider, ok := apiProviderOrError(w, r)
		if !ok {
			return
		}
		req, typ, ok := s.parseDNSProviderRequest(w, r)
		if !ok {
			return
		}
		if !s.checkDNSProviderName(w, r, req.Name, 0) {
			return
		}
		values, ok := s.mergeDNSValues(w, r, typ, req.Values, nil)
		if !ok {
			return
		}
		cfg, _ := json.Marshal(values)
		row := database.DNSProvider{Name: req.Name, ModuleID: req.ModuleID, Config: database.JSON(cfg)}
		if err := s.DB.Create(&row).Error; err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}
		if !s.enqueueSyncTLS(w, r, provider) {
			return
		}
		h.ResultResponseJSON(w, r, http.StatusCreated,
			s.dnsProviderView(row, typ.Fields, s.availableModules(), nil))
	}
}

// UpdateDNSProvider changes a credential. An empty or absent secret keeps the
// stored value unless the module changed, which discards the old values.
// Route: PUT /caddy/dns-providers/{id}
func (s Handler) UpdateDNSProvider() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		provider, ok := apiProviderOrError(w, r)
		if !ok {
			return
		}
		var row database.DNSProvider
		if err := s.DB.First(&row, chi.URLParam(r, "id")).Error; err != nil {
			h.ResultErrorJSON(w, r, http.StatusNotFound, "dns provider not found", nil)
			return
		}
		req, typ, ok := s.parseDNSProviderRequest(w, r)
		if !ok {
			return
		}
		if !s.checkDNSProviderName(w, r, req.Name, row.ID) {
			return
		}

		var stored map[string]any
		if row.ModuleID == req.ModuleID {
			stored = decodeConfig(row.Config)
		}
		values, ok := s.mergeDNSValues(w, r, typ, req.Values, stored)
		if !ok {
			return
		}
		cfg, _ := json.Marshal(values)
		row.Name, row.ModuleID, row.Config = req.Name, req.ModuleID, database.JSON(cfg)
		if err := s.DB.Save(&row).Error; err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}
		if !s.enqueueSyncTLS(w, r, provider) {
			return
		}
		usage, err := s.dnsProviderUsage()
		if err != nil {
			h.ResultErrorJSON(w, r, http.StatusInternalServerError, err.Error(), nil)
			return
		}
		h.ResultResponseJSON(w, r, http.StatusOK,
			s.dnsProviderView(row, typ.Fields, s.availableModules(), usage[row.ID]))
	}
}

// DeleteDNSProvider removes a credential no host references. It is a hard
// delete so the name can be reused.
// Route: DELETE /caddy/dns-providers/{id}
func (s Handler) DeleteDNSProvider() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		provider, ok := apiProviderOrError(w, r)
		if !ok {
			return
		}
		var row database.DNSProvider
		if err := s.DB.First(&row, chi.URLParam(r, "id")).Error; err != nil {
			h.ResultErrorJSON(w, r, http.StatusNotFound, "dns provider not found", nil)
			return
		}
		usage, err := s.dnsProviderUsage()
		if err != nil {
			h.ResultErrorJSON(w, r, http.StatusInternalServerError, err.Error(), nil)
			return
		}
		if used := usage[row.ID]; len(used) > 0 {
			h.ResultErrorJSON(w, r, http.StatusConflict, "dns provider is used by hosts", used)
			return
		}
		if err := s.DB.Unscoped().Delete(&database.DNSProvider{}, row.ID).Error; err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}
		if !s.enqueueSyncTLS(w, r, provider) {
			return
		}
		h.ResultResponseJSON(w, r, http.StatusOK, true)
	}
}

// parseDNSProviderRequest decodes the body and resolves the requested type.
// It writes the 400 response itself and reports ok=false on failure.
func (s Handler) parseDNSProviderRequest(w http.ResponseWriter, r *http.Request) (dnsProviderRequest, dnsschema.Provider, bool) {
	var req dnsProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
		return req, dnsschema.Provider{}, false
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		h.ResultErrorJSON(w, r, http.StatusBadRequest, "name is required", nil)
		return req, dnsschema.Provider{}, false
	}
	types, err := s.dnsSchemas().Providers(r.Context())
	if err != nil {
		h.ResultErrorJSON(w, r, http.StatusInternalServerError, err.Error(), nil)
		return req, dnsschema.Provider{}, false
	}
	for _, t := range types {
		if t.ModuleID == req.ModuleID {
			return req, t, true
		}
	}
	h.ResultErrorJSON(w, r, http.StatusBadRequest, "unknown DNS provider module "+req.ModuleID, nil)
	return req, dnsschema.Provider{}, false
}

// checkDNSProviderName rejects a name used by another credential (400).
func (s Handler) checkDNSProviderName(w http.ResponseWriter, r *http.Request, name string, selfID uint) bool {
	var n int64
	if err := s.DB.Model(&database.DNSProvider{}).Where("name = ? AND id <> ?", name, selfID).Count(&n).Error; err != nil {
		h.ResultErrorJSON(w, r, http.StatusInternalServerError, err.Error(), nil)
		return false
	}
	if n > 0 {
		h.ResultErrorJSON(w, r, http.StatusBadRequest, "a DNS provider with this name already exists", nil)
		return false
	}
	return true
}

// mergeDNSValues validates the submitted values against the type and returns
// the config to store: the submitted values, with every secret that is empty
// or absent taken from stored.
func (s Handler) mergeDNSValues(w http.ResponseWriter, r *http.Request, typ dnsschema.Provider, raw json.RawMessage, stored map[string]any) (map[string]any, bool) {
	values := map[string]any{}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &values); err != nil || values == nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, "values must be a JSON object", nil)
			return nil, false
		}
	}
	secrets := secretKeys(typ.Fields, stored, values)
	for key := range secrets {
		if v, present := values[key]; !present || v == nil || v == "" {
			delete(values, key)
			if old, had := stored[key]; had && old != nil && old != "" {
				values[key] = old
			}
		}
	}
	if typ.Source != dnsschema.SourceNone {
		if errs := (&schema.Schema{Fields: typ.Fields}).Validate(values); len(errs) > 0 {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, "schema validation failed", errs)
			return nil, false
		}
	}
	return values, true
}

// secretKeys returns the secret keys among the given configs: the fields typed
// secret, plus keys without a field definition that look like credentials.
func secretKeys(fields []schema.Field, configs ...map[string]any) map[string]bool {
	defined := map[string]bool{}
	secrets := map[string]bool{}
	for _, f := range fields {
		defined[f.Key] = true
		if f.Type == schema.FieldSecret {
			secrets[f.Key] = true
		}
	}
	for _, cfg := range configs {
		for key := range cfg {
			if !defined[key] && dnsschema.IsSecretKey(key) {
				secrets[key] = true
			}
		}
	}
	return secrets
}

func decodeConfig(raw database.JSON) map[string]any {
	var cfg map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &cfg)
	}
	return cfg
}

// dnsProviderView renders a stored credential without its secrets.
func (s Handler) dnsProviderView(row database.DNSProvider, fields []schema.Field, available map[string]bool, usedBy []dnsProviderHost) dnsProviderResponse {
	cfg := decodeConfig(row.Config)
	secrets := secretKeys(fields, cfg)
	values := map[string]any{}
	set := []string{}
	for key, v := range cfg {
		if secrets[key] {
			if v != nil && v != "" {
				set = append(set, key)
			}
			continue
		}
		values[key] = v
	}
	sort.Strings(set)
	if usedBy == nil {
		usedBy = []dnsProviderHost{}
	}
	return dnsProviderResponse{
		ID: row.ID, Name: row.Name, ModuleID: row.ModuleID,
		Values: values, SecretsSet: set, UsedBy: usedBy, Available: available[row.ModuleID],
	}
}

// providerFields maps module ID to its fields; empty when the resolver fails,
// in which case masking falls back to the key heuristic.
func (s Handler) providerFields(r *http.Request) map[string][]schema.Field {
	out := map[string][]schema.Field{}
	types, err := s.dnsSchemas().Providers(r.Context())
	if err != nil {
		logger.Warn("resolve DNS provider types: %v", err)
		return out
	}
	for _, t := range types {
		out[t.ModuleID] = t.Fields
	}
	return out
}

// availableModules is the set of module IDs present in the Caddy build.
func (s Handler) availableModules() map[string]bool {
	out := map[string]bool{}
	modules, err := s.listModules()()
	if err != nil {
		logger.Warn("list caddy modules: %v", err)
		return out
	}
	for _, m := range modules {
		out[m.ID] = true
	}
	return out
}

// dnsProviderUsage maps credential ID to the hosts referencing it.
func (s Handler) dnsProviderUsage() (map[uint][]dnsProviderHost, error) {
	var hosts []database.Host
	if err := s.DB.Where("dns_provider_id IS NOT NULL").Order("id").Find(&hosts).Error; err != nil {
		return nil, err
	}
	out := map[uint][]dnsProviderHost{}
	for _, host := range hosts {
		out[*host.DNSProviderID] = append(out[*host.DNSProviderID], dnsProviderHost{ID: host.ID, Domains: host.Domains})
	}
	return out, nil
}

// enqueueSyncTLS queues a TLS policy sync, writing a 400 if the queue refuses.
func (s Handler) enqueueSyncTLS(w http.ResponseWriter, r *http.Request, provider *caddy.APIProvider) bool {
	db, listModules := s.DB, s.listModules()
	if err := jobqueue.AddJob(jobqueue.Job{
		Name: "CaddySyncTLS",
		// A failed sync must not stop the app (the job queue exits on a
		// returned error), so it is logged and the next change retries.
		Action: func() error {
			if err := reconcile.SyncTLS(db, provider, listModules); err != nil {
				logger.Warn("TLS sync failed: %v", err)
			}
			return nil
		},
	}); err != nil {
		h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
		return false
	}
	return true
}
