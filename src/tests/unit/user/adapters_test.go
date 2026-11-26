package unit_test

import (
	"context"
	"strings"
	"testing"

	"tubes1_wbd/backend/dbmanagement/adapters"
	"tubes1_wbd/backend/dbmanagement/adapters/mariadb"
	"tubes1_wbd/backend/dbmanagement/adapters/postgres"
)

func TestAdapterRegistrySelectsDialectAndRejectsDuplicates(t *testing.T) {
	registry, err := adapters.NewRegistry(postgres.New(), mariadb.New())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		input, name string
		port        int
	}{{"", "postgresql", 5432}, {" postgres ", "postgresql", 5432}, {"POSTGRESQL", "postgresql", 5432}, {"maria", "mariadb", 3306}, {" MariaDB ", "mariadb", 3306}} {
		adapter, err := registry.Select(tc.input)
		if err != nil {
			t.Fatal(err)
		}
		if adapter.Name() != tc.name || adapter.DefaultPort() != tc.port {
			t.Fatalf("wrong adapter for %q", tc.input)
		}
	}
	_, err = registry.Select("sqlite")
	wantError(t, err, adapters.ErrUnsupported)
	if _, err := adapters.NewRegistry(nil); err == nil {
		t.Fatal("nil adapter accepted")
	}
	if _, err := adapters.NewRegistry(postgres.New(), postgres.New()); err == nil {
		t.Fatal("duplicate adapter accepted")
	}
	supported := registry.Supported()
	supported[0] = "mutated"
	if registry.Supported()[0] == "mutated" {
		t.Fatal("registry exposed its internal supported list")
	}
}
func TestIdentifierQuoting(t *testing.T) {
	cases := []struct {
		name    string
		adapter adapters.Adapter
		parts   []string
		want    string
	}{
		{"PostgreSQL reserved names", postgres.New(), []string{"public", "select"}, `"public"."select"`},
		{"PostgreSQL embedded delimiter", postgres.New(), []string{`odd"name`}, `"odd""name"`},
		{"MariaDB reserved names", mariadb.New(), []string{"target", "select"}, "`target`.`select`"},
		{"MariaDB embedded delimiter", mariadb.New(), []string{"odd`name"}, "`odd``name`"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.adapter.QuoteIdentifier(tc.parts...)
			if err != nil || got != tc.want {
				t.Fatalf("quoted identifier = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
	for _, adapter := range []adapters.Adapter{postgres.New(), mariadb.New()} {
		for _, parts := range [][]string{nil, {""}, {"null\x00byte"}, {string([]byte{0xff})}, {strings.Repeat("x", 65)}} {
			_, err := adapter.QuoteIdentifier(parts...)
			wantError(t, err, adapters.ErrInvalid)
		}
	}
}
func TestExplorerRejectsInvalidPaginationWithoutQuerying(t *testing.T) {
	db, _, _ := newSQLStub(t)
	for _, adapter := range []adapters.Adapter{postgres.New(), mariadb.New()} {
		for _, options := range []adapters.PageOptions{{Limit: -1}, {Limit: 501}, {Offset: -1}, {OrderBy: make([]adapters.SortColumn, 17)}} {
			_, err := adapter.TableData(context.Background(), db, "target", "people", options)
			wantError(t, err, adapters.ErrInvalid)
		}
	}
}
func TestMariaDBExplorerRejectsOtherDatabase(t *testing.T) {
	db, _, _ := newSQLStub(t, scalarStep("SELECT DATABASE()", "allowed"))
	_, err := mariadb.New().Tables(context.Background(), db, "forbidden")
	wantError(t, err, adapters.ErrInvalid)
}
