package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	"schakker_shared/observability"
	"tubes1_wbd/backend/dbmanagement/adapters"
	"tubes1_wbd/backend/migration/engines"
)

//go:embed jobs.sql
var JobsSchema string

func RunJob(ctx context.Context, db *sql.DB, adapter adapters.Adapter, r engines.JobRequest, h engines.JobHooks) (out engines.JobReceipt, err error) {
	out = engines.JobReceipt{Namespace: r.Namespace, JobID: r.JobID, ExecutionID: r.ExecutionID, SQLHash: r.SQLHash, ActionHash: r.ActionHash, Operation: r.Operation}
	if r.Namespace == "" || r.SQLHash != engines.SQLDigest(r.SQL) || r.ActionHash == "" || h.Guard == nil || h.Progress == nil {
		return out, adapters.ErrInvalid
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return out, err
	}
	defer conn.Close()
	lockCtx, span := observability.Start(ctx, "target.lock.wait")
	_, err = conn.ExecContext(lockCtx, `SELECT pg_catalog.pg_advisory_lock(73421108)`)
	span.End()
	if err != nil {
		return out, err
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		var unlocked bool
		if conn.QueryRowContext(cleanup, `SELECT pg_catalog.pg_advisory_unlock(73421108)`).Scan(&unlocked) != nil || !unlocked {
			conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	found, err := ReadJobReceipt(ctx, conn, r, &out)
	if err != nil {
		return out, engines.ErrJobUncertain
	}
	if found {
		if h.Reconciled != nil {
			h.Reconciled()
		}
		return out, nil
	}
	if r.Batch != nil && r.Operation != "rollback" {
		var exists bool
		if err = conn.QueryRowContext(ctx, `SELECT to_regclass('_schakker.batch_jobs') IS NOT NULL`).Scan(&exists); err != nil {
			return out, engines.ErrJobUncertain
		}
		if exists {
			source := r.JobID
			if r.Operation == "batch_cancel" {
				source = r.OriginalJobID
			}
			p, _, _, readErr := readBatch(ctx, conn, r.Namespace, source)
			if readErr == nil {
				out.Progress = p
			} else if !errors.Is(readErr, sql.ErrNoRows) {
				return out, engines.ErrJobUncertain
			}
		}
	}
	if h.Reconciled != nil {
		h.Reconciled()
	}
	if err = h.Guard(ctx); err != nil {
		return out, err
	}
	if _, err = observability.WrapSQL(conn).ExecContext(ctx, JobsSchema); err != nil {
		return out, err
	}
	if r.Batch != nil {
		if r.Operation == "rollback" {
			return rollbackCompletedBatch(ctx, conn, adapter, r, h, out)
		}
		return runBatch(ctx, conn, adapter, r, h, out)
	}
	statements, err := (Engine{}).Parse(r.SQL)
	if err != nil {
		return out, err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return out, err
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
	s := &session{tx: tx, adapter: adapter}
	if err = s.Prepare(ctx, engines.Receipt{TotalStatements: len(statements)}); err != nil {
		return out, err
	}
	for _, statement := range statements {
		if err = h.Guard(ctx); err != nil {
			return out, err
		}
		queryCtx, querySpan := observability.Start(ctx, "migration.query")
		started := time.Now()
		observability.Event(queryCtx, "sql.start", nil, "job_id", r.JobID, "query", "[redacted]")
		err = s.Execute(queryCtx, statement)
		observability.SQL(queryCtx, statement.SQL, 0, s.lastRows, started, err)
		querySpan.End()
		if err != nil {
			return out, err
		}
	}
	if _, err = observability.WrapSQL(tx).ExecContext(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); err != nil {
		return out, err
	}
	if err = s.verifyProtectedMetadata(ctx); err != nil {
		return out, err
	}
	if err = h.Guard(ctx); err != nil {
		return out, err
	}
	if err = writeJobReceipt(ctx, tx, r, &out); err != nil {
		return out, err
	}
	s.jobReceiptException = r.ExecutionID
	if _, err = observability.WrapSQL(tx).ExecContext(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); err != nil {
		return out, err
	}
	if err = s.verifyProtectedMetadata(ctx); err != nil {
		return out, err
	}
	var confirmed engines.JobReceipt
	if found, err = ReadJobReceipt(ctx, tx, r, &confirmed); err != nil || !found || confirmed.Proof() != out.Proof() {
		return out, engines.ErrProtectedMetadata
	}
	if err = observability.Commit(ctx, tx.Commit); err != nil {
		return out, engines.ErrJobUncertain
	}
	committed = true
	observability.Event(ctx, "sql.commit", nil, "job_id", r.JobID, "execution_id", r.ExecutionID)
	return out, nil
}

func ReadJobReceipt(ctx context.Context, q adapters.Querier, r engines.JobRequest, out *engines.JobReceipt) (bool, error) {
	q = observability.WrapSQL(q)
	var exists bool
	if err := q.QueryRowContext(ctx, `SELECT to_regclass('_schakker.job_receipts') IS NOT NULL`).Scan(&exists); err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	var got engines.JobReceipt
	var raw []byte
	err := q.QueryRowContext(ctx, `SELECT namespace,job_id::text,execution_id::text,sql_hash,action_hash,operation,committed_at,progress FROM _schakker.job_receipts WHERE namespace=$1 AND job_id=$2`, r.Namespace, r.JobID).Scan(&got.Namespace, &got.JobID, &got.ExecutionID, &got.SQLHash, &got.ActionHash, &got.Operation, &got.CommittedAt, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if json.Unmarshal(raw, &got.Progress) != nil || got.ExecutionID != r.ExecutionID || got.SQLHash != r.SQLHash || got.ActionHash != r.ActionHash || got.Operation != r.Operation {
		return false, engines.ErrJobUncertain
	}
	*out = got
	out.CommittedAt = out.CommittedAt.UTC().Truncate(time.Microsecond)
	return true, nil
}
func writeJobReceipt(ctx context.Context, tx *sql.Tx, r engines.JobRequest, out *engines.JobReceipt) error {
	raw, _ := json.Marshal(out.Progress)
	err := observability.WrapSQL(tx).QueryRowContext(ctx, `INSERT INTO _schakker.job_receipts(namespace,job_id,execution_id,sql_hash,action_hash,operation,progress) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING committed_at`, r.Namespace, r.JobID, r.ExecutionID, r.SQLHash, r.ActionHash, r.Operation, string(raw)).Scan(&out.CommittedAt)
	out.CommittedAt = out.CommittedAt.UTC().Truncate(time.Microsecond)
	return err
}
