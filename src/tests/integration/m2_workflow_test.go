package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/database/dao"
	admindao "tubes1_wbd_admin/database/dao"
)

// This is a policy-contract test adapter, not an execution worker. The production
// queue belongs to P3; only review reservations/history projection are tested here.
type policyDispatcher struct {
	mu    sync.Mutex
	calls int
}

func (d *policyDispatcher) Enqueue(ctx context.Context, tx *dao.Store, plan migration.ApprovedPlan, key string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	q, err := tx.TransactionQueries()
	if err != nil {
		return "", err
	}
	var id string
	err = q.QueryRowContext(ctx, `INSERT INTO migration_jobs(review_id,database_id,operation,requested_by,idempotency_key) VALUES($1,$2,$3,$4,$5) RETURNING id`, plan.ReviewID, plan.DatabaseID, plan.Operation, plan.ActorID, key).Scan(&id)
	return id, err
}

type recoverySource struct{ source migration.RecoverySource }

func (r *recoverySource) CancellationSource(context.Context, string) (migration.RecoverySource, error) {
	return r.source, nil
}

type workflowFixture struct {
	*fixture
	workflow         *migration.Workflow
	database         int64
	editors, readers admindao.Group
	reader           dao.User
	dispatcher       *policyDispatcher
}

func newWorkflowFixture(t *testing.T) *workflowFixture {
	t.Helper()
	f := newFixture(t, true)
	db := f.register(t)
	editorGroup := f.grant(t, f.editor, db.ID, access.ViewEdit)
	reader := f.user(t, "review.reader")
	readerGroup := f.grant(t, reader.ID, db.ID, access.ViewOnly)
	must(t, f.adminAuth.SetGroupLeader(f.ctx, f.admin, editorGroup.ID, f.editor))
	must(t, f.adminAuth.SetGroupLeader(f.ctx, f.admin, readerGroup.ID, reader.ID))
	dispatcher := &policyDispatcher{}
	f.migrations.ConfigureWorkflow(migration.WorkflowOptions{Dispatcher: dispatcher, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))})
	return &workflowFixture{fixture: f, workflow: f.migrations.Workflow(), database: db.ID, editors: editorGroup, readers: readerGroup, reader: reader, dispatcher: dispatcher}
}
func reversible(title string) migration.DefinitionInput {
	return migration.DefinitionInput{Title: title, UpSQL: "CREATE TABLE " + title + "(id integer);", DownSQL: "DROP TABLE " + title + ";"}
}
func (f *workflowFixture) definition(t *testing.T, title string) dao.MigrationRevision {
	t.Helper()
	r, err := f.workflow.CreateDefinition(f.ctx, f.editor, f.database, reversible(title))
	must(t, err)
	return r
}
func (f *workflowFixture) state(t *testing.T) dao.MigrationState {
	t.Helper()
	s, err := f.workflow.State(f.ctx, f.editor, f.database)
	must(t, err)
	return s
}
func (f *workflowFixture) submit(t *testing.T, r dao.MigrationRevision) dao.MigrationReview {
	t.Helper()
	s := f.state(t)
	v, err := f.workflow.SubmitReview(f.ctx, f.editor, migration.ReviewInput{RevisionID: r.ID, Action: "up", ExpectedGeneration: s.Generation})
	must(t, err)
	return v
}
func (f *workflowFixture) approve(t *testing.T, r dao.MigrationReview) dao.MigrationReview {
	t.Helper()
	r, err := f.workflow.Decide(f.ctx, f.editor, r.ID, f.editors.ID, "approve", "")
	must(t, err)
	r, err = f.workflow.Decide(f.ctx, f.reader.ID, r.ID, f.readers.ID, "approve", "")
	must(t, err)
	if r.State != "approved" {
		t.Fatal("all leaders did not approve")
	}
	return r
}
func (f *workflowFixture) commit(t *testing.T, r dao.MigrationReview, number int) dao.CommittedExecution {
	t.Helper()
	job, err := f.workflow.Enqueue(f.ctx, f.editor, r.ID, fmt.Sprintf("request-%d", number))
	must(t, err)
	plan, err := f.workflow.ValidateApprovedExecution(f.ctx, r.ID, job)
	must(t, err)
	proof := migration.VerifiedCommit{ReviewID: r.ID, JobID: job, ExecutionID: fmt.Sprintf("10000000-0000-4000-8000-%012d", number), ActualSQL: plan.SQL, TargetReceipt: fmt.Sprintf("verified-test-receipt-%d", number), CommittedAt: time.Now().UTC().Truncate(time.Microsecond)}
	execution, err := f.workflow.RecordCommittedExecution(f.ctx, proof)
	must(t, err)
	replay, err := f.workflow.RecordCommittedExecution(f.ctx, proof)
	must(t, err)
	if replay.ExecutionID != execution.ExecutionID {
		t.Fatal("commit replay changed execution")
	}
	proof.ActualSQL += " SELECT 1;"
	_, err = f.workflow.RecordCommittedExecution(f.ctx, proof)
	wantError(t, err, migration.ErrWorkflowConflict)
	return execution
}
func TestM2AllGroupsApproveIncludingViewOnlyAndNoBypass(t *testing.T) {
	f := newWorkflowFixture(t)
	r := f.definition(t, "approval_a")
	review := f.submit(t, r)
	if len(review.AuthoritySnapshot.Groups) != 2 {
		t.Fatal("read-only group was omitted")
	}
	_, err := f.workflow.PrepareApprovedAction(f.ctx, f.editor, review.ID)
	wantError(t, err, migration.ErrWorkflowConflict)
	_, err = f.workflow.Decide(f.ctx, f.reader.ID, review.ID, f.editors.ID, "approve", "")
	wantError(t, err, access.ErrForbidden)
	_, err = f.workflow.CreateDefinition(f.ctx, f.reader.ID, f.database, reversible("reader_write"))
	wantError(t, err, access.ErrForbidden)
	_, err = f.workflow.Decide(f.ctx, f.editor, review.ID, f.editors.ID, "approve", "")
	must(t, err)
	_, err = f.workflow.Enqueue(f.ctx, f.editor, review.ID, "too-early")
	wantError(t, err, migration.ErrWorkflowConflict)
	_, err = f.workflow.Decide(f.ctx, f.reader.ID, review.ID, f.readers.ID, "approve", "")
	must(t, err)
	plan, err := f.workflow.PrepareApprovedAction(f.ctx, f.editor, review.ID)
	must(t, err)
	if plan.SQL != r.UpSQL || plan.RevisionHash != r.SQLHash || plan.AuthorityRevision < 1 {
		t.Fatal("approved content binding lost")
	}
	standalone := migration.NewWorkflow(f.runtimeStore, f.userAccess, migration.WorkflowOptions{})
	_, err = standalone.Enqueue(f.ctx, f.editor, review.ID, "no-worker")
	wantError(t, err, migration.ErrWorkflowUnavailable)
	// Concurrent identical submissions must reserve exactly one job.
	var wg sync.WaitGroup
	results := make(chan string, 4)
	failures := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			job, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "same-request")
			results <- job
			failures <- err
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		must(t, err)
	}
	var job string
	for result := range results {
		if job != "" && result != job {
			t.Fatal("duplicate jobs")
		}
		job = result
	}
	if f.dispatcher.calls != 1 {
		t.Fatalf("enqueue called %d times", f.dispatcher.calls)
	}
	_, err = f.workflow.ValidateApprovedExecution(f.ctx, review.ID, "00000000-0000-4000-8000-999999999999")
	wantError(t, err, migration.ErrWorkflowConflict)
	_, err = f.workflow.EditDefinition(f.ctx, f.editor, r.DefinitionID, r.ID, reversible("changed"))
	wantError(t, err, migration.ErrWorkflowConflict)
}
func TestM2AuthorityChangesInvalidateApprovalsAndMissingLeadersBlock(t *testing.T) {
	f := newWorkflowFixture(t)
	r := f.definition(t, "authority_a")
	review := f.approve(t, f.submit(t, r))
	outsider := f.user(t, "leader.other")
	must(t, f.adminAuth.SetMembership(f.ctx, f.admin, outsider.ID, f.readers.ID, true))
	must(t, f.adminAuth.SetGroupLeader(f.ctx, f.admin, f.readers.ID, outsider.ID))
	_, err := f.workflow.PrepareApprovedAction(f.ctx, f.editor, review.ID)
	wantError(t, err, migration.ErrWorkflowConflict)
	current, err := f.workflow.Review(f.ctx, f.editor, review.ID)
	must(t, err)
	if current.State != "invalidated" {
		t.Fatal("invalidation rolled back")
	}
	must(t, f.adminAuth.SetGroupLeader(f.ctx, f.admin, f.readers.ID, f.reader.ID))
	next, err := f.workflow.SubmitReview(f.ctx, f.editor, migration.ReviewInput{RevisionID: r.ID, Action: "up"})
	must(t, err)
	f.approve(t, next)
	must(t, f.adminAuth.SetGrant(f.ctx, f.admin, f.readers.ID, f.database, admindao.ViewEdit))
	_, err = f.workflow.Enqueue(f.ctx, f.editor, next.ID, "stale-grant")
	wantError(t, err, migration.ErrWorkflowConflict)
	unconfigured := f.group(t, "Unconfigured group")
	must(t, f.adminAuth.SetGrant(f.ctx, f.admin, unconfigured.ID, f.database, admindao.ViewOnly))
	_, err = f.workflow.SubmitReview(f.ctx, f.editor, migration.ReviewInput{RevisionID: r.ID, Action: "up"})
	wantError(t, err, migration.ErrWorkflowConflict)
}
func TestM2LeaderMembershipAndActivationInvariants(t *testing.T) {
	f := newWorkflowFixture(t)
	outsider := f.user(t, "leader.outsider")
	if err := f.adminAuth.SetGroupLeader(f.ctx, f.admin, f.editors.ID, outsider.ID); err == nil {
		t.Fatal("nonmember leader accepted")
	}
	if err := f.adminAuth.SetMembership(f.ctx, f.admin, f.editor, f.editors.ID, false); err == nil {
		t.Fatal("leader membership removed")
	}
	if err := f.adminAuth.UpdateUser(f.ctx, f.admin, f.editor, "Editor", false, ""); err == nil {
		t.Fatal("active leader deactivated")
	}
	must(t, f.adminAuth.SetMembership(f.ctx, f.admin, outsider.ID, f.editors.ID, true))
	must(t, f.adminAuth.SetGroupLeader(f.ctx, f.admin, f.editors.ID, outsider.ID))
	must(t, f.adminAuth.SetMembership(f.ctx, f.admin, f.editor, f.editors.ID, false))
	must(t, f.adminAuth.UpdateUser(f.ctx, f.admin, f.editor, "Editor", false, ""))
	must(t, f.adminAuth.DeleteGroup(f.ctx, f.admin, f.editors.ID))
}
func TestM2RevisionsAreImmutableAndRejectionCanBeResubmitted(t *testing.T) {
	f := newWorkflowFixture(t)
	r := f.definition(t, "immutable_a")
	review := f.submit(t, r)
	_, err := f.workflow.Decide(f.ctx, f.reader.ID, review.ID, f.readers.ID, "reject", "Missing safety explanation")
	must(t, err)
	current, err := f.workflow.Review(f.ctx, f.editor, review.ID)
	must(t, err)
	if current.State != "rejected" || current.Decisions[0].Note == "" {
		t.Fatal("rejection evidence missing")
	}
	replacement, err := f.workflow.EditDefinition(f.ctx, f.editor, r.DefinitionID, r.ID, reversible("immutable_b"))
	must(t, err)
	_, err = f.workflow.EditDefinition(f.ctx, f.editor, r.DefinitionID, r.ID, reversible("stale_edit"))
	wantError(t, err, migration.ErrWorkflowConflict)
	if _, err = f.dbs.Website.ExecContext(f.ctx, `UPDATE migration_revisions SET up_sql='SELECT 1' WHERE id=$1`, r.ID); err == nil {
		t.Fatal("frozen revision changed directly")
	}
	approved := f.approve(t, f.submit(t, replacement))
	f.commit(t, approved, 1)
	_, err = f.workflow.EditDefinition(f.ctx, f.editor, replacement.DefinitionID, replacement.ID, reversible("executed_edit"))
	wantError(t, err, migration.ErrWorkflowConflict)
	if _, err = f.dbs.Website.ExecContext(f.ctx, `DELETE FROM migration_execution_history`); err == nil {
		t.Fatal("committed history deleted")
	}
	if _, err = f.dbs.Website.ExecContext(f.ctx, `UPDATE migration_execution_history SET sql_snapshot='SELECT 9'`); err == nil {
		t.Fatal("committed history rewritten")
	}
}
func TestM2RollbackForwardLinearStateAndExactHistoryPair(t *testing.T) {
	f := newWorkflowFixture(t)
	a := f.commit(t, f.approve(t, f.submit(t, f.definition(t, "chain_a"))), 1)
	b := f.commit(t, f.approve(t, f.submit(t, f.definition(t, "chain_b"))), 2)
	rollback, err := f.workflow.RollbackReview(f.ctx, f.editor, b.ExecutionID, f.state(t).Generation)
	must(t, err)
	reversed := f.commit(t, f.approve(t, rollback), 3)
	if reversed.SQL != "DROP TABLE chain_b;" || reversed.ReversesExecutionID == nil || *reversed.ReversesExecutionID != b.ExecutionID {
		t.Fatal("rollback did not bind original down/execution")
	}
	version, err := f.runtimeStore.MigrationVersion(f.ctx, f.database)
	must(t, err)
	if version != 1 {
		t.Fatalf("active version is %d after rollback", version)
	}
	forward, err := f.workflow.ForwardReview(f.ctx, f.editor, b.ExecutionID, f.state(t).Generation)
	must(t, err)
	reapplied := f.commit(t, f.approve(t, forward), 4)
	if reapplied.SQL != b.SQL {
		t.Fatal("forward changed original SQL up")
	}
	c := f.commit(t, f.approve(t, f.submit(t, f.definition(t, "chain_c"))), 5)
	_, err = f.workflow.RollbackReview(f.ctx, f.editor, reapplied.ExecutionID, f.state(t).Generation)
	wantError(t, err, migration.ErrWorkflowConflict)
	history, err := f.workflow.History(f.ctx, f.reader.ID, f.database, true, 50, 0)
	must(t, err)
	if len(history) != 3 || history[0].ExecutionID != c.ExecutionID || history[1].ExecutionID != reapplied.ExecutionID || history[2].ExecutionID != a.ExecutionID {
		t.Fatalf("wrong E1-E5 filter: %#v", history)
	}
	all, err := f.workflow.History(f.ctx, f.reader.ID, f.database, false, 50, 0)
	must(t, err)
	if len(all) != 5 {
		t.Fatal("history filter destroyed records")
	}
	// Roll back C then B. Forwarding C first must fail, then B alone must succeed.
	r, err := f.workflow.RollbackReview(f.ctx, f.editor, c.ExecutionID, f.state(t).Generation)
	must(t, err)
	f.commit(t, f.approve(t, r), 6)
	r, err = f.workflow.RollbackReview(f.ctx, f.editor, reapplied.ExecutionID, f.state(t).Generation)
	must(t, err)
	f.commit(t, f.approve(t, r), 7)
	_, err = f.workflow.ForwardReview(f.ctx, f.editor, c.ExecutionID, f.state(t).Generation)
	wantError(t, err, migration.ErrWorkflowConflict)
	r, err = f.workflow.ForwardReview(f.ctx, f.editor, reapplied.ExecutionID, f.state(t).Generation)
	must(t, err)
	f.commit(t, f.approve(t, r), 8)
	if f.state(t).ActiveVersion != 2 || len(f.state(t).ForwardStack) != 1 {
		t.Fatal("forward automatically ran next migration")
	}
	// New branch clears only forward opportunities, preserving old committed facts.
	f.commit(t, f.approve(t, f.submit(t, f.definition(t, "chain_d"))), 9)
	_, err = f.workflow.ForwardReview(f.ctx, f.editor, c.ExecutionID, f.state(t).Generation)
	wantError(t, err, migration.ErrWorkflowConflict)
}
func TestM2IrreversibleAndFailedAttemptsDoNotMoveHead(t *testing.T) {
	f := newWorkflowFixture(t)
	invalid := reversible("unsafe_a")
	invalid.DownSQL = ""
	_, err := f.workflow.CreateDefinition(f.ctx, f.editor, f.database, invalid)
	wantError(t, err, migration.ErrDefinitionInvalid)
	invalid.Irreversible = true
	invalid.IrreversibleReason = "Data cannot be reconstructed safely"
	r, err := f.workflow.CreateDefinition(f.ctx, f.editor, f.database, invalid)
	must(t, err)
	e := f.commit(t, f.approve(t, f.submit(t, r)), 1)
	_, err = f.workflow.RollbackReview(f.ctx, f.editor, e.ExecutionID, f.state(t).Generation)
	wantError(t, err, migration.ErrWorkflowConflict)
	r = f.definition(t, "failed_a")
	review := f.approve(t, f.submit(t, r))
	job, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "failed-attempt")
	must(t, err)
	_, err = f.workflow.ValidateApprovedExecution(f.ctx, review.ID, job)
	must(t, err)
	// A worker failure has no verified receipt and never invokes the commit hook.
	if f.state(t).ActiveVersion != 1 {
		t.Fatal("queue/run advanced the active position")
	}
	history, err := f.workflow.History(f.ctx, f.editor, f.database, false, 50, 0)
	must(t, err)
	if len(history) != 1 {
		t.Fatal("failed attempt entered committed history")
	}
}
func TestM2BatchCancelRequiresFreshAllLeaderApprovalAndSafeSource(t *testing.T) {
	f := newWorkflowFixture(t)
	revision := f.definition(t, "cancel_a")
	original := f.approve(t, f.submit(t, revision))
	job, err := f.workflow.Enqueue(f.ctx, f.editor, original.ID, "original-partial-job")
	must(t, err)
	_, err = f.dbs.Website.ExecContext(f.ctx, `UPDATE migration_jobs SET status='partial' WHERE id=$1`, job)
	must(t, err)
	source := &recoverySource{migration.RecoverySource{JobID: job, DatabaseID: f.database, RevisionID: revision.ID, Fingerprint: "checkpoint-and-undo-scope", Paused: true, PartiallyCommitted: true, SafeCompensation: false}}
	workflow := migration.NewWorkflow(f.runtimeStore, f.userAccess, migration.WorkflowOptions{Dispatcher: f.dispatcher, Recovery: source})
	input := migration.ReviewInput{RevisionID: revision.ID, Action: "batch_cancel", TargetJobID: &source.source.JobID}
	_, err = workflow.SubmitReview(f.ctx, f.editor, input)
	wantError(t, err, migration.ErrWorkflowConflict)
	source.source.SafeCompensation = true
	review, err := workflow.SubmitReview(f.ctx, f.editor, input)
	must(t, err)
	_, err = workflow.PrepareApprovedAction(f.ctx, f.editor, review.ID)
	wantError(t, err, migration.ErrWorkflowConflict)
	_, err = workflow.Decide(f.ctx, f.editor, review.ID, f.editors.ID, "approve", "")
	must(t, err)
	_, err = workflow.Decide(f.ctx, f.reader.ID, review.ID, f.readers.ID, "approve", "")
	must(t, err)
	plan, err := workflow.PrepareApprovedAction(f.ctx, f.editor, review.ID)
	must(t, err)
	if plan.SQL != revision.DownSQL || plan.TargetJobID == nil {
		t.Fatal("cancel lost immutable down/scope")
	}
	source.source.Fingerprint = "changed-checkpoint"
	_, err = workflow.Enqueue(f.ctx, f.editor, review.ID, "cancel-changed")
	wantError(t, err, migration.ErrWorkflowConflict)
}

