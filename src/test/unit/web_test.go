package unit_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

var frontendRoutes = []string{"/", "/login", "/dashboard", "/databases", "/explorer", "/sql-viewer", "/migrations", "/migrations/new"}

func TestWebServesSameSPAShellForDirectNavigation(t *testing.T) {
	handler := newWebHandler(t, false)
	var entry string
	for _, route := range frontendRoutes {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, route+"?db=1", nil))
		if response.Code != 200 || !strings.Contains(response.Body.String(), `<div id="root"></div>`) {
			t.Fatalf("SPA entry unavailable on %s: %d", route, response.Code)
		}
		if entry == "" {
			entry = response.Body.String()
		}
		if response.Body.String() != entry {
			t.Fatalf("%s injected page data into HTML", route)
		}
		if len(response.Result().Cookies()) != 0 {
			t.Fatal("SPA shell unexpectedly issued authentication cookies")
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("SPA shell can be cached")
		}
	}
}

func TestWebRequiresSessionForProtectedAPIs(t *testing.T) {
	handler := newWebHandler(t, false)
	for _, route := range frontendRoutes[2:] {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(method, "/api"+route, strings.NewReader(`{}`)))
			if response.Code != http.StatusUnauthorized || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") || response.Header().Get("Location") != "" {
				t.Errorf("protected API %s %s returned %d", method, route, response.Code)
			}
		}
	}
}

func TestLoginAPIIssuesProtectedCSRFChallenge(t *testing.T) {
	for _, production := range []bool{false, true} {
		t.Run(debugBool(production), func(t *testing.T) {
			handler := newWebHandler(t, production)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/session", nil))
			var body struct{ CSRF string }
			if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &body) != nil || body.CSRF == "" {
				t.Fatal("login CSRF challenge unavailable")
			}
			var challenge *http.Cookie
			for _, cookie := range response.Result().Cookies() {
				if cookie.Name == "schakker_login_csrf" {
					challenge = cookie
				}
			}
			if challenge == nil || !challenge.HttpOnly || challenge.SameSite != http.SameSiteLaxMode || challenge.Secure != production {
				t.Fatal("login cookie flags incorrect")
			}
			if strings.Contains(response.Body.String(), challenge.Value) {
				t.Fatal("login challenge cookie exposed to page scripts")
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("login challenge can be cached")
			}
		})
	}
}

func TestLoginRejectsMissingCSRFBeforeCheckingCredentials(t *testing.T) {
	handler := newWebHandler(t, false)
	request := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"username":"employee","password":"`+testPassword+`"}`))
	request.Header.Set("Content-Type", "application/json")
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
	for _, route := range []string{"/dashboard", "/api/dashboard", "/api/session", "/api/login"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, route, nil))
		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("DELETE %s returned %d", route, response.Code)
		}
	}
	for _, route := range []string{"/login", "/api/session"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodHead, route, nil))
		if response.Code != 200 || response.Body.Len() != 0 {
			t.Fatal("HEAD returned a response body")
		}
	}
	for _, route := range []string{"/missing-page", "/api/missing-page", "/assets/missing.js"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, route, nil))
		if response.Code != 404 {
			t.Fatalf("unknown route %s was not rejected", route)
		}
	}
}

func TestUserServerRemovesAdministrativeRoutes(t *testing.T) {
	handler := newWebHandler(t, false)
	for _, route := range []string{"/admin/users", "/admin/groups", "/admin/access", "/admin/connections"} {
		for _, prefix := range []string{"", "/api"} {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, prefix+route, nil))
			if response.Code != 404 {
				t.Fatalf("administrative route survived on user server: %s%s %d", prefix, route, response.Code)
			}
		}
	}
}
