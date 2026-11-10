package setup

import (
	"context"
	"database/sql"
	"errors"
)

// Validation is read-only. Provisioning is an operator responsibility.
func ValidateSchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("admin database is required")
	}
	rows, err := db.QueryContext(ctx, `SELECT a.migration_pending,s.admin_id,t.token_hash,u.auth_version,u.rotation_version,g.name,m.group_id,p.access_level,d.encrypted_password FROM admin_users a CROSS JOIN admin_sessions s CROSS JOIN admin_session_tokens t CROSS JOIN users u CROSS JOIN access_groups g CROSS JOIN user_groups m CROSS JOIN group_database_permissions p CROSS JOIN managed_databases d LIMIT 0`)
	if err != nil {
		return errors.New("admin control-plane schema is missing or incompatible; provision it externally")
	}
	if err := rows.Close(); err != nil {
		return err
	}
	var mixed bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('public.migration_drafts') IS NOT NULL OR to_regclass('public.migration_executions') IS NOT NULL OR to_regclass('public.sessions') IS NOT NULL`).Scan(&mixed); err != nil || mixed {
		return errors.New("admin control plane requires a separate database without ordinary runtime tables")
	}
	return nil
}
func ValidateRuntimeRole(ctx context.Context, db *sql.DB) error {
	var privileged bool
	if err := db.QueryRowContext(ctx, `SELECT rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls FROM pg_roles WHERE rolname=current_user`).Scan(&privileged); err != nil {
		return errors.New("admin database role could not be checked")
	}
	if privileged {
		return errors.New("use a restricted PostgreSQL admin application role provisioned externally")
	}
	return nil
}
