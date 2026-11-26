package integration_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"tubes1_wbd/backend/migration"
	"tubes1_wbd/backend/migration/engines"
	"tubes1_wbd/backend/worker"
	"tubes1_wbd/database/dao"
)

func receiptFixture(t *testing.T) (*workflowFixture, *worker.Runner, dao.MigrationJob) {
	t.Helper()
	f, _, runner := newWorkerFixture(t)
	id := enqueueDefinition(t, f, migration.DefinitionInput{Title: "Receipt guard", UpSQL: "CREATE TABLE public.receipt_guard_demo(id integer PRIMARY KEY,note text)", DownSQL: "DROP TABLE public.receipt_guard_demo"})
	_, err := runner.RunOnce(f.ctx)
	must(t, err)
	return f, runner, assertJob(t, f, id, "succeeded")
}

func assertOriginalReceipt(t *testing.T, f *workflowFixture, original dao.MigrationJob, count int) {
	t.Helper()
	var actual int
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT count(*) FROM _schakker.job_receipts`).Scan(&actual))
	if actual != count {
		t.Fatalf("receipt count = %d, want %d", actual, count)
	}
	var execution, hash string
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT execution_id,sql_hash FROM _schakker.job_receipts WHERE job_id=$1`, original.ID).Scan(&execution, &hash))
	if execution != original.ExecutionID || hash != original.SQLHash {
		t.Fatal("an earlier commit receipt changed")
	}
}

func TestPostgreSQLRejectsReceiptAttacksBeforeApproval(t *testing.T) {
	f, runner, original := receiptFixture(t)
	attacks := []string{
		`DO $$BEGIN DELETE FROM _schakker.job_receipts; END$$`,
		`DO $$BEGIN EXECUTE 'DELETE FROM ' || quote_ident('_sch' || 'akker') || '.job_receipts'; END$$`,
		`SELECT "pg_catalog"."set_config"('search_path','_schakker',true); DELETE FROM job_receipts`,
		`DELETE FROM U&"\005fschakker".job_receipts`,
		`CREATE OR REPLACE FUNCTION public.erase_receipts() RETURNS void LANGUAGE sql AS $$DELETE FROM _schakker.job_receipts$$`,
	}
	for _, query := range attacks {
		_, err := f.migrations.Analyze(f.ctx, f.editor, f.database, query)
		wantError(t, err, migration.ErrUnsafeSQL)
		_, err = f.workflow.CreateDefinition(f.ctx, f.editor, f.database, migration.DefinitionInput{Title: "Rejected receipt attack", UpSQL: query, DownSQL: "SELECT 1"})
		wantError(t, err, migration.ErrDefinitionInvalid)
		assertOriginalReceipt(t, f, original, 1)
	}
	identity, raw := f.login(t, "it.editor")
	csrf, err := f.access.CSRFToken(identity)
	must(t, err)
	form := url.Values{"csrf": {csrf}, "action": {"analyze"}, "db": {fmt.Sprint(f.database)}, "description": {"Rejected SQL"}, "sql": {attacks[0]}}
	handler := userHandler(t, f.fixture)
	response := apiRequest(t, handler, http.MethodPost, "/migrations/new", raw, form)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "Migration SQL cannot run procedural blocks") {
		t.Fatalf("unsafe SQL did not show the policy rejection: %d", response.Code)
	}
	id := enqueueDefinition(t, f, migration.DefinitionInput{Title: "Normal migration", UpSQL: "INSERT INTO public.receipt_guard_demo VALUES(1,'normal migration')", DownSQL: "DELETE FROM public.receipt_guard_demo WHERE id=1"})
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, id, "succeeded")
	if f.state(t).ActiveVersion != 2 {
		t.Fatal("normal migration did not advance the active version")
	}
	assertOriginalReceipt(t, f, original, 2)
	history, err := f.workflow.History(f.ctx, f.editor, f.database, false, 50, 0)
	must(t, err)
	if len(history) != 2 {
		t.Fatal("rejected attacks changed successful migration history")
	}
}

