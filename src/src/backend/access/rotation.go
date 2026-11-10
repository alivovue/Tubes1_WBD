package access

import (
	"context"
	"errors"
	"time"

	"schakker_shared/auth"

	"tubes1_wbd/database/dao"
)

func (s *Service) rotationToken(previousHash []byte) string {
	return auth.RotationToken(s.config.Key, previousHash)
}
func (s *Service) rotateSession(ctx context.Context, store *dao.Store, session dao.Session, hash []byte, now time.Time) (string, error) {
	replacement := s.rotationToken(hash)
	if err := store.ReplaceSessionToken(ctx, hash, now, auth.MinTime(now.Add(s.config.GracePeriod), session.AbsoluteExpires)); err != nil {
		return "", err
	}
	if err := store.InsertSessionToken(ctx, session.ID, auth.TokenHash(replacement), now); err != nil {
		return "", err
	}
	return replacement, nil
}
func (s *Service) replacementForGraceToken(ctx context.Context, store *dao.Store, sessionID string, hash []byte, now time.Time) (string, error) {
	for range 64 {
		replacement := s.rotationToken(hash)
		hash = auth.TokenHash(replacement)
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
