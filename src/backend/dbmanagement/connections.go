package dbmanagement

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"strconv"
	"strings"
	"time"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement/adapters"
	"tubes1_wbd/database/dao"
)

type ConnectionInput struct {
	DBMS         string
	Name         string
	Host         string
	Port         int
	DatabaseName string
	Username     string
	Password     string
	SSLMode      string
	Active       bool
}
type Target struct {
	DB      *sql.DB
	Adapter adapters.Adapter
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
			if hasHistory && (old.DBMS != in.DBMS || old.Host != in.Host || old.Port != in.Port || old.DatabaseName != in.DatabaseName) {
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
		metadata := dao.DatabaseMetadata{Database: dao.Database{DBMS: in.DBMS, ID: id, Name: in.Name, Host: in.Host, Port: in.Port, DatabaseName: in.DatabaseName, SSLMode: in.SSLMode, Active: in.Active}, Username: in.Username, EncryptedPassword: encrypted}
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
	if id > 0 {
		old, err := store.DatabaseMetadata(ctx, id)
		if err != nil {
			return in, err
		}
		if in.DBMS == "" {
			in.DBMS = old.DBMS
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
	in.DBMS = adapters.Normalize(in.DBMS)
	adapter, err := s.registry.Select(in.DBMS)
	if err != nil {
		return in, err
	}
	if in.Port == 0 {
		in.Port = adapter.DefaultPort()
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Host = strings.ToLower(strings.TrimSpace(in.Host))
	if in.SSLMode == "" {
		in.SSLMode = "verify-full"
	}
	if in.Name == "" || len(in.Name) > 150 || strings.ContainsRune(in.Name, 0) || in.Host == "" || len(in.Host) > 255 || strings.ContainsAny(in.Host, "/\\\x00 \r\n\t") || in.Port < 1 || in.Port > 65535 || in.DatabaseName == "" || len(in.DatabaseName) > 100 || strings.ContainsRune(in.DatabaseName, 0) || in.Username == "" || len(in.Username) > 100 || strings.ContainsRune(in.Username, 0) || len(in.Password) > 4096 {
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
	a, err := s.registry.Select(in.DBMS)
	if err != nil {
		return nil, err
	}
	return a.Connect(ctx, adapters.Connection{Host: in.Host, Port: in.Port, DatabaseName: in.DatabaseName, Username: in.Username, Password: in.Password, SSLMode: in.SSLMode, ConnectTimeout: s.config.ConnectTimeout, QueryTimeout: s.config.QueryTimeout})
}
func (s *Service) open(ctx context.Context, actor, database int64, permission access.Permission) (Target, error) {
	if err := s.access.RequirePermission(ctx, actor, database, permission); err != nil {
		return Target{}, err
	}
	m, err := s.store.DatabaseMetadata(ctx, database)
	if err != nil {
		return Target{}, err
	}
	if !m.Active {
		return Target{}, ErrForbidden
	}
	a, err := s.registry.Select(m.DBMS)
	if err != nil {
		return Target{}, err
	}
	password, err := s.decrypt(m.EncryptedPassword)
	if err != nil {
		return Target{}, err
	}
	if s.config.Production && m.SSLMode != "verify-full" {
		return Target{}, ErrConnection
	}
	db, err := s.connect(ctx, ConnectionInput{DBMS: m.DBMS, Host: m.Host, Port: m.Port, DatabaseName: m.DatabaseName, Username: m.Username, Password: password, SSLMode: m.SSLMode})
	if err != nil {
		return Target{}, err
	}
	return Target{DB: db, Adapter: a}, nil
}
func (s *Service) OpenMigrationTarget(ctx context.Context, actor, database int64) (Target, error) {
	return s.open(ctx, actor, database, access.ViewEdit)
}
func (s *Service) OpenForMigration(ctx context.Context, actor, database int64) (*sql.DB, error) {
	target, err := s.OpenMigrationTarget(ctx, actor, database)
	return target.DB, err
}
func (s *Service) DBMS(ctx context.Context, actor, database int64) (string, error) {
	if err := s.access.RequirePermission(ctx, actor, database, access.ViewOnly); err != nil {
		return "", err
	}
	m, err := s.store.DatabaseMetadata(ctx, database)
	if err != nil {
		return "", err
	}
	a, err := s.registry.Select(m.DBMS)
	if err != nil {
		return "", err
	}
	return a.Name(), nil
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
	for _, value := range []string{m.DBMS, m.Host, strconv.Itoa(m.Port), m.DatabaseName, m.Username, m.SSLMode} {
		mac.Write([]byte(value))
		mac.Write([]byte{0})
	}
	mac.Write(m.EncryptedPassword)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
