package integration_test

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"tubes1_wbd/backend/access"
)

func TestSQLViewerHTTPRejectsMalformedBuilderInputsWithoutChangingTarget(t *testing.T) {
	f := newFixture(t, true)
	target := f.register(t)
	f.grant(t, f.editor, target.ID, access.ViewEdit)
	identity, cookie := f.login(t, "it.editor")
	csrf, err := f.access.CSRFToken(identity)
	must(t, err)
	h := userHandler(t, f)
	for _, tc := range []struct{ name, key, value string }{{"non-numeric limit", "limit", "abc"}, {"non-numeric offset", "offset", "abc"}, {"negative limit", "limit", "-1"}, {"excessive limit", "limit", "501"}, {"negative offset", "offset", "-1"}, {"invalid direction", "direction", "sideways"}, {"unknown column", "columns", "missing"}, {"sort injection", "sort", "name; DROP TABLE employees"}, {"unknown filter column", "filter_column", "missing"}, {"invalid operator", "filter_operator", "OR 1=1"}, {"invalid numeric literal", "filter_value", "NaN"}} {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{"csrf": {csrf}, "db": {fmt.Sprint(target.ID)}, "action": {"build_query"}, "schema": {"public"}, "table": {"employees"}, "columns": {"name"}, "filter_column": {"salary"}, "filter_operator": {">="}, "filter_type": {"number"}, "filter_value": {"5000"}, "limit": {"10"}, "offset": {"0"}, "direction": {"asc"}}
			form.Set(tc.key, tc.value)
			res := apiRequest(t, h, http.MethodPost, "/sql-viewer", cookie, form)
			if res.Code != 400 {
				t.Fatalf("malformed builder accepted: %d %s", res.Code, res.Body.String())
			}
		})
	}
	for _, count := range []int{1, 21} {
		form := url.Values{"csrf": {csrf}, "db": {fmt.Sprint(target.ID)}, "action": {"build_query"}, "schema": {"public"}, "table": {"employees"}}
		for i := 0; i < count; i++ {
			form.Add("filter_column", "salary")
			form.Add("filter_operator", ">=")
			form.Add("filter_type", "number")
			if count > 1 {
				form.Add("filter_value", "5000")
			}
		}
		res := apiRequest(t, h, http.MethodPost, "/sql-viewer", cookie, form)
		if res.Code != 400 {
			t.Fatalf("mismatched or excessive filters accepted: %d", res.Code)
		}
	}
	for _, route := range []string{"/sql-viewer?schema=missing", "/sql-viewer?schema=public&table=missing"} {
		res := apiRequest(t, h, http.MethodGet, route+"&db="+fmt.Sprint(target.ID), cookie, nil)
		if res.Code != 404 {
			t.Fatalf("missing object returned %d", res.Code)
		}
	}
	var count int
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, "SELECT count(*) FROM employees").Scan(&count))
	if count != 2 {
		t.Fatal("invalid builder inputs changed data")
	}
}
