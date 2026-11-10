package unit_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"golang.org/x/crypto/bcrypt"
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

var testDatabases sync.Map

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
	return []sqlStep{{kind: "begin"}, execStep("pg_advisory_xact_lock"), scalarStep("admin_users", allowed)}
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
	testDatabases.Store(store, db)
	t.Cleanup(func() {
		testDatabases.Delete(store)
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
	db, _ := testDatabases.Load(store)
	config.Authority = stubAuthority{db: db.(*sql.DB)}
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
	return queryStep("FROM users WHERE id=", []string{"id", "username", "display_name", "is_active", "version", "rotation"}, []driver.Value{int64(7), "employee", "Employee", active, int64(1), int64(1)})
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

type stubAuthority struct{ db *sql.DB }

func (s stubAuthority) AuthenticateUser(ctx context.Context, name, password string) (dao.User, error) {
	var u dao.User
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT id,username,display_name,is_active,auth_version,rotation_version,password_hash FROM authority_users WHERE lower(username)=lower($1)`, name).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Active, &u.Version, &u.Rotation, &hash)
	if err != nil || !u.Active || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return dao.User{}, access.ErrUnauthenticated
	}
	return u, nil
}
func (s stubAuthority) User(ctx context.Context, id int64) (dao.User, error) {
	var u dao.User
	err := s.db.QueryRowContext(ctx, `SELECT id,username,display_name,is_active,auth_version,rotation_version FROM users WHERE id=$1`, id).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Active, &u.Version, &u.Rotation)
	return u, err
}
func (s stubAuthority) EffectivePermission(ctx context.Context, user, database int64) (dao.Permission, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT effective_permission FROM group_database_permissions WHERE user_id=$1 AND database_id=$2`, user, database).Scan(&n)
	if err != nil {
		return dao.NoAccess, err
	}
	return []dao.Permission{dao.NoAccess, dao.ViewOnly, dao.ViewEdit}[n], nil
}
func (s stubAuthority) AccessibleDatabases(ctx context.Context, user int64) ([]dao.Database, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,dbms,permission FROM managed_databases WHERE user_id=$1`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []dao.Database{}
	for rows.Next() {
		var d dao.Database
		if err := rows.Scan(&d.ID, &d.Name, &d.DBMS, &d.Permission); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
func (s stubAuthority) Connection(ctx context.Context, user, database int64, p dao.Permission) (dao.DatabaseMetadata, error) {
	var m dao.DatabaseMetadata
	err := s.db.QueryRowContext(ctx, `SELECT metadata FROM managed_databases WHERE id=$1`, database).Scan(&m.ID, &m.Name, &m.DBMS, &m.Host, &m.Port, &m.DatabaseName, &m.Active, &m.SSLMode, &m.Username, &m.Password)
	return m, err
}
