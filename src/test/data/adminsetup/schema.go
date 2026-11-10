package setup

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"time"
)

//go:embed schema.sql
var schemaSQL string

func EnsureInitialized(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("admin database is required")
	}
	var exists, mixed bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('public.admin_users') IS NOT NULL,to_regclass('public.users') IS NOT NULL`).Scan(&exists, &mixed); err != nil {
		return errors.New("admin schema could not be checked")
	}
	if mixed {
		return errors.New("admin authentication requires a separate database, without platform tables")
	}
	if !exists {
		return Initialize(ctx, db)
	}
	rows, err := db.QueryContext(ctx, `SELECT u.legacy_source_id,u.migration_pending,s.admin_id,t.token_hash FROM admin_users u CROSS JOIN admin_sessions s CROSS JOIN admin_session_tokens t LIMIT 0`)
	if err != nil {
		return errors.New("admin schema does not match src-admin/database/setup/schema.sql")
	}
	return rows.Close()
}
func Initialize(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("admin database is required")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return errors.New("admin database is unavailable")
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, schemaSQL); err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn.ExecContext(cleanup, "ROLLBACK")
		return errors.New("admin schema initialization failed; use a separate empty database")
	}
	return nil
}
