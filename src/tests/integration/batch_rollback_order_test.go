package integration_test

import (
	"fmt"
	"testing"

	"tubes1_wbd/backend/migration"
	"tubes1_wbd/backend/migration/engines"
	"tubes1_wbd/backend/worker"
	"tubes1_wbd/database/dao"
)

func completedBatch(t *testing.T, f *workflowFixture, runner *worker.Runner) dao.MigrationJob {
	t.Helper()
	revision := batchDefinition(t, f, 6, 2)
	_, err := f.dbs.Managed.ExecContext(f.ctx, `ALTER TABLE batch_counters ADD other bigint NOT NULL DEFAULT 0`)
	must(t, err)
	review := f.approve(t, f.submit(t, revision))
	id, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "first-batch")
	must(t, err)
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	return assertJob(t, f, id, "succeeded")
}

func enqueueRollback(t *testing.T, f *workflowFixture, execution string) string {
	t.Helper()
	review, err := f.workflow.RollbackReview(f.ctx, f.editor, execution, f.state(t).Generation)
	must(t, err)
	review = f.approve(t, review)
	id, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, fmt.Sprintf("rollback-%d", review.ID))
	must(t, err)
	return id
}

func TestBatchRollbackAfterApprovedUpdates(t *testing.T) {
	for _, batch := range []bool{true, false} {
		for _, column := range []string{"value", "other"} {
			t.Run(fmt.Sprintf("batch=%v/column=%s", batch, column), func(t *testing.T) {
				f, _, runner := newWorkerFixture(t)
				first := completedBatch(t, f, runner)
				up, down := "UPDATE batch_counters SET "+column+"="+column+"+2 WHERE id<=4", "UPDATE batch_counters SET "+column+"="+column+"-2 WHERE id<=4"
				if batch {
					var err error
					up, down, err = engines.BatchSQL(engines.BatchSpec{Schema: "public", Table: "batch_counters", Key: "id", Column: column, Delta: 2, Size: 2, ExpectedRows: 6})
					must(t, err)
				}
				secondID := enqueueDefinition(t, f, migration.DefinitionInput{Title: "Following update", UpSQL: up, DownSQL: down})
				_, err := runner.RunOnce(f.ctx)
				must(t, err)
				second := assertJob(t, f, secondID, "succeeded")
				for _, execution := range []string{second.ExecutionID, first.ExecutionID} {
					id := enqueueRollback(t, f, execution)
					_, err := runner.RunOnce(f.ctx)
					must(t, err)
					assertJob(t, f, id, "succeeded")
				}
				var values, other int64
				must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT sum(value),sum(other) FROM batch_counters`).Scan(&values, &other))
				if values != 0 || other != 0 || f.state(t).ActiveVersion != 0 {
					t.Fatalf("LIFO rollback did not restore data: %d %d", values, other)
				}
				for _, execution := range []string{first.ExecutionID, second.ExecutionID} {
					review, err := f.workflow.ForwardReview(f.ctx, f.editor, execution, f.state(t).Generation)
					must(t, err)
					review = f.approve(t, review)
					id, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, fmt.Sprintf("forward-%d", review.ID))
					must(t, err)
					_, err = runner.RunOnce(f.ctx)
					must(t, err)
					assertJob(t, f, id, "succeeded")
				}
			})
		}
	}
}

func TestApprovedUpdatesDoNotHideExternalBatchWrites(t *testing.T) {
	f, _, runner := newWorkerFixture(t)
	first := completedBatch(t, f, runner)
	_, err := f.dbs.Managed.ExecContext(f.ctx, `UPDATE batch_counters SET value=value WHERE id=1`)
	must(t, err)
	id := enqueueDefinition(t, f, migration.DefinitionInput{Title: "Trusted update after external edit", UpSQL: "UPDATE batch_counters SET value=value+2", DownSQL: "UPDATE batch_counters SET value=value-2"})
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	second := assertJob(t, f, id, "succeeded")
	down := enqueueRollback(t, f, second.ExecutionID)
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, down, "succeeded")
	down = enqueueRollback(t, f, first.ExecutionID)
	_, err = runner.RunOnce(f.ctx)
	wantError(t, err, engines.ErrUnsafeCompensation)
	assertJob(t, f, down, "failed")
}

func TestBatchRollbackAfterCancellingALaterPartialBatch(t *testing.T) {
	f, _, runner := newWorkerFixture(t)
	first := completedBatch(t, f, runner)
	up, down, err := engines.BatchSQL(engines.BatchSpec{Schema: "public", Table: "batch_counters", Key: "id", Column: "value", Delta: 2, Size: 2, ExpectedRows: 6})
	must(t, err)
	revision, err := f.workflow.CreateDefinition(f.ctx, f.editor, f.database, migration.DefinitionInput{Title: "Later partial batch", UpSQL: up, DownSQL: down})
	must(t, err)
	review := f.approve(t, f.submit(t, revision))
	id, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "later-partial")
	must(t, err)
	stopAfterBatch(runner, 1)
	_, err = runner.RunOnce(f.ctx)
	if err == nil {
		t.Fatal("batch interruption was ignored")
	}
	assertJob(t, f, id, "partial")
	cancel, err := f.workflow.SubmitReview(f.ctx, f.editor, migration.ReviewInput{RevisionID: revision.ID, Action: "batch_cancel", TargetJobID: &id, ExpectedGeneration: f.state(t).Generation})
	must(t, err)
	cancel = f.approve(t, cancel)
	cancellation, err := f.workflow.Enqueue(f.ctx, f.editor, cancel.ID, "cancel-later-partial")
	must(t, err)
	runner.Execute = nil
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, cancellation, "reverted")
	downID := enqueueRollback(t, f, first.ExecutionID)
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, downID, "succeeded")
	var sum int64
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT sum(value) FROM batch_counters`).Scan(&sum))
	if sum != 0 || f.state(t).ActiveVersion != 0 {
		t.Fatal("cancellation prevented rollback of the preceding batch")
	}
}

func TestFailedUpdatesPreserveBatchVersionExpectations(t *testing.T) {
	for _, tamper := range []bool{false, true} {
		t.Run(fmt.Sprint(tamper), func(t *testing.T) {
			f, _, runner := newWorkerFixture(t)
			first := completedBatch(t, f, runner)
			failure := "SELECT 1/0"
			if tamper {
				_, err := f.dbs.Managed.ExecContext(f.ctx, `CREATE FUNCTION public.erase_version_snapshot() RETURNS int LANGUAGE plpgsql AS $$BEGIN DELETE FROM pg_temp._schakker; RETURN 1; END$$`)
				must(t, err)
				failure = "SELECT public.erase_version_snapshot()"
			}
			id := enqueueDefinition(t, f, migration.DefinitionInput{Title: "Fail after update", UpSQL: "UPDATE batch_counters SET value=value+2; " + failure, DownSQL: "SELECT 1"})
			_, err := runner.RunOnce(f.ctx)
			if tamper {
				wantError(t, err, engines.ErrProtectedMetadata)
			} else if err == nil {
				t.Fatal("division by zero succeeded")
			}
			assertJob(t, f, id, "failed")
			down := enqueueRollback(t, f, first.ExecutionID)
			_, err = runner.RunOnce(f.ctx)
			must(t, err)
			assertJob(t, f, down, "succeeded")
		})
	}
}

func TestMigrationCannotReplaceTheProtectedVersionSnapshot(t *testing.T) {
	f, _, runner := newWorkerFixture(t)
	first := completedBatch(t, f, runner)
	_, err := f.dbs.Managed.ExecContext(f.ctx, `CREATE FUNCTION public.replace_version_snapshot() RETURNS int LANGUAGE plpgsql AS $$BEGIN
 CREATE TEMP TABLE snapshot_copy AS SELECT * FROM pg_temp._schakker;
 DROP TABLE pg_temp._schakker;
 CREATE TEMP VIEW _schakker AS SELECT * FROM snapshot_copy;
 RETURN 1;
END$$`)
	must(t, err)
	id := enqueueDefinition(t, f, migration.DefinitionInput{Title: "Replace internal snapshot", UpSQL: "UPDATE batch_counters SET value=value+2; SELECT public.replace_version_snapshot()", DownSQL: "SELECT 1"})
	_, err = runner.RunOnce(f.ctx)
	wantError(t, err, engines.ErrProtectedMetadata)
	assertJob(t, f, id, "failed")
	down := enqueueRollback(t, f, first.ExecutionID)
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, down, "succeeded")
}