func workflowRequest(t *testing.T, handler http.Handler, method, route, cookie, csrf string, in any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(in)
	must(t, err)
	req := httptest.NewRequest(method, "/api"+route, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: access.CookieName, Value: cookie})
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}
func TestM2WorkflowHTTPEnforcesSessionCSRFAndTypedJSON(t *testing.T) {
	f := newWorkflowFixture(t)
	identity, cookie := f.login(t, "it.editor")
	csrf, err := f.access.CSRFToken(identity)
	must(t, err)
	handler := userHandler(t, f.fixture)
	payload := map[string]any{"database_id": f.database, "title": "http_a", "up_sql": "CREATE TABLE http_a(id integer)", "down_sql": "DROP TABLE http_a", "irreversible": false}
	if res := workflowRequest(t, handler, "POST", "/migration-definitions", "", csrf, payload); res.Code != 401 {
		t.Fatalf("session bypass %d", res.Code)
	}
	if res := workflowRequest(t, handler, "POST", "/migration-definitions", cookie, "bad", payload); res.Code != 403 {
		t.Fatalf("CSRF bypass %d", res.Code)
	}
	created := workflowRequest(t, handler, "POST", "/migration-definitions", cookie, csrf, payload)
	if created.Code != 201 {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	revision := decodeAPI[dao.MigrationRevision](t, created)
	submitted := workflowRequest(t, handler, "POST", "/migration-reviews", cookie, csrf, map[string]any{"revision_id": revision.ID, "action": "up", "expected_generation": 0})
	if submitted.Code != 201 {
		t.Fatalf("submit: %d %s", submitted.Code, submitted.Body.String())
	}
	review := decodeAPI[dao.MigrationReview](t, submitted)
	res := workflowRequest(t, handler, "POST", fmt.Sprintf("/migration-reviews/%d/enqueue", review.ID), cookie, csrf, map[string]string{"idempotency_key": "unapproved"})
	if res.Code != 409 {
		t.Fatalf("approval bypass: %d %s", res.Code, res.Body.String())
	}
	forbidden := workflowRequest(t, handler, "POST", "/migration-definitions", cookie, csrf, map[string]any{"database_id": f.database, "title": "bad", "up_sql": "SELECT 1", "down_sql": ""})
	if forbidden.Code != 422 {
		t.Fatalf("invalid reversible content: %d", forbidden.Code)
	}
	history := workflowRequest(t, handler, "GET", fmt.Sprintf("/migrations/history?database_id=%d&hide_rollback=true", f.database), cookie, csrf, nil)
	if history.Code != 200 || strings.TrimSpace(history.Body.String()) != "[]" {
		t.Fatalf("history: %d %s", history.Code, history.Body.String())
	}
	decision := workflowRequest(t, handler, "POST", fmt.Sprintf("/migration-reviews/%d/decisions", review.ID), cookie, csrf, map[string]any{"group_id": f.editors.ID, "decision": "approve", "note": ""})
	if decision.Code != 200 {
		t.Fatalf("editor leader decision: %d %s", decision.Code, decision.Body.String())
	}
	readerIdentity, readerCookie := f.login(t, f.reader.Username)
	readerCSRF, err := f.access.CSRFToken(readerIdentity)
	must(t, err)
	decision = workflowRequest(t, handler, "POST", fmt.Sprintf("/migration-reviews/%d/decisions", review.ID), readerCookie, readerCSRF, map[string]any{"group_id": f.readers.ID, "decision": "approve", "note": ""})
	if decision.Code != 200 {
		t.Fatalf("read-only leader decision: %d %s", decision.Code, decision.Body.String())
	}
	queued := workflowRequest(t, handler, "POST", fmt.Sprintf("/migration-reviews/%d/enqueue", review.ID), cookie, csrf, map[string]string{"idempotency_key": "http-approved-request"})
	if queued.Code != 202 {
		t.Fatalf("enqueue: %d %s", queued.Code, queued.Body.String())
	}
	var pending int
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT count(*) FROM migration_jobs WHERE review_id=$1 AND status='pending'`, review.ID).Scan(&pending))
	if pending != 1 {
		t.Fatal("202 was not durably queued")
	}
	var executed bool
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT to_regclass('public.http_a') IS NOT NULL`).Scan(&executed))
	if executed {
		t.Fatal("HTTP enqueue synchronously executed target SQL")
	}
}

