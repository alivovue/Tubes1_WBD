package access

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	IsAdmin     bool   `json:"is_platform_admin"`
	Active      bool   `json:"is_active"`
}

type Group struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}
type Membership struct {
	UserID  int64 `json:"user_id"`
	GroupID int64 `json:"group_id"`
}
type Grant struct {
	GroupID    int64      `json:"group_id"`
	DatabaseID int64      `json:"database_id"`
	Permission Permission `json:"permission"`
}

// Database intentionally contains no connection credential or username.
type Database struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	Host         string     `json:"host,omitempty"`
	DatabaseName string     `json:"database_name,omitempty"`
	SSLMode      string     `json:"sslmode,omitempty"`
	Port         int        `json:"port,omitempty"`
	Active       bool       `json:"is_active,omitempty"`
	Permission   Permission `json:"permission"`
}

type Service struct {
	db        *sql.DB
	config    Config
	dummyHash []byte
}

func NewService(db *sql.DB, config Config) (*Service, error) {
	if db == nil {
		return nil, errors.New("platform database is required")
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	dummy, err := bcrypt.GenerateFromPassword([]byte("dummy-password-not-an-account"), 12)
	if err != nil {
		return nil, err
	}
	return &Service{db: db, config: config, dummyHash: dummy}, nil
}

// OpenDatabase never prints a DSN or a driver error, which may contain credentials.
func OpenDatabase(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, errors.New("konfigurasi koneksi PostgreSQL tidak valid")
	}
	db.SetMaxOpenConns(15)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, errors.New("koneksi PostgreSQL gagal; periksa konfigurasi dan server database")
	}
	return db, nil
}

type scanner interface{ Scan(...any) error }

func scanUser(row scanner) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.IsAdmin, &u.Active)
	return u, err
}

const userColumns = `id, username, display_name, is_platform_admin, is_active`

// BootstrapAdmin only succeeds while the platform has no users. No public seed route exists.
func (s *Service) BootstrapAdmin(ctx context.Context, username, display, password string) error {
	if !usernamePattern.MatchString(username) || !cleanName(display, 150) {
		return ErrInvalid
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(73421106)`); err != nil {
		return err
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users)`).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("%w: bootstrap hanya untuk platform kosong", ErrConflict)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO users(username,display_name,password_hash,is_platform_admin) VALUES($1,$2,$3,TRUE)`, username, display, hash)
	if err != nil {
		return classify(err)
	}
	return tx.Commit()
}

// adminChange serializes administration so concurrent changes cannot remove the last admin.
func (s *Service) adminChange(ctx context.Context, actor int64, change func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(73421106)`); err != nil {
		return err
	}
	var allowed bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND is_active AND is_platform_admin)`, actor).Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	if err = change(tx); err != nil {
		return classify(err)
	}
	return classify(tx.Commit())
}

func classify(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23505", "23503":
			return ErrConflict
		case "23514", "22001", "22P02":
			return ErrInvalid
		}
	}
	return err
}

func affected(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) CreateUser(ctx context.Context, actor int64, username, display, password string, admin bool) error {
	if !usernamePattern.MatchString(username) || !cleanName(display, 150) {
		return ErrInvalid
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return s.adminChange(ctx, actor, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO users(username,display_name,password_hash,is_platform_admin,created_by) VALUES($1,$2,$3,$4,$5)`, username, strings.TrimSpace(display), hash, admin, actor)
		return err
	})
}

func (s *Service) UpdateUser(ctx context.Context, actor, id int64, display string, admin, active bool, password string) error {
	if !cleanName(display, 150) {
		return ErrInvalid
	}
	var hash string
	var err error
	if password != "" {
		hash, err = HashPassword(password)
		if err != nil {
			return err
		}
	}
	return s.adminChange(ctx, actor, func(tx *sql.Tx) error {
		var wasAdmin, wasActive bool
		if err := tx.QueryRowContext(ctx, `SELECT is_platform_admin,is_active FROM users WHERE id=$1`, id).Scan(&wasAdmin, &wasActive); err != nil {
			return err
		}
		if wasAdmin && wasActive && (!admin || !active) {
			var count int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE is_platform_admin AND is_active`).Scan(&count); err != nil {
				return err
			}
			if count <= 1 {
				return fmt.Errorf("%w: platform harus memiliki minimal satu admin aktif", ErrInvalid)
			}
		}
		_, err := tx.ExecContext(ctx, `UPDATE users SET display_name=$2,is_platform_admin=$3,is_active=$4,password_hash=CASE WHEN $5='' THEN password_hash ELSE $5 END,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, id, strings.TrimSpace(display), admin, active, hash)
		if err != nil {
			return err
		}
		// Privilege/password changes revoke every token (including grace tokens).
		// The required fresh login creates a new identifier after privilege elevation.
		if wasAdmin != admin || wasActive != active || hash != "" {
			return s.revokeUsers(ctx, tx, `SELECT id FROM users WHERE id=$1`, id)
		}
		return nil
	})
}

