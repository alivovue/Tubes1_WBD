package unit_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	mysql "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/backend/migration/engines"
	mariaengine "tubes1_wbd/backend/migration/engines/mariadb"
	pgengine "tubes1_wbd/backend/migration/engines/postgres"
)

func TestMigrationAnalysisRequiresEditPermission(t *testing.T) {
	_, store, stub := newSQLStub(t)
	_, _, service := newServices(t, store)
	calls := []struct {
		name string
		call func() error
	}{
		{"analyze", func() error { _, err := service.Analyze(context.Background(), 7, 3, "SELECT 1"); return err }},
		{"analyze schema", func() error {
			_, err := service.AnalyzeDesired(context.Background(), 7, 3, emptySnapshot("postgresql"))
			return err
		}},
		{"analyze changes", func() error { _, err := service.AnalyzeChanges(context.Background(), 7, 3, nil); return err }},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			stub.append(permissionStep(access.ViewOnly))
			wantError(t, tc.call(), access.ErrForbidden)
		})
	}
}
func TestCommittedHistoryAllowsReaders(t *testing.T) {
	_, store, stub := newSQLStub(t)
	_, _, service := newServices(t, store)
	stub.append(permissionStep(access.ViewOnly), queryStep("FROM migration_execution_history h", []string{"history"}))
	history, err := service.Workflow().History(context.Background(), 7, 3, false, 50, 0)
	if err != nil || len(history) != 0 {
		t.Fatalf("reader history request failed: %v", err)
	}
	stub.append(permissionStep(access.NoAccess))
	_, err = service.Workflow().History(context.Background(), 7, 3, false, 50, 0)
	wantError(t, err, access.ErrForbidden)
	stub.append(permissionStep(access.ViewOnly))
	_, err = service.Workflow().History(context.Background(), 7, 3, false, 201, 0)
	wantError(t, err, access.ErrInvalid)
}
func TestMigrationEngineModesAndRegistry(t *testing.T) {
	pg := pgengine.New()
	maria := mariaengine.New()
	registry := engines.NewRegistry(pg, maria)
	for _, name := range []string{"postgres", "postgresql", "maria", "mariadb"} {
		engine, err := registry.Select(name)
		if err != nil || engine == nil {
			t.Fatalf("engine %q unavailable: %v", name, err)
		}
	}
	_, err := registry.Select("sqlite")
	wantError(t, err, engines.ErrUnsupported)
	mode, warnings, err := pg.Assess(context.Background(), nil, nil)
	if err != nil || mode != engines.Transactional || len(warnings) != 0 {
		t.Fatal("PostgreSQL transaction mode incorrect")
	}
	for _, tc := range []struct {
		name, query string
		unsafe      int64
		mode        string
	}{{"InnoDB DML", "UPDATE people SET id=2", 0, engines.Transactional}, {"unsafe DML dependency", "UPDATE people SET id=2", 1, engines.NonAtomic}, {"DDL", "ALTER TABLE people ADD COLUMN age integer", 0, engines.NonAtomic}} {
		t.Run(tc.name, func(t *testing.T) {
			steps := []sqlStep{scalarStep("SELECT DATABASE()", "target")}
			if tc.name != "DDL" {
				steps = append(steps, scalarStep("information_schema.TABLES", tc.unsafe))
			}
			db, _, _ := newSQLStub(t, steps...)
			statements, err := maria.Parse(tc.query)
			if err != nil {
				t.Fatal(err)
			}
			mode, warnings, err := maria.Assess(context.Background(), db, statements)
			if err != nil || mode != tc.mode {
				t.Fatalf("mode = %s, %v; want %s", mode, err, tc.mode)
			}
			if tc.mode == engines.NonAtomic && (len(warnings) == 0 || !strings.Contains(warnings[0], "partial")) {
				t.Fatal("non-atomic execution warning missing")
			}
		})
	}
}
func TestMigrationFailuresDoNotLeakDriverDetails(t *testing.T) {
	pg := pgengine.New()
	maria := mariaengine.New()
	pgMessage := pg.SafeFailure(&pgconn.PgError{Code: "23505", Message: "secret row data", Detail: "password=hidden"})
	if !strings.Contains(pgMessage, "23505") || strings.Contains(pgMessage, "secret") || strings.Contains(pgMessage, "hidden") {
		t.Fatal("PostgreSQL driver details leaked")
	}
	native := &mysql.MySQLError{Number: 1062, Message: "secret row data"}
	copy(native.SQLState[:], "23000")
	mariaMessage := maria.SafeFailure(native)
	if !strings.Contains(mariaMessage, "1062") || strings.Contains(mariaMessage, "secret") {
		t.Fatal("MariaDB driver details leaked")
	}
	dependency := pg.SafeFailure(&pgconn.PgError{Code: "2BP01", Message: "secret object details"})
	if !strings.Contains(dependency, "CASCADE") || strings.Contains(dependency, "secret") {
		t.Fatal("PostgreSQL dependency error did not provide safe CASCADE guidance")
	}
	for _, number := range []uint16{1075, 1553, 1828, 1833} {
		message := maria.SafeFailure(&mysql.MySQLError{Number: number, Message: "secret object details"})
		if !strings.Contains(message, "key") || strings.Contains(message, "secret") {
			t.Fatal("MariaDB dependency error did not provide safe key guidance")
		}
	}
	for _, engine := range []engines.Engine{pg, maria} {
		for _, failure := range []error{context.Canceled, context.DeadlineExceeded, errors.New("password=hidden")} {
			if strings.Contains(engine.SafeFailure(failure), "hidden") {
				t.Fatal("raw execution error leaked")
			}
		}
	}
}
func TestSQLHashBindsExactText(t *testing.T) {
	hash := migration.SQLHash("SELECT 1")
	if len(hash) != 64 || hash != migration.SQLHash("SELECT 1") || hash == migration.SQLHash("SELECT 1;") || hash == migration.SQLHash("SELECT 2") {
		t.Fatal("SQL hash did not bind the exact reviewed text")
	}
}
