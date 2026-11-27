package observability_test

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel"
	"schakker_shared/observability"
)

type resultDriver struct{}
type resultConn struct{}
type resultRows struct {
	query string
	n     int
}

func (resultDriver) Open(string) (driver.Conn, error)  { return resultConn{}, nil }
func (resultConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (resultConn) Begin() (driver.Tx, error)           { return nil, errors.New("unused") }
func (resultConn) Close() error                        { return nil }
func (resultConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(query, "immediate_error") {
		return nil, &pgconn.PgError{Code: "22012"}
	}
	return &resultRows{query: query}, nil
}
func (r *resultRows) Columns() []string { return []string{"value"} }
func (r *resultRows) Close() error {
	if strings.Contains(r.query, "close_error") {
		return &pgconn.PgError{Code: "57014"}
	}
	return nil
}
func (r *resultRows) Next(dest []driver.Value) error {
	if strings.Contains(r.query, "first_error") || r.n > 0 && strings.Contains(r.query, "late_error") {
		return &pgconn.PgError{Code: "22012"}
	}
	if r.n > 0 || strings.Contains(r.query, "no_rows") {
		return io.EOF
	}
	r.n++
	dest[0] = int64(1)
	if strings.Contains(r.query, "scan_error") {
		dest[0] = "not an integer"
	}
	return nil
}
func init() { sql.Register("schakker-result-test", resultDriver{}) }

func sqlEvents(t *testing.T, logs string) []map[string]any {
	t.Helper()
	var events []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		if line == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event["event"] == "sql.result" {
			events = append(events, event)
		}
	}
	return events
}

func TestSQLResultTracksReadingAndReportsFailuresOnce(t *testing.T) {
	for _, tc := range []struct {
		name, mode, state       string
		row, closeEarly, noRows bool
	}{
		{name: "late query error", mode: "late_error", state: "22012"},
		{name: "query execution error", mode: "immediate_error", state: "22012"},
		{name: "query conversion error", mode: "scan_error"},
		{name: "query close error", mode: "close_error", state: "57014", closeEarly: true},
		{name: "successful query", mode: "success"},
		{name: "early close", mode: "success", closeEarly: true},
		{name: "row read error", mode: "first_error", state: "22012", row: true},
		{name: "row conversion error", mode: "scan_error", row: true},
		{name: "row immediate error", mode: "immediate_error", state: "22012", row: true},
		{name: "successful row", mode: "success", row: true},
		{name: "missing row is not a SQL failure", mode: "no_rows", row: true, noRows: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range []string{"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"} {
				t.Setenv(key, "")
			}
			logger, provider, propagator := slog.Default(), otel.GetTracerProvider(), otel.GetTextMapPropagator()
			defer func() {
				slog.SetDefault(logger)
				otel.SetTracerProvider(provider)
				otel.SetTextMapPropagator(propagator)
			}()
			var logs, spans bytes.Buffer
			shutdown, err := observability.Configure("sql-results-test", &logs, &spans)
			if err != nil {
				t.Fatal(err)
			}
			defer shutdown(context.Background())
			db, err := sql.Open("schakker-result-test", "")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			q := observability.WrapSQL(db)
			ctx := context.Background()
			query := "SELECT value FROM " + tc.mode
			var readErr error
			if tc.row {
				row := q.QueryRowContext(ctx, query, "private-parameter")
				if tc.mode != "immediate_error" && len(sqlEvents(t, logs.String())) != 0 {
					t.Fatal("result emitted before Scan")
				}
				_ = row.Err()
				var value int
				readErr = row.Scan(&value)
				_ = row.Err()
			} else {
				rows, err := q.QueryContext(ctx, query, "private-parameter")
				readErr = err
				if err == nil {
					if len(sqlEvents(t, logs.String())) != 0 {
						t.Fatal("result emitted before reading")
					}
					// Reading time belongs to the SQL span and duration.
					time.Sleep(10 * time.Millisecond)
					for rows.Next() {
						var value int
						if readErr = rows.Scan(&value); readErr != nil || tc.closeEarly {
							break
						}
					}
					readErr = errors.Join(readErr, rows.Err(), rows.Close())
					_ = rows.Close()
				}
			}
			if tc.noRows && !errors.Is(readErr, sql.ErrNoRows) {
				t.Fatal("missing row error changed")
			}
			wantFailed := strings.Contains(tc.mode, "error")
			if (readErr != nil && !tc.noRows) != wantFailed {
				t.Fatalf("unexpected read result: %v", readErr)
			}
			events := sqlEvents(t, logs.String())
			if len(events) != 1 {
				t.Fatalf("got %d SQL results", len(events))
			}
			event := events[0]
			if (event["status"] == "failed") != wantFailed || event["sqlstate"] != tc.state {
				t.Fatalf("wrong SQL result: %#v", event)
			}
			if !tc.row && tc.mode != "immediate_error" && event["duration_ms"].(float64) < 10 {
				t.Fatal("reading duration missing")
			}
			if strings.Contains(logs.String(), "private-parameter") {
				t.Fatal("parameter leaked")
			}
			if err := shutdown(ctx); err != nil {
				t.Fatal(err)
			}
			var span map[string]any
			if err := json.Unmarshal(bytes.TrimSpace(spans.Bytes()), &span); err != nil {
				t.Fatal(err)
			}
			status := span["Status"].(map[string]any)["Code"]
			if (status == "Error") != wantFailed {
				t.Fatalf("wrong span status: %v", status)
			}
		})
	}
}
