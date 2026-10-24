package unit_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"tubes1_wbd/backend"
)

func newWebHandler(t *testing.T, production bool) http.Handler {
	t.Helper()
	_, store, _ := newSQLStub(t)
	auth, databases, migrations := newServices(t, store)
	_, filename, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	handler, err := backend.NewHandlerWithServices(root, backend.Services{Access: auth, Databases: databases, Migrations: migrations, Production: production})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}
func TestWebRequiresSessionForProtectedPages(t *testing.T) {
	handler := newWebHandler(t, false)
	for _, route := range []string{"/dashboard", "/databases", "/explorer", "/migrations", "/migrations/new", "/admin/users", "/admin/groups", "/admin/access", "/admin/connections"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, route, nil))
		if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/login" {
			t.Errorf("unprotected page %s returned %d", route, response.Code)
		}
	}
}
func TestLoginFormIssuesProtectedCSRFChallenge(t *testing.T) {
	for _, production := range []bool{false, true} {
		t.Run(debugBool(production), func(t *testing.T) {
			handler := newWebHandler(t, production)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/login", nil))
			if response.Code != 200 || !strings.Contains(response.Body.String(), `name="csrf"`) {
				t.Fatal("login CSRF form unavailable")
			}
			cookies := response.Result().Cookies()
			if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Secure != production {
				t.Fatal("login cookie flags incorrect")
			}
			if strings.Contains(response.Body.String(), cookies[0].Value) {
				t.Fatal("login challenge cookie exposed to page scripts")
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("login page can be cached")
			}
		})
	}
}
func TestLoginRejectsMissingCSRFBeforeCheckingCredentials(t *testing.T) {
	handler := newWebHandler(t, false)
	request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(url.Values{"username": {"employee"}, "password": {testPassword}}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF returned %d; want 403", response.Code)
	}
	if strings.Contains(response.Body.String(), testPassword) {
		t.Fatal("submitted password echoed into the response")
	}
}
func TestWebRejectsUnsupportedMethodsAndKeepsHEADBodyEmpty(t *testing.T) {
	handler := newWebHandler(t, false)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/dashboard", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE returned %d", response.Code)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodHead, "/login", nil))
	if response.Code != 200 || response.Body.Len() != 0 {
		t.Fatal("HEAD returned a response body")
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/missing-page", nil))
	if response.Code != 404 {
		t.Fatal("unknown route was not rejected")
	}
}
