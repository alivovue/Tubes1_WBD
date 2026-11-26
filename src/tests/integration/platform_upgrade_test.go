package integration_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"testing"

	"schakker_schemas/platform"
	fixtures "schakker_tests/fixtures/setup"
	"tubes1_wbd/database/setup"
)

func prepareLegacyHistory(t *testing.T, f *workflowFixture) platform.UpgradeOptions {
	t.Helper()
	_, err := f.runtimeStore.DatabaseNamespace(f.ctx, f.database, "original-m1-namespace")
	must(t, err)
	_, err = f.dbs.Website.ExecContext(f.ctx, `CREATE TABLE migration_executions (
 id bigint PRIMARY KEY,database_id bigint,executed_by bigint,description text,sql_text text,
 attempted_version bigint,applied_version bigint,status text,execution_mode text,
 completed_statements integer,started_at timestamptz,finished_at timestamptz);
 CREATE TABLE migration_drafts(id bigint PRIMARY KEY,database_id bigint,created_by bigint,title text,sql_text text,operations jsonb,created_at timestamptz);`)
	must(t, err)
	query := `CREATE TABLE legacy_upgrade_probe(id bigint PRIMARY KEY);`
	hash := sha256.Sum256([]byte(query))
	_, err = f.dbs.Managed.ExecContext(f.ctx, query+`CREATE SCHEMA IF NOT EXISTS _schakker;
 CREATE TABLE _schakker.migration_receipts(namespace text,attempt_id bigint,version bigint,sql_hash text,applied_at timestamptz DEFAULT now());`)
	must(t, err)
	_, err = f.dbs.Managed.ExecContext(f.ctx, `INSERT INTO _schakker.migration_receipts(namespace,attempt_id,version,sql_hash) VALUES('original-m1-namespace',1,1,$1)`, hex.EncodeToString(hash[:]))
	must(t, err)
	_, err = f.dbs.Website.ExecContext(f.ctx, `INSERT INTO migration_executions VALUES(1,$1,$2,'Legacy committed',$3,1,1,'succeeded','transactional',1,now()-interval '2 minutes',now()-interval '1 minute'),
 (2,$1,$2,'Legacy failed','SELECT 1/0;',2,NULL,'failed','transactional',0,now()-interval '1 minute',now());`, f.database, f.editor, query)
	must(t, err)
	_, err = f.dbs.Website.ExecContext(f.ctx, `INSERT INTO migration_drafts VALUES(1,$1,$2,'Legacy SQL draft','SELECT 2;',NULL,now()),(2,$1,$2,'Legacy visual draft',NULL,'[]',now())`, f.database, f.editor)
	must(t, err)
	return platform.UpgradeOptions{Apply: true, Verify: func(ctx context.Context, e platform.LegacyExecution, namespace string) (platform.LegacyReceipt, error) {
		var receipt platform.LegacyReceipt
		err := f.dbs.Managed.QueryRowContext(ctx, `SELECT version,sql_hash,applied_at FROM _schakker.migration_receipts WHERE namespace=$1 AND attempt_id=$2`, namespace, e.ID).Scan(&receipt.Version, &receipt.SQLHash, &receipt.CommittedAt)
		return receipt, err
	}}
}

func TestLegacyMetadataUpgradePreservesHistoryNamespaceAndContinuesM2(t *testing.T) {
	f, jobs, runner := newWorkerFixture(t)
	options := prepareLegacyHistory(t, f)
	if err := setup.ValidateSchema(f.ctx, f.dbs.Website); err == nil {
		t.Fatal("unimported legacy history did not block startup")
	}
	options.Apply = false
	preview, err := platform.Upgrade(f.ctx, f.dbs.Website, options)
	must(t, err)
	if preview.Applied || preview.Executions != 2 || preview.Drafts != 1 || preview.ArchivedVisualDrafts != 1 {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	var count int
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT count(*) FROM migration_execution_history`).Scan(&count))
	if count != 0 {
		t.Fatal("dry run changed metadata")
	}
	options.Apply = true
	report, err := platform.Upgrade(f.ctx, f.dbs.Website, options)
	must(t, err)
	if !report.Applied {
		t.Fatal("upgrade did not commit")
	}
	must(t, setup.ValidateSchema(f.ctx, f.dbs.Website))
	if f.state(t).ActiveVersion != 1 {
		t.Fatal("legacy version was reset")
	}
	namespace, err := f.runtimeStore.ExistingDatabaseNamespace(f.ctx, f.database)
	must(t, err)
	if namespace != "original-m1-namespace" {
		t.Fatal("receipt namespace changed")
	}
	events, err := f.workflow.ExecutionHistory(f.ctx, f.editor, f.database, false, 50, 0)
	must(t, err)
	if len(events) != 2 || events[0].Status != "failed" || !events[1].IsCommitted {
		t.Fatalf("history not imported: %+v", events)
	}
	if _, err = f.dbs.Website.ExecContext(f.ctx, `DELETE FROM migration_executions`); err == nil {
		t.Fatal("old service could rewrite archived history")
	}
	if _, err = f.dbs.Website.ExecContext(f.ctx, `UPDATE migration_legacy_imports SET legacy_id=9`); err == nil {
		t.Fatal("mapping was mutable")
	}
	_, err = jobs.Retry(f.ctx, f.editor, events[0].JobID)
	if err == nil {
		t.Fatal("unreviewed legacy SQL became retryable")
	}
	revision, err := f.runtimeStore.Revision(f.ctx, events[1].RevisionID)
	must(t, err)
	if !revision.Irreversible || revision.DownSQL != "" {
		t.Fatal("invented rollback SQL for legacy migration")
	}
	id := enqueueMigration(t, f, "after_m1_upgrade")
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	assertJob(t, f, id, "succeeded")
	if f.state(t).ActiveVersion != 2 {
		t.Fatal("M2 execution did not continue the M1 baseline")
	}
	again, err := platform.Upgrade(f.ctx, f.dbs.Website, options)
	must(t, err)
	if again.Executions != 0 || again.Drafts != 0 || f.state(t).ActiveVersion != 2 {
		t.Fatal("repeat upgrade reset or duplicated history")
	}
}

func TestLegacyUpgradeRejectsWrongReceiptAndPartialEffectsAtomically(t *testing.T) {
	f := newWorkflowFixture(t)
	options := prepareLegacyHistory(t, f)
	options.Verify = func(context.Context, platform.LegacyExecution, string) (platform.LegacyReceipt, error) {
		return platform.LegacyReceipt{}, errors.New("receipt absent")
	}
	if _, err := platform.Upgrade(f.ctx, f.dbs.Website, options); err == nil {
		t.Fatal("accepted an unverified baseline")
	}
	var count int
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT count(*) FROM migration_definitions`).Scan(&count))
	if count != 0 {
		t.Fatal("failed upgrade left partial metadata")
	}
	_, err := f.dbs.Website.ExecContext(f.ctx, `UPDATE migration_executions SET execution_mode='non_atomic',completed_statements=1 WHERE status='failed'`)
	must(t, err)
	if _, err = platform.Upgrade(f.ctx, f.dbs.Website, options); err == nil {
		t.Fatal("partial legacy effects were ignored")
	}
}

