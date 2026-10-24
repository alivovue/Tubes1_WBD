package mariadb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"

	"tubes1_wbd/backend/dbmanagement/adapters"
	"tubes1_wbd/backend/migration/engines"
)

type session struct {
	conn      *sql.Conn
	tx        *sql.Tx
	adapter   adapters.Adapter
	mode      string
	lock      string
	once      sync.Once
	receipt   engines.Receipt
	completed int
	total     int
	uncertain bool
}

func (Engine) Open(ctx context.Context, db *sql.DB, adapter adapters.Adapter, mode string) (engines.Session, error) {
	if mode != engines.Transactional && mode != engines.NonAtomic && mode != engines.Reconcile {
		return nil, adapters.ErrInvalid
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, adapters.ErrConnection
	}
	s := &session{conn: conn, adapter: adapter, mode: mode}
	var database string
	if conn.QueryRowContext(ctx, `SELECT DATABASE()`).Scan(&database) != nil {
		conn.Close()
		return nil, adapters.ErrQuery
	}
	hash := sha256.Sum256([]byte(database))
	s.lock = "schakker:" + hex.EncodeToString(hash[:24])
	var locked *int
	if err := conn.QueryRowContext(ctx, `SELECT GET_LOCK(?,30)`, s.lock).Scan(&locked); err != nil || locked == nil || *locked != 1 {
		s.Close()
		return nil, adapters.ErrQuery
	}
	if mode != engines.Reconcile {
		if err := s.ensureReceipts(ctx); err != nil {
			s.Close()
			return nil, adapters.ErrQuery
		}
	}
	if mode == engines.Transactional {
		s.tx, err = conn.BeginTx(ctx, nil)
		if err != nil {
			s.Close()
			return nil, adapters.ErrQuery
		}
	}
	return s, nil
}
func (s *session) query() adapters.Querier {
	if s.tx != nil {
		return s.tx
	}
	return s.conn
}
func (s *session) Fingerprint(ctx context.Context) (string, error) {
	return s.adapter.Fingerprint(ctx, s.query())
}
func (s *session) Prepare(ctx context.Context, r engines.Receipt) error {
	s.receipt = r
	s.total = r.TotalStatements
	if s.mode == engines.NonAtomic {
		_, err := s.conn.ExecContext(ctx, `INSERT INTO _schakker_migration_receipts(namespace,attempt_id,attempted_version,sql_hash,status,mode,completed_statements,total_statements) VALUES(?,?,?,?,'prepared',?,0,?)`, r.Namespace, r.AttemptID, r.Version, r.SQLHash, s.mode, r.TotalStatements)
		return err
	}
	return nil
}
func (s *session) Execute(ctx context.Context, statement adapters.Statement) error {
	if _, err := s.query().ExecContext(ctx, statement.SQL); err != nil {
		var native *mysql.MySQLError
		if s.mode == engines.NonAtomic && !errors.As(err, &native) {
			s.uncertain = true
		}
		return err
	}
	s.completed++
	if s.mode == engines.NonAtomic {
		if _, err := s.conn.ExecContext(ctx, `UPDATE _schakker_migration_receipts SET completed_statements=?,applied_at=UTC_TIMESTAMP(6) WHERE namespace=? AND attempt_id=?`, s.completed, s.receipt.Namespace, s.receipt.AttemptID); err != nil {
			s.uncertain = true
			return engines.ErrOutcomeUnknown
		}
	}
	return nil
}
func (s *session) Rollback() error {
	if s.tx != nil {
		return s.tx.Rollback()
	}
	if s.mode == engines.NonAtomic && !s.uncertain && s.receipt.AttemptID > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := s.conn.ExecContext(ctx, `UPDATE _schakker_migration_receipts SET status='failed',applied_version=NULL,applied_at=UTC_TIMESTAMP(6) WHERE namespace=? AND attempt_id=? AND status='prepared'`, s.receipt.Namespace, s.receipt.AttemptID)
		return err
	}
	return nil
}
func (s *session) Close() error {
	s.once.Do(func() {
		if s.tx != nil {
			s.tx.Rollback()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := s.conn.ExecContext(ctx, `SELECT RELEASE_LOCK(?)`, s.lock); err != nil {
			s.conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		s.conn.Close()
	})
	return nil
}
func (s *session) KnownFailure(err error) bool {
	if s.mode != engines.NonAtomic {
		return true
	}
	if s.uncertain {
		return false
	}
	return !s.uncertain
}
func (s *session) Progress() (int, int) { return s.completed, s.total }
