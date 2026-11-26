package auth

import (
	"context"
	"errors"
	"time"
)

type TokenStore interface {
	ReplaceSessionToken(context.Context, []byte, time.Time, time.Time) error
	InsertSessionToken(context.Context, string, []byte, time.Time) error
	SessionTokenForSession(context.Context, string, []byte) (SessionToken, error)
}

// Rotation operates on the owning application's transaction after that caller
// has locked and validated the session. It never opens or commits a transaction.
type Rotation struct {
	Key         []byte
	GracePeriod time.Duration
	NotFound    error
}

func (r Rotation) Rotate(ctx context.Context, store TokenStore, sessionID string, absolute time.Time, hash []byte, now time.Time) (string, error) {
	replacement := RotationToken(r.Key, hash)
	if err := store.ReplaceSessionToken(ctx, hash, now, MinTime(now.Add(r.GracePeriod), absolute)); err != nil {
		return "", err
	}
	if err := store.InsertSessionToken(ctx, sessionID, TokenHash(replacement), now); err != nil {
		return "", err
	}
	return replacement, nil
}

func (r Rotation) Recover(ctx context.Context, store TokenStore, sessionID string, hash []byte, now time.Time) (string, error) {
	for range 64 {
		replacement := RotationToken(r.Key, hash)
		hash = TokenHash(replacement)
		token, err := store.SessionTokenForSession(ctx, sessionID, hash)
		if err != nil {
			if errors.Is(err, r.NotFound) {
				return "", nil
			}
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
