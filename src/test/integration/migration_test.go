package integration_test

import (
	"strings"
	"testing"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/migration"
)

func TestIT12SuccessfulMigrationCommitsSchemaReceiptAndHistory(t *testing.T) {
	f := newFixture(t, true)
	target := f.register(t)
	f.grant(t, f.admin, target.ID, access.ViewEdit)
	sql := "CREATE TABLE public.integration_test_orders (id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,amount INTEGER NOT NULL)"
	review, err := f.migrations.Analyze(f.ctx, f.admin, target.ID, sql)
	must(t, err)
	if f.tableExists(t, "integration_test_orders") {
		t.Fatal("analysis applied the migration")
	}
	history, err := f.migrations.History(f.ctx, f.admin, target.ID, 50, 0)
	must(t, err)
	if len(history) != 0 {
		t.Fatal("analysis created execution history")
	}
	execution, err := f.migrations.Execute(f.ctx, f.admin, target.ID, migration.ExecuteInput{Review: review, Description: "Integration order schema"})
	must(t, err)
	if !f.tableExists(t, "integration_test_orders") {
		t.Fatal("successful migration did not create its target table")
	}
	detail, err := f.databases.DescribeTable(f.ctx, f.admin, target.ID, "public", "integration_test_orders")
	must(t, err)
	if len(detail.Columns) != 2 || detail.Columns[0].Identity == "" || detail.Columns[1].Name != "amount" || detail.Columns[1].Nullable {
		t.Fatal("migration target columns differ from the reviewed SQL")
	}
	history, err = f.migrations.History(f.ctx, f.admin, target.ID, 50, 0)
	must(t, err)
	if len(history) != 1 || history[0].ID != execution.ID || history[0].Status != "succeeded" || history[0].AppliedVersion == nil || *history[0].AppliedVersion != 1 || history[0].ExecutedBy != f.admin || history[0].SQL != sql || history[0].FinishedAt == nil || history[0].CompletedStatements != 1 {
		t.Fatalf("successful history is incomplete: %#v", history)
	}
	if migration.SQLHash(history[0].SQL) != review.SQLHash {
		t.Fatal("history SQL differs from the approved hash")
	}
	var hash string
	var version int64
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT sql_hash,version FROM _schakker.migration_receipts WHERE attempt_id=$1`, execution.ID).Scan(&hash, &version))
	if hash != review.SQLHash || version != 1 {
		t.Fatal("committed target receipt does not match platform history")
	}
}
func TestIT13FailedMigrationRollsBackAndDoesNotAdvanceVersion(t *testing.T) {
	f := newFixture(t, true)
	target := f.register(t)
	f.grant(t, f.admin, target.ID, access.ViewEdit)
	sql := "CREATE TABLE public.rolled_back_orders(id INTEGER); INSERT INTO public.employees(name,salary) VALUES ('Rolled back',7000); SELECT 1/0"
	review, err := f.migrations.Analyze(f.ctx, f.admin, target.ID, sql)
	must(t, err)
	execution, err := f.migrations.Execute(f.ctx, f.admin, target.ID, migration.ExecuteInput{Review: review})
	wantError(t, err, migration.ErrExecution)
	if execution.ID == 0 || f.tableExists(t, "rolled_back_orders") {
		t.Fatal("failed migration did not roll back its earlier DDL")
	}
	var rows int
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT count(*) FROM public.employees`).Scan(&rows))
	if rows != 2 {
		t.Fatal("failed migration committed earlier row changes")
	}
	history, err := f.migrations.History(f.ctx, f.admin, target.ID, 50, 0)
	must(t, err)
	if len(history) != 1 || history[0].Status != "failed" || history[0].AppliedVersion != nil || history[0].AttemptedVersion != 1 || history[0].CompletedStatements != 0 || !strings.Contains(history[0].ErrorMessage, "rolled back") {
		t.Fatalf("failed attempt was recorded incorrectly: %#v", history)
	}
	version, err := f.store.MigrationVersion(f.ctx, target.ID)
	must(t, err)
	if version != 0 {
		t.Fatal("failed attempt advanced successful version")
	}
	next, err := f.migrations.Analyze(f.ctx, f.admin, target.ID, "ALTER TABLE public.employees ADD COLUMN department TEXT")
	must(t, err)
	success, err := f.migrations.Execute(f.ctx, f.admin, target.ID, migration.ExecuteInput{Review: next})
	must(t, err)
	if success.AppliedVersion == nil || *success.AppliedVersion != 1 {
		t.Fatal("first successful migration did not reuse version 1")
	}
}
func TestIT14SchemaAndVersionChangesInvalidateMigrationReview(t *testing.T) {
	f := newFixture(t, true)
	target := f.register(t)
	f.grant(t, f.admin, target.ID, access.ViewEdit)
	review, err := f.migrations.Analyze(f.ctx, f.admin, target.ID, "CREATE TABLE public.stale_review_table(id INTEGER)")
	must(t, err)
	_, err = f.dbs.Managed.ExecContext(f.ctx, `ALTER TABLE public.employees ADD COLUMN external_change TEXT`)
	must(t, err)
	_, err = f.migrations.Execute(f.ctx, f.admin, target.ID, migration.ExecuteInput{Review: review})
	wantError(t, err, migration.ErrStale)
	if f.tableExists(t, "stale_review_table") {
		t.Fatal("stale schema review was executed")
	}
	first, err := f.migrations.Analyze(f.ctx, f.admin, target.ID, "INSERT INTO public.employees(name,salary) VALUES ('Approved',8000)")
	must(t, err)
	second, err := f.migrations.Analyze(f.ctx, f.admin, target.ID, "INSERT INTO public.employees(name,salary) VALUES ('Stale',9000)")
	must(t, err)
	_, err = f.migrations.Execute(f.ctx, f.admin, target.ID, migration.ExecuteInput{Review: first})
	must(t, err)
	_, err = f.migrations.Execute(f.ctx, f.admin, target.ID, migration.ExecuteInput{Review: second})
	wantError(t, err, migration.ErrStale)
	var stale int
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT count(*) FROM public.employees WHERE name='Stale'`).Scan(&stale))
	if stale != 0 {
		t.Fatal("stale version review inserted data")
	}
	history, err := f.migrations.History(f.ctx, f.admin, target.ID, 50, 0)
	must(t, err)
	if len(history) != 1 || history[0].Status != "succeeded" {
		t.Fatal("rejected stale reviews created execution attempts")
	}
}
func TestIT15StaleDraftUpdateDoesNotOverwriteNewerSQL(t *testing.T) {
	f := newFixture(t, true)
	target := f.register(t)
	f.grant(t, f.admin, target.ID, access.ViewEdit)
	original, err := f.migrations.SaveDraft(f.ctx, f.admin, target.ID, migration.DraftInput{Title: "Original draft", SQL: "SELECT 1"})
	must(t, err)
	updated, err := f.migrations.SaveDraft(f.ctx, f.admin, target.ID, migration.DraftInput{ID: original.ID, Title: "New draft", SQL: "SELECT 2", ExpectedUpdatedAt: original.UpdatedAt})
	must(t, err)
	if !updated.UpdatedAt.After(original.UpdatedAt) {
		t.Fatal("draft update did not receive a new timestamp")
	}
	_, err = f.migrations.SaveDraft(f.ctx, f.admin, target.ID, migration.DraftInput{ID: original.ID, Title: "Stale draft", SQL: "SELECT 3", ExpectedUpdatedAt: original.UpdatedAt})
	wantError(t, err, access.ErrConflict)
	saved, err := f.migrations.Draft(f.ctx, f.admin, target.ID, original.ID)
	must(t, err)
	if saved.SQL != "SELECT 2" || saved.Title != "New draft" || !saved.UpdatedAt.Equal(updated.UpdatedAt) {
		t.Fatal("stale draft write overwrote newer changes")
	}
	history, err := f.migrations.History(f.ctx, f.admin, target.ID, 50, 0)
	must(t, err)
	if len(history) != 0 {
		t.Fatal("draft changes created executed history")
	}
}
