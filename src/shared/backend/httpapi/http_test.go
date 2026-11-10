package httpapi_test

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"schakker_shared/httpapi"
	"schakker_shared/validation"
)

func TestJSONActionsPreserveScalarAndArrayFields(t *testing.T) {
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"action":"set_grants","db":["7"],"permission":[""]}`))
	r.Header.Set("Content-Type", "application/json; charset=utf-8")
	if err := httpapi.ParseAction(httptest.NewRecorder(), r); err != nil {
		t.Fatal(err)
	}
	if r.PostForm.Get("action") != "set_grants" || len(r.PostForm["db"]) != 1 || r.PostForm["db"][0] != "7" || len(r.PostForm["permission"]) != 1 {
		t.Fatalf("unexpected action fields: %v", r.PostForm)
	}
	for _, raw := range []string{`null`, `{"id":7}`, `{"id":null}`, `{"db":[7]}`, `{} {}`, `{`} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		if !errors.Is(httpapi.ParseAction(httptest.NewRecorder(), r), validation.ErrInvalid) {
			t.Errorf("accepted malformed action %q", raw)
		}
	}
}

func TestStrictJSONAndHEADResponses(t *testing.T) {
	var input struct {
		SQL string `json:"sql"`
	}
	for _, raw := range []string{`{"sql":"SELECT 1","unknown":true}`, `{"sql":"SELECT 1"} {}`} {
		if !errors.Is(httpapi.DecodeStrictJSON(raw, &input), validation.ErrInvalid) {
			t.Errorf("accepted unexpected JSON %s", raw)
		}
	}
	if err := httpapi.DecodeStrictJSON(`{"sql":"SELECT 1"}`, &input); err != nil || input.SQL != "SELECT 1" {
		t.Fatalf("valid JSON rejected: %v", err)
	}
	w := httptest.NewRecorder()
	httpapi.JSON(w, httptest.NewRequest("HEAD", "/", nil), 200, map[string]string{"status": "ok"})
	if w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Length") != "15" || w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("HEAD response changed: %v %q", w.Result(), w.Body.String())
	}
}
