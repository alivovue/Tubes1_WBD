package platform

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// LegacyExecution is immutable M1 evidence. A verifier must read the receipt
// from the original target and namespace; it must never re-execute its SQL.
type LegacyExecution struct {
	ID, DatabaseID, ActorID, Version int64
	SQL, Title, Status, Mode         string
	StartedAt, FinishedAt            time.Time
	CompletedStatements              int
}
type LegacyReceipt struct {
	Version     int64     `json:"version"`
	SQLHash     string    `json:"sql_hash"`
	CommittedAt time.Time `json:"committed_at"`
}
type UpgradeOptions struct {
	Apply  bool
	Verify func(context.Context, LegacyExecution, string) (LegacyReceipt, error)
}
type UpgradeReport struct {
	Applied              bool `json:"applied"`
	Executions           int  `json:"imported_executions"`
	Drafts               int  `json:"imported_sql_drafts"`
	ArchivedVisualDrafts int  `json:"archived_visual_drafts"`
}

// Upgrade explicitly converts pre-refactor platform metadata in one transaction.
// Applications/workers must be stopped. The default dry run rolls everything
// back. Existing rows, namespaces, IDs, receipts and newer M2 state are preserved.
func Upgrade(ctx context.Context, db *sql.DB, options UpgradeOptions) (report UpgradeReport, err error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return report, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SET LOCAL lock_timeout='5s'; SELECT pg_advisory_xact_lock(73421109)`); err != nil {
		return report, err
	}
	if _, err = tx.ExecContext(ctx, `ALTER TABLE public.sessions ADD COLUMN IF NOT EXISTS rotation_version bigint NOT NULL DEFAULT 1`); err != nil {
		return report, err
	}
	// Table locks prevent an accidentally running old service from writing during
	// import. Reject queued/unresolved work instead of guessing target outcomes.
	for _, table := range []string{"users", "managed_databases", "migration_executions", "migration_drafts", "migration_definitions", "migration_reviews", "migration_jobs", "migration_job_attempts", "migration_execution_history", "migration_database_state"} {
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+table).Scan(&exists); err != nil {
			return report, err
		}
		if exists {
			if _, err = tx.ExecContext(ctx, `LOCK TABLE public.`+table+` IN ACCESS EXCLUSIVE MODE`); err != nil {
				return report, err
			}
		}
	}
	var workflow bool
	if err = tx.QueryRowContext(ctx, `SELECT to_regclass('public.migration_definitions') IS NOT NULL`).Scan(&workflow); err != nil {
		return report, err
	}
	if !workflow {
		start := strings.Index(schemaSQL, "CREATE FUNCTION public.migration_content_hash")
		if _, err = tx.ExecContext(ctx, strings.TrimSuffix(strings.TrimSpace(schemaSQL[start:]), "COMMIT;")); err != nil {
			return report, err
		}
	} else {
		var active bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM migration_jobs WHERE status NOT IN('succeeded','failed','reverted'))`).Scan(&active); err != nil {
			return report, err
		}
		if active {
			return report, errors.New("drain or reconcile pending/partial/uncertain jobs before upgrading")
		}
		if err = upgradeAttempts(ctx, tx); err != nil {
			return report, err
		}
		if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS public.migration_legacy_imports (
 source_kind text NOT NULL CHECK(source_kind IN('execution','draft')),legacy_id bigint NOT NULL,
 definition_id bigint NOT NULL REFERENCES public.migration_definitions(id),execution_id uuid,
 imported_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(source_kind,legacy_id))`); err != nil {
			return report, err
		}
		if _, err = tx.ExecContext(ctx, `DROP TRIGGER IF EXISTS append_only_legacy_imports ON public.migration_legacy_imports;
 CREATE TRIGGER append_only_legacy_imports BEFORE UPDATE OR DELETE ON public.migration_legacy_imports FOR EACH ROW EXECUTE FUNCTION public.protect_migration_history()`); err != nil {
			return report, err
		}
	}
	var legacy bool
	if err = tx.QueryRowContext(ctx, `SELECT to_regclass('public.migration_executions') IS NOT NULL`).Scan(&legacy); err != nil {
		return report, err
	}
	if legacy {
		var unsafe bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM migration_executions WHERE status='running' OR finished_at IS NULL OR (status='failed' AND execution_mode='non_atomic' AND completed_statements>0))`).Scan(&unsafe); err != nil {
			return report, err
		}
		if unsafe {
			return report, errors.New("legacy execution has unresolved or partial target effects; reconcile it before upgrading")
		}
		if report.Executions, err = importExecutions(ctx, tx, options); err != nil {
			return report, err
		}
		if err = archiveTable(ctx, tx, "migration_executions"); err != nil {
			return report, err
		}
	}
	if err = tx.QueryRowContext(ctx, `SELECT to_regclass('public.migration_drafts') IS NOT NULL`).Scan(&legacy); err != nil {
		return report, err
	}
	if legacy {
		if report.Drafts, report.ArchivedVisualDrafts, err = importDrafts(ctx, tx); err != nil {
			return report, err
		}
		if err = archiveTable(ctx, tx, "migration_drafts"); err != nil {
			return report, err
		}
	}
	if err = preserveRuntimeGrants(ctx, tx); err != nil {
		return report, err
	}
	if options.Apply {
		err = tx.Commit()
		report.Applied = err == nil
	}
	return report, err
}

