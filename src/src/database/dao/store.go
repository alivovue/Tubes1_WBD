package dao

import (
	"context"
	"database/sql"
	"errors"

	sqldb "schakker_shared/database"
	"schakker_shared/observability"
)

var (
	ErrNotFound = sqldb.ErrNotFound
	ErrConflict = sqldb.ErrConflict
	ErrInvalid  = sqldb.ErrInvalid
)

type Executor = sqldb.Executor

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
		return sqldb.Classify(err)
	}
	defer tx.Rollback()
	if err := change(&Store{db: d.db, q: observability.WrapSQL(tx), inTx: true, conn: d.conn, lockSlots: d.lockSlots}); err != nil {
		return sqldb.Classify(err)
	}
	return sqldb.Classify(observability.Commit(ctx, tx.Commit))
}
