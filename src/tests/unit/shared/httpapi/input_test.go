package httpapi_test

import (
	"errors"
	"net/http/httptest"
	"schakker_shared/httpapi"
	"schakker_shared/validation"
	"strings"
	"testing"
)

func TestIDAndOffsetValidationRejectsOverflowAndOutOfRangeValues(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		want  int64
		valid bool
	}{{"", 0, true}, {"1", 1, true}, {"9223372036854775807", 9223372036854775807, true}, {"0", 0, false}, {"-1", 0, false}, {"1.5", 0, false}, {"9223372036854775808", 0, false}, {"abc", 0, false}} {
		t.Run("id/"+tc.raw, func(t *testing.T) {
			n, err := httpapi.ID(tc.raw)
			if (err == nil) != tc.valid || (tc.valid && n != tc.want) {
				t.Fatalf("id=%d err=%v", n, err)
			}
		})
	}
	for _, tc := range []struct {
		raw   string
		want  int
		valid bool
	}{{"", 0, true}, {"0", 0, true}, {"100000000", 100000000, true}, {"-1", 0, false}, {"100000001", 0, false}, {"1.1", 0, false}, {"999999999999999999999", 0, false}} {
		t.Run("offset/"+tc.raw, func(t *testing.T) {
			n, err := httpapi.Offset(tc.raw)
			if (err == nil) != tc.valid || (tc.valid && n != tc.want) {
				t.Fatalf("offset=%d err=%v", n, err)
			}
		})
	}
}
func TestActionParsingRejectsWrongContentTypeAndOversizeBodies(t *testing.T) {
	for _, tc := range []struct{ content, body string }{{"text/plain", `{"action":"save"}`}, {"application/x-www-form-urlencoded", "action=save"}, {"application/json", `{"sql":"` + strings.Repeat("x", 4<<20) + `"}`}} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
		r.Header.Set("Content-Type", tc.content)
		if !errors.Is(httpapi.ParseAction(httptest.NewRecorder(), r), validation.ErrInvalid) {
			t.Fatal("invalid content accepted")
		}
	}
}
func TestSameOriginRejectsForeignPortsCredentialsAndDowngrades(t *testing.T) {
	for _, tc := range []struct {
		target, origin string
		want           bool
	}{{"https://app.example/", "https://app.example", true}, {"https://app.example/", "", true}, {"https://app.example/", "http://app.example", false}, {"https://app.example/", "https://app.example:444", false}, {"https://app.example/", "https://user@app.example", false}, {"https://app.example/", "null", false}, {"http://app.example/", "http://app.example", true}, {"http://app.example/", "https://app.example", true}} {
		r := httptest.NewRequest("POST", tc.target, nil)
		r.Header.Set("Origin", tc.origin)
		if httpapi.SameOrigin(r) != tc.want {
			t.Errorf("target=%s origin=%s", tc.target, tc.origin)
		}
	}
}
func TestUnencodableJSONProducesSafeErrorInsteadOfPartialSuccess(t *testing.T) {
	w := httptest.NewRecorder()
	httpapi.JSON(w, httptest.NewRequest("GET", "/", nil), 200, map[string]any{"unsupported": make(chan int)})
	if w.Code != 500 || !strings.Contains(w.Body.String(), "could not be encoded") {
		t.Fatalf("bad encoding failure: %d %s", w.Code, w.Body.String())
	}
}
