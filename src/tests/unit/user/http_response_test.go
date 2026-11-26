package unit_test

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/controlplane"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/backend/migration/diff"

	"tubes1_wbd/backend/api/response"
	"tubes1_wbd/backend/dbmanagement"
)

func TestPageResponsePreservesDisplayPrecisionNullsAndByteValues(t *testing.T) {
	rows := [][]any{{[]byte("text"), int64(9007199254740993), false, nil, math.NaN(), "<script>bad()</script>"}}
	data := response.Data{Data: &dbmanagement.DataPage{Rows: rows}, ViewerResult: &dbmanagement.ReadResult{Rows: [][]any{{[]byte("query"), float64(1.25), nil}}}}
	w := httptest.NewRecorder()
	response.Write(w, httptest.NewRequest("GET", "/api/explorer", nil), &data)
	if w.Code != 200 {
		t.Fatalf("response status %d", w.Code)
	}
	var body struct {
		Data         struct{ Rows [][]any }
		ViewerResult struct{ Rows [][]any }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := []any{"text", "9007199254740993", "false", nil, "NaN", "<script>bad()</script>"}
	if !reflect.DeepEqual(body.Data.Rows[0], want) || !reflect.DeepEqual(body.ViewerResult.Rows[0], []any{"query", "1.25", nil}) {
		t.Fatalf("lossy display response: %s", w.Body.String())
	}
}
func TestPageResponseHEADReturnsMetadataWithoutBody(t *testing.T) {
	w := httptest.NewRecorder()
	response.Write(w, httptest.NewRequest("HEAD", "/api/dashboard", nil), &response.Data{})
	if w.Body.Len() != 0 || w.Header().Get("Content-Length") == "" {
		t.Fatal("HEAD returned a body or no length")
	}
}
func TestMigrationHTTPErrorStatusCoversDomainFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"definition", migration.ErrDefinitionInvalid, 422}, {"workflow conflict", migration.ErrWorkflowConflict, 409}, {"authority outage", migration.ErrWorkflowUnavailable, 503}, {"control plane", controlplane.ErrUnavailable, 503},
		{"permission", access.ErrForbidden, 403}, {"not found", access.ErrNotFound, 404}, {"stale analysis", migration.ErrStale, 409}, {"stale snapshot", dbmanagement.ErrStaleSnapshot, 409}, {"duplicate", access.ErrConflict, 409},
		{"unsafe SQL", migration.ErrUnsafeSQL, 400}, {"no changes", diff.ErrNoChanges, 400}, {"viewer write", dbmanagement.ErrReadOnly, 400}, {"viewer size", dbmanagement.ErrViewerResultSize, 400}, {"viewer query", dbmanagement.ErrViewerQuery, 400},
		{"unsupported", dbmanagement.ErrUnsupported, 400}, {"invalid", access.ErrInvalid, 400}, {"connection", dbmanagement.ErrConnection, 502}, {"catalog query", dbmanagement.ErrQuery, 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			message, status := response.ErrorStatus(fmt.Errorf("private driver details: %w", tc.err))
			if status != tc.status || message == "" || strings.Contains(message, "private driver") {
				t.Fatalf("status=%d message=%q", status, message)
			}
		})
	}
}
