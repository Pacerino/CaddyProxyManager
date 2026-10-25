package pluginmgr

import "github.com/Pacerino/CaddyProxyManager/internal/caddy/supervisor"

var _ Restarter = (*supervisor.Supervisor)(nil)
