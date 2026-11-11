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
	workflow, err := db.QueryContext(ctx, `SELECT r.up_sql,r.down_sql,v.authority_revision,h.reverses_execution_id,s.active_stack,j.status,j.execution_id,j.attempt,j.max_attempts,j.action_hash,j.sql_hash,j.error_class,j.cancellation_job_id,a.outcome,p.last_cursor,p.reverted_batches,p.reverted_rows FROM migration_revisions r CROSS JOIN migration_reviews v CROSS JOIN migration_execution_history h CROSS JOIN migration_database_state s CROSS JOIN migration_jobs j CROSS JOIN migration_job_attempts a CROSS JOIN migration_batch_progress p LIMIT 0`)
	if err != nil {
		return errors.New("platform database schema is incomplete; provision the current schema outside the application")
	}
	if err = workflow.Close(); err != nil {
		return err
	}
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
