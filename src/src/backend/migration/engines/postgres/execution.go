package postgres

import (
	"context"
	"database/sql"
	"sync"

	"tubes1_wbd/backend/dbmanagement/adapters"
	"tubes1_wbd/backend/migration/engines"
)

type session struct {
	tx        *sql.Tx
	adapter   adapters.Adapter
	once      sync.Once
	completed int
	total     int
	protected *protectedMetadata
}

func (Engine) Open(ctx context.Context, db *sql.DB, adapter adapters.Adapter, mode string) (engines.Session, error) {
	if mode != engines.Transactional && mode != engines.Reconcile {
		return nil, adapters.ErrInvalid
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: mode == engines.Reconcile})
	if err != nil {
		return nil, adapters.ErrConnection
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_catalog.pg_advisory_xact_lock(73421108)`); err != nil {
		tx.Rollback()
		return nil, adapters.ErrQuery
	}
	return &session{tx: tx, adapter: adapter}, nil
}
func (s *session) Fingerprint(ctx context.Context) (string, error) {
	return s.adapter.Fingerprint(ctx, s.tx)
}
func (s *session) Prepare(ctx context.Context, r engines.Receipt) error {
	s.total = r.TotalStatements
	if _, err := s.tx.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS _schakker; CREATE TABLE IF NOT EXISTS _schakker.migration_receipts(namespace text NOT NULL,attempt_id bigint NOT NULL,version bigint NOT NULL,sql_hash text NOT NULL,applied_at timestamptz NOT NULL DEFAULT pg_catalog.clock_timestamp(),PRIMARY KEY(namespace,attempt_id),UNIQUE(namespace,version)); LOCK TABLE _schakker.migration_receipts IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return err
	}
	var err error
	s.protected, err = s.captureProtectedMetadata(ctx)
	return err
}
func (s *session) Execute(ctx context.Context, statement adapters.Statement) error {
	statements, err := (Engine{}).Parse(statement.SQL)
	if err != nil {
		return err
	}
	if len(statements) != 1 || s.protected == nil {
		return engines.ErrUnsafeSQL
	}
	if _, err := s.tx.ExecContext(ctx, `SELECT pg_catalog.set_config('standard_conforming_strings','on',true)`); err != nil {
		return err
	}
	if _, err := s.tx.ExecContext(ctx, statement.SQL); err != nil {
		return err
	}
	s.completed++
	return nil
}
func (s *session) Rollback() error         { return s.tx.Rollback() }
func (s *session) Close() error            { s.once.Do(func() { s.tx.Rollback() }); return nil }
func (s *session) KnownFailure(error) bool { return true }
func (s *session) Progress() (int, int)    { return s.completed, s.total }
func (s *session) Resolve(context.Context, engines.Receipt, bool) error {
	return engines.ErrUnsupported
}
