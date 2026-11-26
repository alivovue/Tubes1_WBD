package unit_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"tubes1_wbd/database/dao"
)

func TestDAOTransactionCommitsAndRollsBack(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failure error
		ending  string
	}{{"success", nil, "commit"}, {"failure", errors.New("mutation failed"), "rollback"}} {
		t.Run(tc.name, func(t *testing.T) {
			mutation := execStep("INSERT INTO users")
			mutation.err = tc.failure
			_, store, _ := newSQLStub(t, sqlStep{kind: "begin"}, mutation, sqlStep{kind: tc.ending})
			err := store.Transaction(context.Background(), func(tx *dao.Store) error {
				return tx.Transaction(context.Background(), func(nested *dao.Store) error {
					return nested.RememberUser(context.Background(), dao.User{ID: 7, Username: "employee", DisplayName: "Employee", Active: true})
				})
			})
			wantError(t, err, tc.failure)
		})
	}
}
func TestDAOClassifiesDatabaseFailures(t *testing.T) {
	unknown := errors.New("temporary storage failure")
	cases := []struct {
		name          string
		failure, want error
	}{
		{"duplicate", &pgconn.PgError{Code: "23505"}, dao.ErrConflict}, {"foreign key", &pgconn.PgError{Code: "23503"}, dao.ErrConflict},
		{"check constraint", &pgconn.PgError{Code: "23514"}, dao.ErrInvalid}, {"too long", &pgconn.PgError{Code: "22001"}, dao.ErrInvalid}, {"invalid value", &pgconn.PgError{Code: "22P02"}, dao.ErrInvalid}, {"unknown", unknown, unknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, store, _ := newSQLStub(t, sqlStep{kind: "query", query: "FROM users WHERE id=", err: tc.failure})
			_, err := store.User(context.Background(), 7)
			wantError(t, err, tc.want)
		})
	}
	_, store, _ := newSQLStub(t, queryStep("FROM users WHERE id=", []string{"id", "name", "display", "admin", "active"}))
	_, err := store.User(context.Background(), 7)
	wantError(t, err, dao.ErrNotFound)
}
func TestDAODatabaseLockAlwaysReleases(t *testing.T) {
	for _, failure := range []error{nil, errors.New("callback failed")} {
		acquire := execStep("SELECT pg_advisory_lock(")
		acquire.check = func(args []driver.NamedValue) { wantArg(t, args, 0, int64(-3)) }
		release := execStep("SELECT pg_advisory_unlock(")
		release.check = acquire.check
		_, store, _ := newSQLStub(t, acquire, scalarStep("active_version", int64(5)), release)
		err := store.WithDatabaseLock(context.Background(), 3, func(locked *dao.Store) error {
			version, err := locked.MigrationVersion(context.Background(), 3)
			if err != nil {
				return err
			}
			if version != 5 {
				t.Error("incorrect version under lock")
			}
			return failure
		})
		wantError(t, err, failure)
	}
	db, store, _ := newSQLStub(t)
	db.SetMaxOpenConns(1)
	wantError(t, store.WithDatabaseLock(context.Background(), 3, func(*dao.Store) error { return nil }), dao.ErrInvalid)
	wantError(t, store.WithDatabaseLock(context.Background(), 0, func(*dao.Store) error { return nil }), dao.ErrInvalid)
}
func TestRuntimeIdentitySnapshotRejectsReuseOfAHistoricalID(t *testing.T) {
	_, store, _ := newSQLStub(t, sqlStep{kind: "exec", query: "WHERE users.username=EXCLUDED.username", affected: 0})
	wantError(t, store.RememberUser(context.Background(), dao.User{ID: 7, Username: "different.identity", DisplayName: "New account", Active: true}), dao.ErrConflict)
}
