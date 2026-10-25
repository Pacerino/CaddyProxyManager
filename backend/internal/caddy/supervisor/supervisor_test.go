package supervisor

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// fakeCaddy writes a POSIX shell script that records "<pid> <args>" to <name>.out
// and then execs sleep, so signals hit the recorded PID directly.
func fakeCaddy(t *testing.T, dir, name, tail string) (script, out string) {
	t.Helper()
	script = filepath.Join(dir, name)
	out = script + ".out"
	body := fmt.Sprintf("#!/bin/sh\necho \"$$ $*\" >> %q\n%s\n", out, tail)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script, out
}

func lines(out string) []string {
	b, _ := os.ReadFile(out)
	s := strings.TrimSpace(string(b))
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func waitLines(t *testing.T, out string, n int) []string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if l := lines(out); len(l) >= n {
			return l
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d lines in %s, have %v", n, out, lines(out))
	return nil
}

func pidOf(t *testing.T, line string) int {
	t.Helper()
	pid, err := strconv.Atoi(strings.Fields(line)[0])
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

func newSup(bin string, cfg string) *Supervisor {
	return &Supervisor{
		Binary:     func() string { return bin },
		ConfigFile: cfg,
		Backoff:    func(int) time.Duration { return 0 },
	}
}

func TestSupervisor_StartPassesArgs(t *testing.T) {
	dir := t.TempDir()
	bin, out := fakeCaddy(t, dir, "caddy", "exec sleep 1000")
	s := newSup(bin, "/cfg/Caddyfile.json")
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	l := waitLines(t, out, 1)
	if want := "run --resume --config /cfg/Caddyfile.json"; !strings.HasSuffix(l[0], want) {
		t.Fatalf("args = %q, want suffix %q", l[0], want)
	}
}

func TestSupervisor_RestartUsesCurrentBinary(t *testing.T) {
	dir := t.TempDir()
	binA, outA := fakeCaddy(t, dir, "caddyA", "exec sleep 1000")
	binB, outB := fakeCaddy(t, dir, "caddyB", "exec sleep 1000")
	var cur atomic.Value
	cur.Store(binA)
	var restarts int32
	s := newSup("", "c.json")
	s.Binary = func() string { return cur.Load().(string) }
	s.OnRestart = func() { atomic.AddInt32(&restarts, 1) }
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	first := pidOf(t, waitLines(t, outA, 1)[0])

	cur.Store(binB)
	if err := s.Restart(t.Context()); err != nil {
		t.Fatal(err)
	}
	second := pidOf(t, waitLines(t, outB, 1)[0])
	if first == second {
		t.Fatal("expected a new process")
	}
	if syscall.Kill(first, 0) == nil {
		t.Fatal("old process still alive")
	}
	time.Sleep(100 * time.Millisecond)
	if n := atomic.LoadInt32(&restarts); n != 0 {
		t.Fatalf("OnRestart called %d times after intentional Restart", n)
	}
	if len(lines(outA)) != 1 {
		t.Fatalf("old binary started again: %v", lines(outA))
	}
}

func testCrash(t *testing.T, crash func(pid int)) {
	dir := t.TempDir()
	bin, out := fakeCaddy(t, dir, "caddy", "exec sleep 1000")
	called := make(chan struct{}, 4)
	s := newSup(bin, "c.json")
	s.OnRestart = func() { called <- struct{}{} }
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	first := pidOf(t, waitLines(t, out, 1)[0])
	crash(first)
	l := waitLines(t, out, 2)
	if pidOf(t, l[1]) == first {
		t.Fatal("expected a new pid")
	}
	select {
	case <-called:
	case <-time.After(10 * time.Second):
		t.Fatal("OnRestart not called")
	}
	time.Sleep(100 * time.Millisecond)
	if len(called) != 0 || len(lines(out)) != 2 {
		t.Fatalf("unexpected extra restarts: %d calls, %d starts", len(called), len(lines(out)))
	}
}

func TestSupervisor_RestartsAfterCrash(t *testing.T) {
	dir := t.TempDir()
	bin, out := fakeCaddy(t, dir, "caddy", "if [ ! -f "+dir+"/ran ]; then touch "+dir+"/ran; exit 3; fi\nexec sleep 1000")
	called := make(chan struct{}, 4)
	s := newSup(bin, "c.json")
	s.OnRestart = func() { called <- struct{}{} }
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	waitLines(t, out, 2)
	select {
	case <-called:
	case <-time.After(10 * time.Second):
		t.Fatal("OnRestart not called")
	}
	time.Sleep(100 * time.Millisecond)
	if len(called) != 0 || len(lines(out)) != 2 {
		t.Fatalf("expected exactly one restart, got %d calls, %d starts", len(called), len(lines(out)))
	}
}

func TestSupervisor_RestartsAfterSIGKILL(t *testing.T) {
	testCrash(t, func(pid int) {
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
			t.Fatal(err)
		}
	})
}

func TestSupervisor_StopNoRestart(t *testing.T) {
	dir := t.TempDir()
	bin, out := fakeCaddy(t, dir, "caddy", "exec sleep 1000")
	var restarts int32
	s := newSup(bin, "c.json")
	s.OnRestart = func() { atomic.AddInt32(&restarts, 1) }
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	pid := pidOf(t, waitLines(t, out, 1)[0])
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	if syscall.Kill(pid, 0) == nil {
		t.Fatal("process still alive after Stop")
	}
	time.Sleep(200 * time.Millisecond)
	if len(lines(out)) != 1 || atomic.LoadInt32(&restarts) != 0 {
		t.Fatalf("restarted after Stop: %v, %d", lines(out), restarts)
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

func TestSupervisor_RestartEscalatesToKill(t *testing.T) {
	dir := t.TempDir()
	// Ignores TERM; only SIGKILL stops it.
	bin, out := fakeCaddy(t, dir, "caddy", "trap '' TERM\nwhile true; do sleep 0.05; done")
	s := newSup(bin, "c.json")
	s.TermTimeout = 200 * time.Millisecond
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	waitLines(t, out, 1)
	time.Sleep(100 * time.Millisecond) // let the trap install
	if err := s.Restart(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitLines(t, out, 2)
}

func TestSupervisor_StopDuringBackoffCancelsRestart(t *testing.T) {
	dir := t.TempDir()
	bin, out := fakeCaddy(t, dir, "caddy", "exit 1")
	s := newSup(bin, "c.json")
	s.Backoff = func(int) time.Duration { return 300 * time.Millisecond }
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitLines(t, out, 1)
	time.Sleep(50 * time.Millisecond)
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if n := len(lines(out)); n != 1 {
		t.Fatalf("restarted despite Stop: %d starts", n)
	}
}

func TestDefaultBackoff(t *testing.T) {
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second}
	for i, w := range want {
		if got := defaultBackoff(i); got != w {
			t.Errorf("defaultBackoff(%d) = %v, want %v", i, got, w)
		}
	}
	if got := defaultBackoff(1000); got != 30*time.Second {
		t.Errorf("large attempt = %v", got)
	}
}
