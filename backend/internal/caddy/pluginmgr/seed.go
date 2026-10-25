package pluginmgr

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/Pacerino/CaddyProxyManager/internal/logger"
)

// EnsureSeeded makes sure a managed "current" binary exists for a systemd
// setup, whose unit runs <store>/current: without one the suggested drop-in
// would point the unit at a missing file. The first time, the configured
// (shipped) binary is copied into the store, marked as the seed and promoted;
// later a missing "current" is restored from the seed. With a current binary
// it does nothing. The system binary is only read, never written.
func (m *Manager) EnsureSeeded() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.busy {
		return nil
	}
	if _, ok := m.Store.Current(); ok {
		return nil
	}
	if seed, ok := m.Store.Seed(); ok {
		return m.Store.Promote(seed)
	}

	bin, err := exec.LookPath(m.ShippedBinary)
	if err != nil {
		return fmt.Errorf("finding the Caddy binary: %w", err)
	}
	f, err := os.Open(bin)
	if err != nil {
		return fmt.Errorf("reading the Caddy binary: %w", err)
	}
	defer f.Close()
	path, _, err := m.Store.Put(f)
	if err != nil {
		return fmt.Errorf("copying the Caddy binary: %w", err)
	}
	if err := m.Store.SetSeed(path); err != nil {
		return fmt.Errorf("marking the seed binary: %w", err)
	}
	if err := m.Store.Promote(path); err != nil {
		return fmt.Errorf("promoting the seed binary: %w", err)
	}
	logger.Info("Copied %s to %s as the managed Caddy binary", bin, path)
	return nil
}
