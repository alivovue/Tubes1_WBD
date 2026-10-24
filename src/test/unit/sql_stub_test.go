package unit_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/database/dao"
)

type sqlStep struct {
	kind, query string
	columns     []string
	rows        [][]driver.Value
	err         error
	affected    int64
	check       func([]driver.NamedValue)
}
type sqlStub struct {
	t     *testing.T
	mu    sync.Mutex
	steps []sqlStep
}
type stubConnector struct{ state *sqlStub }
type stubDriver struct{}
type stubConn struct{ state *sqlStub }
type stubTx struct{ state *sqlStub }
type stubRows struct {
	columns  []string
	rows     [][]driver.Value
	position int
}

func queryStep(query string, columns []string, rows ...[]driver.Value) sqlStep {
	return sqlStep{kind: "query", query: query, columns: columns, rows: rows}
}
func scalarStep(query string, value driver.Value) sqlStep {
	return queryStep(query, []string{"value"}, []driver.Value{value})
}
func execStep(query string) sqlStep { return sqlStep{kind: "exec", query: query, affected: 1} }
func transactionSteps(allowed bool) []sqlStep {
	return []sqlStep{{kind: "begin"}, execStep("pg_advisory_xact_lock"), scalarStep("is_platform_admin", allowed)}
}
func newSQLStub(t *testing.T, steps ...sqlStep) (*sql.DB, *dao.Store, *sqlStub) {
	t.Helper()
	state := &sqlStub{t: t, steps: steps}
	db := sql.OpenDB(stubConnector{state: state})
	db.SetMaxOpenConns(4)
	store, err := dao.New(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		state.mu.Lock()
		defer state.mu.Unlock()
		if len(state.steps) > 0 {
			t.Errorf("%d database operations were not performed; next: %s %s", len(state.steps), state.steps[0].kind, state.steps[0].query)
		}
	})
	return db, store, state
}
func (s *sqlStub) append(steps ...sqlStep) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steps = append(s.steps, steps...)
}
func (s *sqlStub) take(kind, query string, args []driver.NamedValue) (sqlStep, error) {
	s.t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.steps) == 0 {
		s.t.Errorf("unexpected database operation: %s %s", kind, query)
		return sqlStep{}, errors.New("unexpected database operation")
	}
	step := s.steps[0]
	s.steps = s.steps[1:]
	normalized := strings.Join(strings.Fields(query), " ")
	if kind != step.kind || !strings.Contains(normalized, step.query) {
		s.t.Errorf("database operation %s %q; want %s containing %q", kind, normalized, step.kind, step.query)
		return step, errors.New("unexpected database operation")
	}
	if step.check != nil {
		step.check(args)
	}
	return step, step.err
}
func (c stubConnector) Connect(context.Context) (driver.Conn, error) {
	return &stubConn{state: c.state}, nil
}
func (c stubConnector) Driver() driver.Driver       { return stubDriver{} }
func (stubDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use the stub connector") }
func (c *stubConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not supported by this stub")
}
func (c *stubConn) Close() error { return nil }
func (c *stubConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *stubConn) BeginTx(_ context.Context, _ driver.TxOptions) (driver.Tx, error) {
	_, err := c.state.take("begin", "", nil)
	if err != nil {
		return nil, err
	}
	return &stubTx{state: c.state}, nil
}
func (c *stubConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	step, err := c.state.take("exec", query, args)
	return driver.RowsAffected(step.affected), err
}
func (c *stubConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	step, err := c.state.take("query", query, args)
	if err != nil {
		return nil, err
	}
	return &stubRows{columns: step.columns, rows: step.rows}, nil
}
func (tx *stubTx) Commit() error      { _, err := tx.state.take("commit", "", nil); return err }
func (tx *stubTx) Rollback() error    { _, err := tx.state.take("rollback", "", nil); return err }
func (r *stubRows) Columns() []string { return r.columns }
func (r *stubRows) Close() error      { return nil }
func (r *stubRows) Next(dest []driver.Value) error {
	if r.position == len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.position])
	r.position++
	return nil
}

var testTime = time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)
var testKey = []byte("0123456789abcdef0123456789abcdef")

const testPassword = "valid-unit-password-123"
const testSession = "session-unit-test"
const testToken = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func newAccess(t *testing.T, store *dao.Store, clock func() time.Time) *access.Service {
	t.Helper()
	config := access.DefaultConfig(testKey)
	if clock == nil {
		clock = func() time.Time { return testTime }
	}
	config.Now = clock
	service, err := access.NewService(store, config)
	if err != nil {
		t.Fatal(err)
	}
	return service
}
func newServices(t *testing.T, store *dao.Store) (*access.Service, *dbmanagement.Service, *migration.Service) {
	t.Helper()
	auth := newAccess(t, store, nil)
	databases, err := dbmanagement.NewService(store, auth, dbmanagement.DefaultConfig(testKey, false))
	if err != nil {
		t.Fatal(err)
	}
	migrations, err := migration.NewService(store, auth, databases)
	if err != nil {
		t.Fatal(err)
	}
	return auth, databases, migrations
}
func wantError(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("error = %v; want %v", got, want)
	}
}
func wantArg(t *testing.T, args []driver.NamedValue, index int, want any) {
	t.Helper()
	if index >= len(args) {
		t.Fatalf("argument %d missing", index)
	}
	if !reflect.DeepEqual(args[index].Value, want) {
		t.Errorf("argument %d = %v; want %v", index, args[index].Value, want)
	}
}
func userStep(admin, active bool) sqlStep {
	return queryStep("FROM users WHERE id=", []string{"id", "username", "display_name", "is_platform_admin", "is_active"}, []driver.Value{int64(7), "employee", "Employee", admin, active})
}
func permissionStep(permission dao.Permission) sqlStep {
	level := int64(0)
	if permission == dao.ViewOnly {
		level = 1
	}
	if permission == dao.ViewEdit {
		level = 2
	}
	return scalarStep("group_database_permissions", level)
}
func hashArgument(t *testing.T, value driver.Value) []byte {
	t.Helper()
	hash, ok := value.([]byte)
	if !ok {
		t.Fatalf("hashed token has type %T", value)
	}
	return hash
}
