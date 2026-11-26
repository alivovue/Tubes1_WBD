package backend_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"tubes1_wbd_admin/backend/internalapi"
	"tubes1_wbd_admin/database/dao"
)

func TestPrivateAuthorityRejectsBrowserAndUnauthenticatedRequestsBeforeSQL(t *testing.T) {
	var calls atomic.Int32
	db := sql.OpenDB(noSQLConnector{calls: &calls})
	defer db.Close()
	store, err := dao.New(db)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("s", 48)
	handler, err := internalapi.NewHandler(store, make([]byte, 32), token)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, auth, origin, cookie, fetch, remote string }{
		{name: "missing token", remote: "127.0.0.1:2"},
		{name: "wrong token", auth: "Bearer wrong", remote: "127.0.0.1:2"},
		{name: "browser origin", auth: "Bearer " + token, origin: "https://admin.test", remote: "127.0.0.1:2"},
		{name: "browser fetch", auth: "Bearer " + token, fetch: "cors", remote: "127.0.0.1:2"},
		{name: "session cookie", auth: "Bearer " + token, cookie: "schakker_admin_session=x", remote: "127.0.0.1:2"},
		{name: "unencrypted remote", auth: "Bearer " + token, remote: "192.0.2.4:2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/internal/v1/users/1", nil)
			req.RemoteAddr = tc.remote
			req.Header.Set("Authorization", tc.auth)
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("Cookie", tc.cookie)
			req.Header.Set("Sec-Fetch-Mode", tc.fetch)
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != 401 || res.Header().Get("Cache-Control") != "no-store" || res.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatalf("private boundary failed: %d", res.Code)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatal("rejected private request reached SQL")
	}
	public := handlerForPrivateBoundary(t)
	for _, route := range []string{"/internal/v1/users/1", "/api/internal/v1/users/1", "/internal/v1/authenticate-user"} {
		req := httptest.NewRequest(http.MethodGet, route, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res := httptest.NewRecorder()
		public.ServeHTTP(res, req)
		if res.Code != 404 {
			t.Fatalf("public listener exposes %s", route)
		}
	}
}
func handlerForPrivateBoundary(t *testing.T) http.Handler { return handler(t, false) }
