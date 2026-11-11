package integration_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	admindao "tubes1_wbd_admin/database/dao"

	"tubes1_wbd/backend"
	"tubes1_wbd/backend/api"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/backend/migration/engines"
	pgengine "tubes1_wbd/backend/migration/engines/postgres"
	"tubes1_wbd/backend/queue"
	"tubes1_wbd/backend/worker"
	"tubes1_wbd/database/dao"
)

func p3Fixture(t *testing.T) (*workflowFixture, *queue.Service, *worker.Runner) {
	t.Helper()
	f := newWorkflowFixture(t)
	jobs := backend.ConfigureExecution(f.runtimeStore, f.userAccess, f.databases, f.migrations)
	f.workflow = f.migrations.Workflow()
	runner := worker.New(f.runtimeStore, f.workflow, f.databases, "worker-one")
	return f, jobs, runner
}
func enqueueP3(t *testing.T, f *workflowFixture, title string) string {
	t.Helper()
	review := f.approve(t, f.submit(t, f.definition(t, title)))
	id, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, title)
	must(t, err)
	return id
}
func assertJob(t *testing.T, f *workflowFixture, id, status string) dao.MigrationJob {
	t.Helper()
	j, err := f.runtimeStore.Job(f.ctx, id)
	must(t, err)
	if j.Status != status {
		t.Fatalf("job status=%s want=%s error=%s", j.Status, status, j.ErrorClass)
	}
	return j
}
func TestP3WorkerExecutesApprovedJobAndRollbackForward(t *testing.T) {
	f, jobs, r := p3Fixture(t)
	id := enqueueP3(t, f, "worker_a")
	assertJob(t, f, id, "pending")
	var exists bool
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT to_regclass('public.worker_a') IS NOT NULL`).Scan(&exists))
	if exists {
		t.Fatal("enqueue executed SQL")
	}
	claimed, err := r.RunOnce(f.ctx)
	must(t, err)
	if !claimed {
		t.Fatal("job not claimed")
	}
	j := assertJob(t, f, id, "succeeded")
	if f.state(t).ActiveVersion != 1 {
		t.Fatal("committed head missing")
	}
	_, _, err = jobs.Status(f.ctx, f.reader.ID, id)
	must(t, err)
	review, err := f.workflow.RollbackReview(f.ctx, f.editor, j.ExecutionID, f.state(t).Generation)
	must(t, err)
	review = f.approve(t, review)
	down, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "rollback-a")
	must(t, err)
	_, err = r.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, down, "succeeded")
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT to_regclass('public.worker_a') IS NOT NULL`).Scan(&exists))
	if exists || f.state(t).ActiveVersion != 0 {
		t.Fatal("rollback did not commit original down")
	}
	review, err = f.workflow.ForwardReview(f.ctx, f.editor, j.ExecutionID, f.state(t).Generation)
	must(t, err)
	review = f.approve(t, review)
	forward, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "forward-a")
	must(t, err)
	_, err = r.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, forward, "succeeded")
	history, err := f.workflow.History(f.ctx, f.reader.ID, f.database, true, 50, 0)
	must(t, err)
	if len(history) != 1 || history[0].JobID != forward {
		t.Fatal("exact reversal filtering incorrect")
	}
}
func TestP3LostCommitAcknowledgmentAndDuplicateDeliveryDoNotReplaySQL(t *testing.T) {
	f, jobs, r := p3Fixture(t)
	id := enqueueP3(t, f, "receipt_a")
	r.Execute = func(ctx context.Context, target *dbmanagement.Target, request engines.JobRequest, hooks engines.JobHooks) (engines.JobReceipt, error) {
		receipt, err := pgengine.RunJob(ctx, target.DB, target.Adapter, request, hooks)
		if err != nil {
			return receipt, err
		}
		return receipt, engines.ErrJobUncertain
	}
	_, err := r.RunOnce(f.ctx)
	must(t, err)
	j := assertJob(t, f, id, "succeeded")
	_, err = f.dbs.Website.ExecContext(f.ctx, `UPDATE migration_jobs SET status='uncertain',attempt=max_attempts WHERE id=$1`, id)
	must(t, err)
	_, err = jobs.Retry(f.ctx, f.editor, id)
	must(t, err)
	r.Execute = nil
	_, err = r.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, id, "succeeded")
	_, err = f.dbs.Website.ExecContext(f.ctx, `UPDATE migration_jobs SET status='running',lease_owner='dead-process',lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, id)
	must(t, err)
	r.Execute = nil
	_, err = r.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, id, "succeeded")
	history, err := f.workflow.History(f.ctx, f.editor, f.database, false, 50, 0)
	must(t, err)
	if len(history) != 1 || history[0].ExecutionID != j.ExecutionID {
		t.Fatal("duplicate delivery appended/replayed migration")
	}
}
func TestP3StatementFailureRollsBackAndDoesNotAdvanceHistory(t *testing.T) {
	f, _, r := p3Fixture(t)
	revision, err := f.workflow.CreateDefinition(f.ctx, f.editor, f.database, migration.DefinitionInput{Title: "failure", UpSQL: "CREATE TABLE worker_failed(id integer); INSERT INTO no_such_table VALUES(1);", DownSQL: "DROP TABLE worker_failed;"})
	must(t, err)
	review := f.approve(t, f.submit(t, revision))
	id, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "failure")
	must(t, err)
	_, err = r.RunOnce(f.ctx)
	if err == nil {
		t.Fatal("statement failure disappeared")
	}
	assertJob(t, f, id, "failed")
	var exists bool
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT to_regclass('public.worker_failed') IS NOT NULL`).Scan(&exists))
	if exists || f.state(t).ActiveVersion != 0 {
		t.Fatal("failed transaction partially committed")
	}
}

