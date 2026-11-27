package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"schakker_shared/observability"
	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/database/dao"
	admindao "tubes1_wbd_admin/database/dao"
)

func captureSQLLogs(t *testing.T) (*telemetryBuffer, *telemetryBuffer, func()) {
	t.Helper()
	for _, key := range []string{"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"} {
		t.Setenv(key, "")
	}
	logger, provider, propagator := slog.Default(), otel.GetTracerProvider(), otel.GetTextMapPropagator()
	var logs, spans telemetryBuffer
	shutdown, err := observability.Configure("sql-integration", &logs, &spans)
	must(t, err)
	stop := func() { must(t, shutdown(context.Background())) }
	t.Cleanup(func() {
		stop()
		slog.SetDefault(logger)
		otel.SetTracerProvider(provider)
		otel.SetTextMapPropagator(propagator)
	})
	return &logs, &spans, stop
}

func loggedEvents(t *testing.T, logs string) []map[string]any {
	t.Helper()
	var events []map[string]any
	decoder := json.NewDecoder(strings.NewReader(logs))
	for {
		var event map[string]any
		err := decoder.Decode(&event)
		if err == io.EOF {
			break
		}
		must(t, err)
		events = append(events, event)
	}
	return events
}

func TestSQLViewerLogsDeferredReadFailureAndErrorSpan(t *testing.T) {
	f := newFixture(t, true)
	target := f.register(t)
	f.grant(t, f.editor, target.ID, access.ViewOnly)
	_, err := f.dbs.Managed.ExecContext(f.ctx, `CREATE TABLE stream_error_rows AS SELECT i,repeat('x',1024) AS padding FROM generate_series(1,100) i`)
	must(t, err)
	logs, spans, stop := captureSQLLogs(t)
	_, err = f.databases.ReadQuery(f.ctx, f.editor, target.ID, `SELECT i,padding,1/(100-i) FROM stream_error_rows`)
	wantError(t, err, dbmanagement.ErrQuery)
	stop()
	found := false
	for _, event := range loggedEvents(t, logs.String()) {
		if event["event"] == "sql.result" && strings.Contains(event["query"].(string), "schakker_viewer_result") {
			if found || event["status"] != "failed" || event["sqlstate"] != "22012" || event["affected_rows"] != float64(99) {
				t.Fatalf("incorrect streamed SQL result: %#v", event)
			}
			found = true
			spanFound := false
			for _, span := range loggedEvents(t, spans.String()) {
				if span["SpanContext"].(map[string]any)["SpanID"] == event["span_id"] {
					spanFound = true
					if span["Status"].(map[string]any)["Code"] != "Error" {
						t.Fatal("deferred failure missing from query span")
					}
				}
			}
			if !spanFound {
				t.Fatal("SQL result has no exported span")
			}
		}
	}
	if !found {
		t.Fatal("streamed SQL failure was not logged")
	}
}

func TestSQLAnalysisLogsPlanFingerprintAndCommit(t *testing.T) {
	f := newFixture(t, true)
	target := f.register(t)
	f.grant(t, f.editor, target.ID, access.ViewEdit)
	logs, _, _ := captureSQLLogs(t)
	_, err := f.migrations.Analyze(f.ctx, f.editor, target.ID, `UPDATE employees SET salary=salary+1`)
	must(t, err)
	var plan, fingerprint, commit bool
	for _, event := range loggedEvents(t, logs.String()) {
		if event["event"] == "sql.commit" {
			commit = true
		}
		if event["event"] != "sql.result" {
			continue
		}
		query, _ := event["query"].(string)
		plan = plan || strings.Contains(query, "EXPLAIN")
		fingerprint = fingerprint || strings.Contains(query, "string_agg")
	}
	if !plan || !fingerprint || !commit {
		t.Fatalf("missing analysis telemetry: plan=%v fingerprint=%v commit=%v", plan, fingerprint, commit)
	}
}

func TestMetadataRollbackAndDatabaseLockAreLogged(t *testing.T) {
	f := newFixture(t, true)
	for _, admin := range []bool{false, true} {
		t.Run(map[bool]string{false: "user", true: "admin"}[admin], func(t *testing.T) {
			logs, _, _ := captureSQLLogs(t)
			expected := errors.New("forced transaction failure")
			var err error
			if admin {
				err = f.store.Transaction(f.ctx, func(*admindao.Store) error { return expected })
			} else {
				err = f.runtimeStore.Transaction(f.ctx, func(*dao.Store) error { return expected })
			}
			wantError(t, err, expected)
			found := false
			for _, event := range loggedEvents(t, logs.String()) {
				found = found || event["event"] == "sql.rollback" && event["status"] == "succeeded"
			}
			if !found {
				t.Fatal("transaction rollback was not recorded")
			}
		})
	}
	logs, _, _ := captureSQLLogs(t)
	must(t, f.runtimeStore.WithDatabaseLock(f.ctx, 123, func(*dao.Store) error { return nil }))
	var lock, unlock bool
	for _, event := range loggedEvents(t, logs.String()) {
		if event["event"] != "sql.result" {
			continue
		}
		query, _ := event["query"].(string)
		lock = lock || strings.Contains(query, "pg_advisory_lock")
		unlock = unlock || strings.Contains(query, "pg_advisory_unlock")
	}
	if !lock || !unlock {
		t.Fatal("advisory lock SQL missing from audit")
	}
}
