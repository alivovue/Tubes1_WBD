package setup

import (
	"context"
	"database/sql"
	"errors"
)

// ValidateSchema only checks an existing, independent authentication database.
func ValidateSchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("admin database is required")
	}
	var exists, mixed bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('public.admin_users') IS NOT NULL,to_regclass('public.users') IS NOT NULL`).Scan(&exists, &mixed); err != nil {
		return errors.New("admin schema could not be checked")
	}
	if mixed {
		return errors.New("admin authentication requires a separate database without platform tables")
	}
	if !exists {
		return errors.New("admin schema is missing; provision the database outside the application")
	}
	rows, err := db.QueryContext(ctx, `SELECT u.legacy_source_id,u.migration_pending,s.admin_id,t.token_hash FROM admin_users u CROSS JOIN admin_sessions s CROSS JOIN admin_session_tokens t LIMIT 0`)
	if err != nil {
		return errors.New("admin schema is incompatible; provision the required schema outside the application")
	}
	return rows.Close()
}