func TestP3HiddenReceiptTamperingRollsBack(t *testing.T) {
	f, _, r := p3Fixture(t)
	first := enqueueP3(t, f, "protected_first")
	_, err := r.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, first, "succeeded")
	_, err = f.dbs.Managed.ExecContext(f.ctx, `CREATE FUNCTION public.tamper_p3() RETURNS bigint LANGUAGE plpgsql AS $$ BEGIN DELETE FROM _schakker.job_receipts; RETURN 1; END $$`)
	must(t, err)
	revision, err := f.workflow.CreateDefinition(f.ctx, f.editor, f.database, migration.DefinitionInput{Title: "hidden receipt deletion", UpSQL: "SELECT public.tamper_p3();", DownSQL: "SELECT 1;"})
	must(t, err)
	review := f.approve(t, f.submit(t, revision))
	id, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "tamper")
	must(t, err)
	_, err = r.RunOnce(f.ctx)
	if !errors.Is(err, engines.ErrProtectedMetadata) {
		t.Fatalf("hidden bookkeeping write accepted: %v", err)
	}
	assertJob(t, f, id, "failed")
	var receipts int
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT count(*) FROM _schakker.job_receipts`).Scan(&receipts))
	if receipts != 1 || f.state(t).ActiveVersion != 1 {
		t.Fatal("tampering committed")
	}
}

func TestP3AuthorizedEditorCanReconcileAfterOwnerLosesAccess(t *testing.T) {
	f, jobs, r := p3Fixture(t)
	id := enqueueP3(t, f, "new_reconciler")
	_, err := r.RunOnce(f.ctx)
	must(t, err)
	replacement := f.user(t, "recovery.editor")
	must(t, f.adminAuth.SetMembership(f.ctx, f.admin, replacement.ID, f.editors.ID, true))
	must(t, f.adminAuth.SetGroupLeader(f.ctx, f.admin, f.editors.ID, replacement.ID))
	must(t, f.adminAuth.SetMembership(f.ctx, f.admin, f.editor, f.editors.ID, false))
	_, err = f.dbs.Website.ExecContext(f.ctx, `UPDATE migration_jobs SET status='uncertain',attempt=max_attempts WHERE id=$1`, id)
	must(t, err)
	_, err = jobs.Retry(f.ctx, replacement.ID, id)
	must(t, err)
	_, err = r.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, id, "succeeded")
	history, err := f.workflow.History(f.ctx, replacement.ID, f.database, false, 50, 0)
	must(t, err)
	if len(history) != 1 || history[0].ActorID != f.editor {
		t.Fatal("reconciliation rewrote the original actor/history")
	}
}

func TestP3ExpiredLeaseWithoutTargetAuthorityStaysBlockedUntilReconciled(t *testing.T) {
	f, jobs, r := p3Fixture(t)
	id := enqueueP3(t, f, "expired_authority")
	_, err := f.runtimeStore.ClaimJob(f.ctx, "dead-worker", 3*time.Second)
	must(t, err)
	replacement := f.user(t, "lease.recovery.editor")
	must(t, f.adminAuth.SetMembership(f.ctx, f.admin, replacement.ID, f.editors.ID, true))
	must(t, f.adminAuth.SetGroupLeader(f.ctx, f.admin, f.editors.ID, replacement.ID))
	must(t, f.adminAuth.SetMembership(f.ctx, f.admin, f.editor, f.editors.ID, false))
	_, err = f.dbs.Website.ExecContext(f.ctx, `UPDATE migration_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, id)
	must(t, err)
	_, err = r.RunOnce(f.ctx)
	wantError(t, err, engines.ErrJobUncertain)
	assertJob(t, f, id, "uncertain")
	_, err = jobs.Retry(f.ctx, replacement.ID, id)
	must(t, err)
	_, err = r.RunOnce(f.ctx)
	if err == nil {
		t.Fatal("reconciliation unexpectedly executed SQL")
	}
	assertJob(t, f, id, "failed")
	var exists bool
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT to_regclass('public.expired_authority') IS NOT NULL`).Scan(&exists))
	if exists {
		t.Fatal("stale authority SQL was replayed")
	}
}

func TestP3NewEditorCanCancelAfterOriginalOwnerLosesAccess(t *testing.T) {
	f, _, r := p3Fixture(t)
	revision := batchDefinition(t, f, 20, 10)
	review := f.approve(t, f.submit(t, revision))
	id, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "reassign-owner")
	must(t, err)
	stopAfterBatch(r, 1)
	_, _ = r.RunOnce(f.ctx)
	assertJob(t, f, id, "partial")
	replacement := f.user(t, "replacement.editor")
	must(t, f.adminAuth.SetMembership(f.ctx, f.admin, replacement.ID, f.editors.ID, true))
	must(t, f.adminAuth.SetGroupLeader(f.ctx, f.admin, f.editors.ID, replacement.ID))
	must(t, f.adminAuth.SetMembership(f.ctx, f.admin, f.editor, f.editors.ID, false))
	cancel, err := f.workflow.SubmitReview(f.ctx, replacement.ID, migration.ReviewInput{RevisionID: revision.ID, Action: "batch_cancel", TargetJobID: &id})
	must(t, err)
	_, err = f.workflow.Decide(f.ctx, replacement.ID, cancel.ID, f.editors.ID, "approve", "")
	must(t, err)
	_, err = f.workflow.Decide(f.ctx, f.reader.ID, cancel.ID, f.readers.ID, "approve", "")
	must(t, err)
	undo, err := f.workflow.Enqueue(f.ctx, replacement.ID, cancel.ID, "replacement-cancel")
	must(t, err)
	r.Execute = nil
	_, err = r.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, undo, "reverted")
}
func batchDefinition(t *testing.T, f *workflowFixture, rows int64, size int) dao.MigrationRevision {
	t.Helper()
	_, err := f.dbs.Managed.ExecContext(f.ctx, `CREATE TABLE public.batch_counters(id bigint PRIMARY KEY,value bigint NOT NULL)`)
	must(t, err)
	_, err = f.dbs.Managed.ExecContext(f.ctx, `INSERT INTO public.batch_counters SELECT i,0 FROM generate_series(1,$1::bigint) i`, rows)
	must(t, err)
	up, down, err := engines.BatchSQL(engines.BatchSpec{Schema: "public", Table: "batch_counters", Key: "id", Column: "value", Delta: 3, Size: size, ExpectedRows: rows})
	must(t, err)
	r, err := f.workflow.CreateDefinition(f.ctx, f.editor, f.database, migration.DefinitionInput{Title: "batch counters", UpSQL: up, DownSQL: down})
	must(t, err)
	return r
}
func stopAfterBatch(r *worker.Runner, batches int64) {
	r.Execute = func(ctx context.Context, target *dbmanagement.Target, request engines.JobRequest, h engines.JobHooks) (engines.JobReceipt, error) {
		original := h.Progress
		h.Progress = func(ctx context.Context, p engines.JobProgress) error {
			if err := original(ctx, p); err != nil {
				return err
			}
			if p.Batches >= batches {
				return context.Canceled
			}
			return nil
		}
		return pgengine.RunJob(ctx, target.DB, target.Adapter, request, h)
	}
}
func TestP3BatchResumeUsesTargetCheckpointAndFixedIDs(t *testing.T) {
	f, jobs, r := p3Fixture(t)
	revision := batchDefinition(t, f, 25, 10)
	review := f.approve(t, f.submit(t, revision))
	id, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "batch-resume")
	must(t, err)
	stopAfterBatch(r, 1)
	_, err = r.RunOnce(f.ctx)
	if err == nil {
		t.Fatal("injected interruption ignored")
	}
	assertJob(t, f, id, "partial")
	var changed int
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT count(*) FROM batch_counters WHERE value=3`).Scan(&changed))
	if changed != 10 {
		t.Fatalf("partial rows=%d", changed)
	}
	next := f.definition(t, "must_wait")
	_, err = f.workflow.SubmitReview(f.ctx, f.editor, migration.ReviewInput{RevisionID: next.ID, Action: "up"})
	wantError(t, err, migration.ErrWorkflowConflict)
	_, err = f.dbs.Managed.ExecContext(f.ctx, `INSERT INTO batch_counters VALUES(100,0)`)
	must(t, err)
	_, err = jobs.Retry(f.ctx, f.editor, id)
	must(t, err)
	r.Execute = nil
	_, err = r.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, id, "succeeded")
	var sum int
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT sum(value) FROM batch_counters`).Scan(&sum))
	if sum != 75 {
		t.Fatalf("batch replay or enlarged scope, sum=%d", sum)
	}
	_, p, err := jobs.Status(f.ctx, f.reader.ID, id)
	must(t, err)
	if p.Batches != 3 || p.Rows != 25 {
		t.Fatalf("wrong projected progress: %#v", p)
	}
}

func TestP3AutomaticRetryResumesOnlyUncommittedBatches(t *testing.T) {
	f, _, r := p3Fixture(t)
	revision := batchDefinition(t, f, 25, 10)
	review := f.approve(t, f.submit(t, revision))
	id, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "automatic-resume")
	must(t, err)
	stopAfterBatch(r, 1)
	_, _ = r.RunOnce(f.ctx)
	j := assertJob(t, f, id, "partial")
	if j.ErrorClass != "temporary_failure" {
		t.Fatal("temporary batch failure not scheduled")
	}
	_, err = f.dbs.Website.ExecContext(f.ctx, `UPDATE migration_jobs SET next_attempt_at=clock_timestamp()-interval '1 second' WHERE id=$1`, id)
	must(t, err)
	r.Execute = nil
	_, err = r.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, id, "succeeded")
	var sum int
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT sum(value) FROM batch_counters`).Scan(&sum))
	if sum != 75 {
		t.Fatal("automatic retry replayed a committed batch")
	}
}
func TestP3RetryLimitRebuildsLostProjectionAndKeepsPartialBlocked(t *testing.T) {
	f, _, r := p3Fixture(t)
	revision := batchDefinition(t, f, 25, 10)
	review := f.approve(t, f.submit(t, revision))
	id, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "exhausted-partial")
	must(t, err)
	stopAfterBatch(r, 1)
	_, _ = r.RunOnce(f.ctx)
	_, err = f.dbs.Website.ExecContext(f.ctx, `DELETE FROM migration_batch_progress WHERE job_id=$1`, id)
	must(t, err)
	_, err = f.dbs.Website.ExecContext(f.ctx, `UPDATE migration_jobs SET attempt=max_attempts,status='running',lease_owner='dead-worker',lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, id)
	must(t, err)
	r.Execute = nil
	_, err = r.RunOnce(f.ctx)
	if err == nil {
		t.Fatal("SQL retry limit ignored")
	}
	assertJob(t, f, id, "partial")
	p, err := f.runtimeStore.BatchProgress(f.ctx, id)
	must(t, err)
	if p.Batches != 1 || p.Rows != 10 {
		t.Fatalf("target checkpoint not reprojected: %#v", p)
	}
	next := f.definition(t, "after_exhaustion")
	_, err = f.workflow.SubmitReview(f.ctx, f.editor, migration.ReviewInput{RevisionID: next.ID, Action: "up"})
	wantError(t, err, migration.ErrWorkflowConflict)
}
func TestP3FreshCancellationCanReplaceAnInvalidatedReview(t *testing.T) {
	f, _, r := p3Fixture(t)
	revision := batchDefinition(t, f, 20, 10)
	review := f.approve(t, f.submit(t, revision))
	id, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "stale-cancel-source")
	must(t, err)
	stopAfterBatch(r, 1)
	_, _ = r.RunOnce(f.ctx)
	input := migration.ReviewInput{RevisionID: revision.ID, Action: "batch_cancel", TargetJobID: &id}
	old, err := f.workflow.SubmitReview(f.ctx, f.editor, input)
	must(t, err)
	old = f.approve(t, old)
	oldJob, err := f.workflow.Enqueue(f.ctx, f.editor, old.ID, "stale-cancel-old")
	must(t, err)
	f.user(t, "authority.changed")
	r.Execute = nil
	_, err = r.RunOnce(f.ctx)
	if err == nil {
		t.Fatal("stale authority cancellation executed")
	}
	stored, err := f.workflow.Review(f.ctx, f.editor, old.ID)
	must(t, err)
	if stored.State != "invalidated" {
		t.Fatal("stale cancellation was not invalidated")
	}
	fresh, err := f.workflow.SubmitReview(f.ctx, f.editor, input)
	must(t, err)
	fresh = f.approve(t, fresh)
	freshJob, err := f.workflow.Enqueue(f.ctx, f.editor, fresh.ID, "stale-cancel-fresh")
	must(t, err)
	assertJob(t, f, oldJob, "failed")
	_, err = r.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, freshJob, "reverted")
}
func TestP3ApprovedCancellationReversesOnlyCommittedBatches(t *testing.T) {
	f, _, r := p3Fixture(t)
	revision := batchDefinition(t, f, 25, 10)
	review := f.approve(t, f.submit(t, revision))
	id, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "batch-cancel")
	must(t, err)
	stopAfterBatch(r, 2)
	_, err = r.RunOnce(f.ctx)
	if err == nil {
		t.Fatal("interruption missing")
	}
	assertJob(t, f, id, "partial")
	cancel, err := f.workflow.SubmitReview(f.ctx, f.editor, migration.ReviewInput{RevisionID: revision.ID, Action: "batch_cancel", TargetJobID: &id, ExpectedGeneration: f.state(t).Generation})
	must(t, err)
	cancel = f.approve(t, cancel)
	cancelID, err := f.workflow.Enqueue(f.ctx, f.editor, cancel.ID, "cancel-approved")
	must(t, err)
	r.Execute = nil
	_, err = r.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, cancelID, "reverted")
	assertJob(t, f, id, "reverted")
	var sum int
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT sum(value) FROM batch_counters`).Scan(&sum))
	if sum != 0 {
		t.Fatalf("incomplete compensation: %d", sum)
	}
	if f.state(t).ActiveVersion != 0 {
		t.Fatal("cancel advanced active migration position")
	}
}
func TestP3UnsafeCancellationDoesNotOverwriteExternalChanges(t *testing.T) {
	f, _, r := p3Fixture(t)
	revision := batchDefinition(t, f, 20, 10)
	review := f.approve(t, f.submit(t, revision))
	id, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "unsafe-cancel")
	must(t, err)
	stopAfterBatch(r, 1)
	_, _ = r.RunOnce(f.ctx)
	_, err = f.dbs.Managed.ExecContext(f.ctx, `UPDATE batch_counters SET value=999 WHERE id=1`)
	must(t, err)
	_, err = f.workflow.SubmitReview(f.ctx, f.editor, migration.ReviewInput{RevisionID: revision.ID, Action: "batch_cancel", TargetJobID: &id})
	wantError(t, err, migration.ErrWorkflowConflict)
}

