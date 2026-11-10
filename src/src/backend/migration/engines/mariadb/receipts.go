package mariadb

import (
	"context"
	"database/sql"
	"errors"

	"tubes1_wbd/backend/migration/engines"
)

func (s *session) ensureReceipts(ctx context.Context) error {
	_, err := s.conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS _schakker_migration_receipts(namespace varchar(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,attempt_id bigint NOT NULL,attempted_version bigint NOT NULL,applied_version bigint NULL,sql_hash char(64) CHARACTER SET ascii NOT NULL,status varchar(16) NOT NULL,mode varchar(24) NOT NULL,completed_statements integer NOT NULL,total_statements integer NOT NULL,applied_at datetime(6) NULL,PRIMARY KEY(namespace,attempt_id),UNIQUE KEY applied_version_key(namespace,applied_version)) ENGINE=InnoDB`)
	return err
}
func (s *session) Receipt(ctx context.Context, namespace string, id int64) (engines.Receipt, error) {
	r := engines.Receipt{Namespace: namespace, AttemptID: id, Status: "missing"}
	var exists int
	if err := s.query().QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='_schakker_migration_receipts'`).Scan(&exists); err != nil {
		return r, err
	}
	if exists == 0 {
		return r, nil
	}
	var appliedAt sql.NullTime
	err := s.query().QueryRowContext(ctx, `SELECT attempted_version,sql_hash,status,mode,completed_statements,total_statements,applied_at FROM _schakker_migration_receipts WHERE namespace=? AND attempt_id=?`, namespace, id).Scan(&r.Version, &r.SQLHash, &r.Status, &r.Mode, &r.CompletedStatements, &r.TotalStatements, &appliedAt)
	if errors.Is(err, sql.ErrNoRows) {
		r.Status = "missing"
		return r, nil
	}
	if err != nil {
		return r, err
	}
	if appliedAt.Valid {
		r.AppliedAt = appliedAt.Time
	}
	if r.Status == "prepared" && r.TotalStatements > 0 && r.CompletedStatements == r.TotalStatements {
		r.Status = "committed"
	}
	return r, nil
}
func (s *session) Commit(ctx context.Context, r engines.Receipt) (engines.Receipt, error) {
	if s.mode == engines.NonAtomic {
		if _, err := s.conn.ExecContext(ctx, `UPDATE _schakker_migration_receipts SET status='committed',applied_version=attempted_version,applied_at=UTC_TIMESTAMP(6) WHERE namespace=? AND attempt_id=?`, r.Namespace, r.AttemptID); err != nil {
			return r, err
		}
		return s.Receipt(ctx, r.Namespace, r.AttemptID)
	}
	if err := s.tx.QueryRowContext(ctx, `SELECT UTC_TIMESTAMP(6)`).Scan(&r.AppliedAt); err != nil {
		return r, err
	}
	var transaction bool
	if err := s.tx.QueryRowContext(ctx, `SELECT @@in_transaction`).Scan(&transaction); err != nil || !transaction {
		return r, engines.ErrOutcomeUnknown
	}
	if _, err := s.tx.ExecContext(ctx, `INSERT INTO _schakker_migration_receipts(namespace,attempt_id,attempted_version,applied_version,sql_hash,status,mode,completed_statements,total_statements,applied_at) VALUES(?,?,?,?,?,'committed',?,?,?,?)`, r.Namespace, r.AttemptID, r.Version, r.Version, r.SQLHash, engines.Transactional, s.completed, s.total, r.AppliedAt); err != nil {
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
func (s *session) Resolve(ctx context.Context, r engines.Receipt, applied bool) error {
	if s.mode != engines.Reconcile || r.Status != "prepared" {
		return engines.ErrUnsupported
	}
	if applied {
		_, err := s.conn.ExecContext(ctx, `UPDATE _schakker_migration_receipts SET status='committed',applied_version=attempted_version,completed_statements=total_statements,applied_at=UTC_TIMESTAMP(6) WHERE namespace=? AND attempt_id=? AND status='prepared'`, r.Namespace, r.AttemptID)
		return err
	}
	_, err := s.conn.ExecContext(ctx, `UPDATE _schakker_migration_receipts SET status='failed',applied_version=NULL,applied_at=UTC_TIMESTAMP(6) WHERE namespace=? AND attempt_id=? AND status='prepared'`, r.Namespace, r.AttemptID)
	return err
}
