package unit_test

import (
	"testing"

	"tubes1_wbd/backend"
)

func TestSchemaDiffViewPairsChangedRows(t *testing.T) {
	before := map[string]any{
		"Name":     "old",
		"Nullable": false,
	}
	after := map[string]any{
		"Default":  nil,
		"Name":     "new",
		"Nullable": true,
	}
	view := backend.BuildSchemaDiffView(before, after)

	if view.Added != 3 || view.Removed != 2 {
		t.Fatalf("expected +3 -2, got +%d -%d", view.Added, view.Removed)
	}
	if len(view.Rows) != 5 {
		t.Fatalf("expected 5 aligned rows, got %d", len(view.Rows))
	}
	if view.Rows[1].Before.Kind != "removed" || view.Rows[1].After.Kind != "added" {
		t.Fatalf("expected changed row to pair removed and added cells, got %#v", view.Rows[1])
	}
	if view.Rows[3].Before.Kind != "empty" || view.Rows[3].After.Kind != "added" {
		t.Fatalf("expected an empty cell opposite the extra added row, got %#v", view.Rows[3])
	}
	if view.Rows[4].Before.Line != 4 || view.Rows[4].After.Line != 5 {
		t.Fatalf("expected independent line numbering, got before %d after %d", view.Rows[4].Before.Line, view.Rows[4].After.Line)
	}
}

func TestSchemaDiffViewLeavesIdenticalSnapshotsUnmarked(t *testing.T) {
	snapshot := map[string]any{"tables": []string{"employees"}}
	view := backend.BuildSchemaDiffView(snapshot, snapshot)

	if view.Added != 0 || view.Removed != 0 {
		t.Fatalf("identical snapshots must have no changes, got +%d -%d", view.Added, view.Removed)
	}
	for index, row := range view.Rows {
		if row.Before.Kind != "equal" || row.After.Kind != "equal" {
			t.Fatalf("row %d was unexpectedly marked as changed: %#v", index, row)
		}
	}
}
