package unit_test

import (
	"context"
	"database/sql/driver"
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

func metadataStep() sqlStep {
	return queryStep("FROM managed_databases WHERE id=", []string{"id", "name", "dbms", "host", "port", "database", "active", "tls", "username", "password"}, []driver.Value{int64(3), "Target", "postgresql", "db.internal", int64(5432), "target", true, "verify-full", "dbuser", []byte("encrypted")})
}
func draftStep(query string) sqlStep {
	return queryStep(query, []string{"id", "database", "creator", "title", "mode", "sql", "created", "updated"}, []driver.Value{int64(4), int64(3), int64(7), "A draft", "sql", "SELECT 1", testTime, testTime})
}
func TestMigrationWritesRequireEditPermission(t *testing.T) {
	_, store, stub := newSQLStub(t)
	_, _, service := newServices(t, store)
	calls := []struct {
		name string
		call func() error
	}{
		{"save draft", func() error {
			_, err := service.SaveDraft(context.Background(), 7, 3, migration.DraftInput{Title: "Draft", SQL: "SELECT 1"})
			return err
		}},
		{"read draft", func() error { _, err := service.Draft(context.Background(), 7, 3, 4); return err }},
		{"list drafts", func() error { _, err := service.Drafts(context.Background(), 7, 3, 50, 0); return err }},
		{"delete draft", func() error { return service.DeleteDraft(context.Background(), 7, 3, 4) }},
		{"analyze", func() error { _, err := service.Analyze(context.Background(), 7, 3, "SELECT 1"); return err }},
		{"analyze schema", func() error {
			_, err := service.AnalyzeDesired(context.Background(), 7, 3, emptySnapshot("postgresql"))
			return err
		}},
		{"analyze changes", func() error { _, err := service.AnalyzeChanges(context.Background(), 7, 3, nil); return err }},
		{"execute", func() error {
			_, err := service.Execute(context.Background(), 7, 3, migration.ExecuteInput{})
			return err
		}},
		{"reconcile", func() error { return service.Reconcile(context.Background(), 7, 3) }},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			stub.append(permissionStep(access.ViewOnly))
			wantError(t, tc.call(), access.ErrForbidden)
		})
	}
}
func TestMigrationHistoryAllowsReaders(t *testing.T) {
	_, store, stub := newSQLStub(t)
	_, _, service := newServices(t, store)
	stub.append(permissionStep(access.ViewOnly), queryStep("FROM migration_executions e", []string{"id"}))
	history, err := service.History(context.Background(), 7, 3, 50, 0)
	if err != nil || len(history) != 0 {
		t.Fatalf("reader history request failed: %v", err)
	}
	stub.append(permissionStep(access.NoAccess))
	_, err = service.History(context.Background(), 7, 3, 50, 0)
	wantError(t, err, access.ErrForbidden)
	stub.append(permissionStep(access.ViewOnly))
	_, err = service.Detail(context.Background(), 7, 3, 0)
	wantError(t, err, access.ErrInvalid)
	stub.append(permissionStep(access.ViewOnly))
	_, err = service.History(context.Background(), 7, 3, 201, 0)
	wantError(t, err, access.ErrInvalid)
}
func TestDraftSaveAndOptimisticUpdate(t *testing.T) {
	_, store, stub := newSQLStub(t)
	_, _, service := newServices(t, store)
	stub.append(permissionStep(access.ViewEdit), permissionStep(access.ViewEdit), metadataStep(), draftStep("INSERT INTO migration_drafts"))
	saved, err := service.SaveDraft(context.Background(), 7, 3, migration.DraftInput{Title: "A draft", SQL: "SELECT 1"})
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID != 4 || saved.SQL != "SELECT 1" {
		t.Fatal("created draft content not returned")
	}
	stub.append(permissionStep(access.ViewEdit), permissionStep(access.ViewEdit), metadataStep(), draftStep("UPDATE migration_drafts SET"))
	_, err = service.SaveDraft(context.Background(), 7, 3, migration.DraftInput{ID: 4, Title: "A draft", SQL: "SELECT 1", ExpectedUpdatedAt: testTime})
	if err != nil {
		t.Fatal(err)
	}
	stub.append(permissionStep(access.ViewEdit), permissionStep(access.ViewEdit), metadataStep())
	_, err = service.SaveDraft(context.Background(), 7, 3, migration.DraftInput{ID: 4, Title: "A draft", SQL: "SELECT 1"})
	wantError(t, err, access.ErrInvalid)
}
func TestDraftOptimisticConflictAndDeletion(t *testing.T) {
	_, store, stub := newSQLStub(t)
	_, _, service := newServices(t, store)
	stub.append(permissionStep(access.ViewEdit), permissionStep(access.ViewEdit), metadataStep(), queryStep("UPDATE migration_drafts SET", []string{"id"}), draftStep("FROM migration_drafts WHERE"))
	_, err := service.SaveDraft(context.Background(), 7, 3, migration.DraftInput{ID: 4, Title: "A draft", SQL: "SELECT 1", ExpectedUpdatedAt: testTime})
	wantError(t, err, access.ErrConflict)
	stub.append(permissionStep(access.ViewEdit), execStep("DELETE FROM migration_drafts"))
	if err := service.DeleteDraft(context.Background(), 7, 3, 4); err != nil {
		t.Fatal(err)
	}
}
func TestExecutionRejectsUntrustedOrEditedReviewBeforeConnecting(t *testing.T) {
	_, store, stub := newSQLStub(t)
	_, _, service := newServices(t, store)
	for _, review := range []migration.Analysis{{}, {DatabaseID: 99, SQL: "SELECT 1", SQLHash: migration.SQLHash("SELECT 1")}, {DatabaseID: 3, BaseVersion: -1}, {DatabaseID: 3, SQL: "SELECT 2", SQLHash: migration.SQLHash("SELECT 1")}, {DatabaseID: 3, SQL: "SELECT 1", SQLHash: migration.SQLHash("SELECT 1"), SchemaFingerprint: "schema", ConnectionFingerprint: "connection", DBMS: "postgresql", Mode: engines.Transactional, Signature: "forged"}} {
		stub.append(permissionStep(access.ViewEdit))
		_, err := service.Execute(context.Background(), 7, 3, migration.ExecuteInput{Review: review})
		wantError(t, err, migration.ErrStale)
	}
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

func TestManualOutcomeResolutionRequiresAdminAndDatabaseEdit(t *testing.T) {
	_, store, stub := newSQLStub(t)
	_, _, service := newServices(t, store)
	stub.append(scalarStep("is_platform_admin", false))
	wantError(t, service.ResolveOutcome(context.Background(), 7, 3, 1, true, "Verified target state"), access.ErrForbidden)
	stub.append(scalarStep("is_platform_admin", true), permissionStep(access.ViewOnly))
	wantError(t, service.ResolveOutcome(context.Background(), 7, 3, 1, true, "Verified target state"), access.ErrForbidden)
	stub.append(scalarStep("is_platform_admin", true), permissionStep(access.ViewEdit))
	wantError(t, service.ResolveOutcome(context.Background(), 7, 3, 1, true, "  "), access.ErrInvalid)
}
