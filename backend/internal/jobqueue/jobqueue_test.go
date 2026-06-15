package jobqueue

import (
	"errors"
	"testing"
	"time"
)

func TestAddJobBeforeStart(t *testing.T) {
	saved := worker
	worker = nil
	t.Cleanup(func() { worker = saved })

	if err := AddJob(Job{Name: "x", Action: func() error { return nil }}); err == nil {
		t.Error("expected error adding job before Start")
	}
}

func TestShutdownBeforeStart(t *testing.T) {
	saved := cancel
	cancel = nil
	t.Cleanup(func() { cancel = saved })

	if err := Shutdown(); err == nil {
		t.Error("expected error shutting down before Start")
	}
}

func TestStartRunJobAndShutdown(t *testing.T) {
	Start()
	defer func() { _ = Shutdown() }()

	runOne := func(name string) {
		t.Helper()
		done := make(chan struct{})
		if err := AddJob(Job{Name: name, Action: func() error { close(done); return nil }}); err != nil {
			t.Fatalf("AddJob: %v", err)
		}
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("job %q did not run within timeout", name)
		}
	}

	runOne("first")
	runOne("second")
}

func TestAddJobs(t *testing.T) {
	Start()
	// AddJobs cancels the queue context once all jobs are added, so no manual
	// Shutdown is needed.

	const n = 3
	done := make(chan struct{}, n)
	jobs := make([]Job, n)
	for i := range jobs {
		jobs[i] = Job{Name: "batch", Action: func() error { done <- struct{}{}; return nil }}
	}
	worker.Queue.AddJobs(jobs)

	for i := 0; i < n; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d/%d batch jobs ran", i, n)
		}
	}
}

func TestJobRun(t *testing.T) {
	ran := false
	j := Job{Name: "t", Action: func() error { ran = true; return nil }}
	if err := j.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !ran {
		t.Error("action did not run")
	}

	jErr := Job{Name: "e", Action: func() error { return errors.New("x") }}
	if err := jErr.Run(); err == nil {
		t.Error("expected error from Run")
	}
}