func TestP3PartialCompensationCanResumeInReverseOrder(t *testing.T) {
	f, jobs, r := p3Fixture(t)
	revision := batchDefinition(t, f, 25, 10)
	review := f.approve(t, f.submit(t, revision))
	id, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "reverse-resume")
	must(t, err)
	stopAfterBatch(r, 2)
	_, _ = r.RunOnce(f.ctx)
	cancel, err := f.workflow.SubmitReview(f.ctx, f.editor, migration.ReviewInput{RevisionID: revision.ID, Action: "batch_cancel", TargetJobID: &id})
	must(t, err)
	cancel = f.approve(t, cancel)
	undo, err := f.workflow.Enqueue(f.ctx, f.editor, cancel.ID, "reverse-approved")
	must(t, err)
	r.Execute = func(ctx context.Context, target *dbmanagement.Target, request engines.JobRequest, h engines.JobHooks) (engines.JobReceipt, error) {
		original := h.Progress
		h.Progress = func(ctx context.Context, p engines.JobProgress) error {
			if err := original(ctx, p); err != nil {
				return err
			}
			if p.RevertedBatches >= 1 {
				return context.Canceled
			}
			return nil
		}
		return pgengine.RunJob(ctx, target.DB, target.Adapter, request, h)
	}
	_, err = r.RunOnce(f.ctx)
	if err == nil {
		t.Fatal("compensation interruption missing")
	}
	assertJob(t, f, undo, "partially_reverted")
	var first, last int
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT value FROM batch_counters WHERE id=1`).Scan(&first))
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT value FROM batch_counters WHERE id=11`).Scan(&last))
	if first != 3 || last != 0 {
		t.Fatal("batches were not reversed in descending order")
	}
	_, err = jobs.Retry(f.ctx, f.editor, undo)
	must(t, err)
	r.Execute = nil
	_, err = r.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, undo, "reverted")
}
func TestP3CompletedBatchRollbackAndForwardUseOriginalSQL(t *testing.T) {
	f, _, r := p3Fixture(t)
	revision := batchDefinition(t, f, 20, 7)
	review := f.approve(t, f.submit(t, revision))
	id, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "complete-batch")
	must(t, err)
	_, err = r.RunOnce(f.ctx)
	must(t, err)
	j := assertJob(t, f, id, "succeeded")
	down, err := f.workflow.RollbackReview(f.ctx, f.editor, j.ExecutionID, f.state(t).Generation)
	must(t, err)
	down = f.approve(t, down)
	undo, err := f.workflow.Enqueue(f.ctx, f.editor, down.ID, "complete-batch-down")
	must(t, err)
	_, err = r.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, undo, "succeeded")
	var sum int
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT sum(value) FROM batch_counters`).Scan(&sum))
	if sum != 0 {
		t.Fatal("completed batch down did not restore rows")
	}
	forward, err := f.workflow.ForwardReview(f.ctx, f.editor, j.ExecutionID, f.state(t).Generation)
	must(t, err)
	forward = f.approve(t, forward)
	id, err = f.workflow.Enqueue(f.ctx, f.editor, forward.ID, "complete-batch-forward")
	must(t, err)
	_, err = r.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, id, "succeeded")
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT sum(value) FROM batch_counters`).Scan(&sum))
	if sum != 60 {
		t.Fatal("forward did not execute original up")
	}
}

