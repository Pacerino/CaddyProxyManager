package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	h "github.com/Pacerino/CaddyProxyManager/internal/api/http"
	"github.com/Pacerino/CaddyProxyManager/internal/caddy"
	"github.com/Pacerino/CaddyProxyManager/internal/caddy/schema"
	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/database"
	"github.com/Pacerino/CaddyProxyManager/internal/jobqueue"

	"github.com/go-chi/chi/v5"
)

// modulesResponse is the payload returned by GetCaddyModules.
type modulesResponse struct {
	// Modules is the flat, sorted list of every module the build reports.
	Modules []caddy.Module `json:"modules"`
	// Plugins is the subset that is not part of stock Caddy, i.e. the
	// user-supplied plugins this build was compiled with.
	Plugins []caddy.Module `json:"plugins"`
}

// GetCaddyModules returns the modules reported by the configured Caddy binary
// via `caddy list-modules --json`.
// Route: GET /caddy/modules
func (s Handler) GetCaddyModules() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		modules, err := caddy.ListModules()
		if err != nil {
			h.ResultErrorJSON(w, r, http.StatusInternalServerError, err.Error(), nil)
			return
		}

		plugins := make([]caddy.Module, 0)
		for _, m := range modules {
			if !m.Standard {
				plugins = append(plugins, m)
			}
		}

		h.ResultResponseJSON(w, r, http.StatusOK, modulesResponse{
			Modules: modules,
			Plugins: plugins,
		})
	}
}

// GetCaddyConfig returns the live Caddy configuration read through the admin
// API. Only available in API mode. An optional ?path= query selects a subtree.
// Route: GET /caddy/config
func (s Handler) GetCaddyConfig() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		provider, ok := apiProviderOrError(w, r)
		if !ok {
			return
		}

		var raw json.RawMessage
		if err := provider.GetConfig(r.URL.Query().Get("path"), &raw); err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadGateway, err.Error(), nil)
			return
		}
		if len(raw) == 0 {
			raw = json.RawMessage("null")
		}
		h.ResultResponseJSON(w, r, http.StatusOK, raw)
	}
}

// GetCaddySchemas returns the global-scope typed configuration schemas.
// Modules without a schema are configured through the raw JSON fallback.
// Route: GET /caddy/schemas
func (s Handler) GetCaddySchemas() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		h.ResultResponseJSON(w, r, http.StatusOK, schema.WithScope(schema.ScopeGlobal))
	}
}

// GetModuleConfigs lists stored module configurations.
// Route: GET /caddy/module-configs
func (s Handler) GetModuleConfigs() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var configs []database.ModuleConfig
		if err := s.DB.Find(&configs).Error; err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}
		h.ResultResponseJSON(w, r, http.StatusOK, configs)
	}
}

// moduleConfigRequest is the body accepted by SetModuleConfig. Either Config
// (raw JSON fallback) or Values (typed form for a known schema) must be set.
type moduleConfigRequest struct {
	ModuleID string          `json:"moduleId"`
	Path     string          `json:"path,omitempty"`
	Enabled  *bool           `json:"enabled,omitempty"`
	Config   json.RawMessage `json:"config,omitempty"`
	Values   map[string]any  `json:"values,omitempty"`
}

// SetModuleConfig creates or updates the configuration for a module and
// applies it to the live Caddy config. When a schema is registered for the
// module, Values are validated and rendered; otherwise raw Config is used.
// Route: PUT /caddy/module-configs
func (s Handler) SetModuleConfig() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		provider, ok := apiProviderOrError(w, r)
		if !ok {
			return
		}

		var req moduleConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}
		if req.ModuleID == "" {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, "moduleId is required", nil)
			return
		}

		path := req.Path
		var rawConfig json.RawMessage

		if sc, found := schema.Get(req.ModuleID); found {
			// Typed form: validate + render through the schema.
			built, err := sc.Build(req.Values)
			if err != nil {
				if ve, isVE := err.(*schema.ValidationError); isVE {
					h.ResultErrorJSON(w, r, http.StatusBadRequest, "schema validation failed", ve.Fields)
					return
				}
				h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
				return
			}
			rawConfig = built
			if path == "" {
				path = sc.Path
			}
		} else {
			// Raw JSON fallback.
			if len(req.Config) == 0 {
				h.ResultErrorJSON(w, r, http.StatusBadRequest, "config is required for modules without a schema", nil)
				return
			}
			if !json.Valid(req.Config) {
				h.ResultErrorJSON(w, r, http.StatusBadRequest, "config is not valid JSON", nil)
				return
			}
			rawConfig = req.Config
		}

		if path == "" {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, "path is required", nil)
			return
		}

		mc := database.ModuleConfig{
			ModuleID: req.ModuleID,
			Path:     path,
			Config:   database.JSON(rawConfig),
			Enabled:  true,
		}
		if req.Enabled != nil {
			mc.Enabled = *req.Enabled
		}

		// Upsert by ModuleID.
		var existing database.ModuleConfig
		if err := s.DB.Where("module_id = ?", req.ModuleID).First(&existing).Error; err == nil {
			mc.ID = existing.ID
		}
		if err := s.DB.Save(&mc).Error; err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}

		if err := jobqueue.AddJob(jobqueue.Job{
			Name:   "CaddyApplyModuleConfig",
			Action: func() error { return provider.ApplyModuleConfig(mc) },
		}); err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}

		h.ResultResponseJSON(w, r, http.StatusOK, mc)
	}
}

// DeleteModuleConfig removes a module configuration and its live config.
// Route: DELETE /caddy/module-configs/{id}
func (s Handler) DeleteModuleConfig() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		provider, ok := apiProviderOrError(w, r)
		if !ok {
			return
		}
		id := chi.URLParam(r, "id")

		var mc database.ModuleConfig
		if err := s.DB.Where("id = ?", id).First(&mc).Error; err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}

		if err := jobqueue.AddJob(jobqueue.Job{
			Name:   "CaddyRemoveModuleConfig",
			Action: func() error { return provider.RemoveModuleConfig(mc) },
		}); err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}

		if err := s.DB.Delete(&database.ModuleConfig{}, mc.ID).Error; err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}
		h.ResultResponseJSON(w, r, http.StatusOK, true)
	}
}

// apiProviderOrError resolves the API provider or writes an error response and
// returns false. Module configuration is only supported in API mode for now.
func apiProviderOrError(w http.ResponseWriter, r *http.Request) (*caddy.APIProvider, bool) {
	if strings.ToLower(config.Configuration.Caddy.Mode) != "api" {
		h.ResultErrorJSON(w, r, http.StatusBadRequest,
			"caddy configuration via the admin API is only available when CPM_CADDY_MODE=api", nil)
		return nil, false
	}
	return caddy.NewAPIProvider(), true
}
