package dao

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrNotFound = errors.New("data tidak ditemukan")
	ErrConflict = errors.New("data sudah ada atau masih digunakan")
	ErrInvalid  = errors.New("input tidak valid")
)

type executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Store struct {
	db   *sql.DB
	q    executor
	inTx bool
}

func New(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("website database is required")
	}
	return &Store{db: db, q: db}, nil
}

func (d *Store) Transaction(ctx context.Context, change func(*Store) error) error {
	if d.inTx {
		return change(d)
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return classify(err)
	}
	defer tx.Rollback()
	if err := change(&Store{db: d.db, q: tx, inTx: true}); err != nil {
		return classify(err)
	}
	return classify(tx.Commit())
}

func (d *Store) LockAdministration(ctx context.Context) error {
	if !d.inTx {
		return errors.New("administration lock requires a DAO transaction")
	}
	_, err := d.q.ExecContext(ctx, `SELECT pg_advisory_xact_lock(73421106)`)
	return classify(err)
}

func classify(err error) error {
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

func affected(result sql.Result, err error) error {
	if err != nil {
		return classify(err)
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
