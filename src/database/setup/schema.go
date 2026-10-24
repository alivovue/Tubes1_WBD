package setup

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"time"
)

//go:embed schema.sql
var schemaSQL string

func Initialize(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("website database is required")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return errors.New("website database is unavailable")
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, schemaSQL); err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn.ExecContext(cleanup, "ROLLBACK")
		return errors.New("website schema initialization failed; use an empty database")
	}
	return nil
}
