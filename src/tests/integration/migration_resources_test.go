package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/api"
	"tubes1_wbd/backend/migration/engines"
)

type definitionResource struct {
	ID         int64 `json:"id"`
	RevisionID int64 `json:"revision_id"`
	Revision   int64 `json:"revision"`
	Generation int64 `json:"expected_generation"`
	Batch      *struct {
		Size int `json:"batch_size"`
	} `json:"batch"`
}
type reviewResource struct {
	ID         int64  `json:"id"`
	Status     string `json:"status"`
	CanEnqueue bool   `json:"can_enqueue"`
	Groups     []struct {
		GroupID   int64 `json:"group_id"`
		CanDecide bool  `json:"can_decide"`
	} `json:"required_groups"`
}
type executionResource struct {
	ID          string `json:"id"`
	SQL         string `json:"sql"`
	Operation   string `json:"operation"`
	Generation  int64  `json:"expected_generation"`
	CanRollback bool   `json:"can_rollback"`
	CanForward  bool   `json:"can_forward"`
}

func TestSPAResourcesExecuteRollbackForwardThroughHTTP(t *testing.T) {
	f, jobs, runner := newWorkerFixture(t)
	handler := http.StripPrefix("/api", api.NewHandler(f.userAccess, f.databases, f.migrations, false, jobs))
	identity, cookie := f.login(t, "it.editor")
	csrf, err := f.access.CSRFToken(identity)
	must(t, err)
	reader, readerCookie := f.login(t, f.reader.Username)
	readerCSRF, err := f.access.CSRFToken(reader)
	must(t, err)
	request := func(method, route, session, token string, body any, key string, want int) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		must(t, err)
		req := httptest.NewRequest(method, "/api"+route, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", token)
		req.Header.Set("Idempotency-Key", key)
		if session != "" {
			req.AddCookie(&http.Cookie{Name: access.CookieName, Value: session})
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != want {
			t.Fatalf("%s %s: %d want %d: %s", method, route, res.Code, want, res.Body.String())
		}
		return res
	}
	databaseQuery := fmt.Sprintf("?database_id=%d", f.database)
	request("GET", "/migration-jobs"+databaseQuery, "", "", nil, "", 401)
	created := request("POST", "/migration-definitions", cookie, csrf, map[string]any{"database_id": f.database, "title": "UI resource", "up_sql": "CREATE TABLE ui_resource(id bigint)", "down_sql": "DROP TABLE ui_resource", "irreversible": false, "irreversible_reason": ""}, "", 201)
	definition := decodeAPI[struct {
		Definition definitionResource `json:"definition"`
	}](t, created).Definition
	edited := request("PATCH", fmt.Sprintf("/migration-definitions/%d", definition.ID), cookie, csrf, map[string]any{"expected_revision_id": definition.RevisionID, "title": "UI resource edited", "up_sql": "CREATE TABLE ui_resource(id bigint)", "down_sql": "DROP TABLE ui_resource", "irreversible": false, "irreversible_reason": ""}, "", 200)
	definition = decodeAPI[struct {
		Definition definitionResource `json:"definition"`
	}](t, edited).Definition
	if definition.Revision != 2 || definition.RevisionID == definition.ID {
		t.Fatal("definition and revision IDs were conflated")
	}
	submitted := request("POST", "/migration-reviews", cookie, csrf, map[string]any{"revision_id": definition.RevisionID, "expected_generation": definition.Generation, "action": "up"}, "", 201)
	review := decodeAPI[struct {
		Review reviewResource `json:"review"`
	}](t, submitted).Review
	if len(review.Groups) != 2 || !review.Groups[0].CanDecide && !review.Groups[1].CanDecide {
		t.Fatal("leader capabilities missing")
	}
	approveAndRun := func(reviewID int64, key string) string {
		t.Helper()
		route := fmt.Sprintf("/migration-reviews/%d", reviewID)
		request("POST", route+"/enqueue", cookie, csrf, map[string]any{}, key, 409)
		request("POST", route+"/decisions", cookie, csrf, map[string]any{"group_id": f.editors.ID, "decision": "approve", "note": "Reviewed"}, "", 200)
		decided := request("POST", route+"/decisions", readerCookie, readerCSRF, map[string]any{"group_id": f.readers.ID, "decision": "approve", "note": "Read only leader approves"}, "", 200)
		view := decodeAPI[struct {
			Review reviewResource `json:"review"`
		}](t, decided).Review
		if view.CanEnqueue {
			t.Fatal("read-only leader can enqueue")
		}
		queued := request("POST", route+"/enqueue", cookie, csrf, map[string]any{}, key, 202)
		id := decodeAPI[struct {
			JobID string `json:"job_id"`
		}](t, queued).JobID
		replayed := request("POST", route+"/enqueue", cookie, csrf, map[string]any{}, key, 202)
		if decodeAPI[struct {
			JobID string `json:"job_id"`
		}](t, replayed).JobID != id {
			t.Fatal("enqueue replay created duplicate work")
		}
		request("POST", route+"/enqueue", cookie, csrf, map[string]any{"idempotency_key": "different"}, key, 400)
		_, err := runner.RunOnce(f.ctx)
		must(t, err)
		assertJob(t, f, id, "succeeded")
		request("GET", "/migration-jobs/"+id, readerCookie, readerCSRF, nil, "", 200)
		return id
	}
	id := approveAndRun(review.ID, "ui-up")
	listed := request("GET", "/migration-jobs"+databaseQuery, cookie, csrf, nil, "", 200)
	var list struct {
		Jobs []struct {
			ID     string `json:"job_id"`
			Status string `json:"status"`
			Rows   int64  `json:"committed_rows"`
		} `json:"jobs"`
	}
	must(t, json.Unmarshal(listed.Body.Bytes(), &list))
	if len(list.Jobs) != 1 || list.Jobs[0].ID != id || list.Jobs[0].Status != "succeeded" {
		t.Fatal("job list not connected to worker state")
	}
	history := func(hide bool, extra string) []executionResource {
		t.Helper()
		res := request("GET", "/migrations/history"+databaseQuery+fmt.Sprintf("&hide_rollback=%t", hide)+extra, cookie, csrf, nil, "", 200)
		return decodeAPI[struct {
			Executions []executionResource `json:"executions"`
		}](t, res).Executions
	}
	up := history(false, "")[0]
	if len(up.ID) != 36 || !up.CanRollback || up.Generation != 1 {
		t.Fatal("UUID or rollback generation missing")
	}
	rollback := request("POST", "/migrations/"+up.ID+"/rollback-reviews", cookie, csrf, map[string]any{"expected_generation": up.Generation}, "", 201)
	approveAndRun(decodeAPI[struct {
		Review reviewResource `json:"review"`
	}](t, rollback).Review.ID, "ui-down")
	if len(history(true, "&limit=1")) != 0 {
		t.Fatal("rollback filtering applied after pagination")
	}
	rolled := history(false, "&cursor=1&limit=1")[0]
	if rolled.ID != up.ID || !rolled.CanForward {
		t.Fatal("forward eligibility or pagination incorrect")
	}
	forward := request("POST", "/migrations/"+up.ID+"/forward-reviews", cookie, csrf, map[string]any{"expected_generation": rolled.Generation}, "", 201)
	approveAndRun(decodeAPI[struct {
		Review reviewResource `json:"review"`
	}](t, forward).Review.ID, "ui-forward")
	visible := history(true, "")
	if len(visible) != 1 || visible[0].Operation != "forward" || visible[0].SQL != up.SQL {
		t.Fatal("forward did not preserve original SQL/history pair")
	}
}

func TestBatchResourcesPreserveApprovedConfiguration(t *testing.T) {
	f, jobs, _ := newWorkerFixture(t)
	handler := http.StripPrefix("/api", api.NewHandler(f.userAccess, f.databases, f.migrations, false, jobs))
	identity, cookie := f.login(t, "it.editor")
	csrf, err := f.access.CSRFToken(identity)
	must(t, err)
	spec := engines.BatchSpec{Schema: "public", Table: "ui_counters", Key: "id", Column: "value", Delta: 3, Size: 7}
	up, down, err := engines.BatchSQL(spec)
	must(t, err)
	res := workflowRequest(t, handler, "POST", "/migration-definitions", cookie, csrf, map[string]any{"database_id": f.database, "title": "UI batch", "up_sql": up, "down_sql": down, "irreversible": false})
	if res.Code != 201 {
		t.Fatalf("batch create: %d %s", res.Code, res.Body.String())
	}
	def := decodeAPI[struct {
		Definition definitionResource `json:"definition"`
	}](t, res).Definition
	if def.Batch == nil || def.Batch.Size != 7 {
		t.Fatal("batch configuration missing from resource")
	}
	bad := workflowRequest(t, handler, "POST", "/migration-definitions", cookie, csrf, map[string]any{"database_id": f.database, "title": "Tampered batch", "up_sql": strings.Replace(up, "+ $1", "+ 9", 1), "down_sql": down, "irreversible": false})
	if bad.Code != 422 {
		t.Fatal("tampered batch template saved for approval")
	}
}
