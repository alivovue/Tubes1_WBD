// Package admin embeds the canonical administration schema for explicit
// provisioning. Application startup validates existing schemas without applying it.
package admin

import (
	"context"
	"database/sql"
	_ "embed"
)

//go:embed schema.sql
var schemaSQL string

func Initialize(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, schemaSQL)
	return err
}