func TestM2AuthorityOutageAndChangesAtRunFailClosed(t *testing.T) {
	f := newWorkflowFixture(t)
	r := f.definition(t, "run_authority")
	review := f.approve(t, f.submit(t, r))
	job, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "run-authority")
	must(t, err)
	outsider := f.user(t, "run.new.member")
	must(t, f.adminAuth.SetMembership(f.ctx, f.admin, outsider.ID, f.readers.ID, true))
	_, err = f.workflow.ValidateApprovedExecution(f.ctx, review.ID, job)
	wantError(t, err, migration.ErrWorkflowConflict)
	current, err := f.workflow.Review(f.ctx, f.editor, review.ID)
	must(t, err)
	if current.State != "invalidated" {
		t.Fatal("worker received stale approval")
	}
	r = f.definition(t, "outage_authority")
	next := f.approve(t, f.submit(t, r))
	f.private.Close()
	_, err = f.workflow.PrepareApprovedAction(f.ctx, f.editor, next.ID)
	if err == nil {
		t.Fatal("authority outage reused approvals")
	}
}
func TestM2OneLeaderMustDecideForEachGroupAndCommitAfterAuthorityChange(t *testing.T) {
	f := newWorkflowFixture(t)
	must(t, f.adminAuth.SetMembership(f.ctx, f.admin, f.editor, f.readers.ID, true))
	must(t, f.adminAuth.SetGroupLeader(f.ctx, f.admin, f.readers.ID, f.editor))
	r := f.definition(t, "same_leader")
	review := f.submit(t, r)
	_, err := f.workflow.Decide(f.ctx, f.editor, review.ID, f.editors.ID, "approve", "")
	must(t, err)
	_, err = f.workflow.PrepareApprovedAction(f.ctx, f.editor, review.ID)
	wantError(t, err, migration.ErrWorkflowConflict)
	_, err = f.workflow.Decide(f.ctx, f.editor, review.ID, f.readers.ID, "approve", "")
	must(t, err)
	job, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "confirmed-then-authority")
	must(t, err)
	plan, err := f.workflow.ValidateApprovedExecution(f.ctx, review.ID, job)
	must(t, err)
	// A receipt proves a commit that predates the next authority change; the fact
	// must remain recordable, without requiring the remote authority to be online.
	f.private.Close()
	proof := migration.VerifiedCommit{ReviewID: review.ID, JobID: job, ExecutionID: "30000000-0000-4000-8000-000000000001", ActualSQL: plan.SQL, TargetReceipt: "verified-before-outage", CommittedAt: time.Now()}
	_, err = f.workflow.RecordCommittedExecution(f.ctx, proof)
	must(t, err)
	_, err = f.workflow.RecordCommittedExecution(f.ctx, proof)
	must(t, err)
	state, err := f.runtimeStore.MigrationState(f.ctx, f.database, false)
	must(t, err)
	if state.ActiveVersion != 1 {
		t.Fatal("verified fact lost after authority outage")
	}
}

