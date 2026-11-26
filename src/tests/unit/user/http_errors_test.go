package unit_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mysql "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/api/response"
)

func TestHTTPErrorResponsesDoNotLeakDriverDetails(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failure error
		status  int
	}{
		{"postgresql", &pgconn.PgError{Code: "23505", Message: "private-row-value", Detail: "password=hidden", Where: "SELECT * FROM internal_table"}, http.StatusInternalServerError},
		{"mariadb", &mysql.MySQLError{Number: 1062, Message: "private-row-value password=hidden internal_table"}, http.StatusInternalServerError},
		{"unexpected", errors.New("password=hidden internal_table"), http.StatusInternalServerError},
		{"wrapped permission", fmt.Errorf("%w: password=hidden", access.ErrForbidden), http.StatusForbidden},
		{"canceled", context.Canceled, http.StatusInternalServerError},
		{"deadline", context.DeadlineExceeded, http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/migrations/new", nil)
			response.Fail(result, request, tc.failure)
			var body map[string]string
			if result.Code != tc.status || json.Unmarshal(result.Body.Bytes(), &body) != nil || body["error"] == "" {
				t.Fatalf("invalid error response: %d %s", result.Code, result.Body.String())
			}
			for _, secret := range []string{"private-row-value", "password=hidden", "internal_table"} {
				if strings.Contains(result.Body.String(), secret) {
					t.Fatalf("HTTP error response leaked %q", secret)
				}
			}
		})
	}
}
