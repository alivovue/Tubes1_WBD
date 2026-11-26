package integration_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"tubes1_wbd/backend/access"
	adminaccess "tubes1_wbd_admin/backend/access"
	admindao "tubes1_wbd_admin/database/dao"
)

func TestAccountHTTPActionsPersistIndependentCredentialsAndRevokeSessions(t *testing.T) {
	f := newFixture(t, true)
	h := adminHandler(t, f)
	identity, cookie := f.login(t, "it.admin")
	csrf, err := f.adminAuth.CSRFToken(toAdminIdentity(identity))
	must(t, err)
	mutate := func(route string, form url.Values, want int) {
		t.Helper()
		form.Set("csrf", csrf)
		res := apiRequest(t, h, http.MethodPost, route, cookie, form)
		if res.Code != want {
			t.Fatalf("%s %s: %d %s", route, form.Get("action"), res.Code, res.Body.String())
		}
		if want == 200 {
			if apiLocation(t, res).Path != route {
				t.Fatal("account action navigated to wrong page")
			}
		}
		if strings.Contains(res.Body.String(), password) {
			t.Fatal("account response exposed password")
		}
	}
	for _, admin := range []bool{false, true} {
		t.Run(map[bool]string{false: "employee", true: "administrator"}[admin], func(t *testing.T) {
			route, create, update := "/admin/users", "create_user", "update_user"
			if admin {
				route, create, update = "/admin/administrators", "create_admin", "update_admin"
			}
			username := "account.http.employee"
			if admin {
				username = "account.http.admin"
			}
			mutate(route, url.Values{"action": {create}, "username": {username}, "display": {"HTTP Account"}, "password": {password}}, 200)
			var account admindao.Credentials
			if admin {
				account, err = f.adminStore.AdminCredentials(f.ctx, username)
			} else {
				account, err = f.adminStore.Credentials(f.ctx, username)
			}
			must(t, err)
			if account.DisplayName != "HTTP Account" || !account.Active || account.PasswordHash == password {
				t.Fatal("account was not persisted with hashed credentials")
			}
			// Credential domains remain independent even though the admin app manages both.
			if admin {
				_, _, err = f.userAccess.Login(f.ctx, username, password, "")
				wantError(t, err, access.ErrUnauthenticated)
			} else {
				_, _, err = f.adminAuth.Login(f.ctx, username, password, "")
				wantError(t, err, adminaccess.ErrUnauthenticated)
			}
			var oldSession string
			if admin {
				_, oldSession, err = f.adminAuth.Login(f.ctx, username, password, "")
			} else {
				_, oldSession, err = f.userAccess.Login(f.ctx, username, password, "")
			}
			must(t, err)
			mutate(route, url.Values{"action": {update}, "id": {fmt.Sprint(account.ID)}, "display": {"Renamed Account"}, "active": {"on"}}, 200)
			if admin {
				_, _, err = f.adminAuth.Authenticate(f.ctx, oldSession)
			} else {
				_, _, err = f.userAccess.Authenticate(f.ctx, oldSession)
			}
			must(t, err)
			replacement := "ChangedPassword123!"
			mutate(route, url.Values{"action": {update}, "id": {fmt.Sprint(account.ID)}, "display": {"Renamed Account"}, "active": {"on"}, "password": {replacement}}, 200)
			if admin {
				_, _, err = f.adminAuth.Authenticate(f.ctx, oldSession)
				wantError(t, err, adminaccess.ErrUnauthenticated)
				_, _, err = f.adminAuth.Login(f.ctx, username, password, "")
				wantError(t, err, adminaccess.ErrUnauthenticated)
				_, oldSession, err = f.adminAuth.Login(f.ctx, username, replacement, "")
			} else {
				_, _, err = f.userAccess.Authenticate(f.ctx, oldSession)
				wantError(t, err, access.ErrUnauthenticated)
				_, _, err = f.userAccess.Login(f.ctx, username, password, "")
				wantError(t, err, access.ErrUnauthenticated)
				_, oldSession, err = f.userAccess.Login(f.ctx, username, replacement, "")
			}
			must(t, err)
			page := apiRequest(t, h, http.MethodGet, route, cookie, nil)
			if page.Code != 200 || !strings.Contains(page.Body.String(), "Renamed Account") || strings.Contains(page.Body.String(), account.PasswordHash) || strings.Contains(page.Body.String(), "PasswordHash") {
				t.Fatal("account listing omitted update or exposed credentials")
			}
			mutate(route, url.Values{"action": {update}, "id": {fmt.Sprint(account.ID)}, "display": {"Disabled Account"}}, 200)
			if admin {
				_, _, err = f.adminAuth.Authenticate(f.ctx, oldSession)
				wantError(t, err, adminaccess.ErrUnauthenticated)
				_, _, err = f.adminAuth.Login(f.ctx, username, replacement, "")
				wantError(t, err, adminaccess.ErrUnauthenticated)
			} else {
				_, _, err = f.userAccess.Authenticate(f.ctx, oldSession)
				wantError(t, err, access.ErrUnauthenticated)
				_, _, err = f.userAccess.Login(f.ctx, username, replacement, "")
				wantError(t, err, access.ErrUnauthenticated)
			}
			mutate(route, url.Values{"action": {update}, "id": {fmt.Sprint(account.ID)}, "display": {"Reactivated Account"}, "active": {"on"}}, 200)
			if admin {
				_, _, err = f.adminAuth.Login(f.ctx, username, replacement, "")
			} else {
				_, _, err = f.userAccess.Login(f.ctx, username, replacement, "")
			}
			must(t, err)
			mutate(route, url.Values{"action": {create}, "username": {strings.ToUpper(username)}, "display": {"Duplicate"}, "password": {password}}, 409)
		})
	}
}
func TestAccountHTTPValidationCannotChangeRoleOrDisableLastAdministrator(t *testing.T) {
	f := newFixture(t, true)
	h := adminHandler(t, f)
	identity, cookie := f.login(t, "it.admin")
	csrf, err := f.adminAuth.CSRFToken(toAdminIdentity(identity))
	must(t, err)
	for _, tc := range []struct {
		name, route string
		form        url.Values
		status      int
	}{
		{"role escalation", "/admin/users", url.Values{"action": {"create_user"}, "admin": {"on"}, "username": {"no.escalation"}, "display": {"No Escalation"}, "password": {password}}, 400},
		{"wrong domain action", "/admin/users", url.Values{"action": {"create_admin"}, "username": {"no.admin"}}, 400},
		{"wrong admin action", "/admin/administrators", url.Values{"action": {"create_user"}}, 400},
		{"invalid identifier", "/admin/users", url.Values{"action": {"update_user"}, "id": {"-1"}}, 400},
		{"missing account", "/admin/users", url.Values{"action": {"update_user"}, "id": {"999999"}, "display": {"Missing"}, "active": {"on"}}, 404},
		{"short password", "/admin/users", url.Values{"action": {"create_user"}, "username": {"short.password"}, "display": {"Bad Password"}, "password": {"short"}}, 400},
		{"last administrator", "/admin/administrators", url.Values{"action": {"update_admin"}, "id": {fmt.Sprint(f.admin)}, "display": {"Last Admin"}}, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.form.Set("csrf", csrf)
			res := apiRequest(t, h, http.MethodPost, tc.route, cookie, tc.form)
			if res.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", res.Code, tc.status, res.Body.String())
			}
		})
	}
	admins, err := f.adminStore.Administrators(f.ctx)
	must(t, err)
	if len(admins) != 1 || !admins[0].Active {
		t.Fatal("invalid actions changed admin accounts")
	}
}
