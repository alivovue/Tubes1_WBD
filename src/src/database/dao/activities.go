package dao

import "context"

type MigrationAttempt struct {
	AttemptNo  int    `json:"attempt_no"`
	Outcome    string `json:"outcome"`
	ErrorClass string `json:"error_class"`
}
type MigrationActivity struct {
	JobID            string             `json:"job_id"`
	ReviewID         int64              `json:"review_id"`
	Operation        string             `json:"operation"`
	Status           string             `json:"status"`
	Attempts         []MigrationAttempt `json:"attempts"`
	CommittedBatches int64              `json:"committed_batches"`
	CommittedRows    int64              `json:"committed_rows"`
	LastCursor       string             `json:"last_cursor"`
	PartialState     string             `json:"partial_state"`
}

// Only operational counters and classified outcomes cross the API boundary.
// Raw driver errors (which can include row values) are deliberately excluded.
func (d *Store) MigrationActivities(ctx context.Context, database int64, limit, offset int) ([]MigrationActivity, error) {
	return workflowList[MigrationActivity](ctx, d, `SELECT row_to_json(x) FROM (
 SELECT j.id AS job_id,j.review_id,j.operation,j.status,
 COALESCE((SELECT jsonb_agg(jsonb_build_object('attempt_no',a.attempt_no,'outcome',a.outcome,'error_class',a.error_class) ORDER BY a.attempt_no)
 FROM migration_job_attempts a WHERE a.job_id=j.id),'[]'::jsonb) AS attempts,
 COALESCE(p.committed_batches,0) AS committed_batches,COALESCE(p.committed_rows,0) AS committed_rows,
 COALESCE(p.last_cursor,'') AS last_cursor,COALESCE(p.partial_state,'') AS partial_state
 FROM migration_jobs j LEFT JOIN migration_batch_progress p ON p.job_id=j.id
 WHERE j.database_id=$1 ORDER BY j.created_at DESC,j.id DESC LIMIT $2 OFFSET $3) x`, database, limit, offset)
}
