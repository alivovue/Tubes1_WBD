package unit_test

import (
	"context"
	"strings"
	"testing"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/dbmanagement/adapters"
	"tubes1_wbd/backend/dbmanagement/adapters/mariadb"
	"tubes1_wbd/backend/dbmanagement/adapters/postgres"
)

func TestSQLViewerValidatesReadQueries(t *testing.T) {
	for _, dbms := range []string{"postgresql", "mariadb"} {
		for _, query := range []string{
			"SELECT 1;",
			"SELECT name, salary FROM employees WHERE salary >= 5000 ORDER BY name LIMIT 10",
			"WITH filtered AS (SELECT * FROM employees WHERE salary > 5000) SELECT COUNT(*) FROM filtered",
			"SELECT e.name, COUNT(*) FROM employees e JOIN employees f ON e.id = f.id GROUP BY e.name",
			"SELECT 'DELETE; DROP TABLE employees; INTO OUTFILE' AS text",
			"/* UPDATE employees */ SELECT SUM(salary), COALESCE(MAX(salary), 0) FROM employees",
			"SELECT * FROM employees WHERE name IN ('Alice', 'Bob') AND salary > (SELECT AVG(salary) FROM employees)",
		} {
			t.Run(dbms+"/"+query, func(t *testing.T) {
				if _, err := adapters.ValidateReadQuery(query, dbms); err != nil {
					t.Fatalf("read query rejected: %v", err)
				}
			})
		}
		for _, query := range []string{
			"", "-- empty", "SELECT 1; DELETE FROM employees", "UPDATE employees SET salary=0", "CREATE TABLE nope(id int)",
			"WITH removed AS (DELETE FROM employees RETURNING *) SELECT * FROM removed",
			"WITH x AS (SELECT 1) UPDATE employees SET salary=0",
			"SELECT * INTO saved FROM employees", "SELECT * FROM employees FOR UPDATE", "SELECT * FROM employees LOCK IN SHARE MODE",
			"SELECT name INTO OUTFILE '/tmp/leak' FROM employees", "SELECT name INTO DUMPFILE '/tmp/leak' FROM employees",
			"SELECT nextval('employees_id_seq')", "SELECT set_config('transaction_read_only','off',true)",
			"SELECT pg_read_file('/etc/passwd')", "SELECT dblink_exec('remote', 'DELETE FROM employees')",
			"SELECT custom_function()", "SELECT public.count(*) FROM employees", `SELECT "custom"()`,
			"SELECT * FROM _schakker.migration_receipts", "SELECT @x := 1", "COMMIT", "EXPLAIN ANALYZE DELETE FROM employees",
		} {
			t.Run(dbms+"/reject/"+query, func(t *testing.T) {
				_, err := adapters.ValidateReadQuery(query, dbms)
				wantError(t, err, adapters.ErrReadOnly)
			})
		}
	}
	_, err := adapters.ValidateReadQuery("SELECT 1 /*! UNION SELECT 2 */", "mariadb")
	wantError(t, err, adapters.ErrReadOnly)
}

func TestSQLViewerBuildsQuotedReadSQLAndRejectsInjectedInputs(t *testing.T) {
	for _, adapter := range []adapters.Adapter{postgres.New(), mariadb.New()} {
		t.Run(adapter.Name(), func(t *testing.T) {
			detail := dbmanagement.TableDetail{Table: dbmanagement.Table{Schema: "public", Name: "employees"}, Columns: []dbmanagement.Column{{Name: "name"}, {Name: "salary"}}}
			spec := dbmanagement.ReadQuerySpec{Schema: "public", Table: "employees", Columns: []string{"name"}, Distinct: true, Filters: []dbmanagement.ReadFilter{{Column: "name", Operator: "=", Value: "O'Brien'; DROP TABLE employees; --", Type: "text"}, {Column: "salary", Operator: ">=", Value: "5000", Type: "number"}}, OrderBy: []dbmanagement.SortColumn{{Name: "salary", Descending: true}}, Limit: 25, Offset: 10}
			query, err := dbmanagement.BuildReadSQL(adapter, detail, spec)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(query, "SELECT DISTINCT") || !strings.Contains(query, "O''Brien''; DROP TABLE employees; --") || !strings.Contains(query, "DESC\nLIMIT 25 OFFSET 10;") {
				t.Fatalf("unexpected generated query: %s", query)
			}
			statements, err := adapters.ParseSQL(query, adapter.Name())
			if err != nil || len(statements) != 1 || statements[0].Command != "SELECT" {
				t.Fatalf("filter value escaped out of SELECT: %#v, %v", statements, err)
			}
			precise := spec
			precise.Filters = []dbmanagement.ReadFilter{{Column: "salary", Operator: "=", Type: "number", Value: "9007199254740993.123456789"}}
			query, err = dbmanagement.BuildReadSQL(adapter, detail, precise)
			if err != nil || !strings.Contains(query, "9007199254740993.123456789") {
				t.Fatalf("numeric filter lost precision: %s (%v)", query, err)
			}
			for _, change := range []func(*dbmanagement.ReadQuerySpec){
				func(s *dbmanagement.ReadQuerySpec) { s.Columns = []string{"name; DELETE FROM employees"} },
				func(s *dbmanagement.ReadQuerySpec) {
					s.Filters = []dbmanagement.ReadFilter{{Column: "name", Operator: "= 1; DROP TABLE employees"}}
				},
				func(s *dbmanagement.ReadQuerySpec) {
					s.Filters = []dbmanagement.ReadFilter{{Column: "salary", Operator: "=", Type: "number", Value: "1; DELETE FROM employees"}}
				},
				func(s *dbmanagement.ReadQuerySpec) {
					s.Filters = []dbmanagement.ReadFilter{{Column: "salary", Operator: "=", Type: "number", Value: "NaN"}}
				},
				func(s *dbmanagement.ReadQuerySpec) { s.Limit = 501 },
				func(s *dbmanagement.ReadQuerySpec) { s.Offset = -1 },
				func(s *dbmanagement.ReadQuerySpec) { s.Table = "another_table" },
			} {
				invalid := spec
				change(&invalid)
				_, err := dbmanagement.BuildReadSQL(adapter, detail, invalid)
				wantError(t, err, access.ErrInvalid)
			}
		})
	}
}

func TestSQLViewerRequiresReadPermissionBeforeConnecting(t *testing.T) {
	_, store, stub := newSQLStub(t)
	_, service, _ := newServices(t, store)
	stub.append(permissionStep(access.NoAccess))
	_, err := service.ReadQuery(context.Background(), 7, 3, "SELECT 1")
	wantError(t, err, access.ErrForbidden)
	stub.append(permissionStep(access.NoAccess))
	_, err = service.BuildReadQuery(context.Background(), 7, 3, dbmanagement.ReadQuerySpec{})
	wantError(t, err, access.ErrForbidden)
}
