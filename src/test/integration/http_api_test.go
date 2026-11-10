package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/database/dao"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/httpapi"
)

func apiRequest(t *testing.T, handler http.Handler, method, route, cookie string, form url.Values, cookieNames ...string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(form)
	must(t, err)
	request := httptest.NewRequest(method, "/api"+route, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", form.Get("csrf"))
	if cookie != "" {
		name := access.CookieName
		if strings.HasPrefix(route, "/admin/") {
			name = httpapi.AdminCookieName
		}
		if len(cookieNames) > 0 {
			name = cookieNames[0]
		}
		request.AddCookie(&http.Cookie{Name: name, Value: cookie})
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("API returned non-JSON: %s", response.Body.String())
	}
	if response.Header().Get("Location") != "" {
		t.Fatal("API returned an HTTP form redirect")
	}
	return response
}

func decodeAPI[T any](t *testing.T, response *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	must(t, json.Unmarshal(response.Body.Bytes(), &value))
	return value
}

func apiLocation(t *testing.T, response *httptest.ResponseRecorder) *url.URL {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("action returned %d: %s", response.Code, response.Body.String())
	}
	result := decodeAPI[struct{ Location string }](t, response)
	if result.Location == "" {
		t.Fatal("action did not return client navigation")
	}
	location, err := url.Parse(result.Location)
	must(t, err)
	return location
}