func TestM2EditInvalidatesUnqueuedApprovedReview(t *testing.T) {
	f := newWorkflowFixture(t)
	revision := f.definition(t, "edit_review")
	review := f.approve(t, f.submit(t, revision))
	replacement, err := f.workflow.EditDefinition(f.ctx, f.editor, revision.DefinitionID, revision.ID, reversible("edited_review"))
	must(t, err)
	original, err := f.workflow.Review(f.ctx, f.editor, review.ID)
	must(t, err)
	if original.State != "invalidated" {
		t.Fatal("editing reused old approval")
	}
	if replacement.ID == revision.ID || replacement.SQLHash == revision.SQLHash {
		t.Fatal("editing rewrote the submitted revision")
	}
	_, err = f.workflow.Enqueue(f.ctx, f.editor, review.ID, "edited-content")
	wantError(t, err, migration.ErrWorkflowConflict)
	f.approve(t, f.submit(t, replacement))
}
func TestM2ActivitiesKeepFailuresOutsideCommittedHistory(t *testing.T) {
	f := newWorkflowFixture(t)
	review := f.approve(t, f.submit(t, f.definition(t, "activity_a")))
	job, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "failed-job")
	must(t, err)
	_, err = f.dbs.Website.ExecContext(f.ctx, `UPDATE migration_jobs SET status='failed' WHERE id=$1;
 `, job)
	must(t, err)
	_, err = f.dbs.Website.ExecContext(f.ctx, `INSERT INTO migration_job_attempts(job_id,attempt_no,outcome,error_class,error_message,finished_at) VALUES($1,1,'failed','syntax','sensitive-row-value-do-not-expose',now())`, job)
	must(t, err)
	activities, err := f.workflow.Activities(f.ctx, f.reader.ID, f.database, 50, 0)
	must(t, err)
	if len(activities) != 1 || activities[0].Status != "failed" || len(activities[0].Attempts) != 1 {
		t.Fatal("failed operational attempt missing")
	}
	raw, err := json.Marshal(activities)
	must(t, err)
	if strings.Contains(string(raw), "sensitive-row-value") {
		t.Fatal("raw SQL/row error leaked")
	}
	history, err := f.workflow.History(f.ctx, f.reader.ID, f.database, true, 50, 0)
	must(t, err)
	if len(history) != 0 {
		t.Fatal("failed job entered committed history")
	}
}

