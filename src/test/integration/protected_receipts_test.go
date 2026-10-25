package integration_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/database/dao"
)

func receiptFixture(t *testing.T) (*fixture, dao.Database, dao.MigrationExecution) {
	t.Helper()
	f := newFixture(t, true)
	target := f.register(t)
	f.grant(t, f.admin, target.ID, access.ViewEdit)
	review, err := f.migrations.Analyze(f.ctx, f.admin, target.ID, "CREATE TABLE public.receipt_guard_demo(id integer PRIMARY KEY,note text)")
	must(t, err)
	execution, err := f.migrations.Execute(f.ctx, f.admin, target.ID, migration.ExecuteInput{Review: review})
	must(t, err)
	return f, target, execution
}

func assertOriginalReceipt(t *testing.T, f *fixture, original dao.MigrationExecution, count int) {
	t.Helper()
	var actual int
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT count(*) FROM _schakker.migration_receipts`).Scan(&actual))
	if actual != count {
		t.Fatalf("receipt count = %d, want %d", actual, count)
	}
	var version int64
	var hash string
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT version,sql_hash FROM _schakker.migration_receipts WHERE attempt_id=$1`, original.ID).Scan(&version, &hash))
	if original.AppliedVersion == nil || version != *original.AppliedVersion || hash != migration.SQLHash(original.SQL) {
		t.Fatal("an earlier commit receipt changed")
	}
}

func TestPostgreSQLRejectsReceiptAttacksAndPreviouslySignedReviews(t *testing.T) {
	f, target, original := receiptFixture(t)
	base, err := f.migrations.Analyze(f.ctx, f.admin, target.ID, "SELECT 1")
	must(t, err)
	attacks := []string{
		`DO $$BEGIN DELETE FROM _schakker.migration_receipts; END$$`,
		`DO $$BEGIN EXECUTE 'DELETE FROM ' || quote_ident('_sch' || 'akker') || '.migration_receipts'; END$$`,
		`SELECT "pg_catalog"."set_config"('search_path','_schakker',true); DELETE FROM migration_receipts`,
		`DELETE FROM U&"\005fschakker".migration_receipts`,
		`CREATE OR REPLACE FUNCTION public.erase_receipts() RETURNS void LANGUAGE sql AS $$DELETE FROM _schakker.migration_receipts$$`,
	}
	for _, query := range attacks {
		_, err := f.migrations.SaveDraft(f.ctx, f.admin, target.ID, migration.DraftInput{Title: "Rejected receipt attack", SQL: query})
		wantError(t, err, migration.ErrUnsafeSQL)
		_, err = f.migrations.Analyze(f.ctx, f.admin, target.ID, query)
		wantError(t, err, migration.ErrUnsafeSQL)
		review := base
		review.SQL = query
		review.SQLHash = migration.SQLHash(query)
		review.Signature = f.databases.SignReview(fmt.Sprintf("%d\x00%s\x00%d\x00%s\x00%s\x00%s\x00%s", review.DatabaseID, review.SQLHash, review.BaseVersion, review.SchemaFingerprint, review.ConnectionFingerprint, review.DBMS, review.Mode))
		execution, err := f.migrations.Execute(f.ctx, f.admin, target.ID, migration.ExecuteInput{Review: review})
		wantError(t, err, migration.ErrUnsafeSQL)
		if execution.ID != 0 {
			t.Fatal("rejected SQL created an execution attempt")
		}
		assertOriginalReceipt(t, f, original, 1)
	}
	identity, raw := f.login(t, "it.admin")
	csrf, err := f.access.CSRFToken(identity)
	must(t, err)
	form := url.Values{"csrf": {csrf}, "action": {"analyze"}, "db": {fmt.Sprint(target.ID)}, "description": {"Rejected SQL"}, "sql": {attacks[0]}}
	request := httptest.NewRequest(http.MethodPost, "/migrations/new", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: access.CookieName, Value: raw})
	response := httptest.NewRecorder()
	promotionHandler(t, f).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "Migration SQL cannot run procedural blocks") {
		t.Fatalf("unsafe SQL did not show the policy rejection: %d", response.Code)
	}
	review, err := f.migrations.Analyze(f.ctx, f.admin, target.ID, "INSERT INTO public.receipt_guard_demo VALUES(1,'normal migration')")
	must(t, err)
	second, err := f.migrations.Execute(f.ctx, f.admin, target.ID, migration.ExecuteInput{Review: review})
	must(t, err)
	if second.AppliedVersion == nil || *second.AppliedVersion != 2 {
		t.Fatal("normal migration did not advance the version")
	}
	assertOriginalReceipt(t, f, original, 2)
	history, err := f.migrations.History(f.ctx, f.admin, target.ID, 50, 0)
	must(t, err)
	if len(history) != 2 {
		t.Fatal("rejected attacks changed successful migration history")
	}
}

