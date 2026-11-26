// Package database supplies SQL mechanics. Callers own connection strings,
// pools, schema validation, transactions and application queries.
package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// OpenPostgres creates a new pool for the supplied connection string. It never
// reads environment variables or retains a process-wide database connection.
func OpenPostgres(ctx context.Context, dsn string) (*sql.DB, error) {
	if dsn == "" {
		return nil, errors.New("metadata database connection is required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, errors.New("konfigurasi koneksi PostgreSQL tidak valid")
	}
	db.SetMaxOpenConns(15)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, errors.New("koneksi PostgreSQL gagal; periksa konfigurasi dan server database")
	}
	return db, nil
}
