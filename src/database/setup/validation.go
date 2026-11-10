package setup

import (
	"context"
	"database/sql"
	"errors"
)

// ValidateSchema checks externally provisioned metadata without creating or
// altering tables, accounts, roles, or data.
func ValidateSchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("platform database is required")
	}
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('public.users') IS NOT NULL`).Scan(&exists); err != nil || !exists {
		return errors.New("platform schema is missing; provision the database outside the application")
	}
	var valid bool
	if err := db.QueryRowContext(ctx, `SELECT count(*)=5 FROM pg_attribute WHERE NOT attisdropped AND ((attrelid='public.users'::regclass AND attname IN ('is_legacy_admin','created_by_admin_id')) OR (attrelid='public.migration_executions'::regclass AND attname IN ('execution_mode','completed_statements','total_statements')) )`).Scan(&valid); err != nil || !valid {
		return errors.New("platform schema is incompatible; provision the required schema outside the application")
	}
	return nil
}
func ValidateRuntimeRole(ctx context.Context, db *sql.DB) error {
	var privileged bool
	if err := db.QueryRowContext(ctx, `SELECT rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls FROM pg_roles WHERE rolname=current_user`).Scan(&privileged); err != nil {
		return errors.New("application database role could not be checked")
	}
	if privileged {
		return errors.New("use a restricted PostgreSQL application role provisioned outside the application")
	}
	return nil
}
