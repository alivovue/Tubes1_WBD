package database

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"schakker_shared/validation"
)

var (
	ErrNotFound = errors.New("data tidak ditemukan")
	ErrConflict = errors.New("data sudah ada atau masih digunakan")
	ErrInvalid  = validation.ErrInvalid
)

type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (Rows, error)
	QueryRowContext(context.Context, string, ...any) Row
}

// Result interfaces allow instrumentation to follow a query through reading,
// conversion and closure, when database/sql can still report a query failure.
type Rows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close() error
	Columns() ([]string, error)
	ColumnTypes() ([]*sql.ColumnType, error)
}

type Row interface {
	Scan(...any) error
	Err() error
}

func Classify(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23505", "23503":
			return ErrConflict
		case "23514", "22001", "22P02":
			return ErrInvalid
		}
	}
	return err
}

func Affected(result sql.Result, err error) error {
	if err != nil {
		return Classify(err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}
