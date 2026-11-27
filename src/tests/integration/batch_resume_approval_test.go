package integration_test

import (
	"fmt"
	"net/http"
	"testing"
	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/api"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/database/dao"
)

func approveResume(t *testing.T, f *workflowFixture, r dao.MigrationReview, leader int64) dao.MigrationReview {
	t.Helper()
	var err error
	r, err = f.workflow.Decide(f.ctx, leader, r.ID, f.editors.ID, "approve", "")
	must(t, err)
	r, err = f.workflow.Decide(f.ctx, f.reader.ID, r.ID, f.readers.ID, "approve", "")
	must(t, err)
	return r
}
func queueResume(t *testing.T, f *workflowFixture, source string, leader int64) string {
	t.Helper()
	review, err := f.workflow.ResumeReview(f.ctx, f.editor, source, f.state(t).Generation)
	must(t, err)
	review = approveResume(t, f, review, leader)
	id, err := f.workflow.Enqueue(f.ctx, leader, review.ID, fmt.Sprintf("resume-%d", review.ID))
	must(t, err)
	return id
}
func TestFreshApprovalsResumeOriginalBatchScope(t *testing.T) {
	f, jobs, runner := newWorkerFixture(t)
	leader := f.user(t, "replacement.leader")
	must(t, f.adminAuth.SetMembership(f.ctx, f.admin, leader.ID, f.editors.ID, true))
	revision := batchDefinition(t, f, 6, 2)
	review := f.approve(t, f.submit(t, revision))
	original, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "original-partial")
	must(t, err)
	// This queued job must not starve the recovery job created after the pause.
	waiting := enqueueMigration(t, f, "waiting_behind_partial")
	stopAfterBatch(runner, 1)
	_, _ = runner.RunOnce(f.ctx)
	assertJob(t, f, original, "partial")
	must(t, f.adminAuth.SetGroupLeader(f.ctx, f.admin, f.editors.ID, leader.ID))
	_, err = jobs.Retry(f.ctx, f.editor, original)
	wantError(t, err, migration.ErrWorkflowConflict)
	_, err = f.dbs.Managed.ExecContext(f.ctx, `INSERT INTO batch_counters VALUES(100,0)`)
	must(t, err)
	fresh, err := f.workflow.ResumeReview(f.ctx, f.editor, original, f.state(t).Generation)
	must(t, err)
	_, err = f.workflow.Enqueue(f.ctx, f.editor, fresh.ID, "premature")
	wantError(t, err, migration.ErrWorkflowConflict)
	fresh = approveResume(t, f, fresh, leader.ID)
	resources, err := f.workflow.Resources(f.ctx, f.editor, f.database)
	must(t, err)
	projected, err := resources.Review(fresh)
	must(t, err)
	if projected["can_enqueue"] != true {
		t.Fatal("SPA cannot queue a freshly approved resume")
	}
	resumed, err := f.workflow.Enqueue(f.ctx, leader.ID, fresh.ID, "freshly-approved")
	must(t, err)
	if resumed == original {
		t.Fatal("fresh approvals overwrote the previous job")
	}
	runner.Execute = nil
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	result := assertJob(t, f, resumed, "succeeded")
	assertJob(t, f, original, "succeeded")
	assertJob(t, f, waiting, "pending")
	if result.ActorID != leader.ID {
		t.Fatal("resume executor attribution changed")
	}
	var sum, newRow int
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT sum(value),max(value) FILTER(WHERE id=100) FROM batch_counters`).Scan(&sum, &newRow))
	if sum != 18 || newRow != 0 {
		t.Fatalf("committed batches replayed or original scope expanded: %d %d", sum, newRow)
	}
	events, err := f.workflow.ExecutionHistory(f.ctx, f.editor, f.database, false, 50, 0)
	must(t, err)
	if len(events) != 2 || events[0].ActorID != leader.ID || !events[0].IsCommitted || events[1].Status != "partial" {
		t.Fatalf("attempt history was overwritten: %#v", events)
	}
	origin, err := f.runtimeStore.BatchOrigin(f.ctx, resumed)
	must(t, err)
	if origin.ID != original {
		t.Fatal("resume lost original checkpoint identity")
	}
	// Drain the older queued review: its original position is now stale.
	_, err = runner.RunOnce(f.ctx)
	wantError(t, err, migration.ErrWorkflowConflict)
	assertJob(t, f, waiting, "failed")
	down, err := f.workflow.RollbackReview(f.ctx, f.editor, result.ExecutionID, f.state(t).Generation)
	must(t, err)
	down = approveResume(t, f, down, leader.ID)
	undo, err := f.workflow.Enqueue(f.ctx, f.editor, down.ID, "rollback-resumed")
	must(t, err)
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, undo, "succeeded")
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT sum(value) FROM batch_counters`).Scan(&sum))
	if sum != 0 || f.state(t).ActiveVersion != 0 {
		t.Fatal("resumed execution could not roll back its original scope")
	}
}

