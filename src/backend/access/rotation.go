package access

import (
	"context"
	"encoding/base64"
	"errors"
	"time"

	"tubes1_wbd/database/dao"
)

func (s *Service) rotationToken(previousHash []byte) string {
	return sign(s.config.Key, "session-rotation", base64.RawURLEncoding.EncodeToString(previousHash))
}
func (s *Service) rotateSession(ctx context.Context, store *dao.Store, session dao.Session, hash []byte, now time.Time) (string, error) {
	replacement := s.rotationToken(hash)
	if err := store.ReplaceSessionToken(ctx, hash, now, minTime(now.Add(s.config.GracePeriod), session.AbsoluteExpires)); err != nil {
		return "", err
	}
	if err := store.InsertSessionToken(ctx, session.ID, tokenHash(replacement), now); err != nil {
		return "", err
	}
	return replacement, nil
}
func (s *Service) rotateSessions(ctx context.Context, store *dao.Store, sessions []dao.Session, now time.Time) error {
	for _, session := range sessions {
		hash, err := store.CurrentSessionTokenHash(ctx, session.ID)
		if err != nil {
			return err
		}
		if _, err := s.rotateSession(ctx, store, session, hash, now); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) rotateUserSessions(ctx context.Context, store *dao.Store, user int64) error {
	now := s.config.Now()
	sessions, err := store.LockUserSessions(ctx, user, now)
	if err != nil {
		return err
	}
	return s.rotateSessions(ctx, store, sessions, now)
}
func (s *Service) rotateGroupSessions(ctx context.Context, store *dao.Store, group int64) error {
	now := s.config.Now()
	sessions, err := store.LockGroupSessions(ctx, group, now)
	if err != nil {
		return err
	}
	return s.rotateSessions(ctx, store, sessions, now)
}
func (s *Service) replacementForGraceToken(ctx context.Context, store *dao.Store, sessionID string, hash []byte, now time.Time) (string, error) {
	for range 64 {
		replacement := s.rotationToken(hash)
		hash = tokenHash(replacement)
		token, err := store.SessionTokenForSession(ctx, sessionID, hash)
		if errors.Is(err, ErrNotFound) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		if token.ReplacedAt == nil {
			return replacement, nil
		}
		if token.GraceExpiresAt == nil || !now.Before(*token.GraceExpiresAt) {
			return "", ErrUnauthenticated
		}
	}
	return "", ErrUnauthenticated
}
func permissionIncreased(previous, next Permission) bool {
	return previous == NoAccess && (next == ViewOnly || next == ViewEdit) || previous == ViewOnly && next == ViewEdit
}
