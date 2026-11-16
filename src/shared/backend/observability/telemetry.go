package observability

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type correlationKey struct{}
type identityKey struct{}
type identity struct{ actor, database int64 }

func WithActor(ctx context.Context, actor, database int64) context.Context {
	value, _ := ctx.Value(identityKey{}).(identity)
	if actor > 0 {
		value.actor = actor
	}
	if database > 0 {
		value.database = database
	}
	return context.WithValue(ctx, identityKey{}, value)
}

func Configure(service string, logs, spans io.Writer) (func(context.Context) error, error) {
	if logs == nil {
		logs = os.Stdout
	}
	if spans == nil {
		spans = os.Stderr
	}
	base := slog.NewJSONHandler(logs, &slog.HandlerOptions{ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
		if a.Key == slog.TimeKey {
			a.Key = "timestamp"
		}
		key := strings.ToLower(a.Key)
		for _, secret := range []string{"password", "secret", "cookie", "authorization", "token", "dsn"} {
			if strings.Contains(key, secret) {
				a.Value = slog.StringValue("[redacted]")
				break
			}
		}
		return a
	}})
	exporter, err := stdouttrace.New(stdouttrace.WithWriter(spans))
	if err != nil {
		return nil, err
	}
	res := resource.NewSchemaless(attribute.String("service.name", service))
	traceOptions := []sdktrace.TracerProviderOption{sdktrace.WithResource(res), sdktrace.WithBatcher(exporter)}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" || os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != "" {
		remote, err := otlptracehttp.New(ctx)
		if err != nil {
			return nil, err
		}
		traceOptions = append(traceOptions, sdktrace.WithBatcher(remote))
	}
	var logsProvider *sdklog.LoggerProvider
	var handler slog.Handler = base
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" || os.Getenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT") != "" {
		remote, err := otlploghttp.New(ctx)
		if err != nil {
			return nil, err
		}
		logsProvider = sdklog.NewLoggerProvider(sdklog.WithResource(res), sdklog.WithProcessor(sdklog.NewBatchProcessor(remote)))
		handler = fanoutHandler{base, otelslog.NewHandler(service, otelslog.WithLoggerProvider(logsProvider))}
	}
	// Redact before fan-out so both console and OTLP see the same safe attributes.
	slog.SetDefault(slog.New(correlatedHandler{handler}))
	provider := sdktrace.NewTracerProvider(traceOptions...)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return func(ctx context.Context) error {
		var logsErr error
		if logsProvider != nil {
			logsErr = logsProvider.Shutdown(ctx)
		}
		return errors.Join(logsErr, provider.Shutdown(ctx))
	}, nil
}

type fanoutHandler []slog.Handler

func (h fanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, next := range h {
		if next.Enabled(ctx, level) {
			return true
		}
	}
	return false
}
func (h fanoutHandler) Handle(ctx context.Context, record slog.Record) error {
	var err error
	for _, next := range h {
		if next.Enabled(ctx, record.Level) {
			err = errors.Join(err, next.Handle(ctx, record))
		}
	}
	return err
}
func (h fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make(fanoutHandler, 0, len(h))
	for _, next := range h {
		out = append(out, next.WithAttrs(attrs))
	}
	return out
}
func (h fanoutHandler) WithGroup(name string) slog.Handler {
	out := make(fanoutHandler, 0, len(h))
	for _, next := range h {
		out = append(out, next.WithGroup(name))
	}
	return out
}
func redact(a slog.Attr) slog.Attr {
	key := strings.ToLower(a.Key)
	for _, secret := range []string{"password", "secret", "cookie", "authorization", "token", "dsn"} {
		if strings.Contains(key, secret) {
			return slog.String(a.Key, "[redacted]")
		}
	}
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		attrs := a.Value.Group()
		safe := make([]slog.Attr, len(attrs))
		for i, attr := range attrs {
			safe[i] = redact(attr)
		}
		a.Value = slog.GroupValue(safe...)
	}
	return a
}

type correlatedHandler struct{ slog.Handler }

func (h correlatedHandler) Handle(ctx context.Context, r slog.Record) error {
	safe := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool { safe.AddAttrs(redact(a)); return true })
	r = safe
	hasTrace := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "trace_id" {
			hasTrace = true
		}
		return true
	})
	if !hasTrace {
		sc := trace.SpanContextFromContext(ctx)
		r.AddAttrs(slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String()), slog.String("correlation_id", Correlation(ctx)))
	}
	return h.Handler.Handle(ctx, r)
}
func (h correlatedHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	safe := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		safe[i] = redact(a)
	}
	return correlatedHandler{h.Handler.WithAttrs(safe)}
}
func (h correlatedHandler) WithGroup(name string) slog.Handler {
	return correlatedHandler{h.Handler.WithGroup(name)}
}

