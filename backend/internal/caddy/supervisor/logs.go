package supervisor

import (
	"bytes"
	"io"
	"strings"
	"sync"
)

const (
	// maxRecentLines is how many of Caddy's output lines RecentLogs keeps.
	maxRecentLines = 50
	// maxPartialLine bounds an unterminated line before it is kept as is.
	maxPartialLine = 64 << 10
)

// logRing keeps the most recent output lines of Caddy.
type logRing struct {
	mu    sync.Mutex
	lines []string
}

func (r *logRing) add(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, line)
	if n := len(r.lines) - maxRecentLines; n > 0 {
		r.lines = append(r.lines[:0:0], r.lines[n:]...)
	}
}

func (r *logRing) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.lines...)
}

// lineWriter copies one output stream of Caddy to out and records its
// complete lines in ring. Each stream of each process gets its own writer.
type lineWriter struct {
	out     io.Writer
	ring    *logRing
	partial []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	_, _ = w.out.Write(p)
	w.partial = append(w.partial, p...)
	for {
		i := bytes.IndexByte(w.partial, '\n')
		if i < 0 {
			break
		}
		w.ring.add(strings.TrimSuffix(string(w.partial[:i]), "\r"))
		w.partial = w.partial[i+1:]
	}
	if len(w.partial) > maxPartialLine {
		w.flush()
	}
	return len(p), nil
}

// flush records an unterminated last line.
func (w *lineWriter) flush() {
	if len(w.partial) > 0 {
		w.ring.add(string(w.partial))
		w.partial = nil
	}
}
