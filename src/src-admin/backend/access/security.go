package access

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"schakker_shared/auth"

	"tubes1_wbd_admin/database/dao"
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
	Logger   *slog.Logger
	Observer interface {
		Start(context.Context, string, int64, int64) (context.Context, func(string))
	}
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

func cleanName(s string, max int) bool { return strings.TrimSpace(s) != "" && len(s) <= max }

func HashPassword(password string) (string, error) { return auth.HashPassword(password) }
