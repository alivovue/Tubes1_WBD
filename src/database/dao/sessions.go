package dao

import (
	"context"
	"time"
)

func (d *Store) CreateSession(ctx context.Context, user int64, now, idle, absolute time.Time) (string, error) {
	var id string
	err := d.q.QueryRowContext(ctx, `INSERT INTO sessions(user_id,created_at,last_seen_at,idle_expires_at,absolute_expires_at) VALUES($1,$2,$2,$3,$4) RETURNING id`, user, now, idle, absolute).Scan(&id)
	return id, classify(err)
}

func (d *Store) InsertSessionToken(ctx context.Context, sessionID string, hash []byte, issued time.Time) error {
	_, err := d.q.ExecContext(ctx, `INSERT INTO session_tokens(session_id,token_hash,issued_at) VALUES($1,$2,$3)`, sessionID, hash, issued)
	return classify(err)
}

func (d *Store) LockSession(ctx context.Context, hash []byte) (Session, error) {
	var session Session
	if !d.inTx {
		return session, ErrInvalid
	}
	err := d.q.QueryRowContext(ctx, `SELECT s.id,s.user_id,s.idle_expires_at,s.absolute_expires_at,s.revoked_at IS NOT NULL FROM sessions s JOIN session_tokens t ON t.session_id=s.id WHERE t.token_hash=$1 FOR UPDATE OF s`, hash).Scan(&session.ID, &session.UserID, &session.IdleExpires, &session.AbsoluteExpires, &session.Revoked)
	return session, classify(err)
}

func (d *Store) SessionToken(ctx context.Context, hash []byte) (SessionToken, error) {
	var token SessionToken
	err := d.q.QueryRowContext(ctx, `SELECT issued_at,replaced_at,grace_expires_at FROM session_tokens WHERE token_hash=$1`, hash).Scan(&token.IssuedAt, &token.ReplacedAt, &token.GraceExpiresAt)
	return token, classify(err)
}

func (d *Store) ReplaceSessionToken(ctx context.Context, hash []byte, now, grace time.Time) error {
	return affected(d.q.ExecContext(ctx, `UPDATE session_tokens SET replaced_at=$2,grace_expires_at=$3 WHERE token_hash=$1`, hash, now, grace))
}

func (d *Store) TouchSession(ctx context.Context, id string, now, idle time.Time) error {
	return affected(d.q.ExecContext(ctx, `UPDATE sessions SET last_seen_at=$2,idle_expires_at=$3 WHERE id=$1`, id, now, idle))
}

func (d *Store) DeleteExpiredTokens(ctx context.Context, id string, now time.Time) error {
	_, err := d.q.ExecContext(ctx, `DELETE FROM session_tokens WHERE session_id=$1 AND grace_expires_at <= $2`, id, now)
	return classify(err)
}

func (d *Store) RevokeSession(ctx context.Context, id string, now time.Time) error {
	_, err := d.q.ExecContext(ctx, `UPDATE sessions SET revoked_at=$2 WHERE id=$1 AND revoked_at IS NULL`, id, now)
	return classify(err)
}

func (d *Store) RevokeTokenSession(ctx context.Context, hash []byte, now time.Time) error {
	_, err := d.q.ExecContext(ctx, `UPDATE sessions SET revoked_at=$2 WHERE id IN (SELECT session_id FROM session_tokens WHERE token_hash=$1) AND revoked_at IS NULL`, hash, now)
	return classify(err)
}

func (d *Store) RevokeUserSessions(ctx context.Context, user int64, now time.Time) error {
	_, err := d.q.ExecContext(ctx, `UPDATE sessions SET revoked_at=$2 WHERE revoked_at IS NULL AND user_id=$1`, user, now)
	return classify(err)
}

func (d *Store) RevokeGroupSessions(ctx context.Context, group int64, now time.Time) error {
	_, err := d.q.ExecContext(ctx, `UPDATE sessions SET revoked_at=$2 WHERE revoked_at IS NULL AND user_id IN (SELECT user_id FROM user_groups WHERE group_id=$1)`, group, now)
	return classify(err)
}

func (d *Store) RevokeDatabaseSessions(ctx context.Context, database int64, now time.Time) error {
	_, err := d.q.ExecContext(ctx, `UPDATE sessions SET revoked_at=$2 WHERE revoked_at IS NULL AND user_id IN (SELECT m.user_id FROM user_groups m JOIN group_database_permissions p ON p.group_id=m.group_id WHERE p.database_id=$1)`, database, now)
	return classify(err)
}
