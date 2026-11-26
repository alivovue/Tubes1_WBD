package unit_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"net"
	"testing"
	"time"
	"tubes1_wbd/backend/dbmanagement/adapters"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/backend/migration/engines"
	"tubes1_wbd/backend/queue"
	"tubes1_wbd/backend/worker"
	"tubes1_wbd/database/dao"
)

func TestWorkerOutcomesRetainCommittedAndCompensatedProgress(t *testing.T) {
	for _, tc := range []struct {
		name, operation string
		err             error
		progress        engines.JobProgress
		attempt         int
		status, class   string
	}{
		{name: "success", status: "succeeded"}, {name: "compensated", operation: "batch_cancel", status: "reverted"},
		{name: "uncertain", err: fmt.Errorf("wrapped: %w", engines.ErrJobUncertain), status: "uncertain", class: "commit_unconfirmed"},
		{name: "transient", err: context.DeadlineExceeded, status: "pending", class: "temporary_failure"},
		{name: "retry exhausted", err: context.DeadlineExceeded, attempt: 3, status: "failed", class: "execution_rejected"},
		{name: "permanent", err: errors.New("SQL rejected"), status: "failed", class: "execution_rejected"},
		{name: "partial transient", err: context.Canceled, progress: engines.JobProgress{Batches: 1}, status: "partial", class: "temporary_failure"},
		{name: "partial permanent", err: errors.New("SQL rejected"), progress: engines.JobProgress{Batches: 1}, status: "partial", class: "batch_incomplete"},
		{name: "partial exhausted", err: context.Canceled, attempt: 3, progress: engines.JobProgress{Batches: 1}, status: "partial", class: "batch_incomplete"},
		{name: "compensation transient", operation: "batch_cancel", err: context.Canceled, progress: engines.JobProgress{RevertedBatches: 1}, status: "partially_reverted", class: "temporary_failure"},
		{name: "compensation permanent", operation: "batch_cancel", err: errors.New("rejected"), progress: engines.JobProgress{RevertedBatches: 1}, status: "partially_reverted", class: "compensation_incomplete"},
		{name: "compensation exhausted", operation: "batch_cancel", err: context.Canceled, attempt: 3, progress: engines.JobProgress{RevertedBatches: 1}, status: "partially_reverted", class: "compensation_incomplete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, class := worker.Outcome(tc.err, dao.MigrationJob{Operation: tc.operation, Attempt: max(1, tc.attempt), MaxAttempts: 3}, tc.progress)
			if status != tc.status || class != tc.class {
				t.Fatalf("outcome=%s/%s want=%s/%s", status, class, tc.status, tc.class)
			}
		})
	}
}
func TestWorkerRetryClassificationIncludesInfrastructureAndWrappedFailures(t *testing.T) {
	for _, err := range []error{migration.ErrWorkflowUnavailable, adapters.ErrConnection, &net.DNSError{IsTimeout: true}, &pgconn.PgError{Code: "55P03"}, &pgconn.PgError{Code: "53300"}, &pgconn.PgError{Code: "57P01"}, &pgconn.PgError{Code: "57P02"}, &pgconn.PgError{Code: "57P03"}} {
		if !worker.Retryable(fmt.Errorf("wrapped: %w", err)) {
			t.Errorf("infrastructure failure not retryable: %v", err)
		}
	}
	if worker.Retryable(errors.New("invalid SQL")) || worker.Retryable(nil) {
		t.Fatal("permanent failure marked transient")
	}
}
func TestQueueRejectsMalformedIdentifiersAndUnsignedWorkBeforeStorage(t *testing.T) {
	s := &queue.Service{}
	for _, id := range []string{"", "not-a-uuid", "../other-job", "12345678-1234-1234-1234-12345678901Z"} {
		_, _, err := s.Status(context.Background(), 1, id)
		if !errors.Is(err, dao.ErrInvalid) {
			t.Fatalf("invalid job identifier %q: %v", id, err)
		}
	}
	for _, plan := range []migration.ApprovedPlan{{SQL: "SELECT 1", SQLHash: "tampered", ActionHash: "approved"}, {SQL: "SELECT 1", SQLHash: migration.SQLHash("SELECT 1")}} {
		_, err := s.Enqueue(context.Background(), nil, plan, "key")
		if !errors.Is(err, dao.ErrInvalid) {
			t.Fatalf("unsigned work reached storage: %v", err)
		}
	}
}
func TestQueueBackoffAndConflictClassification(t *testing.T) {
	for _, tc := range []struct {
		attempt int
		seconds int
	}{{-10, 1}, {0, 1}, {1, 1}, {2, 2}, {6, 32}, {7, 64}, {999, 64}} {
		if queue.RetryDelay(tc.attempt) != time.Duration(tc.seconds)*time.Second {
			t.Errorf("bad backoff at %d", tc.attempt)
		}
	}
	if !queue.Conflict(fmt.Errorf("wrapped: %w", dao.ErrJobLeaseLost)) || !queue.Conflict(dao.ErrConflict) || queue.Conflict(errors.New("other")) {
		t.Fatal("queue conflict classification changed")
	}
}

func TestWorkerRejectsInvalidRuntimeConfigurationBeforeClaimingWork(t *testing.T) {
	for _, name := range []string{"store", "workflow", "databases", "owner", "no concurrency", "excessive concurrency", "short lease", "no polling", "no timeout"} {
		t.Run(name, func(t *testing.T) {
			_, store, _ := newSQLStub(t)
			_, databases, migrations := newServices(t, store)
			runner := worker.New(store, migrations.Workflow(), databases, "test-worker")
			switch name {
			case "store":
				runner.Store = nil
			case "workflow":
				runner.Workflow = nil
			case "databases":
				runner.Databases = nil
			case "owner":
				runner.Owner = ""
			case "no concurrency":
				runner.Concurrency = 0
			case "excessive concurrency":
				runner.Concurrency = 33
			case "short lease":
				runner.Lease = 2 * time.Second
			case "no polling":
				runner.PollInterval = 0
			case "no timeout":
				runner.JobTimeout = 0
			}
			claimed, err := runner.RunOnce(context.Background())
			if claimed || !errors.Is(err, dao.ErrInvalid) {
				t.Fatalf("invalid worker reached queue: claimed=%v err=%v", claimed, err)
			}
			if err := runner.Run(context.Background()); !errors.Is(err, dao.ErrInvalid) {
				t.Fatalf("invalid worker started polling: %v", err)
			}
		})
	}
}
