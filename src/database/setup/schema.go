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
		return errors.New("website database is required")
	}
	var initialized bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('public.users') IS NOT NULL`).Scan(&initialized); err != nil {
		return errors.New("website schema could not be checked")
	}
	if !initialized {
		return Initialize(ctx, db)
	}
	rows, err := db.QueryContext(ctx, `SELECT execution_mode,completed_statements,total_statements FROM public.migration_executions LIMIT 0`)
	if err != nil {
		return errors.New("website database schema does not match database/setup/schema.sql")
	}
	return rows.Close()
}

func Initialize(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("website database is required")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return errors.New("website database is unavailable")
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, schemaSQL); err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn.ExecContext(cleanup, "ROLLBACK")
		return errors.New("website schema initialization failed; use an empty database")
	}
	return nil
}
