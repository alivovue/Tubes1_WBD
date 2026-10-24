package dbmanagement

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/database/dao"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

var (
	ErrConnection = errors.New("target PostgreSQL connection is unavailable")
	ErrQuery      = errors.New("target PostgreSQL operation failed")
	ErrInvalid    = access.ErrInvalid
	ErrForbidden  = access.ErrForbidden
	ErrConflict   = access.ErrConflict
	ErrNotFound   = access.ErrNotFound
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
	store  *dao.Store
	access *access.Service
	config Config
}

type ConnectionInput struct {
	Name         string
	Host         string
	Port         int
	DatabaseName string
	Username     string
	Password     string
	SSLMode      string
	Active       bool
}

func NewService(store *dao.Store, auth *access.Service, config Config) (*Service, error) {
	if store == nil || auth == nil || len(config.Key) != 32 || config.ConnectTimeout <= 0 || config.QueryTimeout <= 0 {
		return nil, ErrInvalid
	}
	config.Key = append([]byte(nil), config.Key...)
	return &Service{store: store, access: auth, config: config}, nil
}

func (s *Service) Databases(ctx context.Context, actor int64) ([]dao.Database, error) {
	return s.access.AccessibleDatabases(ctx, actor)
}

func (s *Service) Connections(ctx context.Context, actor int64) ([]dao.Database, error) {
	if err := s.access.RequireAdmin(ctx, actor); err != nil {
		return nil, err
	}
	return s.store.Databases(ctx)
}

func (s *Service) TestConnection(ctx context.Context, actor, id int64, input ConnectionInput) error {
	if err := s.access.RequireAdmin(ctx, actor); err != nil {
		return err
	}
	in, err := s.prepare(ctx, s.store, id, input)
	if err != nil {
		return err
	}
	db, err := s.connect(ctx, in)
	if err != nil {
		return err
	}
	return db.Close()
}

func (s *Service) SaveConnection(ctx context.Context, actor, id int64, input ConnectionInput) (dao.Database, error) {
	if err := s.access.RequireAdmin(ctx, actor); err != nil {
		return dao.Database{}, err
	}
	var saved dao.Database
	err := s.store.Transaction(ctx, func(tx *dao.Store) error {
		if err := tx.LockAdministration(ctx); err != nil {
			return err
		}
		allowed, err := tx.IsActiveAdmin(ctx, actor)
		if err != nil {
			return err
		}
		if !allowed {
			return ErrForbidden
		}
		if id > 0 {
			if err := tx.LockDatabase(ctx, id); err != nil {
				return err
			}
		}
		in, err := s.prepare(ctx, tx, id, input)
		if err != nil {
			return err
		}
		if id > 0 {
			old, err := tx.DatabaseMetadata(ctx, id)
			if err != nil {
				return err
			}
			hasHistory, err := tx.HasMigrationHistory(ctx, id)
			if err != nil {
				return err
			}
			if hasHistory && (old.Host != in.Host || old.Port != in.Port || old.DatabaseName != in.DatabaseName) {
				return ErrConflict
			}
		}
		db, err := s.connect(ctx, in)
		if err != nil {
			return err
		}
		db.Close()
		encrypted, err := s.encrypt(in.Password)
		if err != nil {
			return err
		}
		metadata := dao.DatabaseMetadata{Database: dao.Database{ID: id, Name: in.Name, Host: in.Host, Port: in.Port, DatabaseName: in.DatabaseName, SSLMode: in.SSLMode, Active: in.Active}, Username: in.Username, EncryptedPassword: encrypted}
		metadata.ID, err = tx.SaveDatabaseMetadata(ctx, actor, metadata, time.Now())
		saved = metadata.Database
		return err
	})
	if err != nil {
		return dao.Database{}, err
	}
	return saved, nil
}

func (s *Service) DisableConnection(ctx context.Context, actor, id int64) error {
	if id < 1 {
		return ErrInvalid
	}
	return s.store.Transaction(ctx, func(tx *dao.Store) error {
		if err := tx.LockAdministration(ctx); err != nil {
			return err
		}
		allowed, err := tx.IsActiveAdmin(ctx, actor)
		if err != nil {
			return err
		}
		if !allowed {
			return ErrForbidden
		}
		if err := tx.LockDatabase(ctx, id); err != nil {
			return err
		}
		return tx.DisableDatabaseMetadata(ctx, id, time.Now())
	})
}

