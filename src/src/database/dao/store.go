package dao

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"schakker_shared/observability"
	"schakker_shared/validation"
)

var (
	ErrNotFound = errors.New("data tidak ditemukan")
	ErrConflict = errors.New("data sudah ada atau masih digunakan")
	ErrInvalid  = validation.ErrInvalid
)

type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type executor = Executor

// TransactionQueries lets P3 insert its durable queue record in the SAME
// metadata transaction as approval reservation. It never opens a target DB.
func (d *Store) TransactionQueries() (Executor, error) {
	if !d.inTx {
		return nil, ErrInvalid
	}
	return d.q, nil
}

type Store struct {
	db        *sql.DB
	q         executor
	inTx      bool
	conn      *sql.Conn
	lockSlots chan struct{}
}

func New(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("website database is required")
	}
	capacity := db.Stats().MaxOpenConnections / 2
	if capacity < 1 {
		capacity = 1
	}
	return &Store{db: db, q: observability.WrapSQL(db), lockSlots: make(chan struct{}, capacity)}, nil
}

func (d *Store) Transaction(ctx context.Context, change func(*Store) error) error {
	if d.inTx {
		return change(d)
	}
	var tx *sql.Tx
	var err error
	if d.conn != nil {
		tx, err = d.conn.BeginTx(ctx, nil)
	} else {
		tx, err = d.db.BeginTx(ctx, nil)
	}
	if err != nil {
		return classify(err)
	}
	defer tx.Rollback()
	if err := change(&Store{db: d.db, q: observability.WrapSQL(tx), inTx: true, conn: d.conn, lockSlots: d.lockSlots}); err != nil {
		return classify(err)
	}
	return classify(observability.Commit(ctx, tx.Commit))
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
