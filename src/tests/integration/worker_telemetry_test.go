package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"schakker_shared/observability"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/backend/migration/engines"
	pgengine "tubes1_wbd/backend/migration/engines/postgres"
)

type telemetryBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *telemetryBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}
func (b *telemetryBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.buffer.String() }
func TestWorkerTelemetryCrossesDurableQueueAndRedactsSQL(t *testing.T) {
	f, _, runner := newWorkerFixture(t)
	for _, name := range []string{"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"} {
		t.Setenv(name, "")
	}
	previousLogger, previousProvider, previousPropagator := slog.Default(), otel.GetTracerProvider(), otel.GetTextMapPropagator()
	var logs, spans telemetryBuffer
	shutdown, err := observability.Configure("worker-integration", &logs, &spans)
	must(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(ctx); err != nil {
			t.Error(err)
		}
		slog.SetDefault(previousLogger)
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
	})
	requestContext, parent := observability.Start(f.ctx, "test.enqueue.request")
	f.ctx = requestContext
	traceID := trace.SpanContextFromContext(requestContext).TraceID()
	id := enqueueDefinition(t, f, migration.DefinitionInput{Title: "Telemetry check", UpSQL: "CREATE TABLE telemetry_probe(id integer, note text DEFAULT 'private-migration-value')", DownSQL: "DROP TABLE telemetry_probe"})
	parent.End()
	persisted, err := f.runtimeStore.Job(f.ctx, id)
	must(t, err)
	restored := observability.Extract(context.Background(), persisted.TraceContext)
	if trace.SpanContextFromContext(restored).TraceID() != traceID || persisted.TraceContext["traceparent"] == "" {
		t.Fatal("durable queue lost the request trace")
	}
	executed := false
	runner.Execute = func(ctx context.Context, target *dbmanagement.Target, request engines.JobRequest, hooks engines.JobHooks) (engines.JobReceipt, error) {
		executed = true
		if trace.SpanContextFromContext(ctx).TraceID() != traceID {
			t.Error("detached worker did not restore queued trace")
		}
		return pgengine.RunJob(ctx, target.DB, target.Adapter, request, hooks)
	}
	workerContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	claimed, err := runner.RunOnce(workerContext)
	must(t, err)
	if !claimed || !executed {
		t.Fatal("worker did not execute real queued work")
	}
	assertJob(t, f, id, "succeeded")
	batch := batchDefinition(t, f, 5, 2)
	batchReview := f.approve(t, f.submit(t, batch))
	batchJob, err := f.workflow.Enqueue(f.ctx, f.editor, batchReview.ID, "telemetry-batches")
	must(t, err)
	_, err = runner.RunOnce(workerContext)
	must(t, err)
	assertJob(t, f, batchJob, "succeeded")
	failure := enqueueDefinition(t, f, migration.DefinitionInput{Title: "Logged failure", UpSQL: "SELECT 1/0;", DownSQL: "SELECT 1;"})
	if _, err = runner.RunOnce(workerContext); err == nil {
		t.Fatal("test SQL unexpectedly succeeded")
	}
	assertJob(t, f, failure, "failed")
	flush, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	must(t, shutdown(flush))
	if strings.Contains(logs.String(), "private-migration-value") {
		t.Fatal("row values leaked into operational logs")
	}
	foundWorkerLog := false
	foundSQL := false
	batches := map[float64]bool{}
	foundFailure, foundRollback := false, false
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var event map[string]any
		must(t, json.Unmarshal([]byte(line), &event))
		query, _ := event["query"].(string)
		if event["event"] == "sql.result" && event["job_id"] == batchJob && event["sql_hash"] == engines.SQLDigest(batch.UpSQL) {
			if number, ok := event["batch_no"].(float64); ok {
				batches[number] = true
			}
			if event["attempt"] != float64(1) || event["revision_id"] != float64(batch.ID) {
				t.Error("batch SQL lost immutable operation context")
			}
		}
		if event["event"] == "sql.result" && event["job_id"] == failure && event["sqlstate"] == "22012" && event["status"] == "failed" {
			foundFailure = true
		}
		if event["event"] == "sql.rollback" && event["job_id"] == failure {
			foundRollback = true
		}
		if event["event"] == "sql.result" && strings.HasPrefix(query, "CREATE TABLE telemetry_probe") {
			foundSQL = true
			for _, key := range []string{"migration_id", "revision_id", "job_id", "execution_id", "attempt", "actor_id", "database_id", "duration_ms", "affected_rows", "parameters", "sqlstate", "error_class"} {
				if event[key] == nil {
					t.Errorf("SQL event missing %s: %#v", key, event)
				}
			}
			if event["job_id"] != id || event["trace_id"] != traceID.String() || event["status"] != "succeeded" {
				t.Fatal("SQL context lost across queue/worker")
			}
		}
		if event["event"] == "worker.finished" && event["job_id"] == id {
			foundWorkerLog = true
			if event["trace_id"] != traceID.String() || event["job_status"] != "succeeded" {
				t.Fatalf("worker completion lost correlation: %#v", event)
			}
		}
	}
	if !foundWorkerLog {
		t.Fatal("worker completion log missing")
	}
	if !foundSQL {
		t.Fatal("safe, attributable SQL query structure was not logged")
	}
	if len(batches) != 3 || !foundFailure || !foundRollback {
		t.Fatalf("batch/failure audit incomplete: batches=%v failure=%v rollback=%v", batches, foundFailure, foundRollback)
	}
	decoder := json.NewDecoder(strings.NewReader(spans.String()))
	foundWorkerSpan, foundQueueSpan := false, false
	for {
		var span struct {
			Name        string
			SpanContext struct{ TraceID string }
		}
		err := decoder.Decode(&span)
		if err == io.EOF {
			break
		}
		must(t, err)
		if span.Name == "worker.execute" || span.Name == "queue.publish" {
			if span.SpanContext.TraceID != traceID.String() {
				t.Fatal("exported queue/worker span belongs to a different trace")
			}
			foundWorkerSpan = foundWorkerSpan || span.Name == "worker.execute"
			foundQueueSpan = foundQueueSpan || span.Name == "queue.publish"
		}
	}
	if !foundWorkerSpan || !foundQueueSpan {
		t.Fatal("queue or worker trace span missing")
	}
}
