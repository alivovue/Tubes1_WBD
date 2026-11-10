package access

import (
	"context"
	"errors"
	"time"

	"tubes1_wbd/database/dao"
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
	user, err := s.authority.AuthenticateUser(ctx, username, password)
	if err != nil {
		return Identity{}, "", err
	}
	if !user.Active || user.ID < 1 || user.Version < 1 {
		return Identity{}, "", ErrUnauthenticated
	}

	raw, err := randomToken()
	if err != nil {
		return Identity{}, "", err
	}
	var identity Identity
	err = s.store.Transaction(ctx, func(tx *dao.Store) error {
		if err := tx.RememberUser(ctx, user); err != nil {
			return err
		}

		now := s.config.Now()
		absolute := now.Add(s.config.AbsoluteLifetime)
		if oldToken != "" {
			if err := tx.RevokeTokenSession(ctx, tokenHash(oldToken), now); err != nil {
				return err
			}
		}
		id, err := tx.CreateSession(ctx, user.ID, now, minTime(now.Add(s.config.IdleTimeout), absolute), absolute, user.Version, user.Rotation)
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
	var sessionID string
	var invalidate bool
	err := s.store.Transaction(ctx, func(tx *dao.Store) error {
		session, err := tx.LockSession(ctx, tokenHash(raw))
		if err != nil {
			return authenticationError(err)
		}
		sessionID = session.ID
		now := s.config.Now()
		token, err := tx.SessionToken(ctx, tokenHash(raw))
		if err != nil {
			return authenticationError(err)
		}
		if session.Revoked || !now.Before(session.IdleExpires) || !now.Before(session.AbsoluteExpires) || (token.ReplacedAt != nil && (token.GraceExpiresAt == nil || !now.Before(*token.GraceExpiresAt))) {
			return ErrUnauthenticated
		}
		user, err := s.authority.User(ctx, session.UserID)
		if err != nil {
			invalidate = errors.Is(err, ErrNotFound) || errors.Is(err, ErrForbidden)
			return authenticationError(err)
		}
		if !user.Active || user.ID != session.UserID || user.Version != session.Version {
			invalidate = true
			return ErrUnauthenticated
		}
		if token.ReplacedAt == nil && (!now.Before(token.IssuedAt.Add(s.config.RotationInterval)) || user.Rotation != session.Rotation) {
			replacement, err = s.rotateSession(ctx, tx, session, tokenHash(raw), now)
		} else if token.ReplacedAt != nil {
			replacement, err = s.replacementForGraceToken(ctx, tx, session.ID, tokenHash(raw), now)
		}
		if err != nil {
			return err
		}
		if user.Rotation != session.Rotation {
			// A grace token delivers the current token first; the next use of that
			// current token performs any pending privilege-change rotation.
			if token.ReplacedAt == nil {
				if err := tx.SessionRotationVersion(ctx, session.ID, user.Rotation); err != nil {
					return err
				}
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
		if invalidate && sessionID != "" {
			if revokeErr := s.store.RevokeSession(ctx, sessionID, s.config.Now()); revokeErr != nil {
				return Identity{}, "", revokeErr
			}
		}
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
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrForbidden) {
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
