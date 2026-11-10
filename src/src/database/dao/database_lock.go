package dao

import (
	"context"
	"database/sql/driver"
	"time"
)

func (d *Store) WithDatabaseLock(ctx context.Context, id int64, action func(*Store) error) error {
	if id < 1 || d.inTx || d.conn != nil {
		return ErrInvalid
	}
	if d.db.Stats().MaxOpenConnections == 1 {
		return ErrInvalid
	}
	select {
	case d.lockSlots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-d.lockSlots }()
	conn, err := d.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(cleanup, `SELECT pg_advisory_unlock($1)`, -id); err != nil {
			conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, -id); err != nil {
		return err
	}
	return action(&Store{db: d.db, q: conn, conn: conn, lockSlots: d.lockSlots})
}

func (d *Store) LockDatabase(ctx context.Context, id int64) error {
	if id < 1 || !d.inTx {
		return ErrInvalid
	}
	_, err := d.q.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, -id)
	return classify(err)
}
