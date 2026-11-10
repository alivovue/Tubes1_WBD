package access

import (
	"errors"
	"time"

	"tubes1_wbd/database/dao"
)

var (
	ErrUnauthenticated = errors.New("silakan login kembali")
	ErrForbidden       = errors.New("akses ditolak")
	ErrInvalid         = dao.ErrInvalid
	ErrConflict        = dao.ErrConflict
	ErrNotFound        = dao.ErrNotFound
)

type Permission = dao.Permission

const (
	NoAccess = dao.NoAccess
	ViewOnly = dao.ViewOnly
	ViewEdit = dao.ViewEdit
)

type Config struct {
	Authority                                                    Authority
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
