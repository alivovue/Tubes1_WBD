package unit_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"tubes1_wbd/database/setup"
)

func TestDatabaseSetupRejectsMissingDependencies(t *testing.T) {
	if _, err := setup.Open(context.Background(), ""); err == nil {
		t.Fatal("missing database configuration accepted")
	}
	if err := setup.Initialize(context.Background(), nil); err == nil {
		t.Fatal("nil database initialized")
	}
	if err := setup.EnsureInitialized(context.Background(), nil); err == nil {
		t.Fatal("nil database accepted")
	}
}
func TestSetupInitializesOnlyAnEmptySchema(t *testing.T) {
	db, _, _ := newSQLStub(t, scalarStep("to_regclass('public.users')", false), execStep("CREATE TABLE public.users"))
	if err := setup.EnsureInitialized(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	db, _, _ = newSQLStub(t, scalarStep("to_regclass('public.users')", true), queryStep("SELECT execution_mode,completed_statements,total_statements", []string{"mode", "completed", "total"}))
	if err := setup.EnsureInitialized(context.Background(), db); err != nil {
		t.Fatal(err)
	}
}
func TestSchemaInitializationFailureRollsBack(t *testing.T) {
	db, _, _ := newSQLStub(t, sqlStep{kind: "exec", query: "CREATE TABLE public.users", err: errors.New("sensitive database detail")}, execStep("ROLLBACK"))
	err := setup.Initialize(context.Background(), db)
	if err == nil || strings.Contains(err.Error(), "sensitive") {
		t.Fatal("schema failure did not return a safe error")
	}
}
func TestExistingSchemaMismatchDoesNotResetDatabase(t *testing.T) {
	db, _, _ := newSQLStub(t, scalarStep("to_regclass('public.users')", true), sqlStep{kind: "query", query: "SELECT execution_mode,completed_statements,total_statements", err: errors.New("missing columns")})
	if err := setup.EnsureInitialized(context.Background(), db); err == nil {
		t.Fatal("incompatible existing schema accepted")
	}
}
