package integration_test

import (
	"context"
	"reflect"
	"testing"
	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/backend/migration/engines"
	pgengine "tubes1_wbd/backend/migration/engines/postgres"
)

func TestApprovalsIgnoreUnrelatedAdministration(t *testing.T) {
	f, _, runner := newWorkerFixture(t)
	review := f.approve(t, f.submit(t, f.definition(t, "isolated_approval")))
	before, err := f.userAccess.MigrationAuthority(f.ctx, f.editor, f.database)
	must(t, err)
	outsider := f.user(t, "unrelated.employee")
	other := f.dbs.Connection()
	other.Name = "Another target"
	other.DatabaseName = "unrelated_database"
	db, err := f.saveConnection(f.ctx, f.admin, 0, other)
	must(t, err)
	// Changes to a different database also rotate/revoke members' sessions. They
	// must not invalidate this database's independently approved migration.
	must(t, f.access.SetGrant(f.ctx, f.admin, f.editors.ID, db.ID, access.ViewEdit))
	must(t, f.access.SetGrant(f.ctx, f.admin, f.editors.ID, db.ID, access.NoAccess))
	group := f.group(t, "Unrelated group")
	must(t, f.access.SetMembership(f.ctx, f.admin, outsider.ID, group.ID, true))
	after, err := f.userAccess.MigrationAuthority(f.ctx, f.editor, f.database)
	must(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("unrelated administration changed target authority")
	}
	id, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "isolated-approval")
	must(t, err)
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, id, "succeeded")
}

func TestRelevantPermissionCyclesInvalidateApprovals(t *testing.T) {
	f := newWorkflowFixture(t)
	review := f.approve(t, f.submit(t, f.definition(t, "permission_cycle")))
	before, err := f.userAccess.MigrationAuthority(f.ctx, f.editor, f.database)
	must(t, err)
	must(t, f.access.SetGrant(f.ctx, f.admin, f.readers.ID, f.database, access.NoAccess))
	must(t, f.access.SetGrant(f.ctx, f.admin, f.readers.ID, f.database, access.ViewOnly))
	after, err := f.userAccess.MigrationAuthority(f.ctx, f.editor, f.database)
	must(t, err)
	if !reflect.DeepEqual(before.Groups, after.Groups) || before.Revision == after.Revision {
		t.Fatal("restored permissions did not retain change evidence")
	}
	_, err = f.workflow.Enqueue(f.ctx, f.editor, review.ID, "stale-cycle")
	wantError(t, err, migration.ErrWorkflowConflict)
	review, err = f.workflow.Review(f.ctx, f.editor, review.ID)
	must(t, err)
	if review.State != "invalidated" {
		t.Fatal("stale review remained valid")
	}
}

func TestUnrelatedAccountCreationDoesNotInterruptBatch(t *testing.T) {
	f, _, runner := newWorkerFixture(t)
	revision := batchDefinition(t, f, 6, 2)
	review := f.approve(t, f.submit(t, revision))
	id, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, "unrelated-during-batch")
	must(t, err)
	changed := false
	runner.Execute = func(ctx context.Context, target *dbmanagement.Target, request engines.JobRequest, hooks engines.JobHooks) (engines.JobReceipt, error) {
		progress := hooks.Progress
		hooks.Progress = func(ctx context.Context, p engines.JobProgress) error {
			if err := progress(ctx, p); err != nil {
				return err
			}
			if p.Batches == 1 && !changed {
				changed = true
				return f.adminAuth.CreateUser(ctx, f.admin, "created.duringbatch", "Unrelated employee", password)
			}
			return nil
		}
		return pgengine.RunJob(ctx, target.DB, target.Adapter, request, hooks)
	}
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, id, "succeeded")
	if !changed {
		t.Fatal("authority change was not injected between commits")
	}
}
