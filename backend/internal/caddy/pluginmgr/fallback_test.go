package pluginmgr

import (
	"strings"
	"testing"
	"time"
)

func TestFallBackToShipped_ClearsManagedBinary(t *testing.T) {
	e := newEnv(t)
	promoteFake(t, e, shippedBin())
	if !e.m.FallBackToShipped("exec format error") {
		t.Fatal("expected a fallback")
	}
	if _, ok := e.m.Store.Current(); ok {
		t.Fatal("current must be cleared")
	}
	if e.changes() != 1 {
		t.Fatalf("OnChanged calls: %d", e.changes())
	}
	op, ok := e.m.LastFinished(OpFallback)
	if !ok || op.Step != StepFailed || !strings.Contains(op.Error, "exec format error") || op.FinishedAt.IsZero() {
		t.Fatalf("fallback op = %+v %v", op, ok)
	}
	// Nothing left to fall back from.
	if e.m.FallBackToShipped("again") {
		t.Fatal("no second fallback expected")
	}
}

func TestFallBackToShipped_NothingManaged(t *testing.T) {
	e := newEnv(t)
	if e.m.FallBackToShipped("boom") {
		t.Fatal("no fallback without a managed binary")
	}
	if _, ok := e.m.LastFinished(OpFallback); ok {
		t.Fatal("no operation expected")
	}
}

func TestFallBackToShipped_RepromotesSeed(t *testing.T) {
	e := seededEnv(t)
	seed, _ := e.m.Store.Seed()
	if e.m.FallBackToShipped("boom") {
		t.Fatal("already on the seed")
	}
	if op := e.run(t, OpAdd, ratelimit); op.Step != StepDone {
		t.Fatalf("add: %+v", op)
	}
	if !e.m.FallBackToShipped("boom") {
		t.Fatal("expected a fallback to the seed")
	}
	if cur, ok := e.m.Store.Current(); !ok || cur != seed {
		t.Fatalf("current = %q %v, want the seed", cur, ok)
	}
}

func TestFallBackToShipped_NotWhileBusy(t *testing.T) {
	e := newEnv(t)
	promoteFake(t, e, shippedBin())
	e.restart.block = make(chan struct{})
	op, err := e.m.Start(OpAdd, ratelimit)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if got, _ := e.m.Get(op.ID); got.Step == StepRestarting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("operation did not reach the restart")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if e.m.FallBackToShipped("boom") {
		t.Fatal("the running operation owns the binary")
	}
	close(e.restart.block)
	waitOp(t, e.m, op.ID)
}
