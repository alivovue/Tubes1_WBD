package integration_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"tubes1_wbd/backend"
	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement"
	fixtures "tubes1_wbd/test/data"
)

func TestSQLViewerReadsWithoutWritingOrRecordingHistory(t *testing.T) {
	f := newFixture(t, true)
	reader, writer := f.user(t, "viewer.reader"), f.user(t, "viewer.writer")
	target := f.register(t)
	f.grant(t, reader.ID, target.ID, access.ViewOnly)
	f.grant(t, writer.ID, target.ID, access.ViewEdit)
	for _, actor := range []int64{reader.ID, writer.ID} {
		result, err := f.databases.ReadQuery(f.ctx, actor, target.ID, "SELECT name, salary FROM public.employees WHERE salary >= 6000 ORDER BY name")
		must(t, err)
		if len(result.Rows) != 1 || fmt.Sprint(result.Rows[0][0]) != "Bob" || result.Columns[0].Name != "name" {
			t.Fatalf("incorrect filtered result: %#v", result)
		}
		result, err = f.databases.ReadQuery(f.ctx, actor, target.ID, "SELECT current_setting('transaction_read_only') AS mode")
		must(t, err)
		if fmt.Sprint(result.Rows[0][0]) != "on" {
			t.Fatal("viewer transaction is not read-only")
		}
		_, err = f.databases.ReadQuery(f.ctx, actor, target.ID, "WITH gone AS (DELETE FROM public.employees RETURNING *) SELECT * FROM gone")
		wantError(t, err, dbmanagement.ErrReadOnly)
		_, err = f.databases.ReadQuery(f.ctx, actor, target.ID, "SELECT * INTO public.viewer_copy FROM public.employees")
		wantError(t, err, dbmanagement.ErrReadOnly)
	}
	_, err := f.databases.ReadQuery(f.ctx, f.admin, target.ID, "SELECT 1")
	wantError(t, err, access.ErrForbidden)
	query, err := f.databases.BuildReadQuery(f.ctx, reader.ID, target.ID, dbmanagement.ReadQuerySpec{Schema: "public", Table: "employees", Columns: []string{"name"}, Filters: []dbmanagement.ReadFilter{{Column: "salary", Operator: ">=", Value: "5000", Type: "number"}}, OrderBy: []dbmanagement.SortColumn{{Name: "salary", Descending: true}}, Limit: 1})
	must(t, err)
	result, err := f.databases.ReadQuery(f.ctx, reader.ID, target.ID, query)
	must(t, err)
	if len(result.Rows) != 1 || fmt.Sprint(result.Rows[0][0]) != "Bob" {
		t.Fatalf("visual query produced unexpected result: %#v", result)
	}
	result, err = f.databases.ReadQuery(f.ctx, reader.ID, target.ID, "SELECT * FROM employees WHERE salary > 99999")
	must(t, err)
	if len(result.Rows) != 0 || len(result.Columns) != 3 {
		t.Fatal("empty query result lost its column metadata")
	}
	result, err = f.databases.ReadQuery(f.ctx, reader.ID, target.ID, "WITH RECURSIVE n AS (SELECT 1 AS i UNION ALL SELECT i + 1 FROM n WHERE i < 600) SELECT i FROM n")
	must(t, err)
	if len(result.Rows) != dbmanagement.ViewerRowLimit || !result.HasMore {
		t.Fatalf("viewer did not cap a large result: %d rows, more=%v", len(result.Rows), result.HasMore)
	}
	history, err := f.migrations.History(f.ctx, reader.ID, target.ID, 50, 0)
	must(t, err)
	if len(history) != 0 || f.tableExists(t, "viewer_copy") {
		t.Fatal("viewer changed schema or wrote migration history")
	}
	var count int
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM employees").Scan(&count))
	if count != 2 {
		t.Fatal("viewer changed employee data")
	}
}

