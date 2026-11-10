package dbmanagement

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"strconv"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement/adapters"
)

type ConnectionInput struct {
	DBMS, Name, Host, DatabaseName, Username, Password, SSLMode string
	Port                                                        int
	Active                                                      bool
}

type Target struct {
	DB      *sql.DB
	Adapter adapters.Adapter
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
	m, err := s.access.DatabaseConnection(ctx, actor, database, permission)
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
	if s.config.Production && m.SSLMode != "verify-full" {
		return Target{}, ErrConnection
	}
	db, err := s.connect(ctx, ConnectionInput{DBMS: m.DBMS, Host: m.Host, Port: m.Port, DatabaseName: m.DatabaseName, Username: m.Username, Password: m.Password, SSLMode: m.SSLMode})
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
	m, err := s.access.DatabaseConnection(ctx, actor, database, access.ViewOnly)
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
	m, err := s.access.DatabaseConnection(ctx, actor, database, access.ViewEdit)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, s.config.Key)
	for _, value := range []string{m.DBMS, m.Host, strconv.Itoa(m.Port), m.DatabaseName, m.Username, m.SSLMode} {
		mac.Write([]byte(value))
		mac.Write([]byte{0})
	}
	mac.Write([]byte(m.Password))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
