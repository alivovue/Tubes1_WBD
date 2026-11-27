package observability_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"schakker_shared/observability"
)

func TestConsoleTraceCrossesQueueAndRedactsSecrets(t *testing.T) {
	previousLogger := slog.Default()
	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
	})
	var logs, spans bytes.Buffer
	shutdown, err := observability.Configure("test", &logs, &spans)
	if err != nil {
		t.Fatal(err)
	}
	ctx, parent := observability.Start(context.Background(), "api.enqueue")
	carrier := observability.Inject(ctx)
	workerCtx, child := observability.Start(observability.Extract(context.Background(), carrier), "worker.execute")
	if trace.SpanContextFromContext(ctx).TraceID() != trace.SpanContextFromContext(workerCtx).TraceID() {
		t.Fatal("queue lost trace identity")
	}
	observability.Event(workerCtx, "auth.result", nil, "password", "must-never-appear", "actor_id", 5)
	observability.SQL(workerCtx, "SELECT 'secret-row-value'", 1, 1, time.Now(), nil)
	child.End()
	parent.End()
	if err = shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "must-never-appear") || strings.Contains(logs.String(), "secret-row-value") {
		t.Fatal("secret logged")
	}
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var event map[string]any
		if err = json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event["timestamp"] == nil || event["trace_id"] == nil || event["span_id"] == nil {
			t.Fatal("missing structured correlation")
		}
	}
	decoder := json.NewDecoder(&spans)
	count := 0
	for {
		var span map[string]any
		err = decoder.Decode(&span)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		count++
	}
	if count != 2 {
		t.Fatalf("console exported %d spans", count)
	}
}

func TestSQLLogsKeepMigrationAndParameterContextWithoutValues(t *testing.T) {
	previous := slog.Default()
	defer slog.SetDefault(previous)
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	ctx := observability.WithActor(context.Background(), 7, 8)
	ctx = observability.WithMigration(ctx, observability.MigrationIdentity{DefinitionID: 9, RevisionID: 10, Revision: 2, ReviewID: 11, JobID: "job", ExecutionID: "execution", Attempt: 3})
	ctx = observability.WithBatch(ctx, 4)
	observability.SQL(ctx, "UPDATE audit_rows SET note=$1 WHERE id=$2", 2, 1, time.Now(), nil, "private-parameter", int64(5))
	if strings.Contains(logs.String(), "private-parameter") {
		t.Fatal("bound parameter leaked")
	}
	var event map[string]any
	if err := json.Unmarshal(logs.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{"actor_id": float64(7), "database_id": float64(8), "definition_id": float64(9), "revision_id": float64(10), "revision": float64(2), "review_id": float64(11), "job_id": "job", "execution_id": "execution", "attempt": float64(3), "batch_no": float64(4)} {
		if event[key] != want {
			t.Fatalf("%s=%v, want %v", key, event[key], want)
		}
	}
	parameters := event["parameters"].(map[string]any)
	if parameters["count"] != float64(2) || parameters["values"] != "[redacted]" || len(parameters["types"].([]any)) != 2 {
		t.Fatal("parameter metadata missing")
	}
	if !strings.Contains(event["query"].(string), "UPDATE audit_rows") {
		t.Fatal("SQL operation missing")
	}
}
