package integration_test

import (
	"fmt"
	"strings"
	"testing"

	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/dbmanagement/adapters"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/backend/migration/diff"
)

func TestVisualDataChangesExecuteBeforeLaterSchemaChanges(t *testing.T) {
	f, _, runner := newWorkerFixture(t)
	operations := []diff.Operation{
		{Kind: "update_row", Schema: "public", Table: "employees", Values: map[string]any{"name": "Promoted"}, Where: map[string]any{"salary": 6000}},
		{Kind: "drop_column", Schema: "public", Table: "employees", Column: adapters.Column{Name: "salary"}},
	}
	analysis, err := f.migrations.AnalyzeChanges(f.ctx, f.editor, f.database, operations)
	must(t, err)
	if strings.Index(analysis.SQL, "UPDATE") > strings.Index(analysis.SQL, "DROP COLUMN") {
		t.Fatal("analysis reordered the data change")
	}
	before, err := f.databases.TableData(f.ctx, f.editor, f.database, "public", "employees", dbmanagement.PageOptions{Limit: 10})
	must(t, err)
	if len(before.Columns) != 3 || fmt.Sprint(before.Rows[1][1]) != "Bob" {
		t.Fatal("visual analysis executed target changes")
	}
	id := enqueueDefinition(t, f, migration.DefinitionInput{Title: "Update then drop", UpSQL: analysis.SQL, Irreversible: true, IrreversibleReason: "Dropped salary values cannot be restored"})
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, id, "succeeded")
	after, err := f.databases.TableData(f.ctx, f.editor, f.database, "public", "employees", dbmanagement.PageOptions{Limit: 10})
	must(t, err)
	if len(after.Columns) != 2 || fmt.Sprint(after.Rows[1][1]) != "Promoted" {
		t.Fatal("execution did not preserve the reviewed operation order")
	}
}
