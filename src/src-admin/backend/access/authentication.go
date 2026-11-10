package access

import (
	"context"
	"errors"
	"time"

	"schakker_shared/auth"

	"tubes1_wbd_admin/database/dao"
)

const CookieName = "schakker_admin_session"

type Identity struct {
	User            User      `json:"user"`
	SessionID       string    `json:"-"`
	AbsoluteExpires time.Time `json:"-"`
}

func (s *Service) Login(ctx context.Context, username, password, oldToken string) (Identity, string, error) {
	if len(username) > 100 || len(password) > 72 {
		return Identity{}, "", ErrUnauthenticated
	}
	credentials, err := s.store.AdminCredentials(ctx, username)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Identity{}, "", err
	}
	compare := []byte(credentials.PasswordHash)
	if err != nil {
		compare = s.dummyHash
	}
	valid := auth.VerifyPassword(string(compare), password)
	if !valid || err != nil || !credentials.Active {
		return Identity{}, "", ErrUnauthenticated
	}
	raw, err := auth.RandomToken()
	if err != nil {
		return Identity{}, "", err
	}
	var identity Identity
	err = s.store.Transaction(ctx, func(tx *dao.Store) error {
		valid, err := tx.LockAdminCredentials(ctx, credentials.ID, credentials.PasswordHash)
		if err != nil {
			return err
		}
		if !valid {
			return ErrUnauthenticated
		}
		user, err := tx.Admin(ctx, credentials.ID)
		if err != nil {
			return err
		}
		now := s.config.Now()
		absolute := now.Add(s.config.AbsoluteLifetime)
		if oldToken != "" {
			if err := tx.RevokeTokenSession(ctx, auth.TokenHash(oldToken), now); err != nil {
				return err
			}
		}
		id, err := tx.CreateSession(ctx, user.ID, now, auth.MinTime(now.Add(s.config.IdleTimeout), absolute), absolute)
		if err != nil {
			return err
		}
		if err := tx.InsertSessionToken(ctx, id, auth.TokenHash(raw), now); err != nil {
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
		session, err := tx.LockSession(ctx, auth.TokenHash(raw))
		if err != nil {
			return authenticationError(err)
		}
		now := s.config.Now()
		token, err := tx.SessionToken(ctx, auth.TokenHash(raw))
		if err != nil {
			return authenticationError(err)
		}
		if session.Revoked || !now.Before(session.IdleExpires) || !now.Before(session.AbsoluteExpires) || (token.ReplacedAt != nil && (token.GraceExpiresAt == nil || !now.Before(*token.GraceExpiresAt))) {
			return ErrUnauthenticated
		}
		user, err := tx.Admin(ctx, session.UserID)
		if err != nil {
			return authenticationError(err)
		}
		if !user.Active {
			return ErrUnauthenticated
		}
		if token.ReplacedAt == nil && !now.Before(token.IssuedAt.Add(s.config.RotationInterval)) {
			replacement, err = s.rotateSession(ctx, tx, session, auth.TokenHash(raw), now)
		} else if token.ReplacedAt != nil {
			replacement, err = s.replacementForGraceToken(ctx, tx, session.ID, auth.TokenHash(raw), now)
		}
		if err != nil {
			return err
		}
		if err := tx.TouchSession(ctx, session.ID, now, auth.MinTime(now.Add(s.config.IdleTimeout), session.AbsoluteExpires)); err != nil {
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
