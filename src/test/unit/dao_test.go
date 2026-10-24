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
			mutation := execStep("INSERT INTO access_groups")
			mutation.err = tc.failure
			_, store, _ := newSQLStub(t, sqlStep{kind: "begin"}, mutation, sqlStep{kind: tc.ending})
			err := store.Transaction(context.Background(), func(tx *dao.Store) error {
				return tx.Transaction(context.Background(), func(nested *dao.Store) error { return nested.SaveGroup(context.Background(), 0, "Engineering", "") })
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
func TestDAOZeroAffectedRowsAreNotFound(t *testing.T) {
	_, store, _ := newSQLStub(t, sqlStep{kind: "exec", query: "UPDATE users SET", affected: 0})
	err := store.UpdateUser(context.Background(), 7, "Employee", false, true, "")
	wantError(t, err, dao.ErrNotFound)
}
func TestDAODatabaseLockAlwaysReleases(t *testing.T) {
	for _, failure := range []error{nil, errors.New("callback failed")} {
		acquire := execStep("SELECT pg_advisory_lock(")
		acquire.check = func(args []driver.NamedValue) { wantArg(t, args, 0, int64(-3)) }
		release := execStep("SELECT pg_advisory_unlock(")
		release.check = acquire.check
		_, store, _ := newSQLStub(t, acquire, scalarStep("MAX(applied_version)", int64(5)), release)
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
func TestDAOHistoryPreservesPartialProgressAndAppliedVersions(t *testing.T) {
	columns := []string{"id", "database", "draft", "executor", "username", "description", "sql", "attempted", "applied", "status", "error", "started", "finished", "mode", "completed", "total"}
	rows := [][]driver.Value{
		{int64(2), int64(3), nil, int64(7), "employee", "Partial DDL", "ALTER TABLE people ADD age INTEGER", int64(2), nil, "failed", "One statement committed", testTime, testTime, "non_atomic", int64(1), int64(2)},
		{int64(1), int64(3), int64(4), int64(7), "employee", "Initial migration", "CREATE TABLE people(id INTEGER)", int64(1), int64(1), "succeeded", "", testTime, testTime, "transactional", int64(1), int64(1)},
	}
	_, store, _ := newSQLStub(t, queryStep("FROM migration_executions e", columns, rows...))
	history, err := store.MigrationHistory(context.Background(), 3, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].AppliedVersion != nil || history[0].CompletedStatements != 1 || history[0].TotalStatements != 2 || history[0].Mode != "non_atomic" {
		t.Fatalf("partial execution history lost: %#v", history)
	}
	if history[1].AppliedVersion == nil || *history[1].AppliedVersion != 1 || history[1].DraftID == nil || *history[1].DraftID != 4 {
		t.Fatal("successful execution history lost version or draft association")
	}
}
func TestDAORejectsInvalidExecutionFinalization(t *testing.T) {
	_, store, _ := newSQLStub(t)
	version := int64(1)
	zero := int64(0)
	for _, tc := range []struct {
		status    string
		version   *int64
		completed int
	}{{"running", nil, 0}, {"succeeded", nil, 0}, {"failed", &version, 0}, {"succeeded", &zero, 0}, {"failed", nil, -1}} {
		wantError(t, store.FinishExecution(context.Background(), 3, 1, tc.status, tc.version, "", testTime, tc.completed), dao.ErrInvalid)
	}
	_, err := store.StartExecution(context.Background(), dao.MigrationExecution{Mode: "unknown"})
	wantError(t, err, dao.ErrInvalid)
}
