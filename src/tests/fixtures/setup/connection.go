// Package setup provides isolated PostgreSQL initialization for tests and the
// explicitly invoked local development launcher, never production startup.
package setup

import (
	"context"
	"database/sql"

	"schakker_shared/database"
)

func OpenConnection(ctx context.Context, dsn string) (*sql.DB, error) {
	return database.OpenPostgres(ctx, dsn)
}
