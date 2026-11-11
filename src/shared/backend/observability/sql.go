package observability

import (
	"context"
	"database/sql"
	"time"
)

type SQLExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type observedSQL struct{ next SQLExecutor }

func Commit(ctx context.Context, commit func() error) error {
	ctx, span := Start(ctx, "database.commit")
	defer span.End()
	err := commit()
	Event(ctx, "sql.commit", err)
	return err
}

func WrapSQL(q SQLExecutor) SQLExecutor {
	if _, ok := q.(observedSQL); ok {
		return q
	}
	return observedSQL{q}
}
func (q observedSQL) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	ctx, span := Start(ctx, "database.exec")
	defer span.End()
	started := time.Now()
	Event(ctx, "sql.start", nil, "query", "[redacted]")
	result, err := q.next.ExecContext(ctx, query, args...)
	var rows int64 = -1
	if err == nil {
		rows, _ = result.RowsAffected()
	}
	SQL(ctx, query, len(args), rows, started, err)
	return result, err
}
func (q observedSQL) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	ctx, span := Start(ctx, "database.query")
	defer span.End()
	started := time.Now()
	Event(ctx, "sql.start", nil, "query", "[redacted]")
	rows, err := q.next.QueryContext(ctx, query, args...)
	SQL(ctx, query, len(args), -1, started, err)
	return rows, err
}
func (q observedSQL) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	ctx, span := Start(ctx, "database.query_row")
	defer span.End()
	started := time.Now()
	Event(ctx, "sql.start", nil, "query", "[redacted]")
	row := q.next.QueryRowContext(ctx, query, args...)
	SQL(ctx, query, len(args), -1, started, row.Err())
	return row
}
