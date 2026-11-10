// Package postgre owns the managed PostgreSQL mock data.
package postgre

import (
	"context"
	"database/sql"
	_ "embed"
)

//go:embed employees.sql
var employeesSQL string

func Seed(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, employeesSQL)
	return err
}