type policyObserver struct{ events []string }

func (o *policyObserver) Start(ctx context.Context, event string, actor, database int64) (context.Context, func(string)) {
	o.events = append(o.events, event)
	return ctx, func(string) {}
}
func TestM2PolicyAuditIsJSONAndSupportsTracingWithoutSecrets(t *testing.T) {
	f := newWorkflowFixture(t)
	var logs bytes.Buffer
	observer := &policyObserver{}
	workflow := migration.NewWorkflow(f.runtimeStore, f.userAccess, migration.WorkflowOptions{Logger: slog.New(slog.NewJSONHandler(&logs, nil)), Observer: observer})
	_, err := workflow.CreateDefinition(f.ctx, f.editor, f.database, reversible("audit_secret_sql"))
	must(t, err)
	if len(observer.events) != 1 || observer.events[0] != "migration.definition.create" {
		t.Fatal("trace hook was not called")
	}
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var event map[string]any
		must(t, json.Unmarshal([]byte(line), &event))
		if event["actor_id"] == nil || event["status"] == nil {
			t.Fatal("missing audit fields")
		}
	}
	if strings.Contains(logs.String(), "audit_secret_sql") || strings.Contains(logs.String(), password) {
		t.Fatal("SQL or credentials leaked into policy audit")
	}
}

