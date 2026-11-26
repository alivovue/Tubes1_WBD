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
	flush, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	must(t, shutdown(flush))
	if strings.Contains(logs.String(), "private-migration-value") || strings.Contains(logs.String(), "CREATE TABLE telemetry_probe") {
		t.Fatal("SQL or row values leaked into operational logs")
	}
	foundWorkerLog := false
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var event map[string]any
		must(t, json.Unmarshal([]byte(line), &event))
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
