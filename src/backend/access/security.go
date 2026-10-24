package access

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"tubes1_wbd/database/dao"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrUnauthenticated = errors.New("silakan login kembali")
	ErrForbidden       = errors.New("akses ditolak")
	ErrInvalid         = dao.ErrInvalid
	ErrConflict        = dao.ErrConflict
	ErrNotFound        = dao.ErrNotFound
	usernamePattern    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{2,99}$`)
)

type Permission = dao.Permission

const (
	NoAccess = dao.NoAccess
	ViewOnly = dao.ViewOnly
	ViewEdit = dao.ViewEdit
)

type Config struct {
	Key                                                          []byte
	IdleTimeout, AbsoluteLifetime, RotationInterval, GracePeriod time.Duration
	Now                                                          func() time.Time
}

func DefaultConfig(key []byte) Config {
	return Config{Key: key, IdleTimeout: 30 * time.Minute,
		AbsoluteLifetime: 8 * time.Hour, RotationInterval: 15 * time.Minute, GracePeriod: 30 * time.Second}
}

func (c *Config) validate() error {
	if len(c.Key) != 32 || c.IdleTimeout <= 0 || c.AbsoluteLifetime <= 0 || c.RotationInterval <= 0 || c.GracePeriod <= 0 || c.GracePeriod >= c.RotationInterval {
		return errors.New("invalid access configuration: require a 32-byte key and positive session durations (grace < rotation)")
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return nil
}

func HashPassword(password string) (string, error) {
	if len(password) < 12 || len(password) > 72 {
		return "", fmt.Errorf("%w: password harus 12–72 byte", ErrInvalid)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(hash), err
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func tokenHash(raw string) []byte { sum := sha256.Sum256([]byte(raw)); return sum[:] }

func sign(key []byte, purpose, value string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(purpose + "\x00" + value))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func cleanName(s string, max int) bool { return strings.TrimSpace(s) != "" && len(s) <= max }
