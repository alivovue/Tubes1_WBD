package database_test

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"schakker_shared/database"
)

func TestClassifyPreservesUnexpectedErrors(t *testing.T) {
	unavailable := errors.New("database unavailable")
	for _, test := range []struct{ input, want error }{
		{nil, nil},
		{fmt.Errorf("query: %w", sql.ErrNoRows), database.ErrNotFound},
		{&pgconn.PgError{Code: "23505"}, database.ErrConflict},
		{fmt.Errorf("insert: %w", &pgconn.PgError{Code: "23503"}), database.ErrConflict},
		{&pgconn.PgError{Code: "23514"}, database.ErrInvalid},
		{&pgconn.PgError{Code: "22001"}, database.ErrInvalid},
		{&pgconn.PgError{Code: "22P02"}, database.ErrInvalid},
		{unavailable, unavailable},
	} {
		if got := database.Classify(test.input); !errors.Is(got, test.want) {
			t.Fatalf("classified %v as %v; want %v", test.input, got, test.want)
		}
	}
	serialization := &pgconn.PgError{Code: "40001"}
	if database.Classify(serialization) != serialization {
		t.Fatal("classification hid a retryable transaction error")
	}
}

func TestAffectedDistinguishesMissingRowsFromFailures(t *testing.T) {
	if err := database.Affected(driver.RowsAffected(0), nil); !errors.Is(err, database.ErrNotFound) {
		t.Fatalf("missing update returned %v", err)
	}
	if err := database.Affected(driver.RowsAffected(1), nil); err != nil {
		t.Fatal(err)
	}
	if err := database.Affected(nil, &pgconn.PgError{Code: "23503"}); !errors.Is(err, database.ErrConflict) {
		t.Fatalf("constraint failure returned %v", err)
	}
}