func TestRepeatedFreshResumeKeepsCheckpointAndHistory(t *testing.T) {
	f, _, runner := newWorkerFixture(t)
	revision := batchDefinition(t, f, 6, 2)
	review := f.approve(t, f.submit(t, revision))
	first, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "first-paused")
	must(t, err)
	stopAfterBatch(runner, 1)
	_, _ = runner.RunOnce(f.ctx)
	assertJob(t, f, first, "partial")
	second := queueResume(t, f, first, f.editor)
	stopAfterBatch(runner, 2)
	_, _ = runner.RunOnce(f.ctx)
	assertJob(t, f, second, "partial")
	third := queueResume(t, f, second, f.editor)
	runner.Execute = nil
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	result := assertJob(t, f, third, "succeeded")
	for _, id := range []string{first, second} {
		assertJob(t, f, id, "succeeded")
	}
	var sum int
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT sum(value) FROM batch_counters`).Scan(&sum))
	if sum != 18 {
		t.Fatal("repeated resume replayed data")
	}
	events, err := f.workflow.ExecutionHistory(f.ctx, f.editor, f.database, false, 50, 0)
	must(t, err)
	if len(events) != 3 {
		t.Fatal("repeated resume lost an attempt")
	}
	down := enqueueRollback(t, f, result.ExecutionID)
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, down, "succeeded")
}

func TestFreshResumeCanReplaceDeactivatedExecutor(t *testing.T) {
	f, _, runner := newWorkerFixture(t)
	leader := f.user(t, "active.replacement")
	must(t, f.adminAuth.SetMembership(f.ctx, f.admin, leader.ID, f.editors.ID, true))
	revision := batchDefinition(t, f, 6, 2)
	review := f.approve(t, f.submit(t, revision))
	original, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "deactivated-executor")
	must(t, err)
	stopAfterBatch(runner, 1)
	_, _ = runner.RunOnce(f.ctx)
	assertJob(t, f, original, "partial")
	generation := f.state(t).Generation
	must(t, f.adminAuth.SetGroupLeader(f.ctx, f.admin, f.editors.ID, leader.ID))
	must(t, f.adminAuth.UpdateUser(f.ctx, f.admin, f.editor, "Former editor", false, ""))
	_, err = f.workflow.ResumeReview(f.ctx, f.editor, original, generation)
	wantError(t, err, access.ErrForbidden)
	fresh, err := f.workflow.ResumeReview(f.ctx, leader.ID, original, generation)
	must(t, err)
	fresh = approveResume(t, f, fresh, leader.ID)
	resumed, err := f.workflow.Enqueue(f.ctx, leader.ID, fresh.ID, "replacement-executor")
	must(t, err)
	runner.Execute = nil
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	result := assertJob(t, f, resumed, "succeeded")
	if result.ActorID != leader.ID {
		t.Fatal("resume reused a deactivated executor")
	}
	var sum int
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT sum(value) FROM batch_counters`).Scan(&sum))
	if sum != 18 {
		t.Fatal("replacement executor did not continue the original checkpoint")
	}
}

func TestFreshResumeFinalizesAlreadyCommittedRows(t *testing.T) {
	f, jobs, runner := newWorkerFixture(t)
	revision := batchDefinition(t, f, 6, 2)
	review := f.approve(t, f.submit(t, revision))
	original, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "paused-before-completion")
	must(t, err)
	stopAfterBatch(runner, 3)
	_, _ = runner.RunOnce(f.ctx)
	assertJob(t, f, original, "partial")
	resumed := queueResume(t, f, original, f.editor)
	runner.Execute = nil
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, resumed, "succeeded")
	var sum, batches int
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT sum(value) FROM batch_counters`).Scan(&sum))
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT count(*) FROM _schakker.batch_receipts WHERE job_id=$1 AND direction='up'`, original).Scan(&batches))
	if sum != 18 || batches != 3 || f.state(t).ActiveVersion != 1 {
		t.Fatal("finalization replayed a committed batch or did not publish its execution")
	}
	// A resumed job's receipt has its new identity; the original scope must
	// still refuse compensation while that completed receipt is reconciled.
	_, err = f.dbs.Website.ExecContext(f.ctx, `UPDATE migration_jobs SET status='uncertain' WHERE id=$1`, resumed)
	must(t, err)
	_, err = f.workflow.SubmitReview(f.ctx, f.editor, migration.ReviewInput{RevisionID: revision.ID, Action: "batch_cancel", TargetJobID: &resumed, ExpectedGeneration: f.state(t).Generation})
	wantError(t, err, migration.ErrWorkflowConflict)
	_, err = jobs.Retry(f.ctx, f.editor, resumed)
	must(t, err)
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, resumed, "succeeded")
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT sum(value) FROM batch_counters`).Scan(&sum))
	if sum != 18 || f.state(t).ActiveVersion != 1 {
		t.Fatal("reconciling the resumed receipt changed committed data or history")
	}
}

func TestResumedPartialBatchCanBeCancelled(t *testing.T) {
	f, _, runner := newWorkerFixture(t)
	revision := batchDefinition(t, f, 6, 2)
	review := f.approve(t, f.submit(t, revision))
	first, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "first-paused")
	must(t, err)
	stopAfterBatch(runner, 1)
	_, _ = runner.RunOnce(f.ctx)
	second := queueResume(t, f, first, f.editor)
	stopAfterBatch(runner, 2)
	_, _ = runner.RunOnce(f.ctx)
	assertJob(t, f, second, "partial")
	cancel, err := f.workflow.SubmitReview(f.ctx, f.editor, migration.ReviewInput{RevisionID: revision.ID, Action: "batch_cancel", TargetJobID: &second, ExpectedGeneration: f.state(t).Generation})
	must(t, err)
	cancel = f.approve(t, cancel)
	id, err := f.workflow.Enqueue(f.ctx, f.editor, cancel.ID, "cancel-resumed")
	must(t, err)
	runner.Execute = nil
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	for _, id := range []string{first, second, id} {
		assertJob(t, f, id, "reverted")
	}
	var sum int
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT sum(value) FROM batch_counters`).Scan(&sum))
	if sum != 0 {
		t.Fatal("compensation skipped earlier jobs' committed batches")
	}
}

