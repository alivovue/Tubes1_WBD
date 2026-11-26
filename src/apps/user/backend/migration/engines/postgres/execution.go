package postgres

import (
	"context"
	"database/sql"
	"schakker_shared/observability"

	"tubes1_wbd/backend/dbmanagement/adapters"
	"tubes1_wbd/backend/migration/engines"
)

type session struct {
	tx                  *sql.Tx
	protected           *protectedMetadata
	jobReceiptException string
	lastRows            int64
}

func (s *session) prepare(ctx context.Context) error {
	var err error
	s.protected, err = s.captureProtectedMetadata(ctx)
	return err
}
func (s *session) execute(ctx context.Context, statement adapters.Statement) error {
	statements, err := (Engine{}).Parse(statement.SQL)
	if err != nil {
		return err
	}
	if len(statements) != 1 || s.protected == nil {
		return engines.ErrUnsafeSQL
	}
	if _, err := observability.WrapSQL(s.tx).ExecContext(ctx, `SELECT pg_catalog.set_config('standard_conforming_strings','on',true)`); err != nil {
		return err
	}
	result, err := s.tx.ExecContext(ctx, statement.SQL)
	if err != nil {
		return err
	}
	s.lastRows, _ = result.RowsAffected()
	return nil
}
