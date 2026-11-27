package unit_test

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"testing"
	"time"

	"schakker_shared/authority"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/backend/migration/engines"
	"tubes1_wbd/database/dao"
)

func resourceJSON(t *testing.T, query string, value any) sqlStep {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return scalarStep(query, string(raw))
}
func resourceActor() sqlStep {
	return queryStep("FROM users WHERE id", []string{"id", "username", "display_name", "is_active"}, []driver.Value{int64(7), "editor", "Editor", true})
}
func resourceView(t *testing.T, permission dao.Permission, blocked bool, state dao.MigrationState) (*migration.Resources, *sqlStub) {
	t.Helper()
	_, store, stub := newSQLStub(t, permissionStep(permission), resourceJSON(t, "migration_database_state", state), scalarStep("SELECT EXISTS(SELECT 1 FROM migration_jobs", blocked))
	_, _, service := newServices(t, store)
	v, err := service.Workflow().Resources(context.Background(), 7, 9)
	if err != nil {
		t.Fatal(err)
	}
	return v, stub
}
func resourceRevision() dao.MigrationRevision {
	return dao.MigrationRevision{ID: 101, DefinitionID: 11, RevisionNo: 2, DatabaseID: 9, Title: "Add note", CreatedBy: 7, UpSQL: "ALTER TABLE employees ADD note text", DownSQL: "ALTER TABLE employees DROP note", SQLHash: "hash"}
}
func TestDefinitionResourceCapabilitiesReflectPermissionAndImmutableLifecycle(t *testing.T) {
	now := time.Now()
	version := int64(3)
	for _, tc := range []struct {
		name       string
		permission dao.Permission
		frozen     *time.Time
		version    *int64
		status     string
		editable   bool
	}{{"draft", dao.ViewEdit, nil, nil, "draft", true}, {"reader", dao.ViewOnly, nil, nil, "draft", false}, {"submitted", dao.ViewEdit, &now, nil, "submitted", false}, {"executed", dao.ViewEdit, &now, &version, "executed", false}} {
		t.Run(tc.name, func(t *testing.T) {
			v, stub := resourceView(t, tc.permission, false, dao.MigrationState{Generation: 5})
			stub.append(resourceActor())
			r := resourceRevision()
			r.FrozenAt = tc.frozen
			r.LogicalVersion = tc.version
			body := v.Definition(r)
			if body["status"] != tc.status || body["editable"] != tc.editable || body["expected_generation"] != int64(5) || body["revision_id"] != int64(101) || body["created_by"] != "editor" {
				t.Fatalf("wrong definition capabilities: %#v", body)
			}
		})
	}
}
func TestReviewResourceRequiresAllValidLeadersAndPreservesReadOnlyLeaderDecision(t *testing.T) {
	for _, tc := range []struct {
		name                                              string
		permission                                        dao.Permission
		state                                             string
		blocked, invalid, missingDecision, queued, resume bool
		action                                            string
		enqueue, resubmit, decide                         bool
	}{
		{name: "approved", permission: dao.ViewEdit, state: "approved", enqueue: true},
		{name: "reader leader", permission: dao.ViewOnly, state: "pending_review", missingDecision: true, decide: true},
		{name: "invalid leader", permission: dao.ViewEdit, state: "approved", invalid: true},
		{name: "missing decision", permission: dao.ViewEdit, state: "approved", missingDecision: true},
		{name: "blocked", permission: dao.ViewEdit, state: "approved", blocked: true},
		{name: "resume", permission: dao.ViewEdit, state: "approved", blocked: true, resume: true, enqueue: true},
		{name: "cancellation", permission: dao.ViewEdit, state: "approved", blocked: true, action: "batch_cancel", enqueue: true},
		{name: "already queued", permission: dao.ViewEdit, state: "approved", queued: true},
		{name: "rejected", permission: dao.ViewEdit, state: "rejected", resubmit: true},
		{name: "invalidated reader", permission: dao.ViewOnly, state: "invalidated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, stub := resourceView(t, tc.permission, tc.blocked, dao.MigrationState{Generation: 5})
			r := resourceRevision()
			stub.append(resourceJSON(t, "migration_revisions", r), resourceActor())
			review := dao.MigrationReview{ID: 22, RevisionID: r.ID, DatabaseID: 9, Action: "up", State: tc.state, SubmittedBy: 7, AuthorityRevision: 9007199254740993, AuthoritySnapshot: authority.Snapshot{Groups: []authority.Group{{ID: 2, Name: "Auditors", LeaderID: 7, LeaderName: "Editor", Permission: "view_only", Valid: !tc.invalid}}}}
			if tc.resume {
				source := "paused-job"
				review.TargetJobID = &source
			}
			if tc.action != "" {
				review.Action = tc.action
			}
			if !tc.missingDecision {
				review.Decisions = []dao.MigrationDecision{{GroupID: 2, ApproverID: 7, Decision: "approve", Note: "Reviewed"}}
			}
			if tc.queued {
				job := "queued-job"
				review.QueuedJobID = &job
			}
			body, err := v.Review(review)
			if err != nil {
				t.Fatal(err)
			}
			groups := body["required_groups"].([]map[string]any)
			if body["can_enqueue"] != tc.enqueue || body["can_resubmit"] != tc.resubmit || groups[0]["can_decide"] != tc.decide || body["authority_revision"] != "9007199254740993" {
				t.Fatalf("wrong review capabilities: %#v", body)
			}
			if tc.action == "batch_cancel" && body["action"] != "cancel_batch" {
				t.Fatal("cancellation action not translated")
			}
		})
	}
}
func TestJobResourceRetryAndCompensationCapabilitiesUseOwnershipProgressAndPermission(t *testing.T) {
	for _, tc := range []struct {
		name, status, operation             string
		permission                          dao.Permission
		actor                               int64
		attempt                             int
		reserved, allReverted, irreversible bool
		retry, cancel                       bool
	}{
		{name: "partial", status: "partial", permission: dao.ViewEdit, actor: 7, retry: true, cancel: true},
		{name: "reader", status: "partial", permission: dao.ViewOnly, actor: 7},
		{name: "other actor", status: "partial", permission: dao.ViewEdit, actor: 8, cancel: true},
		{name: "uncertain other actor", status: "uncertain", permission: dao.ViewEdit, actor: 8, retry: true, cancel: true},
		{name: "exhausted", status: "partial", permission: dao.ViewEdit, actor: 7, attempt: 3, cancel: true},
		{name: "reserved", status: "partial", permission: dao.ViewEdit, actor: 7, reserved: true},
		{name: "fully compensated", status: "partial", permission: dao.ViewEdit, actor: 7, allReverted: true, retry: true},
		{name: "irreversible", status: "partial", permission: dao.ViewEdit, actor: 7, irreversible: true, retry: true},
		{name: "ordinary failure", status: "failed", permission: dao.ViewEdit, actor: 7, retry: true},
		{name: "succeeded", status: "succeeded", permission: dao.ViewEdit, actor: 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, stub := resourceView(t, tc.permission, false, dao.MigrationState{Generation: 5})
			r := resourceRevision()
			r.UpSQL, r.DownSQL, _ = engines.BatchSQL(engines.BatchSpec{Schema: "public", Table: "counters", Key: "id", Column: "value", Delta: 1, Size: 50})
			r.Irreversible = tc.irreversible
			stub.append(resourceJSON(t, "migration_reviews", dao.MigrationReview{RevisionID: r.ID}), resourceJSON(t, "migration_revisions", r), queryStep("SELECT min(a.started_at)", []string{"started", "completed"}, []driver.Value{nil, nil}), resourceActor())
			j := dao.MigrationJob{ID: "job", ReviewID: 22, ActorID: tc.actor, Operation: "up", Status: tc.status, Attempt: max(1, tc.attempt), MaxAttempts: 3}
			if tc.reserved {
				reserved := "cancellation-job"
				j.RecoveryJobID = &reserved
			}
			progress := dao.BatchProgress{Batches: 2, Rows: 100, Cursor: "100"}
			if tc.allReverted {
				progress.RevertedBatches = 2
			}
			body, err := v.Job(j, progress)
			if err != nil {
				t.Fatal(err)
			}
			resume := tc.permission == dao.ViewEdit && tc.status == "partial" && !tc.reserved && !tc.allReverted && !tc.irreversible
			if body["can_resume"] != resume {
				t.Fatalf("wrong fresh approval resume capability: %#v", body)
			}
			if body["retryable"] != tc.retry || body["can_cancel"] != tc.cancel || body["operation"] != "up_batch" || body["committed_rows"] != int64(100) {
				t.Fatalf("wrong job capabilities: %#v", body)
			}
		})
	}
}
func TestResourceProjectionDeniesUnassignedUsersBeforeReadingWorkflowState(t *testing.T) {
	_, store, _ := newSQLStub(t, permissionStep(dao.NoAccess))
	_, _, service := newServices(t, store)
	_, err := service.Workflow().Resources(context.Background(), 7, 9)
	wantError(t, err, migration.ErrForbidden)
}