// Copy the existing platform writer roles' access to new workflow storage.
// This preserves the established role split without making the runtime an owner
// or granting DDL, superuser, new database access, or privileges to PUBLIC.
func preserveRuntimeGrants(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT r.rolname FROM pg_roles r JOIN
 LATERAL aclexplode((SELECT COALESCE(relacl,acldefault('r',relowner)) FROM pg_class WHERE oid='public.users'::regclass)) a ON a.grantee=r.oid
 WHERE NOT r.rolsuper AND has_table_privilege(r.oid,'public.users','SELECT') AND has_table_privilege(r.oid,'public.users','INSERT')
 AND has_table_privilege(r.oid,'public.users','UPDATE') AND has_table_privilege(r.oid,'public.users','DELETE')`)
	if err != nil {
		return err
	}
	var roles []string
	for rows.Next() {
		var role string
		if err = rows.Scan(&role); err != nil {
			rows.Close()
			return err
		}
		roles = append(roles, `"`+strings.ReplaceAll(role, `"`, `""`)+`"`)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, role := range roles {
		if _, err = tx.ExecContext(ctx, `GRANT SELECT,INSERT,UPDATE,DELETE ON TABLE public.migration_definitions,public.migration_revisions,
 public.migration_database_state,public.migration_reviews,public.migration_review_required_groups,public.migration_approvals,
 public.migration_jobs,public.migration_job_attempts,public.migration_batch_progress,public.migration_execution_history,public.migration_legacy_imports TO `+role+`;
 GRANT EXECUTE ON FUNCTION public.migration_content_hash(text,text,boolean,text) TO `+role); err != nil {
			return err
		}
		for _, table := range []string{"migration_definitions", "migration_revisions", "migration_reviews", "migration_job_attempts", "migration_execution_history"} {
			column := "id"
			if table == "migration_execution_history" {
				column = "history_seq"
			}
			var sequence string
			if err = tx.QueryRowContext(ctx, `SELECT pg_get_serial_sequence($1,$2)`, "public."+table, column).Scan(&sequence); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `GRANT USAGE ON SEQUENCE `+sequence+` TO `+role); err != nil {
				return err
			}
		}
	}
	return nil
}

