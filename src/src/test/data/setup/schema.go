package setup

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"time"
)

var ErrAdminMigrationRequired = errors.New("legacy test schema requires the fixture migration helper")

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
	var legacy bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='public.users'::regclass AND attname='is_platform_admin' AND NOT attisdropped)`).Scan(&legacy); err != nil {
		return errors.New("platform schema could not be checked")
	}
	if legacy {
		return ErrAdminMigrationRequired
	}
	var valid bool
	if err := db.QueryRowContext(ctx, `SELECT count(*)=5 FROM pg_attribute WHERE NOT attisdropped AND ((attrelid='public.users'::regclass AND attname IN ('is_legacy_admin','created_by_admin_id')) OR (attrelid='public.migration_executions'::regclass AND attname IN ('execution_mode','completed_statements','total_statements')) )`).Scan(&valid); err != nil || !valid {
		return errors.New("platform database schema does not match database/setup/schema.sql")
	}
	return nil
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
