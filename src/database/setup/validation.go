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
	rows, err := db.QueryContext(ctx, `SELECT u.username,s.authority_version,s.rotation_version,d.connection_options,e.execution_mode,e.completed_statements,e.total_statements FROM users u CROSS JOIN sessions s CROSS JOIN managed_databases d CROSS JOIN migration_executions e LIMIT 0`)
	if err != nil {
		return errors.New("platform runtime schema is incompatible; provision it outside the application")
	}
	rows.Close()
	var secrets bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='users' AND column_name IN ('password_hash','is_platform_admin','is_legacy_admin')) OR to_regclass('public.group_database_permissions') IS NOT NULL OR to_regclass('public.access_groups') IS NOT NULL OR to_regclass('public.user_groups') IS NOT NULL OR to_regclass('public.admin_users') IS NOT NULL OR EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='managed_databases' AND column_name IN ('db_username','encrypted_password'))`).Scan(&secrets); err != nil || secrets {
		return errors.New("platform database still contains authoritative access-control storage; complete the documented control-plane cutover")
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
