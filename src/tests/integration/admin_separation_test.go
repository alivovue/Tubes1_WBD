package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"tubes1_wbd/backend/access"
	userauth "tubes1_wbd/backend/api/auth"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/migration"
	adminhttp "tubes1_wbd_admin/backend/api/auth"
)

func TestSeparatedApplicationsEnforceAuthenticationDomainsAndManagedEndpointBoundaries(t *testing.T) {
	f := newFixture(t, true)
	target := f.register(t)
	f.grant(t, f.editor, target.ID, access.ViewEdit)
	level, err := f.access.EffectivePermission(f.ctx, f.admin, target.ID)
	must(t, err)
	if level != access.NoAccess {
		t.Fatal("administrator inherited a database grant")
	}
	databases, err := f.access.AccessibleDatabases(f.ctx, f.admin)
	must(t, err)
	if len(databases) != 0 {
		t.Fatal("administrator can list accessible managed databases")
	}
	adminIdentity, adminCookie := f.login(t, "it.admin")
	adminCSRF, err := f.adminAuth.CSRFToken(toAdminIdentity(adminIdentity))
	must(t, err)
	editorIdentity, editorCookie := f.login(t, "it.editor")
	editorCSRF, err := f.access.CSRFToken(editorIdentity)
	must(t, err)
	userServer, adminServer := userHandler(t, f), adminHandler(t, f)
	for _, route := range []string{"/dashboard", "/databases", "/explorer", "/sql-viewer", "/migrations", "/migrations/new"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			response := apiRequest(t, userServer, method, route+"?db="+fmt.Sprint(target.ID), adminCookie, url.Values{"csrf": {adminCSRF}, "db": {fmt.Sprint(target.ID)}, "action": {"analyze"}, "sql": {"SELECT 1"}})
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("user server allowed admin %s %s: %d", method, route, response.Code)
			}
			denied := apiRequest(t, adminServer, method, route, adminCookie, nil, adminhttp.AdminCookieName)
			if denied.Code != http.StatusNotFound {
				t.Fatalf("admin server exposes %s %s: %d", method, route, denied.Code)
			}
		}
	}
	for _, route := range []string{"/admin/users", "/admin/groups", "/admin/connections", "/admin/access"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			removed := apiRequest(t, userServer, method, route, adminCookie, nil)
			if removed.Code != 404 {
				t.Fatalf("administrative route survived on user server: %s %d", route, removed.Code)
			}
			denied := apiRequest(t, adminServer, method, route, editorCookie, url.Values{"csrf": {editorCSRF}, "action": {"save_group"}, "name": {"forbidden"}}, adminhttp.AdminCookieName)
			if denied.Code != 401 {
				t.Fatalf("ordinary editor accessed administration: %s %d", route, denied.Code)
			}
		}
		response := apiRequest(t, adminServer, http.MethodGet, route, adminCookie, nil)
		if response.Code != 200 {
			t.Fatalf("admin page unavailable: %s %d", route, response.Code)
		}
		var data map[string]json.RawMessage
		must(t, json.Unmarshal(response.Body.Bytes(), &data))
		for _, field := range []string{"SQL", "Schemas", "Tables", "Data", "ViewerResult", "History", "Execution", "Review"} {
			if _, exists := data[field]; exists {
				t.Fatalf("admin API exposes managed-database payload %s", field)
			}
		}
	}
	deniedCalls := []struct {
		name string
		call func() error
	}{
		{"schemas", func() error { _, err := f.databases.Schemas(f.ctx, f.admin, target.ID); return err }},
		{"tables", func() error { _, err := f.databases.Tables(f.ctx, f.admin, target.ID, "public"); return err }},
		{"columns", func() error {
			_, err := f.databases.DescribeTable(f.ctx, f.admin, target.ID, "public", "employees")
			return err
		}},
		{"rows", func() error {
			_, err := f.databases.TableData(f.ctx, f.admin, target.ID, "public", "employees", dbmanagement.PageOptions{})
			return err
		}},
		{"snapshot", func() error { _, err := f.databases.Snapshot(f.ctx, f.admin, target.ID); return err }},
		{"read SQL", func() error { _, err := f.databases.ReadQuery(f.ctx, f.admin, target.ID, "SELECT 1"); return err }},
		{"query plans", func() error { _, err := f.databases.QueryPlans(f.ctx, f.admin, target.ID, "SELECT 1"); return err }},
		{"drafts", func() error { _, err := f.migrations.Drafts(f.ctx, f.admin, target.ID, 10, 0); return err }},
		{"history", func() error { _, err := f.migrations.History(f.ctx, f.admin, target.ID, 10, 0); return err }},
		{"analysis", func() error { _, err := f.migrations.Analyze(f.ctx, f.admin, target.ID, "SELECT 1"); return err }},
		{"execution", func() error {
			_, err := f.migrations.Execute(f.ctx, f.admin, target.ID, migration.ExecuteInput{})
			return err
		}},
		{"reconciliation", func() error { return f.migrations.Reconcile(f.ctx, f.admin, target.ID) }},
		{"outcome resolution", func() error {
			return f.migrations.ResolveOutcome(f.ctx, f.admin, target.ID, 1, true, "Manually verified")
		}},
	}
	for _, test := range deniedCalls {
		t.Run(test.name, func(t *testing.T) { wantError(t, test.call(), access.ErrForbidden) })
	}
	ordinary := apiRequest(t, userServer, http.MethodGet, "/explorer?db="+fmt.Sprint(target.ID), editorCookie, nil)
	if ordinary.Code != 200 {
		t.Fatalf("ordinary editor lost explorer access: %d", ordinary.Code)
	}
	// Validation is deliberately constrained to the registered connection.
	// A caller-supplied SQL field cannot be executed through this API.
	input := f.dbs.Connection()
	validation := apiRequest(t, adminServer, http.MethodPost, "/admin/connections", adminCookie, url.Values{
		"csrf": {adminCSRF}, "action": {"test_connection"}, "id": {fmt.Sprint(target.ID)}, "name": {input.Name}, "dbms": {input.DBMS}, "host": {input.Host}, "port": {fmt.Sprint(input.Port)}, "database_name": {input.DatabaseName}, "sslmode": {input.SSLMode}, "sql": {"DELETE FROM public.employees"},
	})
	if validation.Code != 400 {
		t.Fatalf("removed connection test endpoint returned: %d %s", validation.Code, validation.Body.String())
	}
	var count int
	must(t, f.dbs.Managed.QueryRowContext(f.ctx, "SELECT count(*) FROM public.employees").Scan(&count))
	if count != 2 {
		t.Fatal("connection validation ran caller-supplied SQL")
	}
}

