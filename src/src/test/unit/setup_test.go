package unit_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"tubes1_wbd/test/data/setup"
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
	db, _, _ = newSQLStub(t, scalarStep("to_regclass('public.users')", true), scalarStep("attname='is_platform_admin'", false), scalarStep("count(*)=5", true))
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
	db, _, _ := newSQLStub(t, scalarStep("to_regclass('public.users')", true), scalarStep("attname='is_platform_admin'", false), scalarStep("count(*)=5", false))
	if err := setup.EnsureInitialized(context.Background(), db); err == nil {
		t.Fatal("incompatible existing schema accepted")
	}
}