func TestHiddenReceiptDeletionRollsBackTheWholeMigration(t *testing.T) {
	f, target, original := receiptFixture(t)
	_, err := f.dbs.Managed.ExecContext(f.ctx, `CREATE FUNCTION public.erase_receipts() RETURNS integer LANGUAGE plpgsql AS $$BEGIN DELETE FROM _schakker.migration_receipts; RETURN 1; END$$`)
	must(t, err)
	query := "CREATE TABLE public.receipt_guard_rolled_back(id integer); INSERT INTO public.receipt_guard_demo VALUES(1,'must roll back'); SELECT public.erase_receipts()"
	review, err := f.migrations.Analyze(f.ctx, f.admin, target.ID, query)
	must(t, err)
	execution, err := f.migrations.Execute(f.ctx, f.admin, target.ID, migration.ExecuteInput{Review: review})
	wantError(t, err, migration.ErrExecution)
	if execution.Status != "failed" || execution.AppliedVersion != nil || execution.CompletedStatements != 0 || !strings.Contains(execution.ErrorMessage, "Protected migration metadata") || !strings.Contains(execution.ErrorMessage, "rolled back") {
		t.Fatalf("metadata tampering was not recorded as a rollback: %#v", execution)
	}
	assertOriginalReceipt(t, f, original, 1)
	if f.tableExists(t, "receipt_guard_rolled_back") {
		t.Fatal("metadata rejection committed the earlier schema change")
	}
	var rows int
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT count(*) FROM public.receipt_guard_demo`).Scan(&rows))
	if rows != 0 {
		t.Fatal("metadata rejection committed the earlier row change")
	}
	version, err := f.store.MigrationVersion(f.ctx, target.ID)
	must(t, err)
	if version != 1 {
		t.Fatal("metadata tampering advanced the migration version")
	}
}

func TestHiddenReceiptDDLAndSessionChangesAreRolledBack(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"receipt column", `ALTER TABLE _schakker.migration_receipts ADD COLUMN unexpected boolean;`},
		{"receipt table", `DROP TABLE _schakker.migration_receipts;`},
		{"session settings", `PERFORM pg_catalog.set_config('search_path','_schakker,public',true);`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, target, original := receiptFixture(t)
			_, err := f.dbs.Managed.ExecContext(f.ctx, `CREATE FUNCTION public.change_receipt_state() RETURNS integer LANGUAGE plpgsql AS $$BEGIN `+tc.body+` RETURN 1; END$$`)
			must(t, err)
			review, err := f.migrations.Analyze(f.ctx, f.admin, target.ID, "SELECT public.change_receipt_state()")
			must(t, err)
			execution, err := f.migrations.Execute(f.ctx, f.admin, target.ID, migration.ExecuteInput{Review: review})
			wantError(t, err, migration.ErrExecution)
			if execution.Status != "failed" || !strings.Contains(execution.ErrorMessage, "Protected migration metadata") {
				t.Fatalf("hidden metadata or session changes were accepted: %#v", execution)
			}
			assertOriginalReceipt(t, f, original, 1)
			var unexpected bool
			must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='_schakker' AND table_name='migration_receipts' AND column_name='unexpected')`).Scan(&unexpected))
			if unexpected {
				t.Fatal("a protected schema change survived rollback")
			}
		})
	}
}

func TestReceiptInsertTriggerCannotErasePreviousCommitEvidence(t *testing.T) {
	for _, trigger := range []string{
		`CREATE TRIGGER prune_receipts AFTER INSERT ON _schakker.migration_receipts FOR EACH ROW EXECUTE FUNCTION public.prune_receipts()`,
		`CREATE CONSTRAINT TRIGGER prune_receipts AFTER INSERT ON _schakker.migration_receipts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.prune_receipts()`,
	} {
		t.Run(trigger, func(t *testing.T) {
			f, target, original := receiptFixture(t)
			_, err := f.dbs.Managed.ExecContext(f.ctx, `CREATE FUNCTION public.prune_receipts() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN DELETE FROM _schakker.migration_receipts WHERE attempt_id<>NEW.attempt_id; RETURN NEW; END$$; `+trigger)
			must(t, err)
			review, err := f.migrations.Analyze(f.ctx, f.admin, target.ID, "INSERT INTO public.receipt_guard_demo VALUES(1,'must roll back')")
			must(t, err)
			execution, err := f.migrations.Execute(f.ctx, f.admin, target.ID, migration.ExecuteInput{Review: review})
			wantError(t, err, migration.ErrExecution)
			if execution.Status != "failed" || !strings.Contains(execution.ErrorMessage, "Protected migration metadata") {
				t.Fatalf("receipt insertion side effects were accepted: %#v", execution)
			}
			assertOriginalReceipt(t, f, original, 1)
			var rows int
			must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT count(*) FROM public.receipt_guard_demo`).Scan(&rows))
			if rows != 0 {
				t.Fatal("receipt insertion side effects committed the migration")
			}
		})
	}
}
