package observability_test

import (
	"context"
	"strings"
	"testing"

	"schakker_shared/observability"
)

func TestSQLStructureMasksLiteralFormsAndComments(t *testing.T) {
	queries := []string{
		`SELECT 'private-value', E'escaped\\private-value', 'doubled''private-value', $tag$private-value$tag$, $$private-value$$, 12345 WHERE id=$1`,
		"SELECT 1 -- private-value\nFROM users /* nested /* private-value */ still-private */ WHERE id=$2",
		`SELECT "private-value", X'70726976617465', .12345, 1.2345e-10, 0x12345`,
		`SELECT 'unterminated-private-value`,
		`SELECT $body$unterminated-private-value`,
	}
	for _, query := range queries {
		safe := observability.SafeSQL(query)
		for _, secret := range []string{"private-value", "still-private", "12345", "70726976617465"} {
			if strings.Contains(safe, secret) {
				t.Fatalf("literal/comment leaked: %q", safe)
			}
		}
		if !strings.Contains(safe, "SELECT") {
			t.Fatal("query structure was lost")
		}
	}
	safe := observability.SafeSQL(`UPDATE users SET password=$1 WHERE id=$2 AND version12=42`)
	if !strings.Contains(safe, "password=$1") || !strings.Contains(safe, "id=$2") || !strings.Contains(safe, "version12=?") {
		t.Fatalf("placeholder/identifier structure lost: %q", safe)
	}
}

func TestSQLQuotedNamesFollowTheActualServerDialect(t *testing.T) {
	query := `SELECT "account_name" FROM "users" WHERE note='private-value'`
	pg := observability.SQLText(observability.WithSQLDialect(context.Background(), "postgresql"), query)
	if !strings.Contains(pg, `"account_name" FROM "users"`) || strings.Contains(pg, "private-value") {
		t.Fatalf("PostgreSQL names or privacy incorrect: %q", pg)
	}
	for _, dialect := range []string{"", "mariadb"} {
		text := observability.SQLText(observability.WithSQLDialect(context.Background(), dialect), `SELECT "private-value"`)
		if strings.Contains(text, "private-value") {
			t.Fatal("ambiguous double-quoted value leaked")
		}
	}
	for _, query := range []string{`SELECT "trailing\" FROM users WHERE note='private-value'`, `SELECT "unterminated private-value`, "SELECT `unterminated private-value"} {
		if strings.Contains(observability.SQLText(observability.WithSQLDialect(context.Background(), "postgresql"), query), "private-value") {
			t.Fatal("quoted identifier swallowed and exposed a private literal")
		}
	}
}