func TestSQLViewerHTTPReaderPageAndCSRFProtection(t *testing.T) {
	f := newFixture(t, true)
	reader := f.user(t, "viewer.http")
	target := f.register(t)
	f.grant(t, reader.ID, target.ID, access.ViewOnly)
	identity, cookie := f.login(t, reader.Username)
	csrf, err := f.access.CSRFToken(identity)
	must(t, err)
	_, filename, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	handler, err := backend.NewHandlerWithServices(root, backend.Services{Access: f.access, Databases: f.databases, Migrations: f.migrations})
	must(t, err)
	request := func(method, route string, form url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, route, strings.NewReader(form.Encode()))
		if method == http.MethodPost {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		r.AddCookie(&http.Cookie{Name: access.CookieName, Value: cookie})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		return response
	}
	response := request(http.MethodGet, fmt.Sprintf("/sql-viewer?db=%d", target.ID), nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "Visual SELECT builder") {
		t.Fatalf("reader viewer page unavailable: %d %s", response.Code, response.Body.String())
	}
	form := url.Values{"db": {fmt.Sprint(target.ID)}, "action": {"run_query"}, "sql": {"SELECT name FROM employees ORDER BY name"}, "csrf": {csrf}}
	response = request(http.MethodPost, "/sql-viewer", form)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "Query results") || !strings.Contains(response.Body.String(), "Alice") {
		t.Fatalf("reader query failed: %d %s", response.Code, response.Body.String())
	}
	visual := url.Values{"db": {fmt.Sprint(target.ID)}, "action": {"build_query"}, "csrf": {csrf}, "schema": {"public"}, "table": {"employees"}, "columns": {"name"}, "filter_column": {"salary"}, "filter_operator": {">="}, "filter_type": {"number"}, "filter_value": {"6000"}, "sort": {"name"}, "direction": {"asc"}, "limit": {"10"}}
	response = request(http.MethodPost, "/sql-viewer", visual)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "SELECT generated") || !strings.Contains(response.Body.String(), "LIMIT 10 OFFSET 0;") || strings.Contains(response.Body.String(), "<h2>Query results</h2>") {
		t.Fatalf("visual preview did not generate SQL without executing: %d %s", response.Code, response.Body.String())
	}
	form.Set("sql", "SELECT '<script>alert(1)</script>' AS payload")
	response = request(http.MethodPost, "/sql-viewer", form)
	if response.Code != 200 || strings.Contains(response.Body.String(), "<script>alert(1)</script>") || !strings.Contains(response.Body.String(), "&lt;script&gt;") {
		t.Fatal("viewer query result was not escaped")
	}
	form.Set("sql", "DROP TABLE employees")
	response = request(http.MethodPost, "/sql-viewer", form)
	if response.Code != 400 || !strings.Contains(response.Body.String(), "read-only SELECT") {
		t.Fatalf("write was not rejected: %d", response.Code)
	}
	form.Set("action", "execute")
	response = request(http.MethodPost, "/sql-viewer", form)
	if response.Code != 400 {
		t.Fatalf("viewer accepted migration action: %d", response.Code)
	}
	form.Set("action", "run_query")
	form.Del("csrf")
	if response := request(http.MethodPost, "/sql-viewer", form); response.Code != 403 {
		t.Fatalf("viewer accepted POST without CSRF: %d", response.Code)
	}
}

func TestSQLViewerMariaDBReadOnlyAndVisualQuery(t *testing.T) {
	requireMariaDBTools(t)
	f := newFixture(t, true)
	reader := f.user(t, "viewer.maria")
	mocks, err := fixtures.OpenWithOptions(f.ctx, t.TempDir(), fixtures.Options{MariaDB: true})
	must(t, err)
	t.Cleanup(mocks.Close)
	input := mocks.MariaDB.Connection()
	target, err := f.databases.SaveConnection(f.ctx, f.admin, 0, input)
	must(t, err)
	f.grant(t, reader.ID, target.ID, access.ViewOnly)
	query, err := f.databases.BuildReadQuery(f.ctx, reader.ID, target.ID, dbmanagement.ReadQuerySpec{Schema: input.DatabaseName, Table: "employees", Columns: []string{"name"}, Filters: []dbmanagement.ReadFilter{{Column: "salary", Operator: ">=", Type: "number", Value: "6000"}}, Limit: 10})
	must(t, err)
	result, err := f.databases.ReadQuery(f.ctx, reader.ID, target.ID, query)
	must(t, err)
	if len(result.Rows) != 1 || string(result.Rows[0][0].([]byte)) != "Bob" {
		t.Fatalf("incorrect MariaDB viewer result: %#v", result)
	}
	result, err = f.databases.ReadQuery(f.ctx, reader.ID, target.ID, "WITH names AS (SELECT name FROM employees) SELECT COUNT(*) AS total FROM names")
	must(t, err)
	if fmt.Sprint(result.Rows[0][0]) != "2" {
		t.Fatalf("MariaDB read CTE failed: %#v", result)
	}
	_, err = f.databases.ReadQuery(f.ctx, reader.ID, target.ID, "SELECT User FROM mysql.user")
	wantError(t, err, dbmanagement.ErrInvalid)
	_, err = f.databases.ReadQuery(f.ctx, reader.ID, target.ID, "DELETE FROM employees")
	wantError(t, err, dbmanagement.ErrReadOnly)
	history, err := f.migrations.History(f.ctx, reader.ID, target.ID, 50, 0)
	must(t, err)
	if len(history) != 0 {
		t.Fatal("MariaDB read query created migration history")
	}
}
