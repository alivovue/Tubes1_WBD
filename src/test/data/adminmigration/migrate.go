// Package migrate preserves legacy authentication upgrade scenarios for tests only.
package migrate

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"time"

	adminsetup "tubes1_wbd/test/data/adminsetup"
)

//go:embed cutover.sql
var cutoverSQL string

type Result struct {
	Administrators  int
	AlreadyComplete bool
}
type account struct {
	id                      int64
	username, display, hash string
	active                  bool
	created, updated        time.Time
}

// Run requires both HTTP servers stopped and owner/migration connections to two
// different databases. Imported accounts remain unable to log in until the
// platform cutover commits. Rerunning activates a previously interrupted import;
// it never resets credentials of an already activated administrator.
func Run(ctx context.Context, platform, admin *sql.DB) (Result, error) {
	var result Result
	if platform == nil || admin == nil {
		return result, errors.New("both migration databases are required")
	}
	var sourceName, destinationName string
	if err := platform.QueryRowContext(ctx, `SELECT current_database()`).Scan(&sourceName); err != nil {
		return result, errors.New("platform database is unavailable")
	}
	if err := admin.QueryRowContext(ctx, `SELECT current_database()`).Scan(&destinationName); err != nil {
		return result, errors.New("admin database is unavailable")
	}
	if sourceName == destinationName {
		return result, errors.New("platform and admin databases must have different names")
	}
	if err := adminsetup.EnsureInitialized(ctx, admin); err != nil {
		return result, err
	}
	// The source marker survives failures before either credentials or schema change.
	if _, err := platform.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS public.admin_auth_migration (singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton),source_id UUID NOT NULL DEFAULT gen_random_uuid(),admin_ids BIGINT[] NOT NULL DEFAULT '{}',cutover_complete BOOLEAN NOT NULL DEFAULT FALSE); INSERT INTO public.admin_auth_migration(singleton) VALUES(TRUE) ON CONFLICT DO NOTHING`); err != nil {
		return result, errors.New("migration marker could not be prepared")
	}
	tx, err := platform.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(73421106)`); err != nil {
		return result, err
	}
	var source string
	var complete bool
	if err = tx.QueryRowContext(ctx, `SELECT source_id,cutover_complete FROM admin_auth_migration WHERE singleton FOR UPDATE`).Scan(&source, &complete); err != nil {
		return result, err
	}
	if complete {
		// Validate every mapping before activating pending imports. Existing admin
		// password changes are deliberately excluded from this recovery check.
		var missing int
		rows, err := tx.QueryContext(ctx, `SELECT unnest(admin_ids) FROM admin_auth_migration WHERE singleton`)
		if err != nil {
			return result, err
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return result, err
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return result, err
		}
		rows.Close()
		for _, id := range ids {
			var exists bool
			if err := admin.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM admin_users WHERE id=$1 AND legacy_source_id=$2 AND legacy_user_id=$1)`, id, source).Scan(&exists); err != nil {
				return result, err
			}
			if !exists {
				missing++
			}
		}
		if missing > 0 {
			return result, errors.New("imported administrator mappings are missing; restore matching database backups before retrying")
		}
		if err := activate(ctx, admin, source); err != nil {
			return result, err
		}
		return Result{Administrators: len(ids), AlreadyComplete: true}, tx.Commit()
	}
	var legacy bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='public.users'::regclass AND attname='is_platform_admin' AND NOT attisdropped)`).Scan(&legacy); err != nil {
		return result, err
	}
	if !legacy {
		return result, errors.New("platform is already on the new schema without a pending legacy migration; no cutover is required")
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,username,display_name,password_hash,is_active,created_at,updated_at FROM users WHERE is_platform_admin ORDER BY id FOR UPDATE`)
	if err != nil {
		return result, err
	}
	var accounts []account
	for rows.Next() {
		var a account
		if err := rows.Scan(&a.id, &a.username, &a.display, &a.hash, &a.active, &a.created, &a.updated); err != nil {
			rows.Close()
			return result, err
		}
		accounts = append(accounts, a)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	rows.Close()
	if err := stage(ctx, admin, source, accounts); err != nil {
		return result, err
	}
	// All destination credentials were verified in a committed transaction before
	// changing the source. Schema, attribution, revocation and scrubbing are atomic.
	if _, err = tx.ExecContext(ctx, cutoverSQL); err != nil {
		return result, errors.New("platform cutover rolled back; copied administrators remain pending; correct the schema issue and rerun")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE admin_auth_migration SET admin_ids=ARRAY(SELECT id FROM users WHERE is_legacy_admin ORDER BY id),cutover_complete=TRUE WHERE singleton`); err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return result, errors.New("platform commit outcome is uncertain; keep both servers stopped and rerun migration to recover")
	}
	if err = activate(ctx, admin, source); err != nil {
		return result, err
	}
	return Result{Administrators: len(accounts)}, nil
}
func stage(ctx context.Context, db *sql.DB, source string, accounts []account) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(73421106)`); err != nil {
		return err
	}
	for _, a := range accounts {
		var collision bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM admin_users WHERE (id=$1 OR lower(username)=lower($2)) AND NOT (id=$1 AND legacy_source_id=$3 AND legacy_user_id=$1 AND migration_pending))`, a.id, a.username, source).Scan(&collision)
		if err != nil {
			return err
		}
		if collision {
			return fmt.Errorf("administrator ID %d conflicts with an existing admin account; resolve the destination conflict before cutover", a.id)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO admin_users(id,username,display_name,password_hash,is_active,created_at,updated_at,legacy_source_id,legacy_user_id,migration_pending) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$1,TRUE) ON CONFLICT(id) DO UPDATE SET username=EXCLUDED.username,display_name=EXCLUDED.display_name,password_hash=EXCLUDED.password_hash,is_active=EXCLUDED.is_active,created_at=EXCLUDED.created_at,updated_at=EXCLUDED.updated_at WHERE admin_users.legacy_source_id=EXCLUDED.legacy_source_id AND admin_users.legacy_user_id=EXCLUDED.legacy_user_id AND admin_users.migration_pending`, a.id, a.username, a.display, a.hash, a.active, a.created, a.updated, source)
		if err != nil {
			return errors.New("administrator copy failed; platform credentials are unchanged")
		}
		var valid bool
		err = tx.QueryRowContext(ctx, `SELECT username=$2 AND display_name=$3 AND password_hash=$4 AND is_active=$5 AND created_at=$6 AND updated_at=$7 AND legacy_source_id=$8 AND legacy_user_id=$1 AND migration_pending FROM admin_users WHERE id=$1`, a.id, a.username, a.display, a.hash, a.active, a.created, a.updated, source).Scan(&valid)
		if err != nil || !valid {
			return errors.New("administrator copy validation failed; platform credentials are unchanged")
		}
	}
	if _, err = tx.ExecContext(ctx, `SELECT setval(pg_get_serial_sequence('admin_users','id'),GREATEST(COALESCE((SELECT MAX(id) FROM admin_users),1),(SELECT last_value FROM admin_users_id_seq)),EXISTS(SELECT 1 FROM admin_users))`); err != nil {
		return err
	}
	return tx.Commit()
}
func activate(ctx context.Context, db *sql.DB, source string) error {
	if _, err := db.ExecContext(ctx, `UPDATE admin_users SET migration_pending=FALSE WHERE legacy_source_id=$1 AND migration_pending`, source); err != nil {
		return errors.New("platform cutover committed but administrator activation is pending; keep both servers stopped and rerun migration")
	}
	return nil
}
