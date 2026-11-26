package postgres

import (
	"context"
	"database/sql"
	"net"
	"net/url"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"tubes1_wbd/backend/dbmanagement/adapters"
	"tubes1_wbd/backend/dbmanagement/adapters/postgres/queryplan"
)

type Adapter struct{}

func New() Adapter           { return Adapter{} }
func (Adapter) Name() string { return "postgresql" }

var ErrInvalid = adapters.ErrInvalid
var ErrQuery = adapters.ErrQuery
var ErrConnection = adapters.ErrConnection
var ErrNotFound = adapters.ErrNotFound

func (Adapter) Connect(ctx context.Context, in adapters.Connection) (*sql.DB, error) {
	u := url.URL{Scheme: "postgresql", User: url.UserPassword(in.Username, in.Password), Host: net.JoinHostPort(in.Host, strconv.Itoa(in.Port)), Path: "/" + in.DatabaseName}
	u.RawQuery = url.Values{"sslmode": {in.SSLMode}, "connect_timeout": {strconv.Itoa(max(1, int(in.ConnectTimeout.Seconds())))}}.Encode()
	cfg, err := pgx.ParseConfig(u.String())
	if err != nil {
		return nil, ErrConnection
	}
	cfg.RuntimeParams["standard_conforming_strings"] = "on"
	cfg.RuntimeParams["client_encoding"] = "UTF8"
	cfg.RuntimeParams["statement_timeout"] = strconv.FormatInt(in.QueryTimeout.Milliseconds(), 10)
	db := stdlib.OpenDB(*cfg)
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	check, cancel := context.WithTimeout(ctx, in.ConnectTimeout)
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

func (Adapter) QuoteIdentifier(parts ...string) (string, error) {
	for _, part := range parts {
		if !adapters.ValidIdentifier(part, 63) {
			return "", ErrInvalid
		}
	}
	if len(parts) == 0 {
		return "", ErrInvalid
	}
	return pgx.Identifier(parts).Sanitize(), nil
}
func (Adapter) QueryPlans(ctx context.Context, q adapters.Querier, statements []adapters.Statement) ([]adapters.Plan, error) {
	return queryplan.Analyze(ctx, q, statements)
}

func (Adapter) DefaultPort() int { return 5432 }
