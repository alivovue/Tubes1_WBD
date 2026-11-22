// Package setup provides isolated PostgreSQL initialization for tests and the
// explicitly invoked local development launcher, never production startup.
package setup

import (
	"context"
	"database/sql"

	runtime "tubes1_wbd/database/setup"
)

func OpenConnection(ctx context.Context, dsn string) (*sql.DB, error) { return runtime.Open(ctx, dsn) }