func TestP3DifferentDatabasesCanExecuteInParallel(t *testing.T) {
	f, _, first := p3Fixture(t)
	otherName := f.dbs.ManagedName + "_other"
	_, err := factory.Cluster.EnsureDatabase(f.ctx, otherName, "schakker_managed", "")
	must(t, err)
	t.Cleanup(func() {
		ctx, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		admin, err := factory.Cluster.Open(ctx, "postgres")
		if err != nil {
			t.Error(err)
			return
		}
		defer admin.Close()
		_, err = admin.ExecContext(ctx, "DROP DATABASE "+pgx.Identifier{otherName}.Sanitize()+" WITH (FORCE)")
		if err != nil {
			t.Error(err)
		}
	})
	in := f.dbs.Connection()
	in.Name = "Other P3 database"
	in.DatabaseName = otherName
	other, err := f.saveConnection(f.ctx, f.admin, 0, in)
	must(t, err)
	must(t, f.adminAuth.SetGrant(f.ctx, f.admin, f.editors.ID, other.ID, admindao.ViewEdit))
	must(t, f.adminAuth.SetGrant(f.ctx, f.admin, f.readers.ID, other.ID, admindao.ViewOnly))
	one := enqueueP3(t, f, "parallel_a")
	revision, err := f.workflow.CreateDefinition(f.ctx, f.editor, other.ID, reversible("parallel_b"))
	must(t, err)
	review, err := f.workflow.SubmitReview(f.ctx, f.editor, migration.ReviewInput{RevisionID: revision.ID, Action: "up"})
	must(t, err)
	review = f.approve(t, review)
	two, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "parallel-second")
	must(t, err)
	second := worker.New(f.runtimeStore, f.workflow, f.databases, "worker-two")
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	execute := func(ctx context.Context, target *dbmanagement.Target, request engines.JobRequest, h engines.JobHooks) (engines.JobReceipt, error) {
		arrived <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return engines.JobReceipt{}, ctx.Err()
		}
		return pgengine.RunJob(ctx, target.DB, target.Adapter, request, h)
	}
	first.Execute = execute
	second.Execute = execute
	results := make(chan error, 2)
	for _, runner := range []*worker.Runner{first, second} {
		go func(r *worker.Runner) { _, err := r.RunOnce(f.ctx); results <- err }(runner)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatal("independent database jobs were serialized")
		}
	}
	close(release)
	for i := 0; i < 2; i++ {
		must(t, <-results)
	}
	assertJob(t, f, one, "succeeded")
	assertJob(t, f, two, "succeeded")
}
func TestP3LeaseFencingAndConcurrentClaims(t *testing.T) {
	f, _, _ := p3Fixture(t)
	id := enqueueP3(t, f, "lease_a")
	first, err := f.runtimeStore.ClaimJob(f.ctx, "worker-one", 3*time.Second)
	must(t, err)
	_, err = f.runtimeStore.ClaimJob(f.ctx, "worker-two", 3*time.Second)
	wantError(t, err, dao.ErrNotFound)
	_, err = f.dbs.Website.ExecContext(f.ctx, `UPDATE migration_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, id)
	must(t, err)
	second, err := f.runtimeStore.ClaimJob(f.ctx, "worker-two", 3*time.Second)
	must(t, err)
	if second.Attempt != first.Attempt+1 {
		t.Fatal("lease generation unchanged")
	}
	wantError(t, f.runtimeStore.HeartbeatJob(f.ctx, first, 3*time.Second), dao.ErrJobLeaseLost)
	wantError(t, f.runtimeStore.FinishJob(f.ctx, first, "succeeded", "", 0), dao.ErrJobLeaseLost)
	must(t, f.runtimeStore.FinishJob(f.ctx, second, "failed", "test", 0))
}
func TestP3TwoWorkersExecuteOneDatabaseOnlyOnce(t *testing.T) {
	f, _, first := p3Fixture(t)
	id := enqueueP3(t, f, "concurrent_a")
	second := worker.New(f.runtimeStore, f.workflow, f.databases, "worker-two")
	var executions atomic.Int32
	execute := func(ctx context.Context, target *dbmanagement.Target, request engines.JobRequest, h engines.JobHooks) (engines.JobReceipt, error) {
		executions.Add(1)
		return pgengine.RunJob(ctx, target.DB, target.Adapter, request, h)
	}
	first.Execute = execute
	second.Execute = execute
	var group sync.WaitGroup
	failures := make(chan error, 2)
	for _, runner := range []*worker.Runner{first, second} {
		group.Add(1)
		go func(r *worker.Runner) {
			defer group.Done()
			_, err := r.RunOnce(f.ctx)
			if err != nil && !errors.Is(err, dao.ErrNotFound) {
				failures <- err
			}
		}(runner)
	}
	group.Wait()
	close(failures)
	for err := range failures {
		must(t, err)
	}
	assertJob(t, f, id, "succeeded")
	if executions.Load() != 1 {
		t.Fatal("duplicate SQL execution")
	}
}
func TestP3JobAPIRequiresSessionCSRFAndDatabasePermission(t *testing.T) {
	f, jobs, _ := p3Fixture(t)
	id := enqueueP3(t, f, "job_api_a")
	handler := api.NewHandler(f.userAccess, f.databases, f.migrations, false, jobs)
	identity, cookie := f.login(t, "it.editor")
	csrf, err := f.access.CSRFToken(identity)
	must(t, err)
	request := func(method, cookieValue, token string) int {
		route := fmt.Sprintf("/migration-jobs/%s", id)
		if method == http.MethodPost {
			route += "/retry"
		}
		return workflowRequest(t, http.StripPrefix("/api", handler), method, route, cookieValue, token, map[string]any{}).Code
	}
	if request("GET", "", "") != 401 {
		t.Fatal("anonymous job access")
	}
	if request("GET", cookie, csrf) != 200 {
		t.Fatal("authorized polling failed")
	}
	if request("POST", cookie, "bad") != 403 {
		t.Fatal("retry CSRF bypass")
	}
	outsider := f.user(t, "job.outsider")
	_, outsideCookie := f.login(t, outsider.Username)
	if request("GET", outsideCookie, "") != 403 {
		t.Fatal("database permission bypass")
	}
}
