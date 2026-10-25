package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// switchable is a Binary func whose result a test (or Fallback) can change.
type switchable struct{ v atomic.Value }

func (b *switchable) get() string  { return b.v.Load().(string) }
func (b *switchable) set(p string) { b.v.Store(p) }

// fallbackTo returns a Fallback that switches bin to target once and records
// the reasons it was called with.
func fallbackTo(bin *switchable, target string, reasons *[]string, mu *sync.Mutex) func(string) bool {
	return func(reason string) bool {
		mu.Lock()
		defer mu.Unlock()
		*reasons = append(*reasons, reason)
		if bin.get() == target {
			return false
		}
		bin.set(target)
		return true
	}
}

func TestSupervisor_RetriesWithoutResume(t *testing.T) {
	dir := t.TempDir()
	// Fails while --resume is passed, like a Caddy whose autosave references
	// modules it lacks.
	bin, out := fakeCaddy(t, dir, "caddy", `case "$*" in *--resume*) exit 1 ;; esac
exec sleep 1000`)
	s := newSup(bin, "c.json")
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	l := waitLines(t, out, quickExitLimit+1)
	for i := range quickExitLimit {
		if !strings.Contains(l[i], "--resume") {
			t.Fatalf("start %d without --resume: %q", i, l[i])
		}
	}
	if got := l[quickExitLimit]; strings.Contains(got, "--resume") || !strings.HasSuffix(got, "run --config c.json") {
		t.Fatalf("retry args = %q", got)
	}
	time.Sleep(200 * time.Millisecond)
	if n := len(lines(out)); n != quickExitLimit+1 {
		t.Fatalf("expected Caddy to stay up, %d starts", n)
	}
}

func TestSupervisor_FallsBackWhenBinaryKeepsExiting(t *testing.T) {
	dir := t.TempDir()
	bad, badOut := fakeCaddy(t, dir, "bad", "exit 1")
	good, goodOut := fakeCaddy(t, dir, "good", "exec sleep 1000")
	var bin switchable
	bin.set(bad)
	var mu sync.Mutex
	var reasons []string
	s := newSup("", "c.json")
	s.Binary = bin.get
	s.Fallback = fallbackTo(&bin, good, &reasons, &mu)
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	l := waitLines(t, goodOut, 1)
	if !strings.HasSuffix(l[0], "run --resume --config c.json") {
		t.Fatalf("fallback binary args = %q", l[0])
	}
	// quickExitLimit exits with --resume, then quickExitLimit without.
	if n := len(lines(badOut)); n != 2*quickExitLimit {
		t.Fatalf("bad binary started %d times, want %d", n, 2*quickExitLimit)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reasons) != 1 || reasons[0] == "" {
		t.Fatalf("Fallback reasons = %q", reasons)
	}
}

func TestSupervisor_FallsBackWhenBinaryCannotStart(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken")
	if err := os.WriteFile(broken, []byte("not a program"), 0o644); err != nil {
		t.Fatal(err)
	}
	good, goodOut := fakeCaddy(t, dir, "good", "exec sleep 1000")
	var bin switchable
	bin.set(broken)
	var mu sync.Mutex
	var reasons []string
	s := newSup("", "c.json")
	s.Binary = bin.get
	s.Fallback = fallbackTo(&bin, good, &reasons, &mu)
	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start must succeed on the fallback binary: %v", err)
	}
	defer s.Stop()
	waitLines(t, goodOut, 1)
	mu.Lock()
	defer mu.Unlock()
	if len(reasons) != 1 || !strings.Contains(reasons[0], "broken") {
		t.Fatalf("Fallback reasons = %q", reasons)
	}
}

func TestSupervisor_FallsBackWhenRestartAfterCrashCannotStart(t *testing.T) {
	dir := t.TempDir()
	first, firstOut := fakeCaddy(t, dir, "first", "exec sleep 1000")
	broken := filepath.Join(dir, "broken")
	if err := os.WriteFile(broken, []byte("not a program"), 0o644); err != nil {
		t.Fatal(err)
	}
	good, goodOut := fakeCaddy(t, dir, "good", "exec sleep 1000")
	var bin switchable
	bin.set(first)
	var mu sync.Mutex
	var reasons []string
	restarted := make(chan struct{}, 1)
	s := newSup("", "c.json")
	s.Binary = bin.get
	s.Fallback = fallbackTo(&bin, good, &reasons, &mu)
	s.OnRestart = func() { restarted <- struct{}{} }
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	pid := pidOf(t, waitLines(t, firstOut, 1)[0])
	bin.set(broken)
	p, _ := os.FindProcess(pid)
	_ = p.Kill()
	waitLines(t, goodOut, 1)
	select {
	case <-restarted:
	case <-time.After(10 * time.Second):
		t.Fatal("OnRestart not called after the fallback start")
	}
}

func TestSupervisor_NoFallbackKeepsRetrying(t *testing.T) {
	dir := t.TempDir()
	bin, out := fakeCaddy(t, dir, "caddy", "exit 1")
	s := newSup(bin, "c.json")
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitLines(t, out, 3*quickExitLimit)
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestSupervisor_RestartResumesAgain(t *testing.T) {
	dir := t.TempDir()
	bin, out := fakeCaddy(t, dir, "caddy", `case "$*" in *--resume*) [ -f `+dir+`/ok ] || exit 1 ;; esac
exec sleep 1000`)
	s := newSup(bin, "c.json")
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	waitLines(t, out, quickExitLimit+1)
	if err := os.WriteFile(filepath.Join(dir, "ok"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Restart(t.Context()); err != nil {
		t.Fatal(err)
	}
	l := waitLines(t, out, quickExitLimit+2)
	if !strings.HasSuffix(l[quickExitLimit+1], "run --resume --config c.json") {
		t.Fatalf("intentional restart args = %q", l[quickExitLimit+1])
	}
}
