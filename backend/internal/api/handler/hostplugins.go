package handler

import (
	"encoding/json"
	"net/http"

	h "github.com/Pacerino/CaddyProxyManager/internal/api/http"
	"github.com/Pacerino/CaddyProxyManager/internal/caddy"
	"github.com/Pacerino/CaddyProxyManager/internal/caddy/schema"
	"github.com/Pacerino/CaddyProxyManager/internal/database"
	"github.com/Pacerino/CaddyProxyManager/internal/jobqueue"
)

// GetHostScopedSchemas returns the typed schemas for plugins configurable per
// host. The frontend filters these against the host's Caddy build.
// Route: GET /caddy/host-schemas
func (s Handler) GetHostScopedSchemas() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		h.ResultResponseJSON(w, r, http.StatusOK, schema.WithScope(schema.ScopeHost))
	}
}

// hostPluginRequest is the body accepted by SetHostPlugin.
type hostPluginRequest struct {
	ModuleID string         `json:"moduleId"`
	Enabled  *bool          `json:"enabled,omitempty"`
	Values   map[string]any `json:"values"`
}

// SetHostPlugin creates or updates a per-host plugin configuration and
// re-applies the host's route.
// Route: PUT /hosts/{hostID}/plugins
func (s Handler) SetHostPlugin() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		hostID, err := getURLParamInt(r, "hostID")
		if err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}

		var req hostPluginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}

		sc, found := schema.Get(req.ModuleID)
		if !found || !sc.HasScope(schema.ScopeHost) {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, "module is not configurable per host", nil)
			return
		}

		handler, err := sc.BuildHandler(req.Values)
		if err != nil {
			if ve, ok := err.(*schema.ValidationError); ok {
				h.ResultErrorJSON(w, r, http.StatusBadRequest, "schema validation failed", ve.Fields)
				return
			}
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}
		handlerJSON, err := json.Marshal(handler)
		if err != nil {
			h.ResultErrorJSON(w, r, http.StatusInternalServerError, err.Error(), nil)
			return
		}

		plugin := database.HostPlugin{
			HostID:   uint(hostID),
			ModuleID: req.ModuleID,
			Handler:  database.JSON(handlerJSON),
			Enabled:  true,
		}
		if req.Enabled != nil {
			plugin.Enabled = *req.Enabled
		}

		// Upsert by (host, module).
		var existing database.HostPlugin
		if err := s.DB.Where("host_id = ? AND module_id = ?", hostID, req.ModuleID).First(&existing).Error; err == nil {
			plugin.ID = existing.ID
		}
		if err := s.DB.Save(&plugin).Error; err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}

		if err := s.reapplyHost(uint(hostID)); err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}

		h.ResultResponseJSON(w, r, http.StatusOK, plugin)
	}
}

// DeleteHostPlugin removes a per-host plugin and re-applies the host's route.
// Route: DELETE /hosts/{hostID}/plugins/{pluginID}
func (s Handler) DeleteHostPlugin() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		hostID, err := getURLParamInt(r, "hostID")
		if err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}
		pluginID, err := getURLParamInt(r, "pluginID")
		if err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}

		if err := s.DB.Where("host_id = ?", hostID).Delete(&database.HostPlugin{}, pluginID).Error; err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}

		if err := s.reapplyHost(uint(hostID)); err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}

		h.ResultResponseJSON(w, r, http.StatusOK, true)
	}
}

// reapplyHost reloads the host with its associations and re-writes its
// configuration through the configured provider via the job queue.
func (s Handler) reapplyHost(hostID uint) error {
	var host database.Host
	if err := s.DB.Where("id = ?", hostID).Preload("Upstreams").Preload("Plugins").First(&host).Error; err != nil {
		return err
	}
	provider, err := caddy.GetProvider()
	if err != nil {
		return err
	}
	return jobqueue.AddJob(jobqueue.Job{
		Name:   "CaddyConfigureHost",
		Action: func() error { return provider.WriteHost(host) },
	})
}
