package caddy

import (
	"encoding/json"

	"github.com/Pacerino/CaddyProxyManager/internal/database"
)

// ApplyModuleConfig patches a module configuration fragment into the live
// Caddy config via the admin API. When the fragment is disabled it is removed
// instead. Only supported in API mode (callers must use APIProvider).
func (p *APIProvider) ApplyModuleConfig(mc database.ModuleConfig) error {
	if !mc.Enabled {
		return p.RemoveModuleConfig(mc)
	}
	var body any
	if len(mc.Config) > 0 {
		if err := json.Unmarshal(mc.Config, &body); err != nil {
			return err
		}
	}
	return p.PatchConfig(mc.Path, body)
}

// RemoveModuleConfig removes a module configuration fragment from the live
// config. Caddy returns an error for a missing path; that is treated as
// already-removed and ignored.
func (p *APIProvider) RemoveModuleConfig(mc database.ModuleConfig) error {
	exists, err := p.configExists("/config/" + trimConfigPrefix(mc.Path))
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	return p.do("DELETE", "/config/"+trimConfigPrefix(mc.Path), nil, nil)
}

// trimConfigPrefix normalises a stored path so it can be appended to /config/.
func trimConfigPrefix(path string) string {
	for len(path) > 0 && path[0] == '/' {
		path = path[1:]
	}
	const prefix = "config/"
	if len(path) >= len(prefix) && path[:len(prefix)] == prefix {
		path = path[len(prefix):]
	}
	return path
}
