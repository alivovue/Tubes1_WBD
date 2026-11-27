package integration_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/backend/migration/engines"
	pgengine "tubes1_wbd/backend/migration/engines/postgres"
)

func TestUnrelatedMigrationDoesNotLockPreviousBatchRows(t *testing.T) {
	f, _, runner := newWorkerFixture(t)
	completedBatch(t, f, runner)
	id := enqueueMigration(t, f, "unrelated_schema_change")
	external, err := f.dbs.Managed.BeginTx(f.ctx, nil)
	must(t, err)
	defer external.Rollback()
	_, err = external.ExecContext(f.ctx, `SELECT id FROM batch_counters WHERE id=1 FOR UPDATE`)
	must(t, err)
	runner.JobTimeout = 5 * time.Second
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, id, "succeeded")
	// The external row lock is deliberately held until after the worker finishes.
}

func TestConcurrentExternalWritesCannotBecomeTrustedBatchVersions(t *testing.T) {
	f, _, runner := newWorkerFixture(t)
	original := completedBatch(t, f, runner)
	id := enqueueDefinition(t, f, migration.DefinitionInput{Title: "Concurrent approved update", UpSQL: `UPDATE batch_counters SET other=other+1`, DownSQL: `UPDATE batch_counters SET other=other-1`})
	guards := 0
	runner.Execute = func(ctx context.Context, target *dbmanagement.Target, request engines.JobRequest, hooks engines.JobHooks) (engines.JobReceipt, error) {
		guard := hooks.Guard
		hooks.Guard = func(ctx context.Context) error {
			if err := guard(ctx); err != nil {
				return err
			}
			guards++
			if guards == 2 {
				_, err := f.dbs.Managed.ExecContext(ctx, `UPDATE batch_counters SET value=value WHERE id=1`)
				return err
			}
			return nil
		}
		return pgengine.RunJob(ctx, target.DB, target.Adapter, request, hooks)
	}
	_, err := runner.RunOnce(f.ctx)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "40001" {
		t.Fatalf("concurrent tuple change was not rejected: %v", err)
	}
	assertJob(t, f, id, "pending")
	var other int
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT sum(other) FROM batch_counters`).Scan(&other))
	if other != 0 {
		t.Fatal("serialization failure committed part of the migration")
	}
	var external bool
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT u.after_xmin<>t.xmin::text FROM _schakker.batch_undo u JOIN batch_counters t ON t.id=u.row_id WHERE u.job_id=$1 AND u.row_id=1`, original.ID).Scan(&external))
	if !external {
		t.Fatal("external no-op write was accepted as a trusted row version")
	}
}
