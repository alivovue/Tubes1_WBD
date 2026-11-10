package integration_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/database/dao"
	admindao "tubes1_wbd_admin/database/dao"
)

type apiGroup struct {
	admindao.Group
	Members []dao.User
	Grants  []struct {
		Database   dao.Database
		Permission dao.Permission
	}
	Editor string
	Focus  bool
}

func groupFromResponse(t *testing.T, response *httptest.ResponseRecorder, id int64) apiGroup {
	t.Helper()
	if response.Code != 200 {
		t.Fatalf("group API returned %d: %s", response.Code, response.Body.String())
	}
	result := decodeAPI[struct{ Groups []apiGroup }](t, response)
	for _, group := range result.Groups {
		if group.ID == id {
			return group
		}
	}
	t.Fatal("group missing from API response")
	return apiGroup{}
}

func TestGroupsPageManagesMembersAndDatabasePermissions(t *testing.T) {
	f := newFixture(t, true)
	employee := f.user(t, "groups.reader")
	target := f.register(t)
	identity, cookie := f.login(t, "it.admin")
	csrf, err := f.adminAuth.CSRFToken(toAdminIdentity(identity))
	must(t, err)
	handler := adminHandler(t, f)
	request := func(method, path string, form url.Values) *httptest.ResponseRecorder {
		return apiRequest(t, handler, method, path, cookie, form)
	}
	response := request(http.MethodPost, "/admin/groups", url.Values{"csrf": {csrf}, "action": {"save_group"}, "name": {"  Merged Analysts  "}, "description": {"Members and database access"}})
	location := apiLocation(t, response)
	if location.Path != "/admin/groups" || location.Query().Get("created") != "Merged Analysts" {
		t.Fatalf("new group navigation: %s", location)
	}
	groups, err := f.store.Groups(f.ctx)
	must(t, err)
	var group admindao.Group
	for _, saved := range groups {
		if saved.Name == "Merged Analysts" {
			group = saved
		}
	}
	if group.ID == 0 {
		t.Fatal("created group was not persisted")
	}
	rawLevel, err := f.store.GroupPermission(f.ctx, group.ID, target.ID)
	must(t, err)
	if rawLevel != admindao.NoAccess {
		t.Fatal("new group gained access without a grant")
	}
	if groupFromResponse(t, request(http.MethodGet, "/admin/groups", nil), group.ID).Editor != "" {
		t.Fatal("overview opened an editor")
	}
	focused := groupFromResponse(t, request(http.MethodGet, location.String(), nil), group.ID)
	if !focused.Focus || focused.Editor != "access" {
		t.Fatal("created group did not focus access")
	}
	for _, editor := range []string{"membership", "access", "information", "invalid"} {
		selected := editor
		if editor == "invalid" {
			selected = ""
		}
		if groupFromResponse(t, request(http.MethodGet, fmt.Sprintf("/admin/groups?group=%d&editor=%s", group.ID, editor), nil), group.ID).Editor != selected {
			t.Fatalf("wrong editor for %s", editor)
		}
	}
	response = request(http.MethodPost, "/admin/groups", url.Values{"csrf": {csrf}, "action": {"membership"}, "id": {fmt.Sprint(group.ID)}, "user": {fmt.Sprint(employee.ID)}, "member": {"yes"}})
	membership := groupFromResponse(t, request(http.MethodGet, apiLocation(t, response).String(), nil), group.ID)
	if membership.Editor != "membership" || len(membership.Members) != 1 || membership.Members[0].Username != employee.Username {
		t.Fatal("saved membership not returned")
	}
	grant := url.Values{"csrf": {csrf}, "action": {"set_grant"}, "id": {fmt.Sprint(group.ID)}, "db": {fmt.Sprint(target.ID)}, "permission": {string(access.ViewOnly)}}
	response = request(http.MethodPost, "/admin/groups", grant)
	location = apiLocation(t, response)
	level, err := f.access.EffectivePermission(f.ctx, employee.ID, target.ID)
	must(t, err)
	if level != access.ViewOnly {
		t.Fatalf("permission: %s", level)
	}
	if location.Path != "/admin/groups" || location.Query().Get("group") != fmt.Sprint(group.ID) || location.Query().Get("editor") != "access" {
		t.Fatalf("save navigated away from its group: %s", location)
	}
	saved := groupFromResponse(t, request(http.MethodGet, location.String(), nil), group.ID)
	if saved.Focus || saved.Editor != "access" || len(saved.Grants) != 1 || saved.Grants[0].Permission != access.ViewOnly {
		t.Fatal("saved access summary is incorrect")
	}
	response = request(http.MethodPost, "/admin/groups", url.Values{"csrf": {csrf}, "action": {"save_group"}, "id": {fmt.Sprint(group.ID)}, "name": {""}})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid group edit: %d", response.Code)
	}
	response = request(http.MethodGet, "/admin/access", nil)
	if apiLocation(t, response).Path != "/admin/groups" {
		t.Fatal("legacy access alias lost")
	}
	grant.Set("permission", string(access.ViewEdit))
	apiLocation(t, request(http.MethodPost, "/admin/access", grant))
	level, err = f.access.EffectivePermission(f.ctx, employee.ID, target.ID)
	must(t, err)
	if level != access.ViewEdit {
		t.Fatal("legacy grant update lost")
	}
	_, employeeCookie := f.login(t, employee.Username)
	grant.Set("permission", "")
	apiLocation(t, request(http.MethodPost, "/admin/groups", grant))
	level, err = f.access.EffectivePermission(f.ctx, employee.ID, target.ID)
	must(t, err)
	if level != access.NoAccess {
		t.Fatal("grant not removed")
	}
	_, _, err = f.access.Authenticate(f.ctx, employeeCookie)
	wantError(t, err, access.ErrUnauthenticated)
	grant.Del("csrf")
	if response := request(http.MethodPost, "/admin/groups", grant); response.Code != http.StatusForbidden {
		t.Fatalf("grant without CSRF: %d", response.Code)
	}
}

