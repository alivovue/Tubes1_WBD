package dao

import "context"

const userColumns = `id, username, display_name, is_platform_admin, is_active`

type scanner interface{ Scan(...any) error }

func scanUser(row scanner) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.IsAdmin, &u.Active)
	return u, classify(err)
}

func (d *Store) User(ctx context.Context, id int64) (User, error) {
	return scanUser(d.q.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1`, id))
}

func (d *Store) Credentials(ctx context.Context, username string) (Credentials, error) {
	var c Credentials
	err := d.q.QueryRowContext(ctx, `SELECT `+userColumns+`,password_hash FROM users WHERE lower(username)=lower($1)`, username).Scan(&c.ID, &c.Username, &c.DisplayName, &c.IsAdmin, &c.Active, &c.PasswordHash)
	return c, classify(err)
}

func (d *Store) LockCredentials(ctx context.Context, id int64, hash string) (bool, error) {
	if !d.inTx {
		return false, ErrInvalid
	}
	var valid bool
	err := d.q.QueryRowContext(ctx, `SELECT is_active AND password_hash=$2 FROM users WHERE id=$1 FOR SHARE`, id, hash).Scan(&valid)
	return valid, classify(err)
}

func (d *Store) UsersExist(ctx context.Context) (bool, error) {
	var exists bool
	err := d.q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users)`).Scan(&exists)
	return exists, classify(err)
}

func (d *Store) IsActiveAdmin(ctx context.Context, id int64) (bool, error) {
	var allowed bool
	err := d.q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND is_active AND is_platform_admin)`, id).Scan(&allowed)
	return allowed, classify(err)
}

func (d *Store) ActiveAdminCount(ctx context.Context) (int, error) {
	var count int
	err := d.q.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE is_platform_admin AND is_active`).Scan(&count)
	return count, classify(err)
}

func (d *Store) CreateUser(ctx context.Context, username, display, passwordHash string, admin bool, creator *int64) error {
	_, err := d.q.ExecContext(ctx, `INSERT INTO users(username,display_name,password_hash,is_platform_admin,created_by) VALUES($1,$2,$3,$4,$5)`, username, display, passwordHash, admin, creator)
	return classify(err)
}

func (d *Store) UpdateUser(ctx context.Context, id int64, display string, admin, active bool, passwordHash string) error {
	return affected(d.q.ExecContext(ctx, `UPDATE users SET display_name=$2,is_platform_admin=$3,is_active=$4,password_hash=CASE WHEN $5='' THEN password_hash ELSE $5 END,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, id, display, admin, active, passwordHash))
}

func (d *Store) Users(ctx context.Context) ([]User, error) {
	rows, err := d.q.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY id`)
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
