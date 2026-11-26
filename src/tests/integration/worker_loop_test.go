package integration_test

import (
	"context"
	"testing"
	"time"
)

func TestWorkerLoopProcessesQueuedMigrationAndStopsOnCancellation(t *testing.T) {
	f, _, runner := newWorkerFixture(t)
	id := enqueueMigration(t, f, "loop_execution")
	runner.Concurrency = 2
	runner.PollInterval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(f.ctx)
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx); close(done) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("worker remained active during cleanup")
		}
	}()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			t.Fatalf("worker exited before completion: %v", err)
		case <-deadline.C:
			j, err := f.runtimeStore.Job(f.ctx, id)
			t.Fatalf("worker did not finish queued migration: status=%s error=%s lookup=%v", j.Status, j.ErrorClass, err)
		case <-ticker.C:
			j, err := f.runtimeStore.Job(f.ctx, id)
			must(t, err)
			if j.Status == "succeeded" {
				cancel()
				select {
				case err := <-done:
					must(t, err)
				case <-time.After(2 * time.Second):
					t.Fatal("worker did not stop on cancellation")
				}
				assertJob(t, f, id, "succeeded")
				return
			}
		}
	}
}
