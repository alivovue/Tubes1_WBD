package observability_test

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	logsproto "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	traceproto "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
	"schakker_shared/observability"
)

func TestBonusOTLPExportsCorrelatedRedactedLogsAndTraces(t *testing.T) {
	previousLogger, previousProvider, previousPropagator := slog.Default(), otel.GetTracerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
	})
	var mu sync.Mutex
	payloads := map[string][]byte{}
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		mu.Lock()
		payloads[r.URL.Path] = append(payloads[r.URL.Path], raw...)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(200)
	}))
	defer collector.Close()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", collector.URL)
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	var console bytes.Buffer
	shutdown, err := observability.Configure("bonus-test", &console, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	ctx, parent := observability.Start(context.Background(), "api.enqueue")
	workerCtx, child := observability.Start(observability.Extract(context.Background(), observability.Inject(ctx)), "worker.execute")
	slog.Default().With("password", "secret-before-fanout").WithGroup("nested").InfoContext(workerCtx, "correlation.test", "cookie", "private-cookie", "event", "test.export")
	child.End()
	parent.End()
	cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = shutdown(cleanup); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, raw := range [][]byte{console.Bytes(), payloads["/v1/logs"]} {
		if bytes.Contains(raw, []byte("secret-before-fanout")) || bytes.Contains(raw, []byte("private-cookie")) {
			t.Fatal("credential reached an exporter")
		}
	}
	var traces traceproto.ExportTraceServiceRequest
	var logs logsproto.ExportLogsServiceRequest
	if err = proto.Unmarshal(payloads["/v1/traces"], &traces); err != nil {
		t.Fatal(err)
	}
	if err = proto.Unmarshal(payloads["/v1/logs"], &logs); err != nil {
		t.Fatal(err)
	}
	if len(traces.ResourceSpans) != 1 || len(logs.ResourceLogs) != 1 {
		t.Fatal("missing OTLP telemetry")
	}
	span := traces.ResourceSpans[0].ScopeSpans[0].Spans[0]
	record := logs.ResourceLogs[0].ScopeLogs[0].LogRecords[0]
	if len(record.TraceId) != 16 || !bytes.Equal(record.TraceId, span.TraceId) {
		t.Fatal("exported log lost queue trace correlation")
	}
}

type metricsDriver struct{}
type metricsConn struct{}

func (metricsDriver) Open(string) (driver.Conn, error)  { return metricsConn{}, nil }
func (metricsConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (metricsConn) Close() error                        { return nil }
func (metricsConn) Begin() (driver.Tx, error)           { return nil, errors.New("unused") }
func init()                                             { sql.Register("schakker-metrics-test", metricsDriver{}) }

func TestBonusMetricsExposeDurableQueueAndRealPoolWaitWithoutIdentityLabels(t *testing.T) {
	db, err := sql.Open("schakker-metrics-test", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wait, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if _, err = db.Conn(wait); err == nil {
		t.Fatal("pool wait was not exercised")
	}
	conn.Close()
	metrics := observability.NewMetrics()
	defer metrics.RegisterPool("platform", db)()
	defer metrics.RegisterQueue(func(context.Context) ([]observability.QueueSample, error) {
		return []observability.QueueSample{{Operation: "up", Status: "partial", Jobs: 2, Retries: 1, Batches: 3, Rows: 300}, {Operation: "private-job-id", Status: "private-user-id", Jobs: 999}}, nil
	})()
	metrics.ObserveAttempt(context.Background(), "up", "partial", time.Second)
	request := httptest.NewRequest("GET", "/metrics", nil)
	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, request)
	body := response.Body.String()
	for _, expected := range []string{`schakker_jobs{operation="up",status="partial"} 2`, `schakker_migration_rows{operation="up"} 300`, `schakker_job_retries_total{operation="up"} 1`, `schakker_database_pool_wait_total{pool="platform"} 1`, `schakker_database_pool_connections{pool="platform",state="idle"} 1`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing metric %s", expected)
		}
	}
	if strings.Contains(body, "private-job-id") || strings.Contains(body, "private-user-id") {
		t.Fatal("unbounded metric label")
	}
	if !strings.Contains(body, `schakker_metrics_collection_success{source="queue"} 1`) {
		t.Fatal("queue collection not reported")
	}
}
func TestBonusOperationalListenerRejectsUnprotectedRemoteBind(t *testing.T) {
	t.Setenv("SCHAKKER_METRICS_ADDR", "0.0.0.0:0")
	t.Setenv("SCHAKKER_METRICS_TOKEN", "")
	if stop, err := observability.StartMetrics(context.Background(), "127.0.0.1:0"); err == nil {
		stop()
		t.Fatal("unprotected operational listener exposed")
	}
}