func TestGroupsBatchAccessSaveIsAtomic(t *testing.T) {
	f := newFixture(t, true)
	employee := f.user(t, "batch.reader")
	group := f.group(t, "Batch access")
	must(t, f.access.SetMembership(f.ctx, f.admin, employee.ID, group.ID, true))
	first := f.register(t)
	other := newFixture(t, false)
	input := other.dbs.Connection()
	input.Name = "Second batch target"
	second, err := f.saveConnection(f.ctx, f.admin, 0, input)
	must(t, err)
	identity, cookie := f.login(t, "it.admin")
	csrf, err := f.adminAuth.CSRFToken(toAdminIdentity(identity))
	must(t, err)
	handler := adminHandler(t, f)
	request := func(method, path string, form url.Values) *httptest.ResponseRecorder {
		return apiRequest(t, handler, method, path, cookie, form)
	}
	form := url.Values{"csrf": {csrf}, "action": {"set_grants"}, "id": {fmt.Sprint(group.ID)}, "db": {fmt.Sprint(first.ID), fmt.Sprint(second.ID)}, "permission": {string(access.ViewEdit), string(access.ViewOnly)}}
	location := apiLocation(t, request(http.MethodPost, "/admin/groups", form))
	assertPermissions := func(firstLevel, secondLevel access.Permission) {
		t.Helper()
		for id, expected := range map[int64]access.Permission{first.ID: firstLevel, second.ID: secondLevel} {
			actual, err := f.access.EffectivePermission(f.ctx, employee.ID, id)
			must(t, err)
			if actual != expected {
				t.Fatalf("database %d permission: got %q, want %q", id, actual, expected)
			}
		}
	}
	assertPermissions(access.ViewEdit, access.ViewOnly)
	saved := groupFromResponse(t, request(http.MethodGet, location.String(), nil), group.ID)
	if saved.Editor != "access" || len(saved.Grants) != 2 {
		t.Fatal("batch access editor missing grants")
	}
	_, employeeCookie := f.login(t, employee.Username)
	for _, invalid := range []url.Values{
		{"db": {fmt.Sprint(first.ID)}, "permission": {"view_only", "view_edit"}},
		{"db": {fmt.Sprint(first.ID), fmt.Sprint(second.ID)}, "permission": {"", "invalid"}},
		{"db": {fmt.Sprint(first.ID), fmt.Sprint(first.ID)}, "permission": {"", "view_edit"}},
		{"db": {fmt.Sprint(first.ID), "999999999"}, "permission": {"", "view_edit"}},
	} {
		invalid.Set("csrf", csrf)
		invalid.Set("action", "set_grants")
		invalid.Set("id", fmt.Sprint(group.ID))
		response := request(http.MethodPost, "/admin/groups", invalid)
		if response.Code != http.StatusBadRequest && response.Code != http.StatusNotFound {
			t.Fatalf("invalid batch: %d", response.Code)
		}
		assertPermissions(access.ViewEdit, access.ViewOnly)
		_, _, err = f.access.Authenticate(f.ctx, employeeCookie)
		must(t, err)
	}
	apiLocation(t, request(http.MethodPost, "/admin/groups", form))
	_, _, err = f.access.Authenticate(f.ctx, employeeCookie)
	must(t, err)
	form["permission"] = []string{string(access.ViewOnly), string(access.ViewEdit)}
	apiLocation(t, request(http.MethodPost, "/admin/groups", form))
	assertPermissions(access.ViewOnly, access.ViewEdit)
	_, _, err = f.access.Authenticate(f.ctx, employeeCookie)
	wantError(t, err, access.ErrUnauthenticated)
	form.Del("csrf")
	if response := request(http.MethodPost, "/admin/groups", form); response.Code != http.StatusForbidden {
		t.Fatalf("batch without CSRF: %d", response.Code)
	}
}

func TestGroupsPermissionControlsRejectOrdinaryEmployees(t *testing.T) {
	f := newFixture(t, true)
	employee := f.user(t, "groups.employee")
	identity, cookie := f.login(t, employee.Username)
	csrf, err := f.userAccess.CSRFToken(identity)
	must(t, err)
	handler := adminHandler(t, f)
	for _, path := range []string{"/admin/groups", "/admin/access"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			form := url.Values{"csrf": {csrf}, "action": {"set_grant"}, "id": {"1"}, "db": {"1"}, "permission": {string(access.ViewEdit)}}
			response := apiRequest(t, handler, method, path, cookie, form)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("employee received %d for %s %s", response.Code, method, path)
			}
		}
	}
}
