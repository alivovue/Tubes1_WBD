package unit_test

import (
	"strings"
	"testing"

	"tubes1_wbd/backend/dbmanagement/adapters"
	"tubes1_wbd/backend/migration"
)

func TestSQLSplitterRespectsDialectLiteralsAndComments(t *testing.T) {
	cases := []struct {
		name, dbms, sql string
		commands        []string
	}{
		{"multiple statements", "postgresql", "SELECT 1; UPDATE people SET name='done';", []string{"SELECT", "UPDATE"}},
		{"semicolon inside string", "postgresql", "SELECT 'a; b''; c'; SELECT 2", []string{"SELECT", "SELECT"}},
		{"dollar quotes", "postgresql", "DO $body$ BEGIN PERFORM 1; PERFORM 2; END $body$; SELECT 1", []string{"DO", "SELECT"}},
		{"nested comments", "postgresql", "/* outer /* nested; */ end */ SELECT 1; -- trailing;\nSELECT 2", []string{"SELECT", "SELECT"}},
		{"escaped string", "postgresql", `SELECT E'one\';two'; SELECT 2`, []string{"SELECT", "SELECT"}},
		{"quoted identifier", "postgresql", `SELECT "semi;colon" FROM "tab""le"; SELECT 2`, []string{"SELECT", "SELECT"}},
		{"MariaDB backticks", "mariadb", "SELECT `semi;colon` FROM `tab``le`; SELECT 2", []string{"SELECT", "SELECT"}},
		{"MariaDB comments", "mariadb", "# comment;\nSELECT 1; -- comment;\nSELECT 'it''s; safe'", []string{"SELECT", "SELECT"}},
		{"empty delimiters", "postgresql", ";; SELECT 1;;;", []string{"SELECT"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			statements, err := adapters.ParseSQL(tc.sql, tc.dbms)
			if err != nil {
				t.Fatal(err)
			}
			if len(statements) != len(tc.commands) {
				t.Fatalf("got %d statements; want %d", len(statements), len(tc.commands))
			}
			for i, statement := range statements {
				if statement.Command != tc.commands[i] || strings.TrimSpace(statement.SQL) == "" {
					t.Fatalf("incorrect statement %d: %#v", i, statement)
				}
			}
		})
	}
}
func TestSQLSplitterRejectsInvalidInput(t *testing.T) {
	for _, tc := range []struct{ name, dbms, sql string }{
		{"empty", "postgresql", ""}, {"comments only", "postgresql", "/* comment */ -- nothing"}, {"unterminated string", "postgresql", "SELECT 'missing"},
		{"unterminated identifier", "postgresql", `SELECT "missing`}, {"unterminated dollar quote", "postgresql", "DO $tag$ missing"},
		{"unterminated comment", "postgresql", "SELECT 1 /* missing"}, {"null byte", "postgresql", "SELECT \x00"}, {"invalid UTF8", "postgresql", string([]byte{0xff})},
		{"too large", "postgresql", strings.Repeat("x", (1<<20)+1)}, {"quoted command", "postgresql", `"SELECT" 1`},
		{"executable comment", "mariadb", "SELECT 1 /*!50000 INTO OUTFILE '/tmp/out' */"}, {"MariaDB executable comment", "mariadb", "/*M! SET x=1 */ SELECT 1"},
		{"MariaDB nested comment", "mariadb", "/* outer /* inner */ */ SELECT 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := adapters.ParseSQL(tc.sql, tc.dbms)
			wantError(t, err, adapters.ErrInvalid)
		})
	}
}
func TestMigrationSQLPolicy(t *testing.T) {
	for _, dbms := range []string{"postgresql", "mariadb"} {
		t.Run(dbms, func(t *testing.T) {
			for _, query := range []string{"CREATE TABLE people(id INTEGER)", "ALTER TABLE people ADD COLUMN age INTEGER", "INSERT INTO people VALUES (1)", "UPDATE people SET id=2", "DELETE FROM people WHERE id=2", "SELECT 'COMMIT; DROP DATABASE fake'", "WITH rows AS (SELECT 1) SELECT * FROM rows"} {
				if _, err := migration.ValidateSQL(query, dbms); err != nil {
					t.Errorf("valid query rejected: %s: %v", query, err)
				}
			}
			for _, query := range []string{"BEGIN; SELECT 1; COMMIT", "ROLLBACK", "SAVEPOINT manual", "SET autocommit=0", "CREATE DATABASE other", "DROP USER employee", "ALTER SYSTEM SET shared_buffers='1GB'"} {
				if _, err := migration.ValidateSQL(query, dbms); err == nil {
					t.Errorf("unsafe query accepted: %s", query)
				}
			}
		})
	}
	for _, query := range []string{"CREATE INDEX CONCURRENTLY idx ON people(id)", "SELECT set_config('role','admin',false)", `SELECT * FROM "_schakker".migration_receipts`} {
		if _, err := migration.ValidateSQL(query, "postgresql"); err == nil {
			t.Errorf("PostgreSQL policy accepted %s", query)
		}
	}
	for _, query := range []string{"SELECT GET_LOCK('migration',1)", "SELECT `RELEASE_ALL_LOCKS`()", "SELECT * FROM _schakker_migration_receipts", "RENAME USER old TO new", "SELECT 1 INTO OUTFILE '/tmp/export'"} {
		if _, err := migration.ValidateSQL(query, "mariadb"); err == nil {
			t.Errorf("MariaDB policy accepted %s", query)
		}
	}
	if _, err := migration.ValidateSQL("SELECT 1", "sqlite"); err == nil {
		t.Fatal("unsupported DBMS accepted")
	}
}
func TestMariaDBReferencesStayWithinSelectedDatabase(t *testing.T) {
	for _, query := range []string{
		"SELECT people.id FROM people WHERE people.id=1", "SELECT p.id,q.id FROM people p JOIN people q ON p.id=q.id", "SELECT target.people.id FROM target.people",
		"WITH c AS (SELECT id FROM people) SELECT c.id FROM c", "SELECT p.id FROM (SELECT id FROM people) p", "INSERT INTO people(id,name) VALUES(1,'x') ON DUPLICATE KEY UPDATE name='x',age=1",
	} {
		statements, err := adapters.ParseSQL(query, "mariadb")
		if err != nil {
			t.Fatal(err)
		}
		if err := adapters.ValidateMariaReferences("target", statements[0]); err != nil {
			t.Errorf("same-database reference rejected: %s: %v", query, err)
		}
	}
	for _, query := range []string{"SELECT * FROM other.secret", "SELECT * FROM people other,other.secret", "UPDATE people other,other.secret s SET s.id=1", "SELECT x.id FROM (SELECT id FROM other.secret) x", "SELECT other.secret.id FROM people other"} {
		statements, err := adapters.ParseSQL(query, "mariadb")
		if err != nil {
			t.Fatal(err)
		}
		if err := adapters.ValidateMariaReferences("target", statements[0]); err == nil {
			t.Errorf("cross-database reference accepted: %s", query)
		}
	}
}
func TestExplainableStatementClassification(t *testing.T) {
	statements, err := adapters.ParseSQL("CREATE TABLE people(id integer); INSERT INTO people VALUES(1); UPDATE people SET id=2; DELETE FROM people; SELECT 1", "postgresql")
	if err != nil {
		t.Fatal(err)
	}
	for i, statement := range statements {
		if statement.Explainable != (i > 0) {
			t.Errorf("%s explainable = %v", statement.Command, statement.Explainable)
		}
	}
}
