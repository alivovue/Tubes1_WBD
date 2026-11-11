package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"schakker_shared/observability"
	"tubes1_wbd/backend/dbmanagement/adapters"
	"tubes1_wbd/backend/migration/engines"
)

func batchIdentifiers(a adapters.Adapter, s engines.BatchSpec) (table, key, column string, err error) {
	if table, err = a.QuoteIdentifier(s.Schema, s.Table); err != nil {
		return
	}
	if key, err = a.QuoteIdentifier(s.Key); err != nil {
		return
	}
	column, err = a.QuoteIdentifier(s.Column)
	return
}
func validateBatchTable(ctx context.Context, q adapters.Querier, s engines.BatchSpec) error {
	q = observability.WrapSQL(q)
	var valid bool
	err := q.QueryRowContext(ctx, `SELECT c.relkind='r' AND NOT c.relrowsecurity AND NOT c.relforcerowsecurity
 AND NOT EXISTS(SELECT 1 FROM pg_inherits WHERE inhrelid=c.oid OR inhparent=c.oid)
 AND NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid=c.oid AND NOT tgisinternal)
 AND NOT EXISTS(SELECT 1 FROM pg_rewrite WHERE ev_class=c.oid)
 AND NOT EXISTS(SELECT 1 FROM pg_constraint WHERE (conrelid=c.oid OR confrelid=c.oid) AND contype NOT IN('p','u','n'))
 AND EXISTS(SELECT 1 FROM pg_attribute a JOIN pg_constraint k ON k.conrelid=a.attrelid AND k.contype='p'
 WHERE a.attrelid=c.oid AND a.attname=$3 AND a.atttypid='pg_catalog.int8'::regtype
 AND a.attnotnull AND cardinality(k.conkey)=1 AND a.attnum=ANY(k.conkey))
 AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname=$4
 AND a.atttypid='pg_catalog.int8'::regtype AND a.attnotnull AND a.attgenerated='')
 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relname=$2`, s.Schema, s.Table, s.Key, s.Column).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return engines.ErrUnsafeSQL
	}
	return nil
}
func readBatch(ctx context.Context, q adapters.Querier, namespace, id string) (p engines.JobProgress, hash string, spec engines.BatchSpec, err error) {
	q = observability.WrapSQL(q)
	var raw []byte
	var cursor *int64
	err = q.QueryRowContext(ctx, `SELECT action_hash,spec,batches,rows_count,last_cursor,total_rows,reverted_batches,reverted_rows,state FROM _schakker.batch_jobs WHERE namespace=$1 AND job_id=$2`, namespace, id).Scan(&hash, &raw, &p.Batches, &p.Rows, &cursor, &p.Total, &p.RevertedBatches, &p.RevertedRows, &p.State)
	if err == nil {
		err = json.Unmarshal(raw, &spec)
	}
	if cursor != nil {
		p.Cursor = strconv.FormatInt(*cursor, 10)
	}
	return
}
func initBatch(ctx context.Context, conn *sql.Conn, a adapters.Adapter, r engines.JobRequest, h engines.JobHooks) error {
	_, _, _, err := readBatch(ctx, conn, r.Namespace, r.JobID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err = validateBatchTable(ctx, conn, *r.Batch); err != nil {
		return err
	}
	table, key, _, err := batchIdentifiers(a, *r.Batch)
	if err != nil {
		return err
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = observability.WrapSQL(tx).ExecContext(ctx, `LOCK TABLE `+table+` IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return err
	}
	if err = validateBatchTable(ctx, tx, *r.Batch); err != nil {
		return err
	}
	var total int64
	if err = observability.WrapSQL(tx).QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&total); err != nil {
		return err
	}
	if r.Batch.ExpectedRows > 0 && total != r.Batch.ExpectedRows {
		return adapters.ErrInvalid
	}
	raw, _ := json.Marshal(r.Batch)
	if _, err = observability.WrapSQL(tx).ExecContext(ctx, `INSERT INTO _schakker.batch_jobs(namespace,job_id,action_hash,spec,total_rows,state) VALUES($1,$2,$3,$4,$5,'running')`, r.Namespace, r.JobID, r.ActionHash, string(raw), total); err != nil {
		return err
	}
	if _, err = observability.WrapSQL(tx).ExecContext(ctx, `INSERT INTO _schakker.batch_undo(namespace,job_id,row_id) SELECT $1,$2,`+key+` FROM `+table, r.Namespace, r.JobID); err != nil {
		return err
	}
	if err = h.Guard(ctx); err != nil {
		return err
	}
	if err = observability.Commit(ctx, tx.Commit); err != nil {
		return engines.ErrJobUncertain
	}
	return nil
}
func runBatch(ctx context.Context, conn *sql.Conn, a adapters.Adapter, r engines.JobRequest, h engines.JobHooks, out engines.JobReceipt) (engines.JobReceipt, error) {
	source := r.JobID
	if r.Operation == "batch_cancel" {
		source = r.OriginalJobID
		if source == "" {
			return out, adapters.ErrInvalid
		}
	} else {
		if err := initBatch(ctx, conn, a, r, h); err != nil {
			return out, err
		}
	}
	for {
		p, hash, spec, err := readBatch(ctx, conn, r.Namespace, source)
		out.Progress = p
		if err != nil {
			return out, err
		}
		if spec != *r.Batch || (r.Operation != "batch_cancel" && hash != r.ActionHash) {
			return out, engines.ErrJobUncertain
		}
		if err = h.Progress(ctx, p); err != nil {
			return out, err
		}
		if err = h.Guard(ctx); err != nil {
			return out, err
		}
		if err = validateBatchTable(ctx, conn, spec); err != nil {
			return out, err
		}
		batchCtx, span := observability.Start(ctx, "migration.batch")
		finished, err := batchTransaction(batchCtx, conn, a, r, h, source, &out)
		span.End()
		if err != nil {
			return out, err
		}
		if finished {
			return out, nil
		}
	}
}
func batchTransaction(ctx context.Context, conn *sql.Conn, a adapters.Adapter, r engines.JobRequest, h engines.JobHooks, source string, out *engines.JobReceipt) (finished bool, err error) {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	committed := false
	defer func() {
		if !committed {
			rollbackErr := tx.Rollback()
			event := "sql.rollback"
			if errors.Is(err, engines.ErrJobUncertain) {
				event = "sql.commit_unconfirmed"
			}
			observability.Event(ctx, event, rollbackErr, "job_id", r.JobID)
		}
	}()
	s := *r.Batch
	table, key, column, err := batchIdentifiers(a, s)
	if err != nil {
		return false, err
	}
	if _, err = observability.WrapSQL(tx).ExecContext(ctx, `LOCK TABLE `+table+` IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return false, err
	}
	if err = validateBatchTable(ctx, tx, s); err != nil {
		return false, err
	}
	reverse := r.Operation == "batch_cancel"
	var batch int64
	var ids []int64
	query := `SELECT row_id FROM _schakker.batch_undo WHERE namespace=$1 AND job_id=$2 AND batch_no IS NULL ORDER BY row_id LIMIT $3 FOR UPDATE`
	if reverse {
		if err = observability.WrapSQL(tx).QueryRowContext(ctx, `SELECT COALESCE(max(batch_no),0) FROM _schakker.batch_undo WHERE namespace=$1 AND job_id=$2 AND batch_no IS NOT NULL AND NOT reverted`, r.Namespace, source).Scan(&batch); err != nil {
			return false, err
		}
		query = `SELECT row_id FROM _schakker.batch_undo WHERE namespace=$1 AND job_id=$2 AND batch_no=$3 AND NOT reverted ORDER BY row_id FOR UPDATE`
	}
	arg := int64(s.Size)
	if reverse {
		arg = batch
	}
	rows, err := observability.WrapSQL(tx).QueryContext(ctx, query, r.Namespace, source, arg)
	if err != nil {
		return false, err
	}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return false, err
		}
		ids = append(ids, id)
		if len(ids) > s.Size {
			rows.Close()
			return false, engines.ErrJobUncertain
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	if len(ids) == 0 {
		state := "completed"
		if reverse {
			state = "reverted"
		}
		if _, err = observability.WrapSQL(tx).ExecContext(ctx, `UPDATE _schakker.batch_jobs SET state=$3 WHERE namespace=$1 AND job_id=$2`, r.Namespace, source, state); err != nil {
			return false, err
		}
		out.Progress.State = state
		if err = h.Guard(ctx); err != nil {
			return false, err
		}
		if err = writeJobReceipt(ctx, tx, r, out); err != nil {
			return false, err
		}
		if err = observability.Commit(ctx, tx.Commit); err != nil {
			return false, engines.ErrJobUncertain
		}
		committed = true
		observability.Event(ctx, "batch.completed", nil, "job_id", r.JobID, "committed_rows", out.Progress.Rows)
		return true, nil
	}
	locked, err := observability.WrapSQL(tx).QueryContext(ctx, `SELECT `+key+` FROM `+table+` WHERE `+key+`=ANY($1::bigint[]) ORDER BY `+key+` FOR UPDATE`, ids)
	if err != nil {
		return false, err
	}
	count := 0
	for locked.Next() {
		var id int64
		if err = locked.Scan(&id); err != nil {
			locked.Close()
			return false, err
		}
		count++
	}
	err = locked.Err()
	locked.Close()
	if err != nil {
		return false, err
	}
	if count != len(ids) {
		return false, engines.ErrUnsafeCompensation
	}
	if reverse {
		var unsafe bool
		err = observability.WrapSQL(tx).QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM _schakker.batch_undo u JOIN `+table+` t ON t.`+key+`=u.row_id WHERE u.namespace=$1 AND u.job_id=$2 AND u.row_id=ANY($3::bigint[]) AND (t.`+column+` IS DISTINCT FROM u.after_value OR t.xmin::text<>u.after_xmin))`, r.Namespace, source, ids).Scan(&unsafe)
		if err != nil {
			return false, err
		}
		if unsafe {
			return false, engines.ErrUnsafeCompensation
		}
	} else {
		batch = out.Progress.Batches + 1
		if _, err = observability.WrapSQL(tx).ExecContext(ctx, `UPDATE _schakker.batch_undo u SET batch_no=$4,before_value=t.`+column+` FROM `+table+` t WHERE u.namespace=$1 AND u.job_id=$2 AND u.row_id=ANY($3::bigint[]) AND t.`+key+`=u.row_id`, r.Namespace, source, ids, batch); err != nil {
			return false, err
		}
	}
	started := time.Now()
	observability.Event(ctx, "sql.start", nil, "job_id", r.JobID, "batch_no", batch, "query", "[redacted]")
	result, err := tx.ExecContext(ctx, r.SQL, s.Delta, ids)
	var changed int64
	if err == nil {
		changed, err = result.RowsAffected()
	}
	observability.SQL(ctx, r.SQL, 2, changed, started, err)
	if err != nil {
		return false, err
	}
	if changed != int64(len(ids)) {
		return false, engines.ErrJobUncertain
	}
	direction := "up"
	state := "running"
	if reverse {
		direction = "down"
		state = "reverting"
		var mismatch bool
		if err = observability.WrapSQL(tx).QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM _schakker.batch_undo u JOIN `+table+` t ON t.`+key+`=u.row_id WHERE u.namespace=$1 AND u.job_id=$2 AND u.row_id=ANY($3::bigint[]) AND t.`+column+` IS DISTINCT FROM u.before_value)`, r.Namespace, source, ids).Scan(&mismatch); err != nil {
			return false, err
		}
		if mismatch {
			return false, engines.ErrUnsafeCompensation
		}
		if _, err = observability.WrapSQL(tx).ExecContext(ctx, `UPDATE _schakker.batch_undo SET reverted=true WHERE namespace=$1 AND job_id=$2 AND row_id=ANY($3::bigint[])`, r.Namespace, source, ids); err != nil {
			return false, err
		}
		if _, err = observability.WrapSQL(tx).ExecContext(ctx, `UPDATE _schakker.batch_jobs SET reverted_batches=reverted_batches+1,reverted_rows=reverted_rows+$3,state=$4 WHERE namespace=$1 AND job_id=$2`, r.Namespace, source, len(ids), state); err != nil {
			return false, err
		}
	} else {
		if _, err = observability.WrapSQL(tx).ExecContext(ctx, `UPDATE _schakker.batch_undo u SET after_value=t.`+column+`,after_xmin=t.xmin::text FROM `+table+` t WHERE u.namespace=$1 AND u.job_id=$2 AND u.row_id=ANY($3::bigint[]) AND t.`+key+`=u.row_id`, r.Namespace, source, ids); err != nil {
			return false, err
		}
		if _, err = observability.WrapSQL(tx).ExecContext(ctx, `UPDATE _schakker.batch_jobs SET batches=$3,rows_count=rows_count+$4,last_cursor=$5 WHERE namespace=$1 AND job_id=$2`, r.Namespace, source, batch, len(ids), ids[len(ids)-1]); err != nil {
			return false, err
		}
	}
	if _, err = observability.WrapSQL(tx).ExecContext(ctx, `INSERT INTO _schakker.batch_receipts(namespace,job_id,batch_no,direction,rows_count,cursor) VALUES($1,$2,$3,$4,$5,$6)`, r.Namespace, source, batch, direction, len(ids), ids[len(ids)-1]); err != nil {
		return false, err
	}
	if err = h.Guard(ctx); err != nil {
		return false, err
	}
	if err = observability.Commit(ctx, tx.Commit); err != nil {
		return false, engines.ErrJobUncertain
	}
	committed = true
	p, _, _, err := readBatch(ctx, conn, r.Namespace, source)
	if err != nil {
		return false, engines.ErrJobUncertain
	}
	out.Progress = p
	observability.Event(ctx, "sql.commit", nil, "job_id", r.JobID, "batch_no", batch, "direction", direction, "affected_rows", changed)
	return false, nil
}

