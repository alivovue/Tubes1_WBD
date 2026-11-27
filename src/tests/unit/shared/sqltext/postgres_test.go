package sqltext_test

import (
	"strings"
	"testing"

	"schakker_shared/sqltext"
)

func TestPostgresGrammarValidationDoesNotResolveOrExecuteObjects(t *testing.T) {
	for _, query := range []string{
		"CREATE TABLE future_table(id integer); INSERT INTO future_table VALUES(1); DROP TABLE future_table",
		"UPDATE a_table_that_does_not_exist SET value=1 WHERE id=$1",
		"ALTER TABLE future_table ADD COLUMN note text DEFAULT 'not executed'",
	} {
		if err := sqltext.ValidatePostgres(query); err != nil {
			t.Fatalf("valid grammar rejected: %v", err)
		}
	}
	for _, query := range []string{"CREATE TABLE invalid(id integer,,,)", "SELECT (1", "UPDATE x SET", "DROP TABLE ;", "SELECT 'unclosed"} {
		if err := sqltext.ValidatePostgres(query); err == nil {
			t.Fatalf("malformed grammar accepted: %q", query)
		}
	}
}

func TestSQLLogTextPreservesStructureAndMasksLiteralAndCommentSecrets(t *testing.T) {
	for _, query := range []string{
		"SELECT 'secret-value', 987654321 /* secret-value */ FROM employees -- secret-value",
		"CREATE TABLE audit_probe(id integer, note text DEFAULT 'secret-value')",
		"ALTER ROLE audit_user PASSWORD 'secret-value'",
		"SELECT $$secret-value$$, E'secret-value', U&'secret-value'",
	} {
		text := sqltext.ForLog(query)
		if text == "[unavailable]" || !strings.Contains(text, "?") {
			t.Fatalf("SQL structure unavailable: %q", text)
		}
		if strings.Contains(text, "secret-value") || strings.Contains(text, "987654321") {
			t.Fatalf("sensitive value survived redaction: %q", text)
		}
	}
	if text := sqltext.ForLog("CREATE TABLE audit_probe(id integer)"); !strings.Contains(text, "CREATE TABLE audit_probe") {
		t.Fatalf("safe DDL was hidden: %q", text)
	}
	if text := sqltext.ForLog("SELECT 'secret-value"); strings.Contains(text, "secret-value") {
		t.Fatal("malformed input leaked")
	}
}