func (s *Service) revokeUsers(ctx context.Context, tx *sql.Tx, userQuery string, args ...any) error {
	_, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=$`+fmt.Sprint(len(args)+1)+` WHERE revoked_at IS NULL AND user_id IN (`+userQuery+`)`, append(args, s.config.Now())...)
	return err
}

func (s *Service) SaveGroup(ctx context.Context, actor, id int64, name, description string) error {
	if !cleanName(name, 100) || len(description) > 2000 {
		return ErrInvalid
	}
	return s.adminChange(ctx, actor, func(tx *sql.Tx) error {
		if id == 0 {
			_, err := tx.ExecContext(ctx, `INSERT INTO access_groups(name,description) VALUES($1,$2)`, strings.TrimSpace(name), description)
			return err
		}
		return affected(tx.ExecContext(ctx, `UPDATE access_groups SET name=$2,description=$3 WHERE id=$1`, id, strings.TrimSpace(name), description))
	})
}

func (s *Service) DeleteGroup(ctx context.Context, actor, id int64) error {
	return s.adminChange(ctx, actor, func(tx *sql.Tx) error {
		if err := s.revokeUsers(ctx, tx, `SELECT user_id FROM user_groups WHERE group_id=$1`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM group_database_permissions WHERE group_id=$1`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM user_groups WHERE group_id=$1`, id); err != nil {
			return err
		}
		return affected(tx.ExecContext(ctx, `DELETE FROM access_groups WHERE id=$1`, id))
	})
}

func (s *Service) SetMembership(ctx context.Context, actor, user, group int64, member bool) error {
	return s.adminChange(ctx, actor, func(tx *sql.Tx) error {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users u CROSS JOIN access_groups g WHERE u.id=$1 AND g.id=$2)`, user, group).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		var err error
		if member {
			_, err = tx.ExecContext(ctx, `INSERT INTO user_groups(user_id,group_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, user, group)
		} else {
			_, err = tx.ExecContext(ctx, `DELETE FROM user_groups WHERE user_id=$1 AND group_id=$2`, user, group)
		}
		if err != nil {
			return err
		}
		return s.revokeUsers(ctx, tx, `SELECT id FROM users WHERE id=$1`, user)
	})
}

func (s *Service) SetGrant(ctx context.Context, actor, group, database int64, permission Permission) error {
	if permission != NoAccess && permission != ViewOnly && permission != ViewEdit {
		return ErrInvalid
	}
	return s.adminChange(ctx, actor, func(tx *sql.Tx) error {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM access_groups g CROSS JOIN managed_databases d WHERE g.id=$1 AND d.id=$2)`, group, database).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		var err error
		if permission == NoAccess {
			_, err = tx.ExecContext(ctx, `DELETE FROM group_database_permissions WHERE group_id=$1 AND database_id=$2`, group, database)
		} else {
			_, err = tx.ExecContext(ctx, `INSERT INTO group_database_permissions(group_id,database_id,access_level,granted_by) VALUES($1,$2,$3,$4) ON CONFLICT(group_id,database_id) DO UPDATE SET access_level=EXCLUDED.access_level,granted_by=EXCLUDED.granted_by,granted_at=CURRENT_TIMESTAMP`, group, database, permission, actor)
		}
		if err != nil {
			return err
		}
		return s.revokeUsers(ctx, tx, `SELECT user_id FROM user_groups WHERE group_id=$1`, group)
	})
}

// EffectivePermission always reads current group grants. Platform-admin status is deliberately absent.
func (s *Service) EffectivePermission(ctx context.Context, user, database int64) (Permission, error) {
	var level int
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(CASE p.access_level WHEN 'view_edit' THEN 2 WHEN 'view_only' THEN 1 ELSE 0 END),0)
	 FROM users u JOIN user_groups m ON m.user_id=u.id JOIN group_database_permissions p ON p.group_id=m.group_id
	 JOIN managed_databases d ON d.id=p.database_id WHERE u.id=$1 AND u.is_active AND d.id=$2 AND d.is_active`, user, database).Scan(&level)
	return []Permission{NoAccess, ViewOnly, ViewEdit}[level], err
}

func (s *Service) RequirePermission(ctx context.Context, user, database int64, required Permission) error {
	p, err := s.EffectivePermission(ctx, user, database)
	if err != nil {
		return err
	}
	if !p.Allows(required) {
		return ErrForbidden
	}
	return nil
}
