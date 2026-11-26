package unit_test

import (
	"context"
	"strings"
	"testing"

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
func TestSQLHashBindsExactText(t *testing.T) {
	hash := migration.SQLHash("SELECT 1")
	if len(hash) != 64 || hash != migration.SQLHash("SELECT 1") || hash == migration.SQLHash("SELECT 1;") || hash == migration.SQLHash("SELECT 2") {
		t.Fatal("SQL hash did not bind the exact reviewed text")
	}
}
