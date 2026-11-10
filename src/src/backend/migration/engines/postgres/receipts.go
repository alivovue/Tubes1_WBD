package postgres

import (
	"context"
	"database/sql"
	"errors"

	"tubes1_wbd/backend/migration/engines"
)

func (s *session) Receipt(ctx context.Context, namespace string, id int64) (engines.Receipt, error) {
	r := engines.Receipt{Namespace: namespace, AttemptID: id, Mode: engines.Transactional, Status: "missing"}
	var exists bool
	if err := s.tx.QueryRowContext(ctx, `SELECT pg_catalog.to_regclass('_schakker.migration_receipts') IS NOT NULL`).Scan(&exists); err != nil {
		return r, err
	}
	if !exists {
		return r, nil
	}
	err := s.tx.QueryRowContext(ctx, `SELECT version,sql_hash,applied_at FROM _schakker.migration_receipts WHERE namespace=$1 AND attempt_id=$2`, namespace, id).Scan(&r.Version, &r.SQLHash, &r.AppliedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return r, nil
	}
	if err == nil {
		r.Status = "committed"
	}
	return r, err
}
func (s *session) Commit(ctx context.Context, r engines.Receipt) (engines.Receipt, error) {
	if _, err := s.tx.ExecContext(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); err != nil {
		return r, err
	}
	if err := s.verifyProtectedMetadata(ctx); err != nil {
		return r, err
	}
	if err := s.tx.QueryRowContext(ctx, `INSERT INTO _schakker.migration_receipts(namespace,attempt_id,version,sql_hash) VALUES($1,$2,$3,$4) RETURNING applied_at`, r.Namespace, r.AttemptID, r.Version, r.SQLHash).Scan(&r.AppliedAt); err != nil {
		return r, err
	}
	if _, err := s.tx.ExecContext(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); err != nil {
		return r, err
	}
	s.protected.receipts[receiptKey{r.Namespace, r.AttemptID}] = protectedReceipt{r.Version, r.SQLHash, r.AppliedAt.UTC()}
	if err := s.verifyProtectedMetadata(ctx); err != nil {
		return r, err
	}
	if err := s.tx.Commit(); err != nil {
		return r, err
	}
	r.Status = "committed"
	r.CompletedStatements = s.completed
	r.TotalStatements = s.total
	return r, nil
}
