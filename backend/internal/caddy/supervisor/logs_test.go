package supervisor

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestLogRing_KeepsLastLines(t *testing.T) {
	var r logRing
	var out bytes.Buffer
	w := &lineWriter{out: &out, ring: &r}
	for i := range maxRecentLines + 10 {
		fmt.Fprintf(w, "line %d\n", i)
	}
	got := r.snapshot()
	if len(got) != maxRecentLines {
		t.Fatalf("kept %d lines, want %d", len(got), maxRecentLines)
	}
	if got[0] != "line 10" || got[len(got)-1] != fmt.Sprintf("line %d", maxRecentLines+9) {
		t.Fatalf("first/last = %q / %q", got[0], got[len(got)-1])
	}
	if !strings.HasPrefix(out.String(), "line 0\n") {
		t.Fatal("output must still be copied through")
	}
}

func TestLogRing_SplitWritesAndFlush(t *testing.T) {
	var r logRing
	w := &lineWriter{out: io.Discard, ring: &r}
	io.WriteString(w, "hel")
	io.WriteString(w, "lo\r\nwor")
	io.WriteString(w, "ld\n\npanic: x")
	w.flush()
	want := []string{"hello", "world", "", "panic: x"}
	if got := r.snapshot(); !slices.Equal(got, want) {
		t.Fatalf("lines = %q, want %q", got, want)
	}
	// The snapshot is a copy.
	r.snapshot()[0] = "changed"
	if r.snapshot()[0] != "hello" {
		t.Fatal("snapshot must not alias the ring")
	}
}

func TestSupervisor_RecentLogs(t *testing.T) {
	dir := t.TempDir()
	bin, out := fakeCaddy(t, dir, "caddy", "echo 'to stdout'\necho 'panic: to stderr' >&2\nexec sleep 1000")
	s := newSup(bin, "c.json")
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	waitLines(t, out, 1)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		got := s.RecentLogs()
		if slices.Contains(got, "to stdout") && slices.Contains(got, "panic: to stderr") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("RecentLogs = %q", s.RecentLogs())
}