func TestHiddenReceiptDeletionRollsBackTheWholeMigration(t *testing.T) {
	f, runner, original := receiptFixture(t)
	_, err := f.dbs.Managed.ExecContext(f.ctx, `CREATE FUNCTION public.erase_receipts() RETURNS integer LANGUAGE plpgsql AS $$BEGIN DELETE FROM _schakker.job_receipts; RETURN 1; END$$`)
	must(t, err)
	query := "CREATE TABLE public.receipt_guard_rolled_back(id integer); INSERT INTO public.receipt_guard_demo VALUES(1,'must roll back'); SELECT public.erase_receipts()"
	id := enqueueDefinition(t, f, migration.DefinitionInput{Title: "Hidden receipt deletion", UpSQL: query, DownSQL: "DROP TABLE public.receipt_guard_rolled_back"})
	_, err = runner.RunOnce(f.ctx)
	if !errors.Is(err, engines.ErrProtectedMetadata) {
		t.Fatalf("metadata tampering was not rejected: %v", err)
	}
	assertJob(t, f, id, "failed")
	assertOriginalReceipt(t, f, original, 1)
	if f.tableExists(t, "receipt_guard_rolled_back") {
		t.Fatal("metadata rejection committed the earlier schema change")
	}
	var rows int
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT count(*) FROM public.receipt_guard_demo`).Scan(&rows))
	if rows != 0 {
		t.Fatal("metadata rejection committed the earlier row change")
	}
	history, err := f.workflow.History(f.ctx, f.editor, f.database, false, 50, 0)
	must(t, err)
	if len(history) != 1 || f.state(t).ActiveVersion != 1 {
		t.Fatal("metadata tampering advanced committed history")
	}
}

func TestHiddenReceiptDDLAndSessionChangesAreRolledBack(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"receipt column", `ALTER TABLE _schakker.job_receipts ADD COLUMN unexpected boolean;`},
		{"receipt table", `DROP TABLE _schakker.job_receipts;`},
		{"session settings", `PERFORM pg_catalog.set_config('search_path','_schakker,public',true);`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, runner, original := receiptFixture(t)
			_, err := f.dbs.Managed.ExecContext(f.ctx, `CREATE FUNCTION public.change_receipt_state() RETURNS integer LANGUAGE plpgsql AS $$BEGIN `+tc.body+` RETURN 1; END$$`)
			must(t, err)
			id := enqueueDefinition(t, f, migration.DefinitionInput{Title: "Hidden receipt change", UpSQL: "SELECT public.change_receipt_state()", DownSQL: "SELECT 1"})
			_, err = runner.RunOnce(f.ctx)
			if !errors.Is(err, engines.ErrProtectedMetadata) {
				t.Fatalf("hidden metadata or session changes were accepted: %v", err)
			}
			assertJob(t, f, id, "failed")
			assertOriginalReceipt(t, f, original, 1)
			var unexpected bool
			must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='_schakker' AND table_name='job_receipts' AND column_name='unexpected')`).Scan(&unexpected))
			if unexpected || f.state(t).ActiveVersion != 1 {
				t.Fatal("a protected schema change survived rollback")
			}
		})
	}
}

func TestReceiptInsertTriggerCannotErasePreviousCommitEvidence(t *testing.T) {
	for _, trigger := range []string{
		`CREATE TRIGGER prune_receipts AFTER INSERT ON _schakker.job_receipts FOR EACH ROW EXECUTE FUNCTION public.prune_receipts()`,
		`CREATE CONSTRAINT TRIGGER prune_receipts AFTER INSERT ON _schakker.job_receipts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.prune_receipts()`,
	} {
		t.Run(trigger, func(t *testing.T) {
			f, runner, original := receiptFixture(t)
			_, err := f.dbs.Managed.ExecContext(f.ctx, `CREATE FUNCTION public.prune_receipts() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN DELETE FROM _schakker.job_receipts WHERE job_id<>NEW.job_id; RETURN NEW; END$$; `+trigger)
			must(t, err)
			id := enqueueDefinition(t, f, migration.DefinitionInput{Title: "Receipt trigger", UpSQL: "INSERT INTO public.receipt_guard_demo VALUES(1,'must roll back')", DownSQL: "DELETE FROM public.receipt_guard_demo WHERE id=1"})
			_, err = runner.RunOnce(f.ctx)
			if !errors.Is(err, engines.ErrProtectedMetadata) {
				t.Fatalf("receipt insertion side effects were accepted: %v", err)
			}
			assertJob(t, f, id, "failed")
			assertOriginalReceipt(t, f, original, 1)
			var rows int
			must(t, f.dbs.Managed.QueryRowContext(f.ctx, `SELECT count(*) FROM public.receipt_guard_demo`).Scan(&rows))
			if rows != 0 {
				t.Fatal("receipt insertion side effects committed the migration")
			}
		})
	}
}
