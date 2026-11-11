package postgres

import (
	"context"
	"database/sql"
	"time"

	"schakker_shared/observability"
	"tubes1_wbd/backend/dbmanagement/adapters"
	"tubes1_wbd/backend/migration/engines"
)

func rollbackCompletedBatch(ctx context.Context, conn *sql.Conn, a adapters.Adapter, r engines.JobRequest, h engines.JobHooks, out engines.JobReceipt) (engines.JobReceipt, error) {
	p, _, spec, err := readBatch(ctx, conn, r.Namespace, r.OriginalJobID)
	if err != nil {
		return out, err
	}
	if spec != *r.Batch || p.State != "completed" || p.Rows != p.Total {
		return out, engines.ErrUnsafeCompensation
	}
	if err = validateBatchTable(ctx, conn, spec); err != nil {
		return out, err
	}
	table, key, column, err := batchIdentifiers(a, spec)
	if err != nil {
		return out, err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if _, err = observability.WrapSQL(tx).ExecContext(ctx, `LOCK TABLE `+table+` IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return out, err
	}
	if err = validateBatchTable(ctx, tx, spec); err != nil {
		return out, err
	}
	var count int64
	err = observability.WrapSQL(tx).QueryRowContext(ctx, `SELECT count(*) FROM (SELECT t.`+key+` FROM `+table+` t JOIN _schakker.batch_undo u ON u.row_id=t.`+key+` WHERE u.namespace=$1 AND u.job_id=$2 AND u.batch_no IS NOT NULL FOR UPDATE OF t) locked`, r.Namespace, r.OriginalJobID).Scan(&count)
	if err != nil {
		return out, err
	}
	if count != p.Rows {
		return out, engines.ErrUnsafeCompensation
	}
	var unsafe bool
	err = observability.WrapSQL(tx).QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM _schakker.batch_undo u JOIN `+table+` t ON t.`+key+`=u.row_id WHERE u.namespace=$1 AND u.job_id=$2 AND u.batch_no IS NOT NULL AND (t.`+column+` IS DISTINCT FROM u.after_value OR t.xmin::text<>u.after_xmin))`, r.Namespace, r.OriginalJobID).Scan(&unsafe)
	if err != nil {
		return out, err
	}
	if unsafe {
		return out, engines.ErrUnsafeCompensation
	}
	if err = h.Guard(ctx); err != nil {
		return out, err
	}
	started := time.Now()
	var changed int64
	err = observability.WrapSQL(tx).QueryRowContext(ctx, `SELECT _schakker.execute_batch_down($1,$2,ARRAY(SELECT row_id FROM _schakker.batch_undo WHERE namespace=$3 AND job_id=$4 AND batch_no IS NOT NULL ORDER BY row_id))`, r.SQL, spec.Delta, r.Namespace, r.OriginalJobID).Scan(&changed)
	observability.SQL(ctx, r.SQL, 2, changed, started, err)
	if err != nil {
		return out, err
	}
	if changed != p.Rows {
		return out, engines.ErrUnsafeCompensation
	}
	if _, err = observability.WrapSQL(tx).ExecContext(ctx, `UPDATE _schakker.batch_undo SET reverted=true WHERE namespace=$1 AND job_id=$2 AND batch_no IS NOT NULL`, r.Namespace, r.OriginalJobID); err != nil {
		return out, err
	}
	if _, err = observability.WrapSQL(tx).ExecContext(ctx, `UPDATE _schakker.batch_jobs SET state='reverted',reverted_batches=batches,reverted_rows=rows_count WHERE namespace=$1 AND job_id=$2`, r.Namespace, r.OriginalJobID); err != nil {
		return out, err
	}
	p.State = "reverted"
	p.RevertedBatches = p.Batches
	p.RevertedRows = p.Rows
	out.Progress = p
	if err = h.Guard(ctx); err != nil {
		out.Progress = engines.JobProgress{}
		return out, err
	}
	if err = writeJobReceipt(ctx, tx, r, &out); err != nil {
		return out, err
	}
	if err = observability.Commit(ctx, tx.Commit); err != nil {
		return out, engines.ErrJobUncertain
	}
	observability.Event(ctx, "sql.commit", nil, "job_id", r.JobID, "operation", "rollback")
	return out, nil
}
