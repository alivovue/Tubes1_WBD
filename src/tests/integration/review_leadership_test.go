package integration_test

import (
	"net/http"
	"strings"
	"testing"

	"tubes1_wbd/backend/api"
	"tubes1_wbd/backend/migration"
	admindao "tubes1_wbd_admin/database/dao"
)

func TestMissingLeaderCanSubmitPendingReviewAndLeaderMayRepresentMultipleGroups(t *testing.T) {
	f, jobs, _ := newWorkerFixture(t)
	unconfigured := f.group(t, "Leader pending")
	must(t, f.adminAuth.SetGrant(f.ctx, f.admin, unconfigured.ID, f.database, admindao.ViewOnly))
	revision := f.definition(t, "shared_leader_review")
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
	_, err = f.workflow.Enqueue(f.ctx, f.editor, view.ID, "missing-leader")
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
	_, err = f.workflow.Enqueue(f.ctx, f.editor, fresh.ID, "leader-ready")
	must(t, err)
}
