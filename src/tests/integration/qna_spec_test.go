package integration_test

import (
	"net/http"
	"strings"
	"testing"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/api"
	"tubes1_wbd/backend/migration"
	admindao "tubes1_wbd_admin/database/dao"
)

func TestM2AttemptHistorySurvivesRetryAndFiltersOnlySuccessfulRollbackPairs(t *testing.T) {
	f, jobs, runner := p3Fixture(t)
	_, err := f.dbs.Managed.ExecContext(f.ctx, `CREATE TABLE qna_history(id bigint PRIMARY KEY);
INSERT INTO qna_history VALUES(1);
CREATE TABLE qna_allow_down(allowed boolean NOT NULL);
INSERT INTO qna_allow_down VALUES(false);
CREATE FUNCTION qna_down() RETURNS integer LANGUAGE plpgsql AS $$BEGIN
 IF NOT (SELECT allowed FROM qna_allow_down) THEN RAISE EXCEPTION 'down refused'; END IF;
 DELETE FROM qna_history WHERE id=1; RETURN 1; END$$`)
	must(t, err)
	input := migration.DefinitionInput{Title: "History retries", UpSQL: "INSERT INTO qna_history VALUES(1);", DownSQL: "SELECT qna_down();"}
	id := enqueueDefinition(t, f, input)
	if _, err = runner.RunOnce(f.ctx); err == nil {
		t.Fatal("duplicate primary key unexpectedly succeeded")
	}
	j := assertJob(t, f, id, "failed")
	events, err := f.workflow.ExecutionHistory(f.ctx, f.reader.ID, f.database, false, 50, 0)
	must(t, err)
	if len(events) != 1 || events[0].Status != "failed" || events[0].SQL != input.UpSQL || events[0].IsCommitted || events[0].AttemptNo != 1 || events[0].ActorID != f.editor || events[0].LogicalVersion != 1 || events[0].FinishedAt == nil || f.state(t).ActiveVersion != 0 {
		t.Fatalf("failed attempt evidence missing: %+v", events)
	}
	failedUp := events[0].ExecutionID
	for _, query := range []string{
		`UPDATE migration_job_attempts SET sql_snapshot='SELECT 9' WHERE job_id=$1`,
		`UPDATE migration_job_attempts SET outcome='succeeded' WHERE job_id=$1`,
		`DELETE FROM migration_job_attempts WHERE job_id=$1`,
	} {
		if _, err = f.dbs.Website.ExecContext(f.ctx, query, id); err == nil {
			t.Fatal("finished attempt evidence was mutable")
		}
	}
	review, err := f.workflow.Review(f.ctx, f.editor, j.ReviewID)
	must(t, err)
	_, err = f.workflow.EditDefinition(f.ctx, f.editor, events[0].DefinitionID, review.RevisionID, reversible("changed_after_failure"))
	wantError(t, err, migration.ErrWorkflowConflict)
	_, err = f.dbs.Managed.ExecContext(f.ctx, `DELETE FROM qna_history`)
	must(t, err)
	_, err = jobs.Retry(f.ctx, f.editor, id)
	must(t, err)
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	j = assertJob(t, f, id, "succeeded")
	events, err = f.workflow.ExecutionHistory(f.ctx, f.reader.ID, f.database, false, 50, 0)
	must(t, err)
	if len(events) != 2 || events[0].ExecutionID != j.ExecutionID || !events[0].IsCommitted || events[0].AttemptNo != 2 || events[1].ExecutionID != failedUp || events[1].Status != "failed" {
		t.Fatal("successful retry overwrote or duplicated the original failed attempt")
	}
	down, err := f.workflow.RollbackReview(f.ctx, f.editor, j.ExecutionID, f.state(t).Generation)
	must(t, err)
	down = f.approve(t, down)
	downID, err := f.workflow.Enqueue(f.ctx, f.editor, down.ID, "qna-down")
	must(t, err)
	if _, err = runner.RunOnce(f.ctx); err == nil {
		t.Fatal("guarded down unexpectedly succeeded")
	}
	assertJob(t, f, downID, "failed")
	events, err = f.workflow.ExecutionHistory(f.ctx, f.reader.ID, f.database, true, 50, 0)
	must(t, err)
	if len(events) != 3 || events[0].Operation != "rollback" || events[0].Status != "failed" || f.state(t).ActiveVersion != 1 {
		t.Fatal("a failed rollback hid history or moved the active position")
	}
	failedDown := events[0].ExecutionID
	_, err = f.dbs.Managed.ExecContext(f.ctx, `UPDATE qna_allow_down SET allowed=true`)
	must(t, err)
	_, err = jobs.Retry(f.ctx, f.editor, downID)
	must(t, err)
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, downID, "succeeded")
	events, err = f.workflow.ExecutionHistory(f.ctx, f.reader.ID, f.database, true, 1, 0)
	must(t, err)
	if len(events) != 1 || events[0].ExecutionID != failedDown || events[0].SQL != input.DownSQL || events[0].Status != "failed" || f.state(t).ActiveVersion != 0 {
		t.Fatal("successful rollback filtering removed failure evidence or ran after pagination")
	}
	older, err := f.workflow.ExecutionHistory(f.ctx, f.reader.ID, f.database, true, 1, 1)
	must(t, err)
	if len(older) != 1 || older[0].ExecutionID != failedUp {
		t.Fatal("failure pagination lost the earlier attempt")
	}
	_, err = f.workflow.ExecutionHistory(f.ctx, f.admin, f.database, false, 50, 0)
	wantError(t, err, access.ErrForbidden)
	permanent := enqueueDefinition(t, f, migration.DefinitionInput{Title: "Permanent failure", UpSQL: "SELECT 1/0;", DownSQL: "SELECT 1;"})
	_, _ = runner.RunOnce(f.ctx)
	assertJob(t, f, permanent, "failed")
	next := enqueueP3(t, f, "qna_after_failure")
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, next, "succeeded")
	if f.state(t).ActiveVersion != 1 {
		t.Fatal("a failed attempt blocked a different migration")
	}
}

