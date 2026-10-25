package dbmanagement

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement/adapters"
	"tubes1_wbd/backend/dbmanagement/adapters/mariadb"
	"tubes1_wbd/backend/dbmanagement/adapters/postgres"
	"tubes1_wbd/database/dao"
)

var (
	ErrConnection          = adapters.ErrConnection
	ErrQuery               = adapters.ErrQuery
	ErrInvalid             = access.ErrInvalid
	ErrForbidden           = access.ErrForbidden
	ErrConflict            = access.ErrConflict
	ErrNotFound            = access.ErrNotFound
	ErrUnsupported         = adapters.ErrUnsupported
	ErrDuplicateConnection = fmt.Errorf("%w: target database is already registered", ErrConflict)
)

type Config struct {
	Key            []byte
	Production     bool
	ConnectTimeout time.Duration
	QueryTimeout   time.Duration
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
func (s *Service) Connections(ctx context.Context, actor int64) ([]dao.Database, error) {
	if err := s.access.RequireAdmin(ctx, actor); err != nil {
		return nil, err
	}
	return s.store.Databases(ctx)
}
func (s *Service) encrypt(value string) ([]byte, error) {
	block, err := aes.NewCipher(s.config.Key)
	if err != nil {
		return nil, ErrConnection
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrConnection
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, ErrConnection
	}
	return gcm.Seal(nonce, nonce, []byte(value), []byte("schakker/target-password/v1")), nil
}

func (s *Service) decrypt(value []byte) (string, error) {
	block, err := aes.NewCipher(s.config.Key)
	if err != nil {
		return "", ErrConnection
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(value) < gcm.NonceSize() {
		return "", ErrConnection
	}
	plain, err := gcm.Open(nil, value[:gcm.NonceSize()], value[gcm.NonceSize():], []byte("schakker/target-password/v1"))
	if err != nil {
		return "", ErrConnection
	}
	return string(plain), nil
}

func (s *Service) SignReview(value string) string {
	mac := hmac.New(sha256.New, s.config.Key)
	mac.Write([]byte("schakker/migration-review/v1\x00" + value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
