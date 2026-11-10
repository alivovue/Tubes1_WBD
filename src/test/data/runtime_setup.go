// Provisioning helpers are exclusively for isolated test fixtures.
package data

import (
	"context"
	"database/sql"
	_ "embed"
)

//go:embed runtime/schema.sql
var runtimeSchema string

//go:embed control/schema.sql
var controlSchema string

func InitializeRuntime(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, runtimeSchema)
	return err
}
func InitializeControlPlane(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, controlSchema)
	return err
}
