package pluginmgr

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Pacerino/CaddyProxyManager/internal/logger"
)

// switchTo makes path the live binary. If Caddy doesn't come up or the
// reconcile reports errors, it rolls back to the previous binary (the
// shipped one for the first managed build); if that fails too, it falls
// back to the shipped binary. On systemd "the shipped binary" is the seed
// copy (Store.Reset), so the unit's ExecStart never disappears.
func (m *Manager) switchTo(ctx context.Context, id, path, sum string) (OpStep, bool, error) {
	_, hadCurrent := m.Store.Current()
	m.setStep(id, StepRestarting)
	if err := m.Store.Promote(path); err != nil {
		m.discard(path)
		return StepFailed, false, fmt.Errorf("switching to the new binary: %w", err)
	}

	switchErr := m.restartAndWait(ctx)
	if switchErr == nil {
		m.setStep(id, StepReapplying)
		switchErr = m.reconcile(ctx)
	}
	if switchErr == nil {
		m.writeSchema(ctx, path, sum)
		m.prune()
		return StepDone, true, nil
	}
	switchErr = fmt.Errorf("new Caddy binary failed: %w%s", switchErr, m.logTail())

	m.setStep(id, StepRestarting)
	var rbErr error
	if hadCurrent {
		rbErr = m.Store.RollBack()
	} else {
		rbErr = m.Store.Reset()
	}
	if rbErr == nil {
		rbErr = m.restartAndWait(ctx)
	}
	if rbErr == nil {
		m.setStep(id, StepReapplying)
		m.prune()
		if err := m.reconcile(ctx); err != nil {
			return StepRolledBack, true, fmt.Errorf("%w; re-applying config after the rollback: %v", switchErr, err)
		}
		return StepRolledBack, true, switchErr
	}

	if !hadCurrent {
		// The rollback target already was the shipped binary.
		return StepFailed, true, fmt.Errorf("%w; rollback to the shipped binary failed: %v", switchErr, rbErr)
	}
	m.setStep(id, StepRestarting)
	shipErr := m.Store.Reset()
	if shipErr == nil {
		shipErr = m.restartAndWait(ctx)
	}
	if shipErr != nil {
		return StepFailed, true, fmt.Errorf("%w; rollback failed: %v; starting the shipped binary failed: %v", switchErr, rbErr, shipErr)
	}
	m.setStep(id, StepReapplying)
	m.prune()
	if err := m.reconcile(ctx); err != nil {
		return StepFailed, true, fmt.Errorf("%w; rollback failed: %v; running the shipped binary, re-applying config failed: %v", switchErr, rbErr, err)
	}
	return StepFailed, true, fmt.Errorf("%w; rollback failed: %v; running the shipped binary", switchErr, rbErr)
}

// restartAndWait restarts Caddy and waits for its admin API.
func (m *Manager) restartAndWait(ctx context.Context) error {
	rctx, cancel := context.WithTimeout(ctx, orDefault(m.restartTimeout, defaultRestartTimeout))
	err := m.Restart.Restart(rctx)
	cancel()
	if err != nil {
		return fmt.Errorf("restarting caddy: %w", err)
	}

	timeout := orDefault(m.pingTimeout, defaultPingTimeout)
	pctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		err := m.Ping(pctx)
		if err == nil {
			return nil
		}
		select {
		case <-pctx.Done():
			return fmt.Errorf("caddy admin API did not answer within %s: %w", timeout, err)
		case <-time.After(orDefault(m.pingInterval, defaultPingInterval)):
		}
	}
}

// reconcile re-applies CPM's config and joins the errors it reports.
func (m *Manager) reconcile(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, orDefault(m.reconcileTimeout, defaultReconcileTimeout))
	defer cancel()
	return errors.Join(m.Reconcile(ctx)...)
}

func (m *Manager) prune() {
	if err := m.Store.Prune(); err != nil {
		logger.Warn("Pruning managed Caddy binaries: %v", err)
	}
}

// logTail formats Caddy's last log lines for an error message.
func (m *Manager) logTail() string {
	if m.CaddyLogs == nil {
		return ""
	}
	lines := m.CaddyLogs()
	if len(lines) == 0 {
		return ""
	}
	if len(lines) > maxLogLines {
		lines = lines[len(lines)-maxLogLines:]
	}
	return "\nLast Caddy log lines:\n" + strings.Join(lines, "\n")
}

// writeSchema regenerates SchemaOut with bin and records bin's SHA-256 in the
// caddy_schema.sha256 sidecar. Failures only log a warning.
func (m *Manager) writeSchema(ctx context.Context, bin, sum string) {
	if m.SchemaOut == "" {
		return
	}
	if err := m.generateSchema(ctx, bin); err != nil {
		logger.Warn("Regenerating the Caddy JSON schema: %v", err)
		return
	}
	sidecar := filepath.Join(filepath.Dir(m.SchemaOut), "caddy_schema.sha256")
	if err := writeFileAtomic(sidecar, []byte(sum+"\n")); err != nil {
		logger.Warn("Writing the Caddy schema checksum: %v", err)
	}
}

func (m *Manager) generateSchema(ctx context.Context, bin string) error {
	ctx, cancel := context.WithTimeout(ctx, orDefault(m.schemaTimeout, defaultSchemaTimeout))
	defer cancel()
	tmp := m.SchemaOut + ".tmp"
	cmd := exec.CommandContext(ctx, bin, "json-schema", "--output", tmp)
	cmd.WaitDelay = time.Second
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("%s json-schema: %w: %s", filepath.Base(bin), err, strings.TrimSpace(string(out)))
	}
	if err := os.Rename(tmp, m.SchemaOut); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
