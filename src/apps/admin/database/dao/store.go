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

type executor = sqldb.Executor

type Store struct {
	db   *sql.DB
	q    executor
	inTx bool
}

func New(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("admin metadata database is required")
	}
	return &Store{db: db, q: observability.WrapSQL(db)}, nil
}
func (d *Store) Transaction(ctx context.Context, change func(*Store) error) error {
	if d.inTx {
		return change(d)
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return sqldb.Classify(err)
	}
	defer tx.Rollback()
	if err := change(&Store{db: d.db, q: observability.WrapSQL(tx), inTx: true}); err != nil {
		return sqldb.Classify(err)
	}
	return sqldb.Classify(observability.Commit(ctx, tx.Commit))
}
func (d *Store) LockAdministration(ctx context.Context) error {
	if !d.inTx {
		return errors.New("administration lock requires a DAO transaction")
	}
	_, err := d.q.ExecContext(ctx, `SELECT pg_advisory_xact_lock(73421106)`)
	return sqldb.Classify(err)
}
