package integration_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/database/dao"
)

func TestGroupsPageManagesMembersAndDatabasePermissions(t *testing.T) {
	f := newFixture(t, true)
	employee := f.user(t, "groups.reader")
	target := f.register(t)
	identity, cookie := f.login(t, "it.admin")
	csrf, err := f.access.CSRFToken(identity)
	must(t, err)
	handler := promotionHandler(t, f)
	request := func(method, path string, form url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		if method == http.MethodPost {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		r.AddCookie(&http.Cookie{Name: access.CookieName, Value: cookie})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		return response
	}

	response := request(http.MethodPost, "/admin/groups", url.Values{"csrf": {csrf}, "action": {"save_group"}, "name": {"  Merged Analysts  "}, "description": {"Members and database access"}})
	if response.Code != http.StatusSeeOther {
		t.Fatalf("create group returned %d: %s", response.Code, response.Body.String())
	}
	location, err := url.Parse(response.Header().Get("Location"))
	must(t, err)
	if location.Path != "/admin/groups" || location.Query().Get("created") != "Merged Analysts" {
		t.Fatalf("new group does not lead to its permission controls: %s", location)
	}
	groups, err := f.store.Groups(f.ctx)
	must(t, err)
	var group dao.Group
	for _, saved := range groups {
		if saved.Name == "Merged Analysts" {
			group = saved
		}
	}
	if group.ID == 0 {
		t.Fatal("created group was not persisted")
	}
	permission, err := f.store.GroupPermission(f.ctx, group.ID, target.ID)
	must(t, err)
	if permission != access.NoAccess {
		t.Fatal("new group gained database access without an explicit grant")
	}
	response = request(http.MethodGet, location.String(), nil)
	body := response.Body.String()
	if response.Code != 200 || !strings.Contains(body, "Database access") || !strings.Contains(body, "Members") || !strings.Contains(body, `aria-label="Merged Analysts database permissions" data-auto-scroll`) || strings.Contains(body, `href="/admin/access"`) {
		t.Fatalf("merged group page did not render its membership and permission controls: %d %s", response.Code, body)
	}
	response = request(http.MethodPost, "/admin/groups", url.Values{"csrf": {csrf}, "action": {"membership"}, "id": {fmt.Sprint(group.ID)}, "user": {fmt.Sprint(employee.ID)}, "member": {"yes"}})
	if response.Code != http.StatusSeeOther {
		t.Fatalf("membership update returned %d", response.Code)
	}
	grant := url.Values{"csrf": {csrf}, "action": {"set_grant"}, "id": {fmt.Sprint(group.ID)}, "db": {fmt.Sprint(target.ID)}, "permission": {string(access.ViewOnly)}}
	response = request(http.MethodPost, "/admin/groups", grant)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("group permission save returned %d: %s", response.Code, response.Body.String())
	}
	permission, err = f.access.EffectivePermission(f.ctx, employee.ID, target.ID)
	must(t, err)
	if permission != access.ViewOnly {
		t.Fatalf("permission saved through Groups was not effective: %s", permission)
	}
	location, err = url.Parse(response.Header().Get("Location"))
	must(t, err)
	if location.Path != "/admin/groups" || location.Query().Get("group") != fmt.Sprint(group.ID) {
		t.Fatalf("saving a grant navigated away from its group: %s", location)
	}
	response = request(http.MethodGet, "/admin/access", nil)
	location, err = url.Parse(response.Header().Get("Location"))
	must(t, err)
	if response.Code != http.StatusSeeOther || location.Path != "/admin/groups" {
		t.Fatal("legacy access page did not redirect to Groups")
	}
	grant.Set("permission", string(access.ViewEdit))
	response = request(http.MethodPost, "/admin/access", grant)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("legacy grant submit failed: %d", response.Code)
	}
	permission, err = f.access.EffectivePermission(f.ctx, employee.ID, target.ID)
	must(t, err)
	if permission != access.ViewEdit {
		t.Fatal("legacy grant submit lost its update")
	}
	_, employeeCookie := f.login(t, employee.Username)
	grant.Set("permission", "")
	response = request(http.MethodPost, "/admin/groups", grant)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("removing a group permission returned %d", response.Code)
	}
	permission, err = f.access.EffectivePermission(f.ctx, employee.ID, target.ID)
	must(t, err)
	if permission != access.NoAccess {
		t.Fatal("No Access selection did not remove the grant")
	}
	_, _, err = f.access.Authenticate(f.ctx, employeeCookie)
	wantError(t, err, access.ErrUnauthenticated)
	grant.Del("csrf")
	if response := request(http.MethodPost, "/admin/groups", grant); response.Code != http.StatusForbidden {
		t.Fatalf("Groups accepted a grant without CSRF: %d", response.Code)
	}
}

func TestGroupsPermissionControlsRejectOrdinaryEmployees(t *testing.T) {
	f := newFixture(t, true)
	employee := f.user(t, "groups.employee")
	identity, cookie := f.login(t, employee.Username)
	csrf, err := f.access.CSRFToken(identity)
	must(t, err)
	handler := promotionHandler(t, f)
	for _, path := range []string{"/admin/groups", "/admin/access"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			form := url.Values{"csrf": {csrf}, "action": {"set_grant"}, "id": {"1"}, "db": {"1"}, "permission": {string(access.ViewEdit)}}
			r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
			r.AddCookie(&http.Cookie{Name: access.CookieName, Value: cookie})
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, r)
			if response.Code != http.StatusForbidden {
				t.Fatalf("ordinary employee received %d for %s %s", response.Code, method, path)
			}
		}
	}
}
