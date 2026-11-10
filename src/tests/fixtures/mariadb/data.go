// Package mariadb owns the managed MariaDB mock data.
package mariadb

import (
	"context"
	"database/sql"
	_ "embed"
)

//go:embed employees.sql
var employeesSQL string

func Seed(ctx context.Context, connection *sql.Conn) error {
	_, err := connection.ExecContext(ctx, employeesSQL)
	return err
}