func TestResumeReviewAPIEnforcesPermissionsCSRFAndApproval(t *testing.T) {
	f, jobs, runner := newWorkerFixture(t)
	revision := batchDefinition(t, f, 6, 2)
	review := f.approve(t, f.submit(t, revision))
	id, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "paused-api")
	must(t, err)
	stopAfterBatch(runner, 1)
	_, _ = runner.RunOnce(f.ctx)
	handler := http.StripPrefix("/api", api.NewHandler(f.userAccess, f.databases, f.migrations, false, jobs))
	route := "/migration-jobs/" + id + "/resume-reviews"
	body := map[string]any{"expected_generation": f.state(t).Generation}
	identity, cookie := f.login(t, "it.editor")
	csrf, err := f.access.CSRFToken(identity)
	must(t, err)
	if res := workflowRequest(t, handler, "POST", route, "", csrf, body); res.Code != 401 {
		t.Fatalf("session: %d", res.Code)
	}
	if res := workflowRequest(t, handler, "POST", route, cookie, "bad", body); res.Code != 403 {
		t.Fatalf("CSRF: %d", res.Code)
	}
	reader, readerCookie := f.login(t, "review.reader")
	readerCSRF, err := f.access.CSRFToken(reader)
	must(t, err)
	if res := workflowRequest(t, handler, "POST", route, readerCookie, readerCSRF, body); res.Code != 403 {
		t.Fatalf("permission: %d", res.Code)
	}
	res := workflowRequest(t, handler, "POST", route, cookie, csrf, body)
	if res.Code != 201 {
		t.Fatalf("resume review: %d %s", res.Code, res.Body.String())
	}
	dto := decodeAPI[struct {
		Review struct {
			ID     int64  `json:"id"`
			Source string `json:"resumes_job_id"`
		} `json:"review"`
	}](t, res)
	if dto.Review.Source != id {
		t.Fatal("API omitted the checkpoint target")
	}
	_, err = f.workflow.Enqueue(f.ctx, f.editor, dto.Review.ID, "without-new-approval")
	wantError(t, err, migration.ErrWorkflowConflict)
	_, err = f.workflow.ResumeReview(f.ctx, f.reader.ID, id, f.state(t).Generation)
	wantError(t, err, access.ErrForbidden)
}

func TestPartialForwardCanResumeWithFreshApprovals(t *testing.T) {
	f, _, runner := newWorkerFixture(t)
	first := completedBatch(t, f, runner)
	down := enqueueRollback(t, f, first.ExecutionID)
	_, err := runner.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, down, "succeeded")
	forward, err := f.workflow.ForwardReview(f.ctx, f.editor, first.ExecutionID, f.state(t).Generation)
	must(t, err)
	forward = f.approve(t, forward)
	paused, err := f.workflow.Enqueue(f.ctx, f.editor, forward.ID, "partial-forward")
	must(t, err)
	stopAfterBatch(runner, 1)
	_, _ = runner.RunOnce(f.ctx)
	assertJob(t, f, paused, "partial")
	resumed := queueResume(t, f, paused, f.editor)
	runner.Execute = nil
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	result := assertJob(t, f, resumed, "succeeded")
	if result.Operation != "forward" || f.state(t).ActiveVersion != 1 {
		t.Fatal("resume lost forward stack semantics")
	}
	down = enqueueRollback(t, f, result.ExecutionID)
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, down, "succeeded")
}
