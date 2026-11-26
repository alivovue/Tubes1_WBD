package integration_test

import (
	"testing"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/migration"
)

func TestIT15StaleDraftUpdateDoesNotOverwriteNewerSQL(t *testing.T) {
	f := newFixture(t, true)
	target := f.register(t)
	f.grant(t, f.editor, target.ID, access.ViewEdit)
	original, err := f.migrations.SaveDraft(f.ctx, f.editor, target.ID, migration.DraftInput{Title: "Original draft", SQL: "SELECT 1"})
	must(t, err)
	updated, err := f.migrations.SaveDraft(f.ctx, f.editor, target.ID, migration.DraftInput{ID: original.ID, Title: "New draft", SQL: "SELECT 2", ExpectedUpdatedAt: original.UpdatedAt})
	must(t, err)
	if !updated.UpdatedAt.After(original.UpdatedAt) {
		t.Fatal("draft update did not receive a new timestamp")
	}
	_, err = f.migrations.SaveDraft(f.ctx, f.editor, target.ID, migration.DraftInput{ID: original.ID, Title: "Stale draft", SQL: "SELECT 3", ExpectedUpdatedAt: original.UpdatedAt})
	wantError(t, err, access.ErrConflict)
	saved, err := f.migrations.Draft(f.ctx, f.editor, target.ID, original.ID)
	must(t, err)
	if saved.SQL != "SELECT 2" || saved.Title != "New draft" || !saved.UpdatedAt.Equal(updated.UpdatedAt) {
		t.Fatal("stale draft write overwrote newer changes")
	}
	history, err := f.migrations.Workflow().History(f.ctx, f.editor, target.ID, false, 50, 0)
	must(t, err)
	if len(history) != 0 {
		t.Fatal("draft changes created executed history")
	}
}