func TestM2MissingLeaderCanSubmitPendingReviewAndLeaderMayRepresentMultipleGroups(t *testing.T) {
	f, jobs, _ := p3Fixture(t)
	unconfigured := f.group(t, "Leader pending")
	must(t, f.adminAuth.SetGrant(f.ctx, f.admin, unconfigured.ID, f.database, admindao.ViewOnly))
	revision := f.definition(t, "qna_leader")
	identity, cookie := f.login(t, "it.editor")
	csrf, err := f.access.CSRFToken(identity)
	must(t, err)
	handler := http.StripPrefix("/api", api.NewHandler(f.userAccess, f.databases, f.migrations, false, jobs))
	response := workflowRequest(t, handler, "POST", "/migration-reviews", cookie, csrf, map[string]any{"revision_id": revision.ID, "action": "up", "expected_generation": 0})
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"status":"pending_review"`) {
		t.Fatalf("missing leader rejected submission: %d %s", response.Code, response.Body.String())
	}
	view := decodeAPI[struct {
		Review reviewResource `json:"review"`
	}](t, response).Review
	if view.CanEnqueue || len(view.Groups) != 3 {
		t.Fatal("missing leader review lost a required group or could execute")
	}
	_, err = f.workflow.Enqueue(f.ctx, f.editor, view.ID, "qna-missing-leader")
	wantError(t, err, migration.ErrWorkflowConflict)
	must(t, f.adminAuth.SetMembership(f.ctx, f.admin, f.editor, unconfigured.ID, true))
	must(t, f.adminAuth.SetGroupLeader(f.ctx, f.admin, unconfigured.ID, f.editor))
	// This deployment invalidates reviews on authority changes, including leader
	// assignment, so the editor requests a fresh snapshot and explicit decisions.
	_, err = f.workflow.Decide(f.ctx, f.editor, view.ID, f.editors.ID, "approve", "")
	wantError(t, err, migration.ErrWorkflowConflict)
	fresh := f.submit(t, revision)
	for _, group := range []struct{ actor, id int64 }{{f.editor, f.editors.ID}, {f.reader.ID, f.readers.ID}, {f.editor, unconfigured.ID}} {
		fresh, err = f.workflow.Decide(f.ctx, group.actor, fresh.ID, group.id, "approve", "")
		must(t, err)
	}
	if fresh.State != "approved" {
		t.Fatal("one leader could not represent both assigned groups")
	}
	_, err = f.workflow.Enqueue(f.ctx, f.editor, fresh.ID, "qna-leader-ready")
	must(t, err)
}
