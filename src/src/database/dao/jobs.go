package dao

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var ErrJobLeaseLost = errors.New("migration job lease lost")

type MigrationJob struct {
	ID                string            `json:"job_id"`
	ExecutionID       string            `json:"execution_id"`
	ReviewID          int64             `json:"review_id"`
	DatabaseID        int64             `json:"database_id"`
	Operation         string            `json:"operation"`
	Status            string            `json:"status"`
	ActorID           int64             `json:"requested_by"`
	Attempt           int               `json:"attempt"`
	MaxAttempts       int               `json:"max_attempts"`
	ErrorClass        string            `json:"error_class"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
	NextAttemptAt     time.Time         `json:"next_attempt_at"`
	LeaseOwner        string            `json:"-"`
	LeaseUntil        *time.Time        `json:"-"`
	ActionHash        string            `json:"-"`
	SQLHash           string            `json:"-"`
	TraceContext      map[string]string `json:"-"`
	CancellationJobID *string           `json:"-"`
}
type BatchProgress struct {
	Batches         int64  `json:"committed_batches"`
	Rows            int64  `json:"committed_rows"`
	Cursor          string `json:"last_cursor"`
	Total           *int64 `json:"total_rows_estimate,omitempty"`
	State           string `json:"partial_state"`
	RevertedBatches int64  `json:"reverted_batches"`
	RevertedRows    int64  `json:"reverted_rows"`
}

func (d *Store) BatchProgress(ctx context.Context, id string) (BatchProgress, error) {
	var p BatchProgress
	err := d.workflowOne(ctx, &p, `SELECT row_to_json(p) FROM migration_batch_progress p WHERE job_id=$1`, id)
	if errors.Is(err, ErrNotFound) {
		return p, nil
	}
	return p, err
}
func (d *Store) Job(ctx context.Context, id string) (MigrationJob, error) {
	var j MigrationJob
	err := d.workflowOne(ctx, &j, `SELECT row_to_json(x) FROM (SELECT j.*,j.id AS job_id FROM migration_jobs j WHERE id=$1) x`, id)
	if err == nil {
		var raw []byte
		err = d.q.QueryRowContext(ctx, `SELECT COALESCE(lease_owner,''),lease_until,action_hash,sql_hash,trace_context,cancellation_job_id::text FROM migration_jobs WHERE id=$1`, id).Scan(&j.LeaseOwner, &j.LeaseUntil, &j.ActionHash, &j.SQLHash, &raw, &j.CancellationJobID)
		if err == nil {
			err = json.Unmarshal(raw, &j.TraceContext)
		}
	}
	return j, classify(err)
}
func (d *Store) InsertJob(ctx context.Context, review, database, actor int64, operation, key, actionHash, sqlHash string, carrier map[string]string) (string, error) {
	q, err := d.TransactionQueries()
	if err != nil {
		return "", err
	}
	if carrier == nil {
		carrier = map[string]string{}
	}
	raw, _ := json.Marshal(carrier)
	var id string
	err = q.QueryRowContext(ctx, `INSERT INTO migration_jobs(review_id,database_id,requested_by,operation,idempotency_key,action_hash,sql_hash,trace_context) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, review, database, actor, operation, key, actionHash, sqlHash, string(raw)).Scan(&id)
	return id, classify(err)
}
func (d *Store) ReserveCancellation(ctx context.Context, original, cancelJob string) error {
	if !d.inTx {
		return ErrInvalid
	}
	var previous *string
	err := d.q.QueryRowContext(ctx, `SELECT cancellation_job_id::text FROM migration_jobs WHERE id=$1 AND status IN('partial','uncertain','partially_reverted') AND lease_owner IS NULL FOR UPDATE`, original).Scan(&previous)
	if err != nil {
		if errors.Is(classify(err), ErrNotFound) {
			return ErrConflict
		}
		return err
	}
	if previous != nil {
		old, err := d.Job(ctx, *previous)
		if err != nil {
			return err
		}
		review, err := d.Review(ctx, old.ReviewID, true)
		if err != nil {
			return err
		}
		if old.LeaseOwner != "" || (old.Status != "failed" && old.Status != "partial" && old.Status != "partially_reverted") || (review.State != "invalidated" && review.State != "rejected") {
			return ErrConflict
		}
		if _, err = d.q.ExecContext(ctx, `UPDATE migration_jobs SET status='failed',error_class='superseded_by_fresh_review',updated_at=clock_timestamp() WHERE id=$1`, old.ID); err != nil {
			return err
		}
	}
	return affected(d.q.ExecContext(ctx, `UPDATE migration_jobs SET cancellation_job_id=$2 WHERE id=$1`, original, cancelJob))
}
func (d *Store) ClaimJob(ctx context.Context, owner string, lease time.Duration) (out MigrationJob, err error) {
	if owner == "" || lease < 3*time.Second {
		return out, ErrInvalid
	}
	err = d.Transaction(ctx, func(tx *Store) error {
		var id string
		err := tx.q.QueryRowContext(ctx, `SELECT j.id FROM migration_jobs j WHERE
 (((j.status='pending' OR (j.status IN('partial','partially_reverted') AND j.error_class='temporary_failure')) AND j.next_attempt_at<=clock_timestamp() AND (j.attempt<j.max_attempts OR j.error_class='reconcile_only'))
 OR (j.status IN('running','reverting') AND j.lease_until<clock_timestamp()))
 AND j.cancellation_job_id IS NULL
 AND NOT EXISTS(SELECT 1 FROM migration_jobs b WHERE b.database_id=j.database_id AND b.id<>j.id
 AND b.status IN('running','reverting','partial','uncertain','partially_reverted')
 AND NOT(j.operation='batch_cancel' AND b.cancellation_job_id=j.id)
 AND (b.status NOT IN('running','reverting') OR b.lease_until>=clock_timestamp() OR (b.created_at,b.id)<(j.created_at,j.id)))
 AND NOT EXISTS(SELECT 1 FROM migration_jobs p WHERE p.database_id=j.database_id AND p.status='pending'
 AND (p.created_at,p.id)<(j.created_at,j.id))
 ORDER BY j.created_at,j.id FOR UPDATE OF j SKIP LOCKED LIMIT 1`).Scan(&id)
		if err != nil {
			return classify(err)
		}
		if _, err = tx.q.ExecContext(ctx, `UPDATE migration_job_attempts SET finished_at=clock_timestamp(),outcome='uncertain',error_class='lease_expired' WHERE job_id=$1 AND finished_at IS NULL`, id); err != nil {
			return err
		}
		if _, err = tx.q.ExecContext(ctx, `UPDATE migration_jobs SET attempt=attempt+1,lease_owner=$2,lease_until=clock_timestamp()+$3*interval '1 millisecond',error_class=CASE WHEN status IN('running','reverting') AND error_class<>'reconcile_only' THEN 'lease_recovery' ELSE error_class END,status=CASE WHEN operation='batch_cancel' THEN 'reverting' ELSE 'running' END,updated_at=clock_timestamp() WHERE id=$1`, id, owner, lease.Milliseconds()); err != nil {
			return err
		}
		out, err = tx.Job(ctx, id)
		if err != nil {
			return err
		}
		_, err = tx.q.ExecContext(ctx, `INSERT INTO migration_job_attempts(job_id,attempt_no) VALUES($1,$2)`, id, out.Attempt)
		return classify(err)
	})
	return out, err
}
func (d *Store) HeartbeatJob(ctx context.Context, j MigrationJob, lease time.Duration) error {
	result, err := d.q.ExecContext(ctx, `UPDATE migration_jobs SET lease_until=clock_timestamp()+$4*interval '1 millisecond',updated_at=clock_timestamp() WHERE id=$1 AND lease_owner=$2 AND attempt=$3 AND lease_until>clock_timestamp() AND status IN('running','reverting')`, j.ID, j.LeaseOwner, j.Attempt, lease.Milliseconds())
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return ErrJobLeaseLost
	}
	return err
}
func (d *Store) OwnJob(ctx context.Context, j MigrationJob) error {
	var owns bool
	err := d.q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM migration_jobs WHERE id=$1 AND lease_owner=$2 AND attempt=$3 AND lease_until>clock_timestamp() AND status IN('running','reverting'))`, j.ID, j.LeaseOwner, j.Attempt).Scan(&owns)
	if err != nil {
		return err
	}
	if !owns {
		return ErrJobLeaseLost
	}
	return nil
}
func (d *Store) SaveBatchProgress(ctx context.Context, j MigrationJob, p BatchProgress) error {
	return d.Transaction(ctx, func(tx *Store) error {
		var id string
		if err := tx.q.QueryRowContext(ctx, `SELECT id FROM migration_jobs WHERE id=$1 AND lease_owner=$2 AND attempt=$3 AND lease_until>clock_timestamp() AND status IN('running','reverting') FOR UPDATE`, j.ID, j.LeaseOwner, j.Attempt).Scan(&id); err != nil {
			if errors.Is(classify(err), ErrNotFound) {
				return ErrJobLeaseLost
			}
			return err
		}
		_, err := tx.q.ExecContext(ctx, `INSERT INTO migration_batch_progress(job_id,committed_batches,committed_rows,last_cursor,total_rows_estimate,partial_state,reverted_batches,reverted_rows) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(job_id) DO UPDATE SET committed_batches=EXCLUDED.committed_batches,committed_rows=EXCLUDED.committed_rows,last_cursor=EXCLUDED.last_cursor,total_rows_estimate=EXCLUDED.total_rows_estimate,partial_state=EXCLUDED.partial_state,reverted_batches=EXCLUDED.reverted_batches,reverted_rows=EXCLUDED.reverted_rows,updated_at=clock_timestamp()`, j.ID, p.Batches, p.Rows, p.Cursor, p.Total, p.State, p.RevertedBatches, p.RevertedRows)
		return classify(err)
	})
}
func (d *Store) FinishJob(ctx context.Context, j MigrationJob, status, errorClass string, delay time.Duration) error {
	switch status {
	case "pending", "succeeded", "failed", "partial", "uncertain", "reverted", "partially_reverted":
	default:
		return ErrInvalid
	}
	return d.Transaction(ctx, func(tx *Store) error {
		result, err := tx.q.ExecContext(ctx, `UPDATE migration_jobs SET status=$4,error_class=$5,lease_owner=NULL,lease_until=NULL,next_attempt_at=clock_timestamp()+$6*interval '1 millisecond',updated_at=clock_timestamp() WHERE id=$1 AND lease_owner=$2 AND attempt=$3 AND lease_until>clock_timestamp() AND status IN('running','reverting')`, j.ID, j.LeaseOwner, j.Attempt, status, errorClass, delay.Milliseconds())
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrJobLeaseLost
		}
		outcome := status
		if status == "pending" {
			outcome = "failed"
		}
		if status == "reverted" {
			outcome = "succeeded"
		}
		if status == "partially_reverted" {
			outcome = "partial"
		}
		_, err = tx.q.ExecContext(ctx, `UPDATE migration_job_attempts SET finished_at=clock_timestamp(),outcome=$3,error_class=$4 WHERE job_id=$1 AND attempt_no=$2`, j.ID, j.Attempt, outcome, errorClass)
		if err == nil && status == "reverted" {
			_, err = tx.q.ExecContext(ctx, `UPDATE migration_jobs SET status='reverted',updated_at=clock_timestamp() WHERE cancellation_job_id=$1`, j.ID)
		}
		return classify(err)
	})
}
func (d *Store) RetryJob(ctx context.Context, id string, recoveryActor ...int64) error {
	class := ""
	actor := int64(0)
	if len(recoveryActor) > 0 {
		actor = recoveryActor[0]
	}
	if actor > 0 {
		class = "reconcile_only"
	}
	err := affected(d.q.ExecContext(ctx, `UPDATE migration_jobs SET status='pending',error_class=$2,trace_context=CASE WHEN $3::bigint>0 THEN trace_context||jsonb_build_object('recovery_actor_id',$3::text) ELSE trace_context-'recovery_actor_id' END,next_attempt_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1 AND status IN('failed','partial','uncertain','partially_reverted') AND (attempt<max_attempts OR (status='uncertain' AND $2='reconcile_only')) AND cancellation_job_id IS NULL AND lease_owner IS NULL`, id, class, actor))
	if errors.Is(err, ErrNotFound) {
		return ErrConflict
	}
	return err
}
