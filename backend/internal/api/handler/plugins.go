package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	h "github.com/Pacerino/CaddyProxyManager/internal/api/http"
	"github.com/Pacerino/CaddyProxyManager/internal/caddy/pluginmgr"
	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/database"
	"github.com/Pacerino/CaddyProxyManager/internal/logger"

	"github.com/go-chi/chi/v5"
)

// maxCatalogResults caps the catalog search response.
const maxCatalogResults = 50

// PluginManager is the part of *pluginmgr.Manager the plugin endpoints use.
type PluginManager interface {
	Installed(ctx context.Context) ([]pluginmgr.InstalledPackage, error)
	Catalog(ctx context.Context) ([]pluginmgr.Package, error)
	Start(kind pluginmgr.OpKind, pkg string) (*pluginmgr.Operation, error)
	Get(id string) (*pluginmgr.Operation, bool)
	Running() (*pluginmgr.Operation, bool)
	UpdateAvailable(ctx context.Context) (managed, shipped string, available bool, err error)
}

type installedPlugin struct {
	Path    string   `json:"path"`
	Version string   `json:"version"`
	Modules []string `json:"modules"`
	Tested  bool     `json:"tested"`
}

type pluginUpdate struct {
	Available bool   `json:"available"`
	Managed   string `json:"managed"`
	Shipped   string `json:"shipped"`
}

type pluginsResponse struct {
	Installed      []installedPlugin     `json:"installed"`
	InstallEnabled bool                  `json:"installEnabled"`
	DisabledReason string                `json:"disabledReason,omitempty"`
	Setup          pluginmgr.SetupStatus `json:"setup"`
	Update         pluginUpdate          `json:"update"`
	AutoUpdate     bool                  `json:"autoUpdate"`
	Operation      *pluginmgr.Operation  `json:"operation,omitempty"`
}

type catalogPlugin struct {
	Path      string   `json:"path"`
	Repo      string   `json:"repo"`
	Modules   []string `json:"modules"`
	Tested    bool     `json:"tested"`
	Installed bool     `json:"installed"`
}

// pluginUsage is a stored config that references a module of a plugin.
type pluginUsage struct {
	Kind string `json:"kind"`
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

// pluginsOrError returns the plugin manager, answering 400 outside API mode.
func (s Handler) pluginsOrError(w http.ResponseWriter, r *http.Request) (PluginManager, bool) {
	if _, ok := apiProviderOrError(w, r); !ok {
		return nil, false
	}
	if s.Plugins == nil {
		h.ResultErrorJSON(w, r, http.StatusServiceUnavailable, "the plugin manager is not available", nil)
		return nil, false
	}
	return s.Plugins, true
}

// installState reports the Caddy setup and, when plugins can't be changed,
// why not.
func (s Handler) installState(ctx context.Context) (setup pluginmgr.SetupStatus, enabled bool, reason string) {
	if s.PluginSetup != nil {
		setup = s.PluginSetup(ctx)
	} else {
		setup = pluginmgr.SetupStatus{State: pluginmgr.SetupUnsupported, Reason: "the Caddy setup is unknown"}
	}
	switch {
	case !config.Configuration.PluginInstall:
		return setup, false, "plugin install is disabled (CPM_PLUGIN_INSTALL=false)"
	case setup.Switchable():
		return setup, true, ""
	case setup.Reason != "":
		return setup, false, setup.Reason
	default:
		return setup, false, "Caddy is not set up for plugin install (" + string(setup.State) + ")"
	}
}

// installEnabledOrError answers 400 with the reason when plugins can't be changed.
func (s Handler) installEnabledOrError(w http.ResponseWriter, r *http.Request) bool {
	if _, enabled, reason := s.installState(r.Context()); !enabled {
		h.ResultErrorJSON(w, r, http.StatusBadRequest, reason, nil)
		return false
	}
	return true
}

// visibleInstalled lists the installed plugins without JSONSchemaPackage.
func visibleInstalled(ctx context.Context, pm PluginManager) ([]pluginmgr.InstalledPackage, error) {
	pkgs, err := pm.Installed(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]pluginmgr.InstalledPackage, 0, len(pkgs))
	for _, p := range pkgs {
		if p.Path != pluginmgr.JSONSchemaPackage {
			out = append(out, p)
		}
	}
	return out, nil
}

// underModule reports whether the package path is the Go module modPath or a
// subdirectory of it.
func underModule(path, modPath string) bool {
	return path == modPath || strings.HasPrefix(path, modPath+"/")
}

// isInstalled reports whether a package path belongs to an installed module.
func isInstalled(path string, installed []pluginmgr.InstalledPackage) bool {
	for _, p := range installed {
		if underModule(path, p.Path) {
			return true
		}
	}
	return false
}