func TestSPAAuthenticationLoginLogoutAndExpiration(t *testing.T) {
	f := newFixture(t, true)
	now := rotationClock(t, f)
	handler := userHandler(t, f)
	challenge := apiRequest(t, handler, http.MethodGet, "/session", "", nil)
	result := decodeAPI[struct{ CSRF string }](t, challenge)
	var challengeCookie *http.Cookie
	for _, cookie := range challenge.Result().Cookies() {
		if cookie.Name == "schakker_login_csrf" {
			challengeCookie = cookie
		}
	}
	if result.CSRF == "" || challengeCookie == nil {
		t.Fatal("login challenge missing")
	}
	login := func(csrf, origin, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", csrf)
		request.Header.Set("Origin", origin)
		request.AddCookie(challengeCookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	body := `{"username":"it.editor","password":"` + password + `"}`
	if response := login("", "", body); response.Code != 403 {
		t.Fatalf("login missing CSRF returned %d", response.Code)
	}
	if response := login(result.CSRF, "https://evil.example", body); response.Code != 403 {
		t.Fatalf("cross-origin login returned %d", response.Code)
	}
	signedIn := login(result.CSRF, "", body)
	apiLocation(t, signedIn)
	var sessionCookie *http.Cookie
	for _, cookie := range signedIn.Result().Cookies() {
		if cookie.Name == access.CookieName && cookie.Value != "" {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil || !sessionCookie.HttpOnly {
		t.Fatal("login did not issue an HttpOnly session cookie")
	}
	session := apiRequest(t, handler, http.MethodGet, "/session", sessionCookie.Value, nil)
	authenticated := decodeAPI[struct {
		User dao.User
		CSRF string
	}](t, session)
	if authenticated.User.Username != "it.editor" || authenticated.CSRF == "" || strings.Contains(session.Body.String(), sessionCookie.Value) {
		t.Fatal("session bootstrap did not protect the identifier")
	}
	if denied := apiRequest(t, handler, http.MethodPost, "/logout", sessionCookie.Value, url.Values{}); denied.Code != 403 {
		t.Fatal("logout accepted missing CSRF")
	}
	malformed := httptest.NewRequest(http.MethodPost, "/api/migrations/new", strings.NewReader(`{"action":"save_group"} {}`))
	malformed.Header.Set("Content-Type", "application/json")
	malformed.Header.Set("X-CSRF-Token", authenticated.CSRF)
	malformed.AddCookie(sessionCookie)
	invalid := httptest.NewRecorder()
	handler.ServeHTTP(invalid, malformed)
	if invalid.Code != 400 {
		t.Fatalf("multiple JSON documents accepted: %d", invalid.Code)
	}
	form := url.Values{"csrf": {authenticated.CSRF}}
	apiLocation(t, apiRequest(t, handler, http.MethodPost, "/logout", sessionCookie.Value, form))
	if denied := apiRequest(t, handler, http.MethodGet, "/dashboard", sessionCookie.Value, nil); denied.Code != 401 {
		t.Fatal("logged-out session remained usable")
	}
	_, nextCookie := f.login(t, "it.editor")
	*now = now.Add(31 * time.Minute)
	if expired := apiRequest(t, handler, http.MethodGet, "/dashboard", nextCookie, nil); expired.Code != 401 {
		t.Fatalf("expired session returned %d", expired.Code)
	}
	fresh := apiRequest(t, handler, http.MethodGet, "/session", nextCookie, nil)
	if decodeAPI[struct{ CSRF string }](t, fresh).CSRF == "" {
		t.Fatal("expired session did not receive a fresh login challenge")
	}
}

func TestMigrationAPIReviewDraftAndExecute(t *testing.T) {
	f := newFixture(t, true)
	reader := f.user(t, "spa.reader")
	target := f.register(t)
	f.grant(t, f.editor, target.ID, access.ViewEdit)
	f.grant(t, reader.ID, target.ID, access.ViewOnly)
	identity, cookie := f.login(t, "it.editor")
	csrf, err := f.access.CSRFToken(identity)
	must(t, err)
	handler := userHandler(t, f)
	form := url.Values{"csrf": {csrf}, "db": {fmt.Sprint(target.ID)}, "description": {"Add SPA note"}, "sql": {"ALTER TABLE public.employees ADD COLUMN spa_note TEXT;"}, "action": {"save_draft"}}
	location := apiLocation(t, apiRequest(t, handler, http.MethodPost, "/migrations/new", cookie, form))
	loaded := apiRequest(t, handler, http.MethodGet, location.String(), cookie, nil)
	draft := decodeAPI[struct {
		DraftID           int64
		DraftUpdated, SQL string
	}](t, loaded)
	if draft.DraftID == 0 || draft.SQL != form.Get("sql") || draft.DraftUpdated == "" {
		t.Fatal("draft did not round-trip through JSON")
	}
	form.Set("draft", fmt.Sprint(draft.DraftID))
	form.Set("draft_updated", draft.DraftUpdated)
	form.Set("action", "analyze")
	analyzed := apiRequest(t, handler, http.MethodPost, "/migrations/new", cookie, form)
	if analyzed.Code != 200 {
		t.Fatalf("analysis returned %d: %s", analyzed.Code, analyzed.Body.String())
	}
	review := decodeAPI[struct {
		Review     migration.Analysis
		SchemaDiff json.RawMessage
	}](t, analyzed)
	if review.Review.Signature == "" || review.Review.SchemaDiffAvailable || review.Review.SchemaDiffMessage == "" {
		t.Fatal("arbitrary SQL did not return its signed review and diff-unavailable state")
	}
	visual := url.Values{"csrf": {csrf}, "db": {fmt.Sprint(target.ID)}, "action": {"analyze_visual"}, "description": {"Add SPA note"}, "operations": {`[{"Kind":"add_column","Schema":"public","Table":"employees","Column":{"Name":"spa_note","DataType":"text","Nullable":true}}]`}}
	visualResponse := apiRequest(t, handler, http.MethodPost, "/migrations/new", cookie, visual)
	visualReview := decodeAPI[struct {
		Review     migration.Analysis
		SchemaDiff json.RawMessage
	}](t, visualResponse)
	if visualResponse.Code != 200 || !visualReview.Review.SchemaDiffAvailable || len(visualReview.SchemaDiff) == 0 || string(visualReview.SchemaDiff) == "null" {
		t.Fatalf("structured diff absent: %d %s", visualResponse.Code, visualResponse.Body.String())
	}
	table, err := f.databases.DescribeTable(f.ctx, f.editor, target.ID, "public", "employees")
	must(t, err)
	for _, column := range table.Columns {
		if column.Name == "spa_note" {
			t.Fatal("analysis executed the migration")
		}
	}
	readerIdentity, readerCookie := f.login(t, reader.Username)
	readerCSRF, err := f.access.CSRFToken(readerIdentity)
	must(t, err)
	denied := apiRequest(t, handler, http.MethodPost, "/migrations/new", readerCookie, url.Values{"csrf": {readerCSRF}, "db": {fmt.Sprint(target.ID)}, "action": {"analyze"}, "sql": {form.Get("sql")}})
	if denied.Code != 403 {
		t.Fatalf("View Only migration analysis returned %d", denied.Code)
	}
	encoded, err := json.Marshal(review.Review)
	must(t, err)
	form.Set("review", string(encoded))
	form.Set("action", "execute")
	missingConfirmation := apiRequest(t, handler, http.MethodPost, "/migrations/new", cookie, form)
	if missingConfirmation.Code != 400 {
		t.Fatalf("execution without confirmation returned %d", missingConfirmation.Code)
	}
	form.Set("confirm", "on")
	form.Set("sql", draft.SQL+" SELECT 1;")
	stale := apiRequest(t, handler, http.MethodPost, "/migrations/new", cookie, form)
	if stale.Code != 409 {
		t.Fatalf("changed SQL returned %d", stale.Code)
	}
	form.Set("sql", draft.SQL)
	location = apiLocation(t, apiRequest(t, handler, http.MethodPost, "/migrations/new", cookie, form))
	if location.Path != "/migrations" || location.Query().Get("id") == "" {
		t.Fatal("execution did not lead to its changelog entry")
	}
	history := apiRequest(t, handler, http.MethodGet, location.String(), cookie, nil)
	execution := decodeAPI[struct{ Execution *dao.MigrationExecution }](t, history)
	if history.Code != 200 || execution.Execution == nil || execution.Execution.Status != "succeeded" {
		t.Fatalf("executed migration absent from history: %s", history.Body.String())
	}
}