func TestUpgradeBackfillsExistingM2AttemptSnapshots(t *testing.T) {
	f, _, runner := newWorkerFixture(t)
	id := enqueueDefinition(t, f, reversible("upgrade_existing_attempt"))
	_, err := runner.RunOnce(f.ctx)
	must(t, err)
	_, err = f.dbs.Website.ExecContext(f.ctx, `DROP TRIGGER immutable_migration_attempt ON migration_job_attempts;
 ALTER TABLE migration_job_attempts DROP COLUMN execution_id,DROP COLUMN definition_id,DROP COLUMN revision_id,
 DROP COLUMN database_id,DROP COLUMN actor_id,DROP COLUMN operation,DROP COLUMN logical_version,DROP COLUMN sql_snapshot,DROP COLUMN target_execution_id;`)
	must(t, err)
	_, err = platform.Upgrade(f.ctx, f.dbs.Website, platform.UpgradeOptions{Apply: true})
	must(t, err)
	var query string
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT sql_snapshot FROM migration_job_attempts WHERE job_id=$1`, id).Scan(&query))
	if query != reversible("upgrade_existing_attempt").UpSQL {
		t.Fatal("backfill lost original revision SQL")
	}
	if _, err = f.dbs.Website.ExecContext(f.ctx, `UPDATE migration_job_attempts SET sql_snapshot='changed'`); err == nil {
		t.Fatal("backfilled evidence not protected")
	}
	must(t, setup.ValidateSchema(f.ctx, f.dbs.Website))
}

func TestUpgradeCreatesWorkflowForAnM1OnlySchema(t *testing.T) {
	f := newWorkflowFixture(t)
	options := prepareLegacyHistory(t, f)
	runtime, err := fixtures.OpenScoped(f.ctx, factory.Cluster, f.dbs.WebsiteName, fixtures.UserRuntimeRole, func(context.Context, *sql.DB) error { return nil }, controlGrants)
	must(t, err)
	defer runtime.Close()
	_, err = f.dbs.Website.ExecContext(f.ctx, `DROP TABLE migration_legacy_imports,migration_approvals,migration_review_required_groups,
 migration_batch_progress,migration_job_attempts,migration_execution_history,migration_jobs,migration_reviews,
 migration_database_state,migration_revisions,migration_definitions CASCADE;
 DROP FUNCTION migration_content_hash(text,text,boolean,text),protect_migration_revision(),protect_migration_history(),protect_migration_attempt() CASCADE;`)
	must(t, err)
	report, err := platform.Upgrade(f.ctx, f.dbs.Website, options)
	must(t, err)
	if report.Executions != 2 || f.state(t).ActiveVersion != 1 {
		t.Fatal("M1-only workflow was not imported")
	}
	must(t, setup.ValidateSchema(f.ctx, f.dbs.Website))
	must(t, setup.ValidateSchema(f.ctx, runtime))
	_, err = runtime.ExecContext(f.ctx, `INSERT INTO migration_definitions(database_id,created_by,title) VALUES($1,$2,'Restricted writer after upgrade')`, f.database, f.editor)
	must(t, err)
	var ddl bool
	must(t, runtime.QueryRowContext(f.ctx, `SELECT has_schema_privilege(current_user,'public','CREATE')`).Scan(&ddl))
	if ddl {
		t.Fatal("upgrade broadened the application role to DDL access")
	}
}