func BatchRecovery(ctx context.Context, q adapters.Querier, a adapters.Adapter, namespace, job string) (engines.JobProgress, string, bool, error) {
	q = observability.WrapSQL(q)
	p, hash, spec, err := readBatch(ctx, q, namespace, job)
	if err != nil {
		return p, "", false, err
	}
	table, key, column, err := batchIdentifiers(a, spec)
	if err != nil {
		return p, "", false, err
	}
	if err = validateBatchTable(ctx, q, spec); err != nil {
		return p, "", false, err
	}
	var unsafe, completed bool
	err = q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM _schakker.job_receipts WHERE namespace=$1 AND job_id=$2), EXISTS(SELECT 1 FROM _schakker.batch_undo u LEFT JOIN `+table+` t ON t.`+key+`=u.row_id WHERE u.namespace=$1 AND u.job_id=$2 AND u.batch_no IS NOT NULL AND NOT u.reverted AND (t.`+key+` IS NULL OR t.`+column+` IS DISTINCT FROM u.after_value OR t.xmin::text<>u.after_xmin))`, namespace, job).Scan(&completed, &unsafe)
	if err != nil {
		return p, "", false, err
	}
	fingerprint := engines.SQLDigest(fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%d\x00%s\x00%d", namespace, job, hash, p.Batches, p.Rows, p.Cursor, p.Total))
	return p, fingerprint, !completed && !unsafe && p.Batches > 0, nil
}