// startOperation starts a plugin operation and answers 202, or 409 when busy.
func startOperation(w http.ResponseWriter, r *http.Request, pm PluginManager, kind pluginmgr.OpKind, pkg string) {
	op, err := pm.Start(kind, pkg)
	switch {
	case errors.Is(err, pluginmgr.ErrBusy):
		h.ResultErrorJSON(w, r, http.StatusConflict, err.Error(), nil)
	case err != nil:
		h.ResultErrorJSON(w, r, http.StatusBadRequest, err.Error(), nil)
	default:
		h.ResultResponseJSON(w, r, http.StatusAccepted, op)
	}
}

// GetPlugins lists the installed plugins with the install and update state.
// Route: GET /caddy/plugins
func (s Handler) GetPlugins() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		pm, ok := s.pluginsOrError(w, r)
		if !ok {
			return
		}
		pkgs, err := visibleInstalled(r.Context(), pm)
		if err != nil {
			h.ResultErrorJSON(w, r, http.StatusInternalServerError, err.Error(), nil)
			return
		}
		resp := pluginsResponse{
			Installed:  make([]installedPlugin, 0, len(pkgs)),
			AutoUpdate: database.GetSetting(s.DB, pluginmgr.SettingAutoUpdate, "false") == "true",
		}
		for _, p := range pkgs {
			mods := p.Modules
			if mods == nil {
				mods = []string{}
			}
			resp.Installed = append(resp.Installed, installedPlugin{Path: p.Path, Version: p.Version, Modules: mods, Tested: pluginmgr.IsTested(p.Path)})
		}
		resp.Setup, resp.InstallEnabled, resp.DisabledReason = s.installState(r.Context())

		managed, shipped, available, err := pm.UpdateAvailable(r.Context())
		if err != nil {
			logger.Warn("Checking for a Caddy update of the managed binary: %v", err)
		}
		resp.Update = pluginUpdate{Available: available, Managed: managed, Shipped: shipped}
		if op, ok := pm.Running(); ok {
			resp.Operation = op
		}
		h.ResultResponseJSON(w, r, http.StatusOK, resp)
	}
}

// GetPluginCatalog searches the build service's plugins by path or module name.
// Route: GET /caddy/plugins/catalog?q=
func (s Handler) GetPluginCatalog() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		pm, ok := s.pluginsOrError(w, r)
		if !ok {
			return
		}
		pkgs, err := pm.Catalog(r.Context())
		if err != nil {
			h.ResultErrorJSON(w, r, http.StatusServiceUnavailable, err.Error(), nil)
			return
		}
		installed, err := visibleInstalled(r.Context(), pm)
		if err != nil {
			logger.Warn("Listing the installed Caddy plugins: %v", err)
		}

		q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
		matches := func(p pluginmgr.Package) bool {
			if q == "" || strings.Contains(strings.ToLower(p.Path), q) {
				return true
			}
			for _, m := range p.Modules {
				if strings.Contains(strings.ToLower(m), q) {
					return true
				}
			}
			return false
		}

		out := make([]catalogPlugin, 0)
		for _, p := range pkgs {
			if p.Path == pluginmgr.JSONSchemaPackage || !matches(p) {
				continue
			}
			mods := p.Modules
			if mods == nil {
				mods = []string{}
			}
			out = append(out, catalogPlugin{Path: p.Path, Repo: p.Repo, Modules: mods, Tested: pluginmgr.IsTested(p.Path), Installed: isInstalled(p.Path, installed)})
			if len(out) == maxCatalogResults {
				break
			}
		}
		h.ResultResponseJSON(w, r, http.StatusOK, out)
	}
}

// AddPlugin starts installing a plugin.
// Route: POST /caddy/plugins
func (s Handler) AddPlugin() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		pm, ok := s.pluginsOrError(w, r)
		if !ok || !s.installEnabledOrError(w, r) {
			return
		}
		var req struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, "invalid request body", nil)
			return
		}
		path := strings.TrimSpace(req.Path)
		switch path {
		case "":
			h.ResultErrorJSON(w, r, http.StatusBadRequest, "path is required", nil)
			return
		case pluginmgr.JSONSchemaPackage:
			h.ResultErrorJSON(w, r, http.StatusBadRequest, path+" is always installed", nil)
			return
		}
		installed, err := visibleInstalled(r.Context(), pm)
		if err != nil {
			h.ResultErrorJSON(w, r, http.StatusInternalServerError, err.Error(), nil)
			return
		}
		if isInstalled(path, installed) {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, path+" is already installed", nil)
			return
		}
		startOperation(w, r, pm, pluginmgr.OpAdd, path)
	}
}