func upgradeAttempts(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DROP TRIGGER IF EXISTS immutable_migration_attempt ON public.migration_job_attempts;
 ALTER TABLE public.migration_job_attempts
 ADD COLUMN IF NOT EXISTS execution_id uuid DEFAULT gen_random_uuid(),
 ADD COLUMN IF NOT EXISTS definition_id bigint REFERENCES public.migration_definitions(id),
 ADD COLUMN IF NOT EXISTS revision_id bigint REFERENCES public.migration_revisions(id),
 ADD COLUMN IF NOT EXISTS database_id bigint REFERENCES public.managed_databases(id),
 ADD COLUMN IF NOT EXISTS actor_id bigint REFERENCES public.users(id),
 ADD COLUMN IF NOT EXISTS operation text,
 ADD COLUMN IF NOT EXISTS logical_version bigint,
 ADD COLUMN IF NOT EXISTS sql_snapshot text,
 ADD COLUMN IF NOT EXISTS target_execution_id uuid;
 UPDATE public.migration_job_attempts a SET definition_id=r.definition_id,revision_id=r.id,
 database_id=j.database_id,actor_id=j.requested_by,operation=j.operation,
 logical_version=CASE WHEN j.operation='rollback' THEN v.expected_active_version ELSE v.expected_active_version+1 END,
 sql_snapshot=CASE WHEN j.operation IN('rollback','batch_cancel') THEN r.down_sql ELSE r.up_sql END,target_execution_id=v.target_execution_id
 FROM public.migration_jobs j JOIN public.migration_reviews v ON v.id=j.review_id JOIN public.migration_revisions r ON r.id=v.revision_id
 WHERE a.job_id=j.id AND (a.definition_id IS NULL OR a.revision_id IS NULL OR a.database_id IS NULL OR a.actor_id IS NULL OR a.operation IS NULL OR a.logical_version IS NULL OR a.sql_snapshot IS NULL);
 ALTER TABLE public.migration_job_attempts ALTER execution_id SET NOT NULL,ALTER definition_id SET NOT NULL,
 ALTER revision_id SET NOT NULL,ALTER database_id SET NOT NULL,ALTER actor_id SET NOT NULL,ALTER operation SET NOT NULL,
 ALTER logical_version SET NOT NULL,ALTER sql_snapshot SET NOT NULL;
 CREATE UNIQUE INDEX IF NOT EXISTS migration_attempt_execution_identity ON public.migration_job_attempts(execution_id);
 CREATE INDEX IF NOT EXISTS migration_attempts_database_order ON public.migration_job_attempts(database_id,finished_at DESC,id DESC);`)
	if err != nil {
		return err
	}
	start := strings.Index(schemaSQL, "CREATE FUNCTION public.protect_migration_attempt()")
	end := strings.Index(schemaSQL[start:], "CREATE TABLE public.migration_batch_progress") + start
	_, err = tx.ExecContext(ctx, strings.Replace(schemaSQL[start:end], "CREATE FUNCTION", "CREATE OR REPLACE FUNCTION", 1))
	return err
}

func importExecutions(ctx context.Context, tx *sql.Tx, options UpgradeOptions) (int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT e.id,e.database_id,e.executed_by,COALESCE(e.applied_version,e.attempted_version),
 e.sql_text,COALESCE(NULLIF(e.description,''),'Imported M1 execution'),e.status,e.execution_mode,e.started_at,e.finished_at,e.completed_statements
 FROM migration_executions e WHERE NOT EXISTS(SELECT 1 FROM migration_legacy_imports m WHERE m.source_kind='execution' AND m.legacy_id=e.id)
 ORDER BY e.database_id,COALESCE(e.applied_version,e.attempted_version),e.started_at,e.id`)
	if err != nil {
		return 0, err
	}
	var executions []LegacyExecution
	for rows.Next() {
		var e LegacyExecution
		if err = rows.Scan(&e.ID, &e.DatabaseID, &e.ActorID, &e.Version, &e.SQL, &e.Title, &e.Status, &e.Mode, &e.StartedAt, &e.FinishedAt, &e.CompletedStatements); err != nil {
			rows.Close()
			return 0, err
		}
		executions = append(executions, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	versions := map[int64]int64{}
	for _, e := range executions {
		var namespace string
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(connection_options->>'migration_namespace','') FROM managed_databases WHERE id=$1`, e.DatabaseID).Scan(&namespace); err != nil {
			return 0, err
		}
		var receipt LegacyReceipt
		if e.Status == "succeeded" {
			if _, exists := versions[e.DatabaseID]; !exists {
				var existing bool
				if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM migration_execution_history WHERE database_id=$1) OR EXISTS(SELECT 1 FROM migration_database_state WHERE database_id=$1 AND (active_version<>0 OR cardinality(forward_stack)>0 OR blocked_reason<>''))`, e.DatabaseID).Scan(&existing); err != nil {
					return 0, err
				}
				if existing {
					return 0, errors.New("M1 and M2 committed chains overlap; manual reconciliation is required")
				}
			}
			if e.Version != versions[e.DatabaseID]+1 || namespace == "" || options.Verify == nil {
				return 0, errors.New("legacy baseline needs consecutive versions, original namespace and target receipt verification")
			}
			receipt, err = options.Verify(ctx, e, namespace)
			hash := sha256.Sum256([]byte(e.SQL))
			if err != nil || receipt.Version != e.Version || receipt.SQLHash != hex.EncodeToString(hash[:]) || receipt.CommittedAt.IsZero() {
				return 0, fmt.Errorf("target receipt verification failed for legacy execution %d", e.ID)
			}
			versions[e.DatabaseID] = e.Version
		}
		definition, revision, err := importRevision(ctx, tx, e.DatabaseID, e.ActorID, e.Title, e.SQL, e.StartedAt, true)
		if err != nil {
			return 0, err
		}
		var review int64
		var job, execution string
		key := fmt.Sprintf("legacy-m1:%d", e.ID)
		if err = tx.QueryRowContext(ctx, `INSERT INTO migration_reviews(revision_id,database_id,action,review_state,authority_revision,authority_snapshot,connection_fingerprint,expected_generation,expected_active_version,action_hash,submitted_by,submitted_at)
 VALUES($1,$2,'up','approved',1,'{"groups":[]}','legacy-import',0,$3,'legacy-import',$4,$5) RETURNING id`, revision, e.DatabaseID, max(int64(0), e.Version-1), e.ActorID, e.StartedAt).Scan(&review); err != nil {
			return 0, err
		}
		if err = tx.QueryRowContext(ctx, `INSERT INTO migration_jobs(review_id,database_id,operation,status,requested_by,idempotency_key,attempt,max_attempts,action_hash,sql_hash,created_at,updated_at)
 VALUES($1,$2,'up',$3,$4,$5,1,1,'legacy-import',encode(sha256(convert_to($6,'UTF8')),'hex'),$7,$8) RETURNING id,execution_id`, review, e.DatabaseID, e.Status, e.ActorID, key, e.SQL, e.StartedAt, e.FinishedAt).Scan(&job, &execution); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE migration_reviews SET queued_job_id=$2,idempotency_key=$3 WHERE id=$1`, review, job, key); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO migration_job_attempts(job_id,attempt_no,execution_id,definition_id,revision_id,database_id,actor_id,operation,logical_version,sql_snapshot,started_at,finished_at,outcome,error_class)
 VALUES($1,1,$2,$3,$4,$5,$6,'up',$7,$8,$9,$10,$11,$12)`, job, execution, definition, revision, e.DatabaseID, e.ActorID, e.Version, e.SQL, e.StartedAt, e.FinishedAt, e.Status, map[bool]string{true: "legacy_failure", false: ""}[e.Status == "failed"]); err != nil {
			return 0, err
		}
		if e.Status == "succeeded" {
			proof, _ := json.Marshal(map[string]any{"source": "M1", "attempt_id": e.ID, "namespace": namespace, "receipt": receipt})
			if _, err = tx.ExecContext(ctx, `UPDATE migration_definitions SET logical_version=$2 WHERE id=$1`, definition, e.Version); err != nil {
				return 0, err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO migration_execution_history(execution_id,job_id,review_id,definition_id,revision_id,database_id,logical_version,actor_id,operation,sql_snapshot,committed_at,target_receipt)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,'up',$9,$10,$11)`, execution, job, review, definition, revision, e.DatabaseID, e.Version, e.ActorID, e.SQL, receipt.CommittedAt, string(proof)); err != nil {
				return 0, err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO migration_database_state(database_id,active_version,generation,active_stack) VALUES($1,1,1,ARRAY[$2::uuid]) ON CONFLICT(database_id) DO UPDATE SET active_version=migration_database_state.active_version+1,generation=migration_database_state.generation+1,active_stack=array_append(migration_database_state.active_stack,$2::uuid),updated_at=now()`, e.DatabaseID, execution); err != nil {
				return 0, err
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO migration_legacy_imports(source_kind,legacy_id,definition_id,execution_id) VALUES('execution',$1,$2,$3)`, e.ID, definition, execution); err != nil {
			return 0, err
		}
	}
	return len(executions), nil
}

func importRevision(ctx context.Context, tx *sql.Tx, database, actor int64, title, query string, at time.Time, frozen bool) (definition, revision int64, err error) {
	if strings.TrimSpace(query) == "" {
		return 0, 0, errors.New("legacy SQL snapshot is empty")
	}
	if strings.TrimSpace(title) == "" {
		title = "Imported M1 migration"
	}
	title = string([]rune(title)[:min(len([]rune(title)), 255)])
	err = tx.QueryRowContext(ctx, `INSERT INTO migration_definitions(database_id,created_by,title,created_at) VALUES($1,$2,$3,$4) RETURNING id`, database, actor, title, at).Scan(&definition)
	if err != nil {
		return
	}
	reason := "Imported M1 SQL; the original SQL down was never stored."
	err = tx.QueryRowContext(ctx, `INSERT INTO migration_revisions(definition_id,revision_no,up_sql,irreversible,irreversible_reason,sql_hash,created_at,frozen_at) VALUES($1,1,$2,true,$3,public.migration_content_hash($2,'',true,$3),$4,CASE WHEN $5 THEN $4::timestamptz ELSE NULL END) RETURNING id`, definition, query, reason, at, frozen).Scan(&revision)
	return
}

func importDrafts(ctx context.Context, tx *sql.Tx) (count, archived int, err error) {
	rows, err := tx.QueryContext(ctx, `SELECT d.id,d.database_id,d.created_by,d.title,COALESCE(d.sql_text,''),d.created_at FROM migration_drafts d WHERE NOT EXISTS(SELECT 1 FROM migration_legacy_imports m WHERE m.source_kind='draft' AND m.legacy_id=d.id) ORDER BY d.id`)
	if err != nil {
		return
	}
	type draft struct {
		id, database, actor int64
		title, query        string
		at                  time.Time
	}
	var drafts []draft
	for rows.Next() {
		var d draft
		if err = rows.Scan(&d.id, &d.database, &d.actor, &d.title, &d.query, &d.at); err != nil {
			rows.Close()
			return
		}
		drafts = append(drafts, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return
	}
	for _, d := range drafts {
		if strings.TrimSpace(d.query) == "" {
			archived++
			continue
		}
		definition, _, failure := importRevision(ctx, tx, d.database, d.actor, d.title, d.query, d.at, false)
		if failure != nil {
			err = failure
			return
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO migration_legacy_imports(source_kind,legacy_id,definition_id) VALUES('draft',$1,$2)`, d.id, definition)
		if err != nil {
			return
		}
		count++
	}
	return
}
func archiveTable(ctx context.Context, tx *sql.Tx, table string) error {
	_, err := tx.ExecContext(ctx, `CREATE OR REPLACE FUNCTION public.protect_legacy_migration() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'legacy migration storage is read-only after upgrade' USING ERRCODE='23514'; END $$;
 DROP TRIGGER IF EXISTS legacy_read_only ON public.`+table+`; CREATE TRIGGER legacy_read_only BEFORE INSERT OR UPDATE OR DELETE ON public.`+table+` FOR EACH ROW EXECUTE FUNCTION public.protect_legacy_migration()`)
	return err
}
