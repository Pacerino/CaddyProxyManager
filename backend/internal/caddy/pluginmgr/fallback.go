package pluginmgr

import (
	"time"

	"github.com/Pacerino/CaddyProxyManager/internal/logger"
)

// FallBackToShipped switches back to the binary Caddy ran before CPM managed
// it (the shipped one, or the seed on systemd) when the managed binary cannot
// run, e.g. after moving the data folder to another architecture. It reports
// whether the binary changed; a running operation owns the binary, so it does
// nothing then. The fallback is recorded as a failed OpFallback operation.
func (m *Manager) FallBackToShipped(reason string) bool {
	m.mu.Lock()
	changed := m.fallBackLocked(reason)
	m.mu.Unlock()
	if changed && m.OnChanged != nil {
		m.OnChanged()
	}
	return changed
}

func (m *Manager) fallBackLocked(reason string) bool {
	if m.busy {
		return false
	}
	cur, ok := m.Store.Current()
	if !ok {
		return false
	}
	if seed, ok := m.Store.Seed(); ok && seed == cur {
		return false
	}
	if err := m.Store.Reset(); err != nil {
		logger.Warn("Switching back to the shipped Caddy binary: %v", err)
		return false
	}
	logger.Warn("The managed Caddy binary could not run (%s); switched back to the shipped binary", reason)

	id, err := newOpID()
	if err != nil {
		id = "fallback-" + time.Now().Format("20060102150405.000000000")
	}
	now := time.Now()
	m.addOpLocked(&Operation{
		ID:         id,
		Kind:       OpFallback,
		Step:       StepFailed,
		Error:      "The managed Caddy binary could not run (" + reason + "); CPM switched back to the shipped Caddy binary.",
		StartedAt:  now,
		FinishedAt: now,
	})
	return true
}
