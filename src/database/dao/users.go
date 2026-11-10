package dao

import "context"

const userColumns = `id, username, display_name, FALSE, is_active`

type scanner interface{ Scan(...any) error }

func scanUser(row scanner) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.IsAdmin, &u.Active)
	return u, classify(err)
}
func (d *Store) userSource() (string, string, string) {
	if d.adminAuth {
		return "admin_users", `id, username, display_name, TRUE, is_active`, "NOT migration_pending"
	}
	return "users", userColumns, "NOT is_legacy_admin"
}
func (d *Store) User(ctx context.Context, id int64) (User, error) {
	table, columns, filter := d.userSource()
	return scanUser(d.q.QueryRowContext(ctx, `SELECT `+columns+` FROM `+table+` WHERE id=$1 AND `+filter, id))
}
func (d *Store) Credentials(ctx context.Context, username string) (Credentials, error) {
	table, columns, filter := d.userSource()
	var c Credentials
	err := d.q.QueryRowContext(ctx, `SELECT `+columns+`,password_hash FROM `+table+` WHERE lower(username)=lower($1) AND `+filter, username).Scan(&c.ID, &c.Username, &c.DisplayName, &c.IsAdmin, &c.Active, &c.PasswordHash)
	return c, classify(err)
}
func (d *Store) LockCredentials(ctx context.Context, id int64, hash string) (bool, error) {
	if !d.inTx {
		return false, ErrInvalid
	}
	table, _, filter := d.userSource()
	var valid bool
	err := d.q.QueryRowContext(ctx, `SELECT is_active AND password_hash=$2 FROM `+table+` WHERE id=$1 AND `+filter+` FOR SHARE`, id, hash).Scan(&valid)
	return valid, classify(err)
}
func (d *Store) UsersExist(ctx context.Context) (bool, error) {
	table, _, _ := d.userSource()
	var exists bool
	err := d.q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+`)`).Scan(&exists)
	return exists, classify(err)
}
func (d *Store) IsActiveAdmin(ctx context.Context, id int64) (bool, error) {
	if !d.adminAuth {
		return false, nil
	}
	var allowed bool
	err := d.q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM admin_users WHERE id=$1 AND is_active AND NOT migration_pending)`, id).Scan(&allowed)
	return allowed, classify(err)
}
func (d *Store) ActiveAdminCount(ctx context.Context) (int, error) {
	if !d.adminAuth {
		return 0, ErrInvalid
	}
	var count int
	err := d.q.QueryRowContext(ctx, `SELECT count(*) FROM admin_users WHERE is_active AND NOT migration_pending`).Scan(&count)
	return count, classify(err)
}
func (d *Store) CreateUser(ctx context.Context, username, display, passwordHash string, admin bool, creator *int64) error {
	if d.adminAuth {
		if !admin {
			return ErrInvalid
		}
		_, err := d.q.ExecContext(ctx, `INSERT INTO admin_users(username,display_name,password_hash) VALUES($1,$2,$3)`, username, display, passwordHash)
		return classify(err)
	}
	if admin {
		return ErrInvalid
	}
	_, err := d.q.ExecContext(ctx, `INSERT INTO users(username,display_name,password_hash,created_by_admin_id) VALUES($1,$2,$3,$4)`, username, display, passwordHash, creator)
	return classify(err)
}
func (d *Store) UpdateUser(ctx context.Context, id int64, display string, admin, active bool, passwordHash string) error {
	if d.adminAuth {
		if !admin {
			return ErrInvalid
		}
		return affected(d.q.ExecContext(ctx, `UPDATE admin_users SET display_name=$2,is_active=$3,password_hash=CASE WHEN $4='' THEN password_hash ELSE $4 END,updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND NOT migration_pending`, id, display, active, passwordHash))
	}
	if admin {
		return ErrInvalid
	}
	if passwordHash == "" {
		return affected(d.q.ExecContext(ctx, `UPDATE users SET display_name=$2,is_active=$3,updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND NOT is_legacy_admin`, id, display, active))
	}
	return affected(d.q.ExecContext(ctx, `UPDATE users SET display_name=$2,is_active=$3,password_hash=$4,updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND NOT is_legacy_admin`, id, display, active, passwordHash))
}
func (d *Store) Users(ctx context.Context) ([]User, error) {
	table, columns, filter := d.userSource()
	rows, err := d.q.QueryContext(ctx, `SELECT `+columns+` FROM `+table+` WHERE `+filter+` ORDER BY id`)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	result := make([]User, 0)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, u)
	}
	return result, classify(rows.Err())
}