func Start(ctx context.Context, name string) (context.Context, trace.Span) {
	return otel.Tracer("schakker").Start(ctx, name)
}
func Correlation(ctx context.Context) string {
	value, _ := ctx.Value(correlationKey{}).(string)
	return value
}
func Inject(ctx context.Context) map[string]string {
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	carrier["correlation_id"] = Correlation(ctx)
	return carrier
}
func Extract(ctx context.Context, carrier map[string]string) context.Context {
	ctx = propagation.TraceContext{}.Extract(ctx, propagation.MapCarrier(carrier))
	return context.WithValue(ctx, correlationKey{}, carrier["correlation_id"])
}

func Event(ctx context.Context, event string, err error, fields ...any) {
	sc := trace.SpanContextFromContext(ctx)
	status := "succeeded"
	level := slog.LevelInfo
	if err != nil {
		status = "failed"
		level = slog.LevelWarn
		trace.SpanFromContext(ctx).SetStatus(codes.Error, "operation failed")
		trace.SpanFromContext(ctx).AddEvent("operation.failed")
	}
	args := []any{"event", event, "status", status, "correlation_id", Correlation(ctx), "trace_id", sc.TraceID().String(), "span_id", sc.SpanID().String()}
	if who, ok := ctx.Value(identityKey{}).(identity); ok {
		args = append(args, "actor_id", who.actor, "database_id", who.database)
	}
	slog.Default().Log(ctx, level, event, append(args, fields...)...)
}
func SQL(ctx context.Context, query string, parameters int, rows int64, started time.Time, err error) {
	hash := sha256.Sum256([]byte(query))
	state := ""
	var pg interface{ SQLState() string }
	if errors.As(err, &pg) {
		state = pg.SQLState()
	}
	Event(ctx, "sql.result", err, "query", "[redacted]", "sql_hash", hex.EncodeToString(hash[:]), "parameter_count", parameters, "parameters", "[redacted]", "affected_rows", rows, "sqlstate", state, "duration_ms", float64(time.Since(started).Microseconds())/1000)
}

type Observer struct{}

func (Observer) Start(ctx context.Context, event string, actor, database int64) (context.Context, func(string)) {
	ctx = WithActor(ctx, actor, database)
	ctx, span := Start(ctx, event)
	span.SetAttributes(attribute.Int64("actor.id", actor), attribute.Int64("database.id", database))
	Event(ctx, event+".start", nil, "actor_id", actor, "database_id", database)
	return ctx, func(status string) {
		if status != "succeeded" {
			span.SetStatus(codes.Error, "operation rejected")
		}
		span.End()
	}
}

func InternalMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := propagation.TraceContext{}.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		if correlation := r.Header.Get("X-Request-ID"); len(correlation) == 32 {
			if _, err := hex.DecodeString(correlation); err == nil {
				ctx = context.WithValue(ctx, correlationKey{}, correlation)
			}
		}
		ctx, span := Start(ctx, "authority.http")
		defer span.End()
		tracked := &responseWriter{ResponseWriter: w}
		next.ServeHTTP(tracked, r.WithContext(ctx))
		observabilityStatus := tracked.status
		if observabilityStatus == 0 {
			observabilityStatus = 200
		}
		if observabilityStatus >= 400 {
			span.SetStatus(codes.Error, "authority request rejected")
		}
		var failure error
		if observabilityStatus >= 400 {
			failure = errors.New("authority request rejected")
		}
		Event(ctx, "authority.http.finished", failure, "http_status", observabilityStatus)
	})
}
func InjectHTTP(ctx context.Context, r *http.Request) {
	propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(r.Header))
	r.Header.Set("X-Request-ID", Correlation(ctx))
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *responseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(data)
}
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			http.Error(w, "Request unavailable", 503)
			return
		}
		correlation := hex.EncodeToString(id[:])
		ctx := context.WithValue(r.Context(), correlationKey{}, correlation)
		ctx, span := Start(ctx, "http."+r.Method)
		defer span.End()
		start := time.Now()
		w.Header().Set("X-Request-ID", correlation)
		tracked := &responseWriter{ResponseWriter: w}
		Event(ctx, "http.start", nil, "method", r.Method)
		next.ServeHTTP(tracked, r.WithContext(ctx))
		status := tracked.status
		if status == 0 {
			status = 200
		}
		span.SetAttributes(attribute.Int("http.response.status_code", status))
		if status >= 400 {
			span.SetStatus(codes.Error, "HTTP request rejected")
		}
		var failure error
		if status >= 400 {
			failure = errors.New("HTTP request rejected")
		}
		Event(ctx, "http.finished", failure, "method", r.Method, "http_status", status, "duration_ms", time.Since(start).Milliseconds())
		httpMetric(r.Method, status, time.Since(start))
	})
}
