package access

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const CookieName = "schakker_session"

// Identity is server-side only. Never serialize SessionID or the opaque cookie value.
type Identity struct {
	User            User      `json:"user"`
	SessionID       string    `json:"-"`
	AbsoluteExpires time.Time `json:"-"`
}

type identityKey struct{}

func CurrentIdentity(ctx context.Context) (Identity, bool) {
	i, ok := ctx.Value(identityKey{}).(Identity)
	return i, ok
}

// Login replaces any previous session only after credentials have been verified.
func (s *Service) Login(ctx context.Context, username, password, oldToken string) (Identity, string, error) {
	var u User
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT `+userColumns+`,password_hash FROM users WHERE lower(username)=lower($1)`, username).Scan(&u.ID, &u.Username, &u.DisplayName, &u.IsAdmin, &u.Active, &hash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Identity{}, "", err
	}
	compare := []byte(hash)
	if err != nil {
		compare = s.dummyHash
	}
	valid := bcrypt.CompareHashAndPassword(compare, []byte(password)) == nil
	if !valid || err != nil || !u.Active {
		return Identity{}, "", ErrUnauthenticated
	}
	raw, err := randomToken()
	if err != nil {
		return Identity{}, "", err
	}
	now := s.config.Now()
	absolute := now.Add(s.config.AbsoluteLifetime)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Identity{}, "", err
	}
	defer tx.Rollback()
	// Lock the account and recheck the exact credential/state to close reset/disable races.
	var stillValid bool
	if err = tx.QueryRowContext(ctx, `SELECT is_active AND password_hash=$2 FROM users WHERE id=$1 FOR SHARE`, u.ID, hash).Scan(&stillValid); err != nil {
		return Identity{}, "", err
	}
	if !stillValid {
		return Identity{}, "", ErrUnauthenticated
	}
	// The role may have changed while bcrypt was running; fetch it under the account lock.
	u, err = scanUser(tx.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1`, u.ID))
	if err != nil {
		return Identity{}, "", err
	}
	if oldToken != "" {
		_, err = tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=$2 WHERE id IN (SELECT session_id FROM session_tokens WHERE token_hash=$1) AND revoked_at IS NULL`, tokenHash(oldToken), now)
		if err != nil {
			return Identity{}, "", err
		}
	}
	var sessionID string
	err = tx.QueryRowContext(ctx, `INSERT INTO sessions(user_id,created_at,last_seen_at,idle_expires_at,absolute_expires_at) VALUES($1,$2,$2,$3,$4) RETURNING id`, u.ID, now, minTime(now.Add(s.config.IdleTimeout), absolute), absolute).Scan(&sessionID)
	if err != nil {
		return Identity{}, "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO session_tokens(session_id,token_hash,issued_at) VALUES($1,$2,$3)`, sessionID, tokenHash(raw), now)
	if err != nil {
		return Identity{}, "", err
	}
	if err = tx.Commit(); err != nil {
		return Identity{}, "", err
	}
	return Identity{User: u, SessionID: sessionID, AbsoluteExpires: absolute}, raw, nil
}

// Authenticate locks the session, so concurrent requests cannot create two current tokens.
// A grace-token request never sets an old cookie back onto the browser.
func (s *Service) Authenticate(ctx context.Context, raw string) (Identity, string, error) {
	if len(raw) != 43 {
		return Identity{}, "", ErrUnauthenticated
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Identity{}, "", err
	}
	defer tx.Rollback()
	var i Identity
	var idle time.Time
	var revoked sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT s.id,s.user_id,s.idle_expires_at,s.absolute_expires_at,s.revoked_at FROM sessions s JOIN session_tokens t ON t.session_id=s.id WHERE t.token_hash=$1 FOR UPDATE OF s`, tokenHash(raw)).Scan(&i.SessionID, &i.User.ID, &idle, &i.AbsoluteExpires, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return Identity{}, "", ErrUnauthenticated
	}
	if err != nil {
		return Identity{}, "", err
	}
	// Evaluate expiry after acquiring the lock, including time spent waiting for it.
	now := s.config.Now()
	// Read token state again after obtaining the session lock (another request may have rotated).
	var issued time.Time
	var replaced, grace sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT issued_at,replaced_at,grace_expires_at FROM session_tokens WHERE token_hash=$1`, tokenHash(raw)).Scan(&issued, &replaced, &grace)
	if errors.Is(err, sql.ErrNoRows) {
		// A newer request may have cleaned up this expired grace token while we waited.
		return Identity{}, "", ErrUnauthenticated
	}
	if err != nil {
		return Identity{}, "", err
	}
	if revoked.Valid || !now.Before(idle) || !now.Before(i.AbsoluteExpires) || (replaced.Valid && (!grace.Valid || !now.Before(grace.Time))) {
		return Identity{}, "", ErrUnauthenticated
	}
	i.User, err = scanUser(tx.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1`, i.User.ID))
	if err != nil {
		return Identity{}, "", err
	}
	if !i.User.Active {
		return Identity{}, "", ErrUnauthenticated
	}
	var replacement string
	if !replaced.Valid && !now.Before(issued.Add(s.config.RotationInterval)) {
		replacement, err = randomToken()
		if err != nil {
			return Identity{}, "", err
		}
		_, err = tx.ExecContext(ctx, `UPDATE session_tokens SET replaced_at=$2,grace_expires_at=$3 WHERE token_hash=$1`, tokenHash(raw), now, minTime(now.Add(s.config.GracePeriod), i.AbsoluteExpires))
		if err != nil {
			return Identity{}, "", err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO session_tokens(session_id,token_hash,issued_at) VALUES($1,$2,$3)`, i.SessionID, tokenHash(replacement), now)
		if err != nil {
			return Identity{}, "", err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE sessions SET last_seen_at=$2,idle_expires_at=$3 WHERE id=$1`, i.SessionID, now, minTime(now.Add(s.config.IdleTimeout), i.AbsoluteExpires))
	if err != nil {
		return Identity{}, "", err
	}
	// Expired rotated tokens are not useful for authentication or audit.
	_, err = tx.ExecContext(ctx, `DELETE FROM session_tokens WHERE session_id=$1 AND grace_expires_at <= $2`, i.SessionID, now)
	if err != nil {
		return Identity{}, "", err
	}
	if err = tx.Commit(); err != nil {
		return Identity{}, "", err
	}
	return i, replacement, nil
}

func (s *Service) Logout(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET revoked_at=$2 WHERE id=$1 AND revoked_at IS NULL`, sessionID, s.config.Now())
	return err
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
