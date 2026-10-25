package pluginmgr

import "context"

// Restarter restarts the running Caddy process on its (possibly new) binary.
// The supervisor package satisfies it without importing this package.
type Restarter interface {
	Restart(ctx context.Context) error
}