func TestM2WorkerHandoffCanReuseTheLockedMetadataConnection(t *testing.T) {
	f := newWorkflowFixture(t)
	review := f.approve(t, f.submit(t, f.definition(t, "locked_handoff")))
	job, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "locked-handoff")
	must(t, err)
	must(t, f.runtimeStore.WithDatabaseLock(f.ctx, f.database, func(locked *dao.Store) error {
		policy := f.workflow.UsingStore(locked)
		plan, err := policy.ValidateApprovedExecution(f.ctx, review.ID, job)
		if err != nil {
			return err
		}
		_, err = policy.RecordCommittedExecution(f.ctx, migration.VerifiedCommit{ReviewID: review.ID, ExecutionID: "40000000-0000-4000-8000-000000000001", JobID: job, ActualSQL: plan.SQL, TargetReceipt: "verified-locked-handoff", CommittedAt: time.Now()})
		return err
	}))
}

func TestM2PartialJobBlocksOtherMigrationActions(t *testing.T) {
	f := newWorkflowFixture(t)
	review := f.approve(t, f.submit(t, f.definition(t, "partial_original")))
	job, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "partial-original")
	must(t, err)
	_, err = f.dbs.Website.ExecContext(f.ctx, `UPDATE migration_jobs SET status='partial' WHERE id=$1`, job)
	must(t, err)
	next := f.definition(t, "blocked_next")
	_, err = f.workflow.SubmitReview(f.ctx, f.editor, migration.ReviewInput{RevisionID: next.ID, Action: "up"})
	wantError(t, err, migration.ErrWorkflowConflict)
	// A known job can be revalidated for safe checkpoint-based recovery by P3;
	// it does not get permission to ignore an explicit unresolved DB blocker.
	_, err = f.workflow.ValidateApprovedExecution(f.ctx, review.ID, job)
	must(t, err)
	_, err = f.dbs.Website.ExecContext(f.ctx, `UPDATE migration_database_state SET blocked_reason='unknown target receipt' WHERE database_id=$1`, f.database)
	must(t, err)
	_, err = f.workflow.ValidateApprovedExecution(f.ctx, review.ID, job)
	wantError(t, err, migration.ErrWorkflowConflict)
	stored, err := f.workflow.Review(f.ctx, f.editor, review.ID)
	must(t, err)
	if stored.State != "approved" {
		t.Fatal("temporary blocker rewrote the immutable approval decision")
	}
}

func TestM2ReviewedWorkflowCannotResetSynchronousVersion(t *testing.T) {
	f := newWorkflowFixture(t)
	analysis, err := f.migrations.Analyze(f.ctx, f.editor, f.database, "CREATE TABLE synchronous_first(id integer)")
	must(t, err)
	execution, err := f.migrations.Execute(f.ctx, f.editor, f.database, migration.ExecuteInput{Review: analysis})
	must(t, err)
	if execution.AppliedVersion == nil || *execution.AppliedVersion != 1 {
		t.Fatal("synchronous execution did not establish its version")
	}
	_, err = f.workflow.CreateDefinition(f.ctx, f.editor, f.database, reversible("reviewed_next"))
	wantError(t, err, migration.ErrWorkflowConflict)
	version, err := f.runtimeStore.MigrationVersion(f.ctx, f.database)
	must(t, err)
	if version != 1 {
		t.Fatal("attempting to start a reviewed workflow reset migration history")
	}
}
