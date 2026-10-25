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
	assertEditor := func(body, selected string) {
		t.Helper()
		_, card, found := strings.Cut(body, fmt.Sprintf(`id="group-%d"`, group.ID))
		if !found {
			t.Fatal("group card is missing")
		}
		card, _, _ = strings.Cut(card, "</article>")
		for _, editor := range []string{"membership", "access", "information"} {
			_, panel, found := strings.Cut(card, `data-group-panel="`+editor+`"`)
			tag, _, _ := strings.Cut(panel, ">")
			if !found || strings.Contains(tag, " hidden") != (editor != selected) {
				t.Fatalf("%s editor visibility does not match selected editor %q", editor, selected)
			}
			button := fmt.Sprintf(`data-group-edit="%s" aria-controls="group-%s-%d" aria-expanded="%t"`, editor, editor, group.ID, editor == selected)
			if !strings.Contains(card, button) {
				t.Fatalf("%s edit button does not match its panel state", editor)
			}
		}
		_, overview, _ := strings.Cut(card, "data-group-overview>")
		overview, _, _ = strings.Cut(overview, `<div class="group-card__actions"`)
		if !strings.Contains(overview, "Database access") || strings.Contains(overview, "<form") || strings.Contains(overview, "<select") || strings.Contains(overview, "<input") {
			t.Fatal("group overview must show read-only membership and access information")
		}
	}
	response = request(http.MethodGet, "/admin/groups", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("group overview returned %d", response.Code)
	}
	assertEditor(response.Body.String(), "")
	response = request(http.MethodGet, location.String(), nil)
	body := response.Body.String()
	if response.Code != 200 || !strings.Contains(body, "Database access") || !strings.Contains(body, "Members") || !strings.Contains(body, `data-group-card data-auto-scroll`) || strings.Contains(body, `href="/admin/access"`) {
		t.Fatalf("merged group page did not render its membership and permission controls: %d %s", response.Code, body)
	}
	assertEditor(body, "access")
	for _, editor := range []string{"membership", "access", "information", "invalid"} {
		response = request(http.MethodGet, fmt.Sprintf("/admin/groups?group=%d&editor=%s", group.ID, editor), nil)
		selected := editor
		if editor == "invalid" {
			selected = ""
		}
		assertEditor(response.Body.String(), selected)
	}
	response = request(http.MethodPost, "/admin/groups", url.Values{"csrf": {csrf}, "action": {"membership"}, "id": {fmt.Sprint(group.ID)}, "user": {fmt.Sprint(employee.ID)}, "member": {"yes"}})
	if response.Code != http.StatusSeeOther {
		t.Fatalf("membership update returned %d", response.Code)
	}
	response = request(http.MethodGet, response.Header().Get("Location"), nil)
	assertEditor(response.Body.String(), "membership")
	if !strings.Contains(response.Body.String(), employee.Username) {
		t.Fatal("membership summary did not include the saved member")
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
	if location.Path != "/admin/groups" || location.Query().Get("group") != fmt.Sprint(group.ID) || location.Query().Get("editor") != "access" {
		t.Fatalf("saving a grant navigated away from its group: %s", location)
	}
	response = request(http.MethodGet, location.String(), nil)
	assertEditor(response.Body.String(), "access")
	if strings.Contains(response.Body.String(), "data-auto-scroll") {
		t.Fatal("saving access must not trigger another animated scroll")
	}
	if !strings.Contains(response.Body.String(), `class="badge group-permission--view">View Only</span>`) {
		t.Fatal("read-only access summary did not reflect the saved permission")
	}
	response = request(http.MethodPost, "/admin/groups", url.Values{"csrf": {csrf}, "action": {"save_group"}, "id": {fmt.Sprint(group.ID)}, "name": {""}})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid group edit returned %d", response.Code)
	}
	assertEditor(response.Body.String(), "information")
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

func TestGroupsBatchAccessSaveIsAtomic(t *testing.T) {
	f := newFixture(t, true)
	employee := f.user(t, "batch.reader")
	group := f.group(t, "Batch access")
	must(t, f.access.SetMembership(f.ctx, f.admin, employee.ID, group.ID, true))
	first := f.register(t)
	input := f.dbs.Connection()
	input.Name = "Second batch target"
	second, err := f.databases.SaveConnection(f.ctx, f.admin, 0, input)
	must(t, err)
	identity, cookie := f.login(t, "it.admin")
	csrf, err := f.access.CSRFToken(identity)
	must(t, err)
	handler := promotionHandler(t, f)
	request := func(method, path string, form url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(&http.Cookie{Name: access.CookieName, Value: cookie})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		return response
	}
	form := url.Values{"csrf": {csrf}, "action": {"set_grants"}, "id": {fmt.Sprint(group.ID)}, "db": {fmt.Sprint(first.ID), fmt.Sprint(second.ID)}, "permission": {string(access.ViewEdit), string(access.ViewOnly)}}
	response := request(http.MethodPost, "/admin/groups", form)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("batch save returned %d: %s", response.Code, response.Body.String())
	}
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
	response = request(http.MethodGet, response.Header().Get("Location"), nil)
	_, panel, _ := strings.Cut(response.Body.String(), fmt.Sprintf(`id="group-access-%d"`, group.ID))
	panel, _, _ = strings.Cut(panel, "</section>")
	if strings.Count(panel, `type="submit"`) != 1 || strings.Count(panel, `name="permission"`) != 2 || !strings.Contains(panel, "Save access") {
		t.Fatal("access editor must submit all databases through one save button")
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
		response = request(http.MethodPost, "/admin/groups", invalid)
		if response.Code != http.StatusBadRequest && response.Code != http.StatusNotFound {
			t.Fatalf("invalid batch returned %d", response.Code)
		}
		assertPermissions(access.ViewEdit, access.ViewOnly)
		_, _, err = f.access.Authenticate(f.ctx, employeeCookie)
		must(t, err)
	}
	response = request(http.MethodPost, "/admin/groups", form)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("unchanged batch save returned %d", response.Code)
	}
	_, _, err = f.access.Authenticate(f.ctx, employeeCookie)
	must(t, err)
	form["permission"] = []string{string(access.ViewOnly), string(access.ViewEdit)}
	response = request(http.MethodPost, "/admin/groups", form)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("mixed permission update returned %d", response.Code)
	}
	assertPermissions(access.ViewOnly, access.ViewEdit)
	_, _, err = f.access.Authenticate(f.ctx, employeeCookie)
	wantError(t, err, access.ErrUnauthenticated)
	form.Del("csrf")
	if response := request(http.MethodPost, "/admin/groups", form); response.Code != http.StatusForbidden {
		t.Fatalf("batch save without CSRF returned %d", response.Code)
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