func (s *Service) prepare(ctx context.Context, store *dao.Store, id int64, in ConnectionInput) (ConnectionInput, error) {
	if id < 0 {
		return in, ErrInvalid
	}
	if id > 0 && (in.Password == "" || in.Username == "") {
		old, err := store.DatabaseMetadata(ctx, id)
		if err != nil {
			return in, err
		}
		if in.Username == "" {
			in.Username = old.Username
		}
		if in.Password == "" {
			in.Password, err = s.decrypt(old.EncryptedPassword)
			if err != nil {
				return in, err
			}
		}
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Host = strings.ToLower(strings.TrimSpace(in.Host))
	if in.SSLMode == "" {
		in.SSLMode = "verify-full"
	}
	if in.Name == "" || len(in.Name) > 150 || in.Host == "" || len(in.Host) > 255 || strings.ContainsAny(in.Host, "/\\\x00 \r\n\t") || in.Port < 1 || in.Port > 65535 || in.DatabaseName == "" || len(in.DatabaseName) > 100 || strings.ContainsRune(in.DatabaseName, 0) || in.Username == "" || len(in.Username) > 100 || strings.ContainsRune(in.Username, 0) || len(in.Password) > 4096 {
		return in, ErrInvalid
	}
	if in.SSLMode != "disable" && in.SSLMode != "require" && in.SSLMode != "verify-full" {
		return in, ErrInvalid
	}
	if s.config.Production && in.SSLMode != "verify-full" {
		return in, ErrInvalid
	}
	return in, nil
}

func (s *Service) connect(ctx context.Context, in ConnectionInput) (*sql.DB, error) {
	u := url.URL{Scheme: "postgresql", User: url.UserPassword(in.Username, in.Password), Host: net.JoinHostPort(in.Host, strconv.Itoa(in.Port)), Path: "/" + in.DatabaseName}
	u.RawQuery = url.Values{"sslmode": {in.SSLMode}, "connect_timeout": {strconv.Itoa(max(1, int(s.config.ConnectTimeout.Seconds())))}}.Encode()
	cfg, err := pgx.ParseConfig(u.String())
	if err != nil {
		return nil, ErrConnection
	}
	cfg.RuntimeParams["standard_conforming_strings"] = "on"
	cfg.RuntimeParams["client_encoding"] = "UTF8"
	cfg.RuntimeParams["statement_timeout"] = strconv.FormatInt(s.config.QueryTimeout.Milliseconds(), 10)
	db := stdlib.OpenDB(*cfg)
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	check, cancel := context.WithTimeout(ctx, s.config.ConnectTimeout)
	defer cancel()
	var platform bool
	err = db.QueryRowContext(check, `SELECT to_regclass('public.users') IS NOT NULL AND to_regclass('public.sessions') IS NOT NULL AND to_regclass('public.session_tokens') IS NOT NULL AND to_regclass('public.group_database_permissions') IS NOT NULL AND to_regclass('public.managed_databases') IS NOT NULL`).Scan(&platform)
	if err != nil {
		db.Close()
		return nil, ErrConnection
	}
	if platform {
		db.Close()
		return nil, ErrInvalid
	}
	return db, nil
}

func (s *Service) open(ctx context.Context, actor, database int64, permission access.Permission) (*sql.DB, error) {
	if err := s.access.RequirePermission(ctx, actor, database, permission); err != nil {
		return nil, err
	}
	m, err := s.store.DatabaseMetadata(ctx, database)
	if err != nil {
		return nil, err
	}
	if !m.Active {
		return nil, ErrForbidden
	}
	password, err := s.decrypt(m.EncryptedPassword)
	if err != nil {
		return nil, err
	}
	if s.config.Production && m.SSLMode != "verify-full" {
		return nil, ErrConnection
	}
	return s.connect(ctx, ConnectionInput{Host: m.Host, Port: m.Port, DatabaseName: m.DatabaseName, Username: m.Username, Password: password, SSLMode: m.SSLMode})
}

func (s *Service) OpenForMigration(ctx context.Context, actor, database int64) (*sql.DB, error) {
	return s.open(ctx, actor, database, access.ViewEdit)
}

func (s *Service) ConnectionFingerprint(ctx context.Context, actor, database int64) (string, error) {
	if err := s.access.RequirePermission(ctx, actor, database, access.ViewEdit); err != nil {
		return "", err
	}
	m, err := s.store.DatabaseMetadata(ctx, database)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, s.config.Key)
	for _, value := range []string{m.Host, strconv.Itoa(m.Port), m.DatabaseName, m.Username, m.SSLMode} {
		mac.Write([]byte(value))
		mac.Write([]byte{0})
	}
	mac.Write(m.EncryptedPassword)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
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
