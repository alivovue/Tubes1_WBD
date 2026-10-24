// Package access implements Schakker's platform authentication and group-based authorization.
package access

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrUnauthenticated = errors.New("silakan login kembali")
	ErrForbidden       = errors.New("akses ditolak")
	ErrInvalid         = errors.New("input tidak valid")
	ErrConflict        = errors.New("data sudah ada atau masih digunakan")
	ErrNotFound        = errors.New("data tidak ditemukan")
	usernamePattern    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{2,99}$`)
)

type Permission string

const (
	NoAccess Permission = ""
	ViewOnly Permission = "view_only"
	ViewEdit Permission = "view_edit"
)

func (p Permission) Allows(required Permission) bool {
	return (required == ViewOnly && (p == ViewOnly || p == ViewEdit)) || (required == ViewEdit && p == ViewEdit)
}

type Config struct {
	// Key is a persistent 32-byte secret, shared by application instances.
	Key                                                          []byte
	SecureCookies                                                bool
	IdleTimeout, AbsoluteLifetime, RotationInterval, GracePeriod time.Duration
	// Now overrides the clock. Leave nil to use time.Now.
	Now func() time.Time
}

// DefaultConfig documents the session policy: 30-minute idle timeout, 8-hour
// absolute lifetime, 15-minute rotation interval, and 30-second grace period.
// Production callers set secure=true so authentication cookies use Secure.
func DefaultConfig(key []byte, secure bool) Config {
	return Config{Key: key, SecureCookies: secure, IdleTimeout: 30 * time.Minute,
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

func encryptSecret(key []byte, value string) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, []byte(value), []byte("schakker/target-password/v1")), nil
}

func decryptSecret(key, value []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(value) < gcm.NonceSize() {
		return "", errors.New("invalid encrypted credential")
	}
	plain, err := gcm.Open(nil, value[:gcm.NonceSize()], value[gcm.NonceSize():], []byte("schakker/target-password/v1"))
	return string(plain), err
}

func cleanName(s string, max int) bool { return strings.TrimSpace(s) != "" && len(s) <= max }