func TestSeparatedLoginsCookiesCSRFAndLogout(t *testing.T) {
	f := newFixture(t, true)
	userServer, adminServer := userHandler(t, f), adminHandler(t, f)
	login := func(handler http.Handler, username, challengeName, sessionName string, want int, existing ...*http.Cookie) *http.Cookie {
		t.Helper()
		challenge := apiRequest(t, handler, http.MethodGet, "/session", "", nil)
		csrf := decodeAPI[struct{ CSRF string }](t, challenge).CSRF
		var challengeCookie *http.Cookie
		for _, cookie := range challenge.Result().Cookies() {
			if cookie.Name == challengeName {
				challengeCookie = cookie
			}
		}
		if challengeCookie == nil || !challengeCookie.HttpOnly {
			t.Fatal("protected login challenge missing")
		}
		body, _ := json.Marshal(map[string]string{"username": username, "password": password})
		request := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(string(body)))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", csrf)
		request.AddCookie(challengeCookie)
		for _, cookie := range existing {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != want {
			t.Fatalf("login %s returned %d: %s", username, response.Code, response.Body.String())
		}
		for _, cookie := range response.Result().Cookies() {
			if cookie.Name == sessionName && cookie.Value != "" {
				if want != 200 {
					t.Fatal("denied role received session cookie")
				}
				return cookie
			}
		}
		if want == 200 {
			t.Fatal("successful login omitted its session cookie")
		}
		return nil
	}
	login(userServer, "it.admin", userauth.UserLoginCookieName, access.CookieName, 401)
	login(adminServer, "it.editor", adminhttp.AdminLoginCookieName, adminhttp.AdminCookieName, 401)
	ordinary := login(userServer, "it.editor", userauth.UserLoginCookieName, access.CookieName, 200)
	administrator := login(adminServer, "it.admin", adminhttp.AdminLoginCookieName, adminhttp.AdminCookieName, 200)
	login(userServer, "it.admin", userauth.UserLoginCookieName, access.CookieName, 401, ordinary)
	login(adminServer, "it.editor", adminhttp.AdminLoginCookieName, adminhttp.AdminCookieName, 401, administrator)
	if ordinary.Name == administrator.Name || ordinary.Value == administrator.Value {
		t.Fatal("applications share a cookie namespace or session")
	}
	for _, test := range []struct {
		handler http.Handler
		name    string
	}{{userServer, "it.editor"}, {adminServer, "it.admin"}} {
		request := httptest.NewRequest(http.MethodGet, "/api/session", nil)
		request.AddCookie(ordinary)
		request.AddCookie(administrator)
		response := httptest.NewRecorder()
		test.handler.ServeHTTP(response, request)
		result := decodeAPI[struct{ User struct{ Username string } }](t, response)
		if response.Code != 200 || result.User.Username != test.name {
			t.Fatal("one application consumed the other's session cookie")
		}
	}
	session := apiRequest(t, adminServer, http.MethodGet, "/session", administrator.Value, nil, adminhttp.AdminCookieName)
	csrf := decodeAPI[struct{ CSRF string }](t, session).CSRF
	if denied := apiRequest(t, adminServer, http.MethodPost, "/admin/groups", administrator.Value, url.Values{"action": {"save_group"}, "name": {"No CSRF"}}); denied.Code != 403 {
		t.Fatal("admin accepted an action without CSRF")
	}
	cross := httptest.NewRequest(http.MethodPost, "/api/admin/groups", strings.NewReader(`{"action":"save_group","name":"Cross origin"}`))
	cross.Header.Set("Content-Type", "application/json")
	cross.Header.Set("X-CSRF-Token", csrf)
	cross.Header.Set("Origin", "https://untrusted.example")
	cross.AddCookie(administrator)
	rejected := httptest.NewRecorder()
	adminServer.ServeHTTP(rejected, cross)
	if rejected.Code != 403 {
		t.Fatal("admin accepted a cross-origin action")
	}
	logout := apiRequest(t, adminServer, http.MethodPost, "/logout", administrator.Value, url.Values{"csrf": {csrf}}, adminhttp.AdminCookieName)
	if logout.Code != 200 {
		t.Fatalf("admin logout failed: %d", logout.Code)
	}
	for _, cookie := range logout.Result().Cookies() {
		if cookie.Name == access.CookieName {
			t.Fatal("admin logout overwrote user cookie")
		}
	}
	_, _, err := f.access.Authenticate(f.ctx, ordinary.Value)
	must(t, err)
	_, _, err = f.adminAuth.Authenticate(f.ctx, administrator.Value)
	wantError(t, err, access.ErrUnauthenticated)
}

func TestOrdinaryAccountCannotBecomeAdministrator(t *testing.T) {
	f := newFixture(t, true)
	target := f.register(t)
	f.grant(t, f.editor, target.ID, access.ViewEdit)
	_, cookie := f.login(t, "it.editor")
	wantError(t, f.access.UpdateUser(f.ctx, f.admin, f.editor, "Editor", true, true, ""), access.ErrInvalid)
	response := apiRequest(t, userHandler(t, f), http.MethodGet, "/explorer?db="+fmt.Sprint(target.ID), cookie, nil)
	if response.Code != 200 {
		t.Fatal("rejected promotion changed the ordinary session")
	}
	must(t, f.adminAuth.CreateAdmin(f.ctx, f.admin, "it.editor", "Independent administrator", password))
	_, _, err := f.adminAuth.Authenticate(f.ctx, cookie)
	wantError(t, err, access.ErrUnauthenticated)
	_, _, err = f.userAccess.Authenticate(f.ctx, cookie)
	must(t, err)
}
