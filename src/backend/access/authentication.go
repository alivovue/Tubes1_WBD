package access

import (
	"context"
	"errors"
	"time"

	"tubes1_wbd/database/dao"

	"golang.org/x/crypto/bcrypt"
)

const CookieName = "schakker_session"

type Identity struct {
	User            User      `json:"user"`
	SessionID       string    `json:"-"`
	AbsoluteExpires time.Time `json:"-"`
}

func (s *Service) Login(ctx context.Context, username, password, oldToken string) (Identity, string, error) {
	if len(username) > 100 || len(password) > 72 {
		return Identity{}, "", ErrUnauthenticated
	}
	credentials, err := s.store.Credentials(ctx, username)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Identity{}, "", err
	}
	compare := []byte(credentials.PasswordHash)
	if err != nil {
		compare = s.dummyHash
	}
	valid := bcrypt.CompareHashAndPassword(compare, []byte(password)) == nil
	if !valid || err != nil || !credentials.Active {
		return Identity{}, "", ErrUnauthenticated
	}
	raw, err := randomToken()
	if err != nil {
		return Identity{}, "", err
	}
	var identity Identity
	err = s.store.Transaction(ctx, func(tx *dao.Store) error {
		valid, err := tx.LockCredentials(ctx, credentials.ID, credentials.PasswordHash)
		if err != nil {
			return err
		}
		if !valid {
			return ErrUnauthenticated
		}
		user, err := tx.User(ctx, credentials.ID)
		if err != nil {
			return err
		}
		now := s.config.Now()
		absolute := now.Add(s.config.AbsoluteLifetime)
		if oldToken != "" {
			if err := tx.RevokeTokenSession(ctx, tokenHash(oldToken), now); err != nil {
				return err
			}
		}
		id, err := tx.CreateSession(ctx, user.ID, now, minTime(now.Add(s.config.IdleTimeout), absolute), absolute)
		if err != nil {
			return err
		}
		if err := tx.InsertSessionToken(ctx, id, tokenHash(raw), now); err != nil {
			return err
		}
		identity = Identity{User: user, SessionID: id, AbsoluteExpires: absolute}
		return nil
	})
	if err != nil {
		return Identity{}, "", err
	}
	return identity, raw, nil
}

func (s *Service) Authenticate(ctx context.Context, raw string) (Identity, string, error) {
	if len(raw) != 43 {
		return Identity{}, "", ErrUnauthenticated
	}
	var identity Identity
	var replacement string
	err := s.store.Transaction(ctx, func(tx *dao.Store) error {
		session, err := tx.LockSession(ctx, tokenHash(raw))
		if err != nil {
			return authenticationError(err)
		}
		now := s.config.Now()
		token, err := tx.SessionToken(ctx, tokenHash(raw))
		if err != nil {
			return authenticationError(err)
		}
		if session.Revoked || !now.Before(session.IdleExpires) || !now.Before(session.AbsoluteExpires) || (token.ReplacedAt != nil && (token.GraceExpiresAt == nil || !now.Before(*token.GraceExpiresAt))) {
			return ErrUnauthenticated
		}
		user, err := tx.User(ctx, session.UserID)
		if err != nil {
			return authenticationError(err)
		}
		if !user.Active {
			return ErrUnauthenticated
		}
		if token.ReplacedAt == nil && !now.Before(token.IssuedAt.Add(s.config.RotationInterval)) {
			replacement, err = randomToken()
			if err != nil {
				return err
			}
			if err := tx.ReplaceSessionToken(ctx, tokenHash(raw), now, minTime(now.Add(s.config.GracePeriod), session.AbsoluteExpires)); err != nil {
				return err
			}
			if err := tx.InsertSessionToken(ctx, session.ID, tokenHash(replacement), now); err != nil {
				return err
			}
		}
		if err := tx.TouchSession(ctx, session.ID, now, minTime(now.Add(s.config.IdleTimeout), session.AbsoluteExpires)); err != nil {
			return err
		}
		if err := tx.DeleteExpiredTokens(ctx, session.ID, now); err != nil {
			return err
		}
		identity = Identity{User: user, SessionID: session.ID, AbsoluteExpires: session.AbsoluteExpires}
		return nil
	})
	if err != nil {
		return Identity{}, "", err
	}
	return identity, replacement, nil
}

func (s *Service) Logout(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return ErrUnauthenticated
	}
	return s.store.RevokeSession(ctx, sessionID, s.config.Now())
}

func authenticationError(err error) error {
	if errors.Is(err, ErrNotFound) {
		return ErrUnauthenticated
	}
	return err
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
