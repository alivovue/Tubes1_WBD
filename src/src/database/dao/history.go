package dao

import (
	"context"
	"encoding/json"
	"errors"

	sqldb "schakker_shared/database"
)

func (d *Store) MigrationState(ctx context.Context, database int64, lock bool) (MigrationState, error) {
	var state MigrationState
	if lock {
		if !d.inTx {
			return state, ErrInvalid
		}
		if _, err := d.q.ExecContext(ctx, `INSERT INTO migration_database_state(database_id) VALUES($1) ON CONFLICT DO NOTHING`, database); err != nil {
			return state, sqldb.Classify(err)
		}
	}
	query := `SELECT row_to_json(s) FROM (SELECT * FROM migration_database_state WHERE database_id=$1`
	if lock {
		query += ` FOR UPDATE`
	}
	query += `) s`
	err := d.workflowOne(ctx, &state, query, database)
	if !lock && errors.Is(err, ErrNotFound) {
		return MigrationState{DatabaseID: database, ActiveStack: []string{}, ForwardStack: []string{}}, nil
	}
	return state, err
}
func (d *Store) SaveMigrationState(ctx context.Context, s MigrationState) error {
	active, _ := json.Marshal(s.ActiveStack)
	forward, _ := json.Marshal(s.ForwardStack)
	return sqldb.Affected(d.q.ExecContext(ctx, `UPDATE migration_database_state SET active_version=$2,generation=$3,
 active_stack=ARRAY(SELECT jsonb_array_elements_text($4::jsonb))::uuid[],forward_stack=ARRAY(SELECT jsonb_array_elements_text($5::jsonb))::uuid[],blocked_reason=$6,updated_at=now() WHERE database_id=$1`, s.DatabaseID, s.ActiveVersion, s.Generation, string(active), string(forward), s.BlockedReason))
}
func (d *Store) CommittedExecution(ctx context.Context, id string) (CommittedExecution, error) {
	var e CommittedExecution
	err := d.workflowOne(ctx, &e, `SELECT row_to_json(x) FROM (SELECT h.*,'succeeded' AS status FROM migration_execution_history h WHERE execution_id=$1) x`, id)
	return e, err
}
func (d *Store) CommittedHistory(ctx context.Context, database int64, hide bool, limit, offset int) ([]CommittedExecution, error) {
	return workflowList[CommittedExecution](ctx, d, `SELECT row_to_json(x) FROM (SELECT h.*,'succeeded' AS status FROM migration_execution_history h WHERE h.database_id=$1 AND (NOT $2 OR (h.operation<>'rollback' AND NOT EXISTS(SELECT 1 FROM migration_execution_history r WHERE r.reverses_execution_id=h.execution_id))) ORDER BY h.history_seq DESC LIMIT $3 OFFSET $4) x`, database, hide, limit, offset)
}

func (d *Store) ExecutionHistory(ctx context.Context, database int64, hide bool, limit, offset int) ([]ExecutionEvent, error) {
	// One successful attempt is represented by its committed execution ID, which
	// remains the rollback target. Other attempts retain their own execution IDs.
	// Filter committed reversal pairs before pagination; failures always remain.
	return workflowList[ExecutionEvent](ctx, d, `WITH events AS (
 SELECT to_jsonb(h)||jsonb_build_object('status','succeeded','is_committed',true,
 'attempt_no',COALESCE(a.attempt_no,0),'started_at',a.started_at,'finished_at',a.finished_at,
 'occurred_at',h.committed_at,'error_class','') AS payload,h.committed_at AS event_time,h.execution_id AS event_id
 FROM migration_execution_history h LEFT JOIN LATERAL (
 SELECT a.attempt_no,a.started_at,a.finished_at FROM migration_job_attempts a
 WHERE a.job_id=h.job_id AND a.outcome='succeeded' ORDER BY a.attempt_no LIMIT 1) a ON true
 WHERE h.database_id=$1 AND (NOT $2 OR (h.operation<>'rollback' AND NOT EXISTS(
 SELECT 1 FROM migration_execution_history r WHERE r.reverses_execution_id=h.execution_id)))
 UNION ALL
 SELECT to_jsonb(a)||jsonb_build_object('status',a.outcome,'is_committed',false,
 'review_id',j.review_id,'occurred_at',a.finished_at) AS payload,a.finished_at,a.execution_id
 FROM migration_job_attempts a JOIN migration_jobs j ON j.id=a.job_id
 WHERE a.database_id=$1 AND a.finished_at IS NOT NULL AND NOT (
 a.outcome='succeeded' AND EXISTS(SELECT 1 FROM migration_execution_history h WHERE h.job_id=a.job_id)
 AND a.attempt_no=(SELECT min(s.attempt_no) FROM migration_job_attempts s WHERE s.job_id=a.job_id AND s.outcome='succeeded'))
 ) SELECT payload FROM events ORDER BY event_time DESC,event_id DESC LIMIT $3 OFFSET $4`, database, hide, limit, offset)
}
func (d *Store) AppendCommittedExecution(ctx context.Context, e CommittedExecution) error {
	_, err := d.q.ExecContext(ctx, `INSERT INTO migration_execution_history(execution_id,job_id,review_id,definition_id,revision_id,database_id,logical_version,actor_id,operation,sql_snapshot,committed_at,reverses_execution_id,target_execution_id,target_receipt) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, e.ExecutionID, e.JobID, e.ReviewID, e.DefinitionID, e.RevisionID, e.DatabaseID, e.LogicalVersion, e.ActorID, e.Operation, e.SQL, e.CommittedAt, e.ReversesExecutionID, e.TargetExecutionID, e.TargetReceipt)
	return sqldb.Classify(err)
}
func (d *Store) SetDefinitionVersion(ctx context.Context, id, version int64) error {
	return sqldb.Affected(d.q.ExecContext(ctx, `UPDATE migration_definitions SET logical_version=$2 WHERE id=$1 AND (logical_version IS NULL OR logical_version=$2)`, id, version))
}
func (d *Store) BlockingWorkflowJob(ctx context.Context, database int64, own *string) (bool, error) {
	var blocked bool
	err := d.q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM migration_jobs WHERE database_id=$1
 AND status IN('running','partial','uncertain','reverting','partially_reverted')
 AND ($2::uuid IS NULL OR id<>$2::uuid))`, database, own).Scan(&blocked)
	return blocked, sqldb.Classify(err)
}

// MigrationVersion reports the current M2 active position, including rollback.
func (d *Store) MigrationVersion(ctx context.Context, database int64) (int64, error) {
	var version int64
	err := d.q.QueryRowContext(ctx, `SELECT COALESCE((SELECT active_version FROM migration_database_state WHERE database_id=$1),0)`, database).Scan(&version)
	return version, sqldb.Classify(err)
}
