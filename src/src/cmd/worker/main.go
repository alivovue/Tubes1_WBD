package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"schakker_shared/observability"
	"tubes1_wbd/backend"
)

func main() {
	if err := run(); err != nil {
		observability.Event(context.Background(), "worker.stopped", err)
		os.Exit(1)
	}
}
func run() error {
	shutdown, err := observability.Configure("schakker-worker", nil, nil)
	if err != nil {
		return err
	}
	defer func() {
		ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		shutdown(ctx)
	}()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runner, closeDB, err := backend.OpenWorker(".")
	if err != nil {
		return err
	}
	defer closeDB()
	observability.Event(ctx, "worker.started", nil, "concurrency", runner.Concurrency)
	return runner.Run(ctx)
}
