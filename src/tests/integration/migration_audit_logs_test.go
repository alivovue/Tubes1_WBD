package integration_test

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"tubes1_wbd/backend/migration"
)

func TestMigrationLogsIdentifySuccessfulAndFailedAttempts(t *testing.T) {
	f, _, runner := newWorkerFixture(t)
	var logs telemetryBuffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	for _, failed := range []bool{false, true} {
		input := migration.DefinitionInput{Title: "Audit context", UpSQL: "CREATE TABLE audit_log_context(id integer, note text DEFAULT 'private-log-value')", DownSQL: "DROP TABLE audit_log_context"}
		if failed {
			input.UpSQL = "SELECT 1/0 /* private-log-value */"
			input.DownSQL = "SELECT 1"
		}
		revision, err := f.workflow.CreateDefinition(f.ctx, f.editor, f.database, input)
		must(t, err)
		review := f.approve(t, f.submit(t, revision))
		id, err := f.workflow.Enqueue(f.ctx, f.editor, review.ID, input.UpSQL)
		must(t, err)
		_, err = runner.RunOnce(f.ctx)
		status := "succeeded"
		if failed {
			status = "failed"
			if err == nil {
				t.Fatal("division by zero succeeded")
			}
		} else {
			must(t, err)
		}
		job := assertJob(t, f, id, status)
		finished, result := false, false
		for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
			var event map[string]any
			must(t, json.Unmarshal([]byte(line), &event))
			if event["job_id"] != id {
				continue
			}
			if event["event"] == "worker.finished" {
				finished = true
				if event["definition_id"] != float64(revision.DefinitionID) || event["revision_id"] != float64(revision.ID) || event["revision"] != float64(1) || event["execution_id"] != job.ExecutionID || event["attempt"] != float64(1) || event["actor_id"] != float64(f.editor) || event["job_status"] != status {
					t.Fatalf("attempt identifiers incomplete: %#v", event)
				}
				snapshot, _ := event["sql_snapshot"].(string)
				if snapshot == "" || snapshot == "[redacted]" || snapshot == "[unavailable]" {
					t.Fatal("attempt SQL snapshot missing")
				}
			}
			if event["event"] == "sql.result" && event["definition_id"] == float64(revision.DefinitionID) {
				result = true
				if event["revision_id"] != float64(revision.ID) || event["attempt"] != float64(1) || event["parameters"] == nil || event["duration_ms"] == nil {
					t.Fatal("SQL context incomplete")
				}
			}
		}
		if !finished || !result {
			t.Fatal("worker or SQL audit event missing")
		}
	}
	if strings.Contains(logs.String(), "private-log-value") {
		t.Fatal("SQL literal/comment secret was logged")
	}
}