// RemovePlugin starts removing a plugin unless one of its modules is in use.
// Route: DELETE /caddy/plugins?path=
func (s Handler) RemovePlugin() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		pm, ok := s.pluginsOrError(w, r)
		if !ok || !s.installEnabledOrError(w, r) {
			return
		}
		path := strings.TrimSpace(r.URL.Query().Get("path"))
		switch path {
		case "":
			h.ResultErrorJSON(w, r, http.StatusBadRequest, "path is required", nil)
			return
		case pluginmgr.JSONSchemaPackage:
			h.ResultErrorJSON(w, r, http.StatusBadRequest, path+" cannot be removed", nil)
			return
		}
		installed, err := visibleInstalled(r.Context(), pm)
		if err != nil {
			h.ResultErrorJSON(w, r, http.StatusInternalServerError, err.Error(), nil)
			return
		}
		// The modules that go away: those of every installed module under
		// path, or of the module containing it when path is a subdirectory.
		var modules []string
		found := false
		for _, p := range installed {
			if underModule(p.Path, path) || underModule(path, p.Path) {
				found = true
				modules = append(modules, p.Modules...)
			}
		}
		if !found {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, path+" is not installed", nil)
			return
		}
		usages, err := s.pluginUsages(modules)
		if err != nil {
			h.ResultErrorJSON(w, r, http.StatusInternalServerError, err.Error(), nil)
			return
		}
		if len(usages) > 0 {
			h.ResultErrorJSON(w, r, http.StatusConflict, "the plugin is in use", usages)
			return
		}
		startOperation(w, r, pm, pluginmgr.OpRemove, path)
	}
}

// pluginUsages lists the DNS providers, host plugins and module configs that
// reference one of the module IDs.
func (s Handler) pluginUsages(modules []string) ([]pluginUsage, error) {
	usages := []pluginUsage{}
	if len(modules) == 0 {
		return usages, nil
	}
	var providers []database.DNSProvider
	if err := s.DB.Where("module_id IN ?", modules).Order("id").Find(&providers).Error; err != nil {
		return nil, err
	}
	for _, p := range providers {
		usages = append(usages, pluginUsage{Kind: "dnsProvider", ID: p.ID, Name: p.Name})
	}

	var hostPlugins []database.HostPlugin
	if err := s.DB.Where("module_id IN ?", modules).Order("id").Find(&hostPlugins).Error; err != nil {
		return nil, err
	}
	for _, hp := range hostPlugins {
		var host database.Host
		name := hp.ModuleID
		if err := s.DB.Select("id", "domains").Limit(1).Find(&host, hp.HostID).Error; err == nil && host.ID != 0 {
			name = host.Domains
		}
		usages = append(usages, pluginUsage{Kind: "hostPlugin", ID: hp.ID, Name: name})
	}

	var configs []database.ModuleConfig
	if err := s.DB.Where("module_id IN ?", modules).Order("id").Find(&configs).Error; err != nil {
		return nil, err
	}
	for _, c := range configs {
		usages = append(usages, pluginUsage{Kind: "moduleConfig", ID: c.ID, Name: c.ModuleID})
	}
	return usages, nil
}

// UpdatePlugins rebuilds the plugin set on the shipped Caddy version.
// Route: POST /caddy/plugins/update
func (s Handler) UpdatePlugins() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		pm, ok := s.pluginsOrError(w, r)
		if !ok || !s.installEnabledOrError(w, r) {
			return
		}
		_, _, available, err := pm.UpdateAvailable(r.Context())
		if err != nil {
			h.ResultErrorJSON(w, r, http.StatusInternalServerError, err.Error(), nil)
			return
		}
		if !available {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, "no Caddy update is available", nil)
			return
		}
		startOperation(w, r, pm, pluginmgr.OpUpdate, "")
	}
}

// GetPluginOperation returns the status of a recent plugin operation.
// Route: GET /caddy/plugins/operations/{id}
func (s Handler) GetPluginOperation() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		pm, ok := s.pluginsOrError(w, r)
		if !ok {
			return
		}
		op, found := pm.Get(chi.URLParam(r, "id"))
		if !found {
			h.ResultErrorJSON(w, r, http.StatusNotFound, "operation not found", nil)
			return
		}
		h.ResultResponseJSON(w, r, http.StatusOK, op)
	}
}

// SetPluginSettings stores the plugin settings.
// Route: PUT /caddy/plugins/settings
func (s Handler) SetPluginSettings() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.pluginsOrError(w, r); !ok {
			return
		}
		var req struct {
			AutoUpdate *bool `json:"autoUpdate"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AutoUpdate == nil {
			h.ResultErrorJSON(w, r, http.StatusBadRequest, "autoUpdate is required", nil)
			return
		}
		value := "false"
		if *req.AutoUpdate {
			value = "true"
		}
		if err := database.SetSetting(s.DB, pluginmgr.SettingAutoUpdate, value); err != nil {
			h.ResultErrorJSON(w, r, http.StatusInternalServerError, err.Error(), nil)
			return
		}
		h.ResultResponseJSON(w, r, http.StatusOK, map[string]bool{"autoUpdate": *req.AutoUpdate})
	}
}
