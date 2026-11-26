package integration_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"tubes1_wbd/backend/api"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/database/setup"
)

func TestM2HistoryShowsFailuresAndDashboardShowsCommittedEvents(t *testing.T) {
	f, jobs, runner := p3Fixture(t)
	must(t, setup.ValidateSchema(f.ctx, f.dbs.Website))
	var oldTable bool
	must(t, f.dbs.Website.QueryRowContext(f.ctx, `SELECT to_regclass('public.migration_executions') IS NOT NULL`).Scan(&oldTable))
	if oldTable {
		t.Fatal("fresh schema still provisions M1 history")
	}
	version, err := f.runtimeStore.MigrationVersion(f.ctx, f.database)
	must(t, err)
	if version != 0 {
		t.Fatal("fresh database has a migration version")
	}
	success := enqueueP3(t, f, "committed_summary")
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	committed := assertJob(t, f, success, "succeeded")
	failed := enqueueDefinition(t, f, migration.DefinitionInput{Title: "Failed attempt", UpSQL: "SELECT 1/0;", DownSQL: "SELECT 1;"})
	if _, err = runner.RunOnce(f.ctx); err == nil {
		t.Fatal("failing SQL unexpectedly succeeded")
	}
	assertJob(t, f, failed, "failed")

	handler := http.StripPrefix("/api", api.NewHandler(f.userAccess, f.databases, f.migrations, false, jobs))
	_, editorCookie := f.login(t, "it.editor")
	_, readerCookie := f.login(t, f.reader.Username)
	type summary struct {
		ID           string    `json:"id"`
		DatabaseID   int64     `json:"database_id"`
		DatabaseName string    `json:"database_name"`
		Title        string    `json:"title"`
		Actor        string    `json:"actor"`
		Operation    string    `json:"operation"`
		CommittedAt  time.Time `json:"committed_at"`
		Version      int64     `json:"version"`
		Status       string    `json:"status"`
	}
	for _, cookie := range []string{editorCookie, readerCookie} {
		dashboard := apiRequest(t, handler, http.MethodGet, "/dashboard", cookie, nil)
		if dashboard.Code != http.StatusOK {
			t.Fatalf("dashboard returned %d: %s", dashboard.Code, dashboard.Body.String())
		}
		recent := decodeAPI[struct{ Recent []summary }](t, dashboard).Recent
		if len(recent) != 1 || recent[0].ID != committed.ExecutionID || recent[0].DatabaseID != f.database || recent[0].DatabaseName == "" || recent[0].Title != "committed_summary" || recent[0].Actor != "it.editor" || recent[0].Operation != "up" || recent[0].CommittedAt.IsZero() || recent[0].Version != 1 {
			t.Fatalf("dashboard did not project the committed M2 job: %+v", recent)
		}
		history := apiRequest(t, handler, http.MethodGet, fmt.Sprintf("/migrations/history?database_id=%d", f.database), cookie, nil)
		if history.Code != http.StatusOK {
			t.Fatalf("history returned %d: %s", history.Code, history.Body.String())
		}
		events := decodeAPI[struct {
			Executions []summary `json:"executions"`
		}](t, history).Executions
		if len(events) != 2 || events[0].Status != "failed" || events[0].Title != "Failed attempt" || !events[0].CommittedAt.IsZero() || events[1].ID != recent[0].ID || !events[1].CommittedAt.Equal(recent[0].CommittedAt) {
			t.Fatal("Changelog did not include the failure alongside the dashboard's committed event")
		}
	}
	activity := apiRequest(t, handler, http.MethodGet, fmt.Sprintf("/migration-jobs?database_id=%d", f.database), readerCookie, nil)
	if activity.Code != http.StatusOK {
		t.Fatalf("jobs returned %d: %s", activity.Code, activity.Body.String())
	}
	attempts := decodeAPI[struct {
		Jobs []struct {
			ID     string `json:"job_id"`
			Status string `json:"status"`
		} `json:"jobs"`
	}](t, activity).Jobs
	statuses := map[string]string{}
	for _, attempt := range attempts {
		statuses[attempt.ID] = attempt.Status
	}
	if len(attempts) != 2 || statuses[success] != "succeeded" || statuses[failed] != "failed" {
		t.Fatalf("Jobs lost the execution attempts: %+v", attempts)
	}
	if retired := apiRequest(t, handler, http.MethodGet, "/migrations", editorCookie, nil); retired.Code != http.StatusNotFound {
		t.Fatalf("retired M1 history API returned %d", retired.Code)
	}
	outsider := f.user(t, "history.outsider")
	_, outsiderCookie := f.login(t, outsider.Username)
	dashboard := apiRequest(t, handler, http.MethodGet, "/dashboard", outsiderCookie, nil)
	if dashboard.Code != http.StatusOK || len(decodeAPI[struct{ Recent []summary }](t, dashboard).Recent) != 0 {
		t.Fatal("dashboard exposed history without database access")
	}
	if denied := apiRequest(t, handler, http.MethodGet, fmt.Sprintf("/migrations/history?database_id=%d", f.database), outsiderCookie, nil); denied.Code != http.StatusForbidden {
		t.Fatalf("history without database access returned %d", denied.Code)
	}

	rollback, err := f.workflow.RollbackReview(f.ctx, f.editor, committed.ExecutionID, f.state(t).Generation)
	must(t, err)
	rollback = f.approve(t, rollback)
	id, err := f.workflow.Enqueue(f.ctx, f.editor, rollback.ID, "summary-rollback")
	must(t, err)
	_, err = runner.RunOnce(f.ctx)
	must(t, err)
	rolledBack := assertJob(t, f, id, "succeeded")
	version, err = f.runtimeStore.MigrationVersion(f.ctx, f.database)
	must(t, err)
	if version != 0 {
		t.Fatal("analysis version ignored the M2 rollback")
	}
	dashboard = apiRequest(t, handler, http.MethodGet, "/dashboard", editorCookie, nil)
	recent := decodeAPI[struct{ Recent []summary }](t, dashboard).Recent
	if dashboard.Code != http.StatusOK || len(recent) != 2 || recent[0].ID != rolledBack.ExecutionID || recent[0].Operation != "rollback" || recent[1].ID != committed.ExecutionID {
		t.Fatalf("dashboard did not order committed events by commit time: %+v", recent)
	}
}
