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

func TestP3ConsoleTraceCrossesQueueAndRedactsSecrets(t *testing.T) {
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
