package auth

import (
	"errors"
	"time"
)

var (
	ErrUnauthenticated = errors.New("silakan login kembali")
	ErrForbidden       = errors.New("akses ditolak")
)

// Identity exposes only the session metadata needed by common HTTP and CSRF
// handling. Each application supplies and validates its own user identity.
type Identity interface {
	SessionInfo() SessionInfo
}

type SessionInfo struct {
	UserID          int64
	SessionID       string
	AbsoluteExpires time.Time
}

type SessionToken struct {
	IssuedAt       time.Time
	ReplacedAt     *time.Time
	GraceExpiresAt *time.Time
}

// CSRF uses the owning application's key and clock. It has no storage or
// authority access and does not make role or permission decisions.
type CSRF[I Identity] struct {
	key []byte
	now func() time.Time
}

func NewCSRF[I Identity](key []byte, now func() time.Time) CSRF[I] {
	return CSRF[I]{key: append([]byte(nil), key...), now: now}
}

func (c CSRF[I]) NewLoginChallenge() (LoginChallenge, error) {
	return NewLoginChallenge(c.key, c.now())
}

func (c CSRF[I]) ValidateLoginCSRF(cookie, provided string) error {
	if !ValidLoginCSRF(c.key, c.now(), cookie, provided) {
		return ErrForbidden
	}
	return nil
}

func (c CSRF[I]) CSRFToken(identity I) (string, error) {
	session := identity.SessionInfo()
	if session.UserID < 1 || session.SessionID == "" || !c.now().Before(session.AbsoluteExpires) {
		return "", ErrUnauthenticated
	}
	return SessionCSRF(c.key, session.SessionID), nil
}

func (c CSRF[I]) ValidateCSRF(identity I, provided string) error {
	expected, err := c.CSRFToken(identity)
	if err != nil {
		return err
	}
	if !Equal(provided, expected) {
		return ErrForbidden
	}
	return nil
}
