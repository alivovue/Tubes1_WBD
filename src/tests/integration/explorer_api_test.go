package integration_test

import (
	"fmt"
	"net/http"
	"testing"
	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/api/response"
)

func TestExplorerHTTPPaginatesAndSortsRealRowsWithoutChangingData(t *testing.T) {
	f := newFixture(t, true)
	target := f.register(t)
	f.grant(t, f.editor, target.ID, access.ViewOnly)
	_, err := f.dbs.Managed.ExecContext(f.ctx, `CREATE TABLE public.explorer_pages(id bigint PRIMARY KEY,name text);INSERT INTO public.explorer_pages SELECT i,'employee-'||i FROM generate_series(1,120) i`)
	must(t, err)
	_, cookie := f.login(t, "it.editor")
	h := userHandler(t, f)
	load := func(offset int) response.Data {
		t.Helper()
		route := fmt.Sprintf("/explorer?db=%d&schema=public&table=explorer_pages&sort=id&direction=desc&offset=%d", target.ID, offset)
		res := apiRequest(t, h, http.MethodGet, route, cookie, nil)
		if res.Code != 200 {
			t.Fatalf("explorer status=%d %s", res.Code, res.Body.String())
		}
		return decodeAPI[response.Data](t, res)
	}
	first, second, last := load(0), load(50), load(100)
	if first.CanEdit || len(first.Data.Rows) != 50 || first.Data.Rows[0][0] != "120" || !first.HasMore || first.Next != 50 || first.Previous != 0 {
		t.Fatalf("bad first page: %#v", first)
	}
	if len(second.Data.Rows) != 50 || second.Data.Rows[0][0] != "70" || !second.HasMore || second.Previous != 0 || second.Next != 100 {
		t.Fatal("bad middle page")
	}
	if len(last.Data.Rows) != 20 || last.Data.Rows[0][0] != "20" || last.HasMore || last.Previous != 50 {
		t.Fatal("bad last page")
	}
	for _, extra := range []string{"&offset=-1", "&offset=100000001", "&direction=sideways", "&sort=id%3BDROP+TABLE+explorer_pages"} {
		route := fmt.Sprintf("/explorer?db=%d&schema=public&table=explorer_pages%s", target.ID, extra)
		res := apiRequest(t, h, http.MethodGet, route, cookie, nil)
		if res.Code != 400 {
			t.Fatalf("invalid explorer query returned %d: %s", res.Code, res.Body.String())
		}
	}
	var count int
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, "SELECT count(*) FROM explorer_pages").Scan(&count))
	if count != 120 {
		t.Fatal("explorer changed target rows")
	}
}
