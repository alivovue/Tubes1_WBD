package access

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestAdminFormsIntegration(t *testing.T) {
	f := integration(t)
	ctx := context.Background()
	handler, err := f.s.Handler()
	must(t, err)
	i, raw := f.login(t, "admin", "admin-password-123")
	cookie := &http.Cookie{Name: CookieName, Value: raw}
	post := func(path string, values url.Values, want int) {
		t.Helper()
		values.Set("csrf_token", f.s.csrf(i))
		status(t, perform(handler, "POST", path, values, cookie), want)
	}
	post("/admin/users/create", url.Values{"username": {"alice"}, "display_name": {"Alice"}, "password": {"employee-password"}}, 303)
	var user int64
	must(t, f.db.QueryRow(`SELECT id FROM users WHERE username='alice'`).Scan(&user))
	post("/admin/users/update", url.Values{"id": {strconv.FormatInt(user, 10)}, "display_name": {"<script>alert(1)</script>"}, "active": {"on"}}, 303)
	post("/admin/groups/save", url.Values{"name": {"Readers"}, "description": {"Read access"}}, 303)
	var group int64
	must(t, f.db.QueryRow(`SELECT id FROM access_groups WHERE name='Readers'`).Scan(&group))
	post("/admin/groups/save", url.Values{"id": {strconv.FormatInt(group, 10)}, "name": {"Readers updated"}}, 303)
	post("/admin/memberships", url.Values{"user_id": {strconv.FormatInt(user, 10)}, "group_id": {strconv.FormatInt(group, 10)}, "member": {"on"}}, 303)
	db := f.database(t, "Products")
	post("/admin/grants", url.Values{"group_id": {strconv.FormatInt(group, 10)}, "database_id": {strconv.FormatInt(db, 10)}, "permission": {"view_edit"}}, 303)
	permission, err := f.s.EffectivePermission(ctx, user, db)
	must(t, err)
	if permission != ViewEdit {
		t.Fatal("admin form grant not persisted")
	}
	_, employeeRaw := f.login(t, "alice", "employee-password")
	employeeCookie := &http.Cookie{Name: CookieName, Value: employeeRaw}
	res := perform(handler, "GET", "/", nil, employeeCookie)
	status(t, res, 200)
	if strings.Contains(res.Body.String(), "<script>alert(1)</script>") || !strings.Contains(res.Body.String(), "&lt;script&gt;") {
		t.Fatal("portal display name was not HTML escaped")
	}
	post("/admin/grants", url.Values{"group_id": {strconv.FormatInt(group, 10)}, "database_id": {strconv.FormatInt(db, 10)}, "permission": {""}}, 303)
	status(t, perform(handler, "GET", "/api/me", nil, employeeCookie), 401)
	post("/admin/memberships", url.Values{"user_id": {strconv.FormatInt(user, 10)}, "group_id": {strconv.FormatInt(group, 10)}}, 303)
	post("/admin/groups/delete", url.Values{"id": {strconv.FormatInt(group, 10)}}, 303)
	post("/admin/databases/disable", url.Values{"id": {strconv.FormatInt(db, 10)}}, 303)
	for _, path := range []string{"/admin/users/update", "/admin/groups/delete", "/admin/memberships", "/admin/grants", "/admin/databases/disable", "/admin/databases/save"} {
		post(path, url.Values{"id": {"invalid"}}, 400)
	}
	post("/admin/groups/delete", url.Values{"id": {"999999"}}, 404)
	post("/admin/users/create", url.Values{"username": {"ALICE"}, "display_name": {"Duplicate"}, "password": {"employee-password"}}, 409)
}

func TestProductionCookieAndCSRFIntegration(t *testing.T) {
	f := integration(t)
	config := DefaultConfig(f.s.config.Key, true)
	config.Now = f.s.config.Now
	production, err := NewService(f.db, config)
	must(t, err)
	handler, err := production.Handler()
	must(t, err)
	page := perform(handler, "GET", "/login", nil)
	status(t, page, 200)
	if page.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("missing HSTS in production")
	}
	for _, cookie := range page.Result().Cookies() {
		if !cookie.Secure || !cookie.HttpOnly {
			t.Fatal("production login cookie missing flags")
		}
	}
	i, raw, err := production.Login(context.Background(), "admin", "admin-password-123", "")
	must(t, err)
	f.clock.Add(int64(config.RotationInterval))
	res := perform(handler, "GET", "/api/me", nil, &http.Cookie{Name: CookieName, Value: raw})
	status(t, res, 200)
	var rotated *http.Cookie
	for _, cookie := range res.Result().Cookies() {
		if cookie.Name == CookieName {
			rotated = cookie
		}
	}
	if rotated == nil || !rotated.Secure || !rotated.HttpOnly || rotated.Value == raw {
		t.Fatal("production rotation cookie missing flags")
	}
	// A form created before rotation remains valid, but an expired login nonce does not.
	status(t, perform(handler, "POST", "/logout", url.Values{"csrf_token": {production.csrf(i)}}, rotated), 303)
	status(t, perform(handler, "POST", "/login", url.Values{"csrf_token": {"invalid"}}, page.Result().Cookies()...), 403)
}
