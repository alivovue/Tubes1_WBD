package auth_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"schakker_shared/auth"
)

type tokenStore struct {
	lookup  func(string, []byte) (auth.SessionToken, error)
	replace func([]byte, time.Time, time.Time) error
	insert  func(string, []byte, time.Time) error
}

func (s tokenStore) SessionTokenForSession(_ context.Context, id string, hash []byte) (auth.SessionToken, error) {
	return s.lookup(id, hash)
}
func (s tokenStore) ReplaceSessionToken(_ context.Context, hash []byte, now, grace time.Time) error {
	return s.replace(hash, now, grace)
}
func (s tokenStore) InsertSessionToken(_ context.Context, id string, hash []byte, now time.Time) error {
	return s.insert(id, hash, now)
}

func TestRotationBoundsGraceAndPropagatesPersistenceFailure(t *testing.T) {
	now := time.Unix(1800000000, 0)
	absolute := now.Add(5 * time.Second)
	key, hash := bytes.Repeat([]byte{1}, 32), auth.TokenHash("old")
	failure := errors.New("storage unavailable")
	for _, failAt := range []string{"", "replace", "insert"} {
		t.Run(failAt, func(t *testing.T) {
			inserted := false
			store := tokenStore{
				replace: func(got []byte, issued, grace time.Time) error {
					if !bytes.Equal(got, hash) || !issued.Equal(now) || !grace.Equal(absolute) {
						t.Fatal("rotation changed the original hash or exceeded absolute expiration")
					}
					if failAt == "replace" {
						return failure
					}
					return nil
				},
				insert: func(id string, got []byte, issued time.Time) error {
					inserted = true
					if id != "own-session" || !bytes.Equal(got, auth.TokenHash(auth.RotationToken(key, hash))) || !issued.Equal(now) {
						t.Fatal("replacement token was not scoped to the owning session")
					}
					if failAt == "insert" {
						return failure
					}
					return nil
				},
			}
			rotation := auth.Rotation{Key: key, GracePeriod: 30 * time.Second}
			raw, err := rotation.Rotate(context.Background(), store, "own-session", absolute, hash, now)
			if failAt != "" {
				if raw != "" || !errors.Is(err, failure) || (failAt == "replace" && inserted) {
					t.Fatal("rotation concealed a persistence failure or returned an unpersisted token")
				}
			} else if err != nil || raw != auth.RotationToken(key, hash) {
				t.Fatalf("rotation failed: %v", err)
			}
		})
	}
}

func TestGraceRecoveryIsScopedAndBounded(t *testing.T) {
	now := time.Unix(1800000000, 0)
	future := now.Add(time.Second)
	missing, unavailable := errors.New("missing token"), errors.New("storage unavailable")
	key, originalHash := bytes.Repeat([]byte{2}, 32), auth.TokenHash("old")
	for _, test := range []struct {
		name    string
		length  int
		grace   *time.Time
		failure error
		wantErr error
	}{
		{"current token", 1, nil, nil, nil},
		{"multiple rotations", 3, &future, nil, nil},
		{"missing successor", 1, nil, fmt.Errorf("lookup: %w", missing), nil},
		{"storage failure", 1, nil, unavailable, unavailable},
		{"expired intermediate token", 2, &now, nil, auth.ErrUnauthenticated},
		{"intermediate token without grace", 2, nil, nil, auth.ErrUnauthenticated},
		{"excessive chain", 65, &future, nil, auth.ErrUnauthenticated},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			expected := originalHash
			var latest string
			store := tokenStore{lookup: func(id string, hash []byte) (auth.SessionToken, error) {
				calls++
				latest = auth.RotationToken(key, expected)
				expected = auth.TokenHash(latest)
				if id != "own-session" || !bytes.Equal(hash, expected) {
					t.Fatal("recovery changed session scope or used a different application's key")
				}
				if test.failure != nil {
					return auth.SessionToken{}, test.failure
				}
				token := auth.SessionToken{}
				if calls < test.length {
					token.ReplacedAt, token.GraceExpiresAt = &now, test.grace
				}
				return token, nil
			}}
			rotation := auth.Rotation{Key: key, NotFound: missing}
			raw, err := rotation.Recover(context.Background(), store, "own-session", originalHash, now)
			if !errors.Is(err, test.wantErr) || calls > 64 {
				t.Fatalf("unexpected recovery result after %d lookups: %v", calls, err)
			}
			if test.wantErr != nil || test.failure != nil {
				if raw != "" {
					t.Fatal("recovery returned an invalid token")
				}
			} else if raw != latest {
				t.Fatal("recovery did not return the latest valid token")
			}
		})
	}
}
