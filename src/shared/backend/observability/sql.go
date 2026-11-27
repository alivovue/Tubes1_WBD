package observability

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/trace"
	sqldb "schakker_shared/database"
)

type SQLExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type observedSQL struct{ next SQLExecutor }

type sqlFormatterKey struct{}

func WithSQLFormatter(ctx context.Context, formatter func(string) string) context.Context {
	return context.WithValue(ctx, sqlFormatterKey{}, formatter)
}

func sqlText(ctx context.Context, query string) string {
	if formatter, ok := ctx.Value(sqlFormatterKey{}).(func(string) string); ok && formatter != nil {
		return formatter(query)
	}
	return SQLText(query)
}

func Commit(ctx context.Context, commit func() error) error {
	ctx, span := Start(ctx, "database.commit")
	defer span.End()
	err := commit()
	Event(ctx, "sql.commit", err)
	return err
}

// Rollback records an actual rollback, without reporting deferred cleanup after
// a completed commit as another transaction outcome.
func Rollback(ctx context.Context, rollback func() error) error {
	ctx, span := Start(ctx, "database.rollback")
	defer span.End()
	err := rollback()
	if !errors.Is(err, sql.ErrTxDone) {
		Event(ctx, "sql.rollback", err)
	}
	return err
}

// WrapSQL accepts a raw database/sql connection, pool or transaction. Internal
// query interfaces carry the resulting executor, so results stay observed.
func WrapSQL(q SQLExecutor) sqldb.Executor {
	return observedSQL{q}
}
func (q observedSQL) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	ctx, span := Start(ctx, "database.exec")
	defer span.End()
	started := time.Now()
	Event(ctx, "sql.start", nil, "query", sqlText(ctx, query))
	result, err := q.next.ExecContext(ctx, query, args...)
	var rows int64 = -1
	if err == nil {
		rows, _ = result.RowsAffected()
	}
	SQL(ctx, query, len(args), rows, started, err, args...)
	return result, err
}
func (q observedSQL) QueryContext(ctx context.Context, query string, args ...any) (sqldb.Rows, error) {
	ctx, span := Start(ctx, "database.query")
	started := time.Now()
	Event(ctx, "sql.start", nil, "query", sqlText(ctx, query))
	rows, err := q.next.QueryContext(ctx, query, args...)
	result := &queryResult{ctx: ctx, span: span, query: query, args: args, started: started}
	if err != nil {
		result.finish(-1, err)
		return nil, err
	}
	return &observedRows{Rows: rows, result: result}, nil
}
func (q observedSQL) QueryRowContext(ctx context.Context, query string, args ...any) sqldb.Row {
	ctx, span := Start(ctx, "database.query_row")
	started := time.Now()
	Event(ctx, "sql.start", nil, "query", sqlText(ctx, query))
	row := q.next.QueryRowContext(ctx, query, args...)
	result := &queryResult{ctx: ctx, span: span, query: query, args: args, started: started}
	if err := row.Err(); err != nil {
		result.finish(-1, err)
	}
	return &observedRow{Row: row, result: result}
}

type queryResult struct {
	ctx     context.Context
	span    trace.Span
	query   string
	args    []any
	started time.Time
	once    sync.Once
}

func (r *queryResult) finish(rows int64, err error) {
	r.once.Do(func() {
		SQL(r.ctx, r.query, len(r.args), rows, r.started, err, r.args...)
		r.span.End()
	})
}

type observedRows struct {
	*sql.Rows
	result *queryResult
	count  atomic.Int64
}

func (r *observedRows) Next() bool {
	if r.Rows.Next() {
		r.count.Add(1)
		return true
	}
	r.result.finish(r.count.Load(), r.Rows.Err())
	return false
}

func (r *observedRows) Scan(dest ...any) error {
	err := r.Rows.Scan(dest...)
	if err != nil {
		r.result.finish(r.count.Load(), err)
	}
	return err
}

func (r *observedRows) Err() error {
	err := r.Rows.Err()
	if err != nil {
		r.result.finish(r.count.Load(), err)
	}
	return err
}

func (r *observedRows) Close() error {
	err := r.Rows.Close()
	r.result.finish(r.count.Load(), errors.Join(r.Rows.Err(), err))
	return err
}

type observedRow struct {
	*sql.Row
	result *queryResult
}

func (r *observedRow) Scan(dest ...any) error {
	err := r.Row.Scan(dest...)
	rows, resultErr := int64(1), err
	if errors.Is(err, sql.ErrNoRows) {
		rows, resultErr = 0, nil
	}
	r.result.finish(rows, resultErr)
	return err
}

func (r *observedRow) Err() error {
	err := r.Row.Err()
	if err != nil {
		r.result.finish(-1, err)
	}
	return err
}
