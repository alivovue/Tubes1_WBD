package unit_test

import (
	"context"
	"database/sql/driver"
	"testing"

	"tubes1_wbd/backend/dbmanagement/adapters"
	"tubes1_wbd/backend/dbmanagement/adapters/mariadb"
	"tubes1_wbd/backend/dbmanagement/adapters/postgres"
)

func tableMetadataSteps(dbms string) []sqlStep {
	if dbms == "postgresql" {
		return []sqlStep{
			queryStep("SELECT c.oid::bigint", []string{"oid", "schema", "name", "kind", "rows"}, []driver.Value{int64(42), "public", "people", "r", int64(2)}),
			queryStep("SELECT a.attname,a.attnum", []string{"name", "position", "type", "nullable", "default", "identity", "generated"}, []driver.Value{"id", int64(1), "integer", false, nil, "", ""}, []driver.Value{"name", int64(2), "varchar(80)", true, nil, "", ""}),
			queryStep("SELECT c.conname,c.contype", []string{"unused"}), queryStep("SELECT c.relname,pg_catalog.pg_get_indexdef", []string{"unused"}),
			queryStep("SELECT c.conname,COALESCE", []string{"unused"}), queryStep("SELECT c.relname,COALESCE", []string{"unused"}),
		}
	}
	return []sqlStep{
		scalarStep("SELECT DATABASE()", "target"), scalarStep("SELECT DATABASE()", "target"),
		queryStep("FROM information_schema.TABLES", []string{"schema", "name", "kind", "rows", "engine", "collation"}, []driver.Value{"target", "people", "r", int64(2), "InnoDB", "utf8mb4_general_ci"}),
		queryStep("FROM information_schema.COLUMNS", []string{"name", "position", "type", "nullable", "default", "extra", "generated"}, []driver.Value{"id", int64(1), "integer", false, nil, "", nil}, []driver.Value{"name", int64(2), "varchar(80)", true, nil, "", nil}),
		queryStep("FROM information_schema.TABLE_CONSTRAINTS", []string{"unused"}), queryStep("FROM information_schema.STATISTICS", []string{"unused"}),
	}
}
func TestTableDataPaginationSortingAndRowBufferIsolation(t *testing.T) {
	for _, adapter := range []adapters.Adapter{postgres.New(), mariadb.New()} {
		t.Run(adapter.Name(), func(t *testing.T) {
			schema := "public"
			query := `SELECT * FROM "public"."people" ORDER BY "id" DESC LIMIT $1 OFFSET $2`
			if adapter.Name() == "mariadb" {
				schema = "target"
				query = "SELECT * FROM `target`.`people` ORDER BY `id` DESC LIMIT ? OFFSET ?"
			}
			buffer := []byte("employee")
			data := queryStep(query, []string{"id", "name"}, []driver.Value{int64(3), buffer}, []driver.Value{int64(4), []byte("next page")})
			data.check = func(args []driver.NamedValue) { wantArg(t, args, 0, int64(2)); wantArg(t, args, 1, int64(2)) }
			steps := append(tableMetadataSteps(adapter.Name()), data)
			db, _, _ := newSQLStub(t, steps...)
			page, err := adapter.TableData(context.Background(), db, schema, "people", adapters.PageOptions{Limit: 1, Offset: 2, OrderBy: []adapters.SortColumn{{Name: "id", Descending: true}}})
			if err != nil {
				t.Fatal(err)
			}
			if page.Limit != 1 || page.Offset != 2 || !page.HasMore || len(page.Rows) != 1 || len(page.Columns) != 2 || page.Columns[1].Name != "name" {
				t.Fatalf("incorrect paginated result: %#v", page)
			}
			buffer[0] = 'x'
			if string(page.Rows[0][1].([]byte)) != "employee" {
				t.Fatal("page retained the driver's mutable row buffer")
			}
		})
	}
}
func TestTableDataRejectsUnknownSortColumns(t *testing.T) {
	for _, adapter := range []adapters.Adapter{postgres.New(), mariadb.New()} {
		t.Run(adapter.Name(), func(t *testing.T) {
			db, _, _ := newSQLStub(t, tableMetadataSteps(adapter.Name())...)
			schema := "public"
			if adapter.Name() == "mariadb" {
				schema = "target"
			}
			_, err := adapter.TableData(context.Background(), db, schema, "people", adapters.PageOptions{OrderBy: []adapters.SortColumn{{Name: "id; DROP TABLE people"}}})
			wantError(t, err, adapters.ErrInvalid)
		})
	}
}
func TestPostgreSQLExplorerHidesInternalSchemas(t *testing.T) {
	db, _, _ := newSQLStub(t)
	for _, schema := range []string{"_schakker", "information_schema", "pg_catalog", "pg_temp_1"} {
		_, err := postgres.New().Tables(context.Background(), db, schema)
		wantError(t, err, adapters.ErrInvalid)
	}
	step := queryStep("FROM pg_catalog.pg_namespace", []string{"schema", "owner"}, []driver.Value{"public", "owner"})
	db, _, _ = newSQLStub(t, step)
	schemas, err := postgres.New().Schemas(context.Background(), db)
	if err != nil || len(schemas) != 1 || schemas[0].Name != "public" {
		t.Fatalf("schema catalog not read: %#v, %v", schemas, err)
	}
}
