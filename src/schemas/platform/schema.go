// Package platform embeds the canonical user metadata schema for explicit
// provisioning. Application startup validates existing schemas without applying it.
package platform

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
