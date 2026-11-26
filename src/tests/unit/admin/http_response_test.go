package unit_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"tubes1_wbd_admin/backend/access"
	"tubes1_wbd_admin/backend/api/response"
	"tubes1_wbd_admin/backend/connectionconfig"
)

func TestAdminHTTPFailuresMapDomainErrorsWithoutLeakingSecrets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{{"forbidden", access.ErrForbidden, 403}, {"not found", access.ErrNotFound, 404}, {"duplicate", connectionconfig.ErrDuplicateConnection, 409}, {"conflict", access.ErrConflict, 409}, {"invalid", access.ErrInvalid, 400}, {"connection", connectionconfig.ErrConnection, 502}, {"unsupported", connectionconfig.ErrUnsupported, 400}, {"unexpected", errors.New("password=private"), 500}} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			response.Fail(w, httptest.NewRequest("POST", "/api/admin/connections", nil), fmt.Errorf("password=private: %w", tc.err))
			var body map[string]string
			if w.Code != tc.status || json.Unmarshal(w.Body.Bytes(), &body) != nil || body["error"] == "" || strings.Contains(w.Body.String(), "password=private") {
				t.Fatalf("unsafe error response: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
func TestAdminRedirectIsJSONClientNavigationWithEncodedParameters(t *testing.T) {
	w := httptest.NewRecorder()
	response.Redirect(w, httptest.NewRequest("POST", "/api/admin/groups", nil), "/admin/groups", "Saved & reviewed", "group", 7, "editor", "access")
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	route, err := url.Parse(body["location"])
	if err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || w.Header().Get("Location") != "" || route.Path != "/admin/groups" || route.Query().Get("notice") != "Saved & reviewed" || route.Query().Get("group") != "7" || route.Query().Get("editor") != "access" {
		t.Fatalf("bad navigation: %s", w.Body.String())
	}
}
