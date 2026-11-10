package dao

import "context"

type scanner interface{ Scan(...any) error }

const userColumns = `id,username,display_name,is_active,auth_version,rotation_version`

func scanUser(row scanner) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Active, &u.Version, &u.Rotation)
	return u, classify(err)
}
func (d *Store) User(ctx context.Context, id int64) (User, error) {
	return scanUser(d.q.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1`, id))
}
func (d *Store) Admin(ctx context.Context, id int64) (User, error) {
	return scanUser(d.q.QueryRowContext(ctx, `SELECT id,username,display_name,is_active,0,0 FROM admin_users WHERE id=$1 AND NOT migration_pending`, id))
}
func (d *Store) Credentials(ctx context.Context, username string) (Credentials, error) {
	var c Credentials
	err := d.q.QueryRowContext(ctx, `SELECT `+userColumns+`,password_hash FROM users WHERE lower(username)=lower($1)`, username).Scan(&c.ID, &c.Username, &c.DisplayName, &c.Active, &c.Version, &c.Rotation, &c.PasswordHash)
	return c, classify(err)
}
func (d *Store) AdminCredentials(ctx context.Context, username string) (Credentials, error) {
	var c Credentials
	err := d.q.QueryRowContext(ctx, `SELECT id,username,display_name,is_active,0,0,password_hash FROM admin_users WHERE lower(username)=lower($1) AND NOT migration_pending`, username).Scan(&c.ID, &c.Username, &c.DisplayName, &c.Active, &c.Version, &c.Rotation, &c.PasswordHash)
	return c, classify(err)
}
func (d *Store) LockAdminCredentials(ctx context.Context, id int64, hash string) (bool, error) {
	if !d.inTx {
		return false, ErrInvalid
	}
	var valid bool
	err := d.q.QueryRowContext(ctx, `SELECT is_active AND password_hash=$2 FROM admin_users WHERE id=$1 AND NOT migration_pending FOR SHARE`, id, hash).Scan(&valid)
	return valid, classify(err)
}
func (d *Store) IsActiveAdmin(ctx context.Context, id int64) (bool, error) {
	var ok bool
	err := d.q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM admin_users WHERE id=$1 AND is_active AND NOT migration_pending)`, id).Scan(&ok)
	return ok, classify(err)
}
func (d *Store) ActiveAdminCount(ctx context.Context) (int, error) {
	var n int
	err := d.q.QueryRowContext(ctx, `SELECT count(*) FROM admin_users WHERE is_active AND NOT migration_pending`).Scan(&n)
	return n, classify(err)
}
func (d *Store) CreateUser(ctx context.Context, username, display, hash string, creator *int64) error {
	_, err := d.q.ExecContext(ctx, `INSERT INTO users(username,display_name,password_hash,created_by_admin_id) VALUES($1,$2,$3,$4)`, username, display, hash, creator)
	return classify(err)
}
func (d *Store) CreateAdmin(ctx context.Context, username, display, hash string) error {
	_, err := d.q.ExecContext(ctx, `INSERT INTO admin_users(username,display_name,password_hash) VALUES($1,$2,$3)`, username, display, hash)
	return classify(err)
}
func (d *Store) UpdateUser(ctx context.Context, id int64, display string, active bool, hash string) error {
	return affected(d.q.ExecContext(ctx, `UPDATE users SET display_name=$2,is_active=$3,password_hash=CASE WHEN $4='' THEN password_hash ELSE $4 END,auth_version=auth_version+CASE WHEN is_active<>$3 OR NOT $3 OR $4<>'' THEN 1 ELSE 0 END,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, id, display, active, hash))
}
func (d *Store) UpdateAdmin(ctx context.Context, id int64, display string, active bool, hash string) error {
	return affected(d.q.ExecContext(ctx, `UPDATE admin_users SET display_name=$2,is_active=$3,password_hash=CASE WHEN $4='' THEN password_hash ELSE $4 END,updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND NOT migration_pending`, id, display, active, hash))
}
func (d *Store) listUsers(ctx context.Context, query string) ([]User, error) {
	rows, err := d.q.QueryContext(ctx, query)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	out := make([]User, 0)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, classify(rows.Err())
}
func (d *Store) Users(ctx context.Context) ([]User, error) {
	return d.listUsers(ctx, `SELECT `+userColumns+` FROM users ORDER BY id`)
}
func (d *Store) Administrators(ctx context.Context) ([]User, error) {
	return d.listUsers(ctx, `SELECT id,username,display_name,is_active,0,0 FROM admin_users WHERE NOT migration_pending ORDER BY id`)
}
func (d *Store) InvalidateUser(ctx context.Context, id int64) error {
	_, err := d.q.ExecContext(ctx, `UPDATE users SET auth_version=auth_version+1 WHERE id=$1`, id)
	return classify(err)
}
func (d *Store) InvalidateGroupUsers(ctx context.Context, id int64) error {
	_, err := d.q.ExecContext(ctx, `UPDATE users SET auth_version=auth_version+1 WHERE id IN (SELECT user_id FROM user_groups WHERE group_id=$1)`, id)
	return classify(err)
}
func (d *Store) InvalidateDatabaseUsers(ctx context.Context, id int64) error {
	_, err := d.q.ExecContext(ctx, `UPDATE users SET auth_version=auth_version+1 WHERE id IN (SELECT m.user_id FROM user_groups m JOIN group_database_permissions p ON p.group_id=m.group_id WHERE p.database_id=$1)`, id)
	return classify(err)
}

func (d *Store) RotateUser(ctx context.Context, id int64) error {
	_, err := d.q.ExecContext(ctx, `UPDATE users SET rotation_version=rotation_version+1 WHERE id=$1`, id)
	return classify(err)
}
func (d *Store) RotateGroupUsers(ctx context.Context, id int64) error {
	_, err := d.q.ExecContext(ctx, `UPDATE users SET rotation_version=rotation_version+1 WHERE id IN (SELECT user_id FROM user_groups WHERE group_id=$1)`, id)
	return classify(err)
}
