// Package supervisor runs Caddy as CPM's child process so CPM can restart it
// on a new binary and bring it back after a crash.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/Pacerino/CaddyProxyManager/internal/logger"
)

const (
	defaultTermTimeout = 10 * time.Second
	maxBackoff         = 30 * time.Second
	// A process that stayed up this long resets the crash backoff.
	healthyUptime = 60 * time.Second
)

// Supervisor keeps one Caddy child process running.
type Supervisor struct {
	// Binary returns the Caddy binary to run; it is evaluated on every (re)start.
	Binary     func() string
	ConfigFile string
	Env        []string
	// OnRestart is called after an unexpected exit was followed by a successful restart.
	OnRestart func()
	// Backoff returns the delay before crash-restart number attempt (0-based).
	// Defaults to 1s doubling, capped at 30s.
	Backoff func(attempt int) time.Duration
	// TermTimeout is how long Restart/Stop wait after SIGTERM before SIGKILL (default 10s).
	TermTimeout time.Duration

	// stdout and stderr receive Caddy's output; nil means CPM's own.
	stdout, stderr io.Writer
	logs           logRing

	mu  sync.Mutex
	cur *proc
}

// RecentLogs returns Caddy's most recent output lines (stdout and stderr),
// oldest first, across restarts.
func (s *Supervisor) RecentLogs() []string { return s.logs.snapshot() }

func orStd(w, std io.Writer) io.Writer {
	if w != nil {
		return w
	}
	return std
}

// proc is one started child. cancel is closed when the exit is intentional
// (Restart/Stop) so the watcher neither restarts nor calls OnRestart.
type proc struct {
	cmd      *exec.Cmd
	done     chan struct{} // closed once the process has exited
	cancel   chan struct{}
	canceled bool // guarded by Supervisor.mu
	started  time.Time
}

func defaultBackoff(attempt int) time.Duration {
	d := time.Second
	for i := 0; i < attempt; i++ {
		d *= 2
		if d >= maxBackoff {
			return maxBackoff
		}
	}
	return d
}

func (s *Supervisor) backoff(attempt int) time.Duration {
	if s.Backoff != nil {
		return s.Backoff(attempt)
	}
	return defaultBackoff(attempt)
}

func (s *Supervisor) termTimeout() time.Duration {
	if s.TermTimeout > 0 {
		return s.TermTimeout
	}
	return defaultTermTimeout
}

// spawn starts a child and its watcher. Caller holds s.mu.
func (s *Supervisor) spawn(attempt int) error {
	cmd := exec.Command(s.Binary(), "run", "--resume", "--config", s.ConfigFile)
	cmd.Env = append(os.Environ(), s.Env...)
	stdout := &lineWriter{out: orStd(s.stdout, os.Stdout), ring: &s.logs}
	stderr := &lineWriter{out: orStd(s.stderr, os.Stderr), ring: &s.logs}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	p := &proc{cmd: cmd, done: make(chan struct{}), cancel: make(chan struct{}), started: time.Now()}
	s.cur = p
	go func() {
		_ = cmd.Wait()
		stdout.flush()
		stderr.flush()
		close(p.done)
		s.handleExit(p, attempt)
	}()
	return nil
}

// Start launches Caddy and returns once it is started.
func (s *Supervisor) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur != nil && !s.cur.canceled {
		return errors.New("supervisor: already started")
	}
	return s.spawn(0)
}

// cancelCurrent marks the current proc intentional and terminates it, waiting
// for the exit. Caller holds s.mu.
func (s *Supervisor) cancelCurrent() {
	p := s.cur
	if p == nil {
		return
	}
	if !p.canceled {
		p.canceled = true
		close(p.cancel)
	}
	select {
	case <-p.done:
	default:
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
		t := time.NewTimer(s.termTimeout())
		defer t.Stop()
		select {
		case <-p.done:
		case <-t.C:
			_ = p.cmd.Process.Kill()
			<-p.done
		}
	}
	s.cur = nil
}

// Restart stops the running Caddy (SIGTERM, then SIGKILL after the term
// timeout) and starts it again with the current Binary(). It never triggers
// the crash-restart path or OnRestart.
func (s *Supervisor) Restart(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelCurrent()
	if err := s.spawn(0); err != nil {
		return fmt.Errorf("supervisor: restart: %w", err)
	}
	return nil
}

// Stop terminates Caddy without restarting it. Stopping a stopped supervisor is a no-op.
func (s *Supervisor) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelCurrent()
	return nil
}

// handleExit runs in the watcher goroutine after p exited. It restarts Caddy
// unless the exit was intentional, retrying failed starts with backoff.
func (s *Supervisor) handleExit(p *proc, attempt int) {
	if time.Since(p.started) >= healthyUptime {
		attempt = 0
	}
	for {
		s.mu.Lock()
		intentional := p.canceled
		s.mu.Unlock()
		if intentional {
			return
		}
		logger.Warn("caddy exited unexpectedly, restarting (attempt %d)", attempt+1)

		t := time.NewTimer(s.backoff(attempt))
		select {
		case <-p.cancel:
			t.Stop()
			return
		case <-t.C:
		}

		s.mu.Lock()
		if p.canceled || s.cur != p {
			s.mu.Unlock()
			return
		}
		err := s.spawn(attempt + 1)
		s.mu.Unlock()
		if err == nil {
			if s.OnRestart != nil {
				s.OnRestart()
			}
			return
		}
		logger.Warn("restarting caddy: %v", err)
		attempt++
	}
}
