package backend_test

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	admin "tubes1_wbd_admin/backend"
	"tubes1_wbd_admin/backend/access"
	httpapi "tubes1_wbd_admin/backend/api/auth"
	"tubes1_wbd_admin/backend/connectionconfig"
	"tubes1_wbd_admin/database/dao"
)

type noSQLDriver struct{}

func (noSQLDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("unexpected SQL connection")
}

type noSQLConnector struct{ calls *atomic.Int32 }

func (c noSQLConnector) Driver() driver.Driver { return noSQLDriver{} }
func (c noSQLConnector) Connect(context.Context) (driver.Conn, error) {
	c.calls.Add(1)
	return nil, errors.New("unexpected SQL connection")
}
func handler(t *testing.T, production bool) http.Handler {
	t.Helper()
	var calls atomic.Int32
	db := sql.OpenDB(noSQLConnector{calls: &calls})
	t.Cleanup(func() {
		db.Close()
		if calls.Load() != 0 {
			t.Fatal("unauthenticated request reached metadata SQL")
		}
	})
	store, err := dao.New(db)
	if err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{21}, 32)
	auth, err := access.NewService(store, access.DefaultConfig(key))
	if err != nil {
		t.Fatal(err)
	}
	authentication := auth
	connections, err := connectionconfig.NewService(store, auth, connectionconfig.DefaultConfig(key, production))
	if err != nil {
		t.Fatal(err)
	}
	_, filename, _, _ := runtime.Caller(0)
	app, err := admin.NewHandlerWithServices(filepath.Join(filepath.Dir(filename), "..", "..", "..", "..", "src-admin"), admin.Services{Authentication: authentication, Access: auth, Connections: connections, Production: production})
	if err != nil {
		t.Fatal(err)
	}
	return app
}
func TestAdminOnlyRoutesAndJSONAuthentication(t *testing.T) {
	app := handler(t, false)
	for _, route := range []string{"/", "/login", "/admin/users", "/admin/groups", "/admin/access", "/admin/connections"} {
		response := httptest.NewRecorder()
		app.ServeHTTP(response, httptest.NewRequest(http.MethodGet, route, nil))
		if response.Code != 200 || !strings.Contains(response.Body.String(), `<div id="root"></div>`) {
			t.Fatalf("SPA unavailable on %s: %d", route, response.Code)
		}
	}
	for _, route := range []string{"/dashboard", "/databases", "/explorer", "/sql-viewer", "/migrations", "/migrations/new"} {
		for _, prefix := range []string{"", "/api"} {
			response := httptest.NewRecorder()
			app.ServeHTTP(response, httptest.NewRequest(http.MethodGet, prefix+route, nil))
			if response.Code != 404 {
				t.Fatalf("admin exposes %s%s: %d", prefix, route, response.Code)
			}
		}
	}
	for _, route := range []string{"/api/admin/users", "/api/admin/groups", "/api/admin/connections"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			request := httptest.NewRequest(method, route, strings.NewReader(`{}`))
			request.AddCookie(&http.Cookie{Name: "schakker_session", Value: strings.Repeat("a", 43)})
			response := httptest.NewRecorder()
			app.ServeHTTP(response, request)
			if response.Code != 401 || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("admin accepted a user cookie on %s: %d", route, response.Code)
			}
		}
	}
}
func TestAdminLoginChallengeAndHTTPMethods(t *testing.T) {
	for _, production := range []bool{false, true} {
		app := handler(t, production)
		response := httptest.NewRecorder()
		app.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/session", nil))
		var result struct{ CSRF string }
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.CSRF == "" {
			t.Fatal("admin login challenge unavailable")
		}
		var challenge *http.Cookie
		for _, cookie := range response.Result().Cookies() {
			if cookie.Name == httpapi.AdminLoginCookieName {
				challenge = cookie
			}
		}
		if challenge == nil || !challenge.HttpOnly || challenge.Secure != production || challenge.SameSite != http.SameSiteLaxMode || strings.Contains(response.Body.String(), challenge.Value) {
			t.Fatal("admin challenge cookie protections incorrect")
		}
		for _, route := range []string{"/login", "/api/session"} {
			head := httptest.NewRecorder()
			app.ServeHTTP(head, httptest.NewRequest(http.MethodHead, route, nil))
			if head.Code != 200 || head.Body.Len() != 0 {
				t.Fatal("HEAD response included a body")
			}
		}
		for _, route := range []string{"/admin/users", "/api/admin/users", "/api/login", "/api/session"} {
			denied := httptest.NewRecorder()
			app.ServeHTTP(denied, httptest.NewRequest(http.MethodDelete, route, nil))
			if denied.Code != 405 {
				t.Fatalf("unsupported method accepted: %s %d", route, denied.Code)
			}
		}
		login := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"username":"admin","password":"never-return-this"}`))
		request.Header.Set("Content-Type", "application/json")
		app.ServeHTTP(login, request)
		if login.Code != 403 || strings.Contains(login.Body.String(), "never-return-this") {
			t.Fatal("login did not reject missing CSRF before checking credentials")
		}
	}
}
