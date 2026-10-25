package pluginmgr

import (
	"context"
	"strings"
	"testing"
)

func TestRun_RecoversFromPanic(t *testing.T) {
	e := newEnv(t)
	e.m.LiveConfig = func(context.Context) ([]byte, error) { panic("live config exploded") }
	op := e.run(t, OpAdd, ratelimit)
	if op.Step != StepFailed || !strings.Contains(op.Error, "live config exploded") {
		t.Fatalf("op: %+v", op)
	}
	// The manager is usable again.
	e.m.LiveConfig = func(context.Context) ([]byte, error) { return []byte(`{"apps":{}}`), nil }
	if op := e.run(t, OpAdd, ratelimit); op.Step != StepDone {
		t.Fatalf("op after the panic: %+v", op)
	}
}
