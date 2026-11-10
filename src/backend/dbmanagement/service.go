package dbmanagement

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"time"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement/adapters"
	"tubes1_wbd/backend/dbmanagement/adapters/mariadb"
	"tubes1_wbd/backend/dbmanagement/adapters/postgres"
	"tubes1_wbd/database/dao"
)

var (
	ErrConnection  = adapters.ErrConnection
	ErrQuery       = adapters.ErrQuery
	ErrInvalid     = access.ErrInvalid
	ErrForbidden   = access.ErrForbidden
	ErrConflict    = access.ErrConflict
	ErrNotFound    = access.ErrNotFound
	ErrUnsupported = adapters.ErrUnsupported
)

type Config struct {
	Key                          []byte
	Production                   bool
	ConnectTimeout, QueryTimeout time.Duration
}

func DefaultConfig(key []byte, production bool) Config {
	return Config{Key: key, Production: production, ConnectTimeout: 5 * time.Second, QueryTimeout: 30 * time.Second}
}

type Service struct {
	store    *dao.Store
	access   *access.Service
	config   Config
	registry *adapters.Registry
}

func NewService(store *dao.Store, auth *access.Service, config Config) (*Service, error) {
	if store == nil || auth == nil || len(config.Key) != 32 || config.ConnectTimeout <= 0 || config.QueryTimeout <= 0 {
		return nil, ErrInvalid
	}
	config.Key = append([]byte(nil), config.Key...)
	registry, err := adapters.NewRegistry(postgres.New(), mariadb.New())
	if err != nil {
		return nil, err
	}
	return &Service{store: store, access: auth, config: config, registry: registry}, nil
}
func (s *Service) SupportedDBMS() []string { return s.registry.Supported() }
func (s *Service) Databases(ctx context.Context, actor int64) ([]dao.Database, error) {
	return s.access.AccessibleDatabases(ctx, actor)
}
func (s *Service) SignReview(value string) string {
	mac := hmac.New(sha256.New, s.config.Key)
	mac.Write([]byte("schakker/migration-review/v1\x00" + value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
