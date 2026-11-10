package dao

import "context"

type scanner interface{ Scan(...any) error }

func (d *Store) User(ctx context.Context, id int64) (User, error) {
	var u User
	err := d.q.QueryRowContext(ctx, `SELECT id,username,display_name,is_active FROM users WHERE id=$1`, id).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Active)
	return u, classify(err)
}

// RememberUser stores only identity/display information needed by local foreign
// keys and history. It is never used as an authentication or permission source.
func (d *Store) RememberUser(ctx context.Context, u User) error {
	result, err := d.q.ExecContext(ctx, `INSERT INTO users(id,username,display_name,is_active) VALUES($1,$2,$3,$4) ON CONFLICT(id) DO UPDATE SET username=EXCLUDED.username,display_name=EXCLUDED.display_name,is_active=EXCLUDED.is_active WHERE users.username=EXCLUDED.username`, u.ID, u.Username, u.DisplayName, u.Active)
	if err != nil {
		return classify(err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrConflict
	}
	return nil
}
