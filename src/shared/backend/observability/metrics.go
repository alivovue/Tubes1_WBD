package observability

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/trace"
)

var operations = []string{"up", "rollback", "forward", "batch_cancel"}
var jobStatuses = []string{"pending", "running", "succeeded", "failed", "partial", "uncertain", "reverting", "reverted", "partially_reverted"}

type QueueSample struct {
	Operation, Status                                           string
	Jobs, Retries, Batches, Rows, RevertedBatches, RevertedRows float64
}
type QueueReader func(context.Context) ([]QueueSample, error)

// Metrics owns a private registry, avoiding duplicate registration and test globals.
// Its labels are fixed enums: identifiers, SQL and connection strings are excluded.
type Metrics struct {
	registry                                                                                                             *prometheus.Registry
	attempts                                                                                                             *prometheus.CounterVec
	queueWait, lockWait, duration, httpDuration                                                                          *prometheus.HistogramVec
	mu                                                                                                                   sync.RWMutex
	pools                                                                                                                map[*sql.DB]string
	queue                                                                                                                QueueReader
	queueGeneration                                                                                                      uint64
	jobs, retries, batches, rows, revertedBatches, revertedRows, poolConnections, poolWait, poolWaitCount, scrapeSuccess *prometheus.Desc
}

var DefaultMetrics = NewMetrics()

func NewMetrics() *Metrics {
	m := &Metrics{registry: prometheus.NewRegistry(), pools: make(map[*sql.DB]string)}
	m.attempts = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "schakker_migration_attempts_total", Help: "Finished worker attempts by classified outcome."}, []string{"operation", "status"})
	buckets := []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 180, 600}
	m.queueWait = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "schakker_queue_wait_seconds", Help: "Time before the first worker claim; excludes retries.", Buckets: buckets}, []string{"operation"})
	m.lockWait = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "schakker_database_lock_wait_seconds", Help: "Metadata lock acquisition time, including connection checkout.", Buckets: buckets}, []string{"kind", "status"})
	m.duration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "schakker_migration_duration_seconds", Help: "Worker attempt duration, including lock wait and recovery.", Buckets: buckets}, []string{"operation", "status"})
	m.httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "schakker_http_request_duration_seconds", Help: "Public API request duration without URL or identity labels.", Buckets: buckets}, []string{"method", "status_class"})
	labels := []string{"operation", "status"}
	m.jobs = prometheus.NewDesc("schakker_jobs", "Durable job counts from the metadata database.", labels, nil)
	m.retries = prometheus.NewDesc("schakker_job_retries_total", "Durable retry claims (attempts after the first).", []string{"operation"}, nil)
	m.batches = prometheus.NewDesc("schakker_migration_batches", "Projected committed batches, including partially applied jobs.", []string{"operation"}, nil)
	m.rows = prometheus.NewDesc("schakker_migration_rows", "Projected committed rows, including partially applied jobs.", []string{"operation"}, nil)
	m.revertedBatches = prometheus.NewDesc("schakker_migration_reverted_batches", "Projected compensated batches.", []string{"operation"}, nil)
	m.revertedRows = prometheus.NewDesc("schakker_migration_reverted_rows", "Projected compensated rows.", []string{"operation"}, nil)
	m.poolConnections = prometheus.NewDesc("schakker_database_pool_connections", "Connections in long-lived application database pools.", []string{"pool", "state"}, nil)
	m.poolWait = prometheus.NewDesc("schakker_database_pool_wait_seconds_total", "Cumulative time waiting for an application database connection.", []string{"pool"}, nil)
	m.poolWaitCount = prometheus.NewDesc("schakker_database_pool_wait_total", "Cumulative application connection-pool waits.", []string{"pool"}, nil)
	m.scrapeSuccess = prometheus.NewDesc("schakker_metrics_collection_success", "Whether the last queue collection succeeded; zero exposes missing evidence.", []string{"source"}, nil)
	m.registry.MustRegister(m, m.attempts, m.queueWait, m.lockWait, m.duration, m.httpDuration, collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return m
}
func bounded(value string, allowed []string) string {
	for _, candidate := range allowed {
		if value == candidate {
			return value
		}
	}
	return "other"
}
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{EnableOpenMetrics: true, Timeout: 5 * time.Second})
}
func (m *Metrics) RegisterPool(name string, db *sql.DB) func() {
	name = bounded(name, []string{"platform", "admin"})
	m.mu.Lock()
	m.pools[db] = name
	m.mu.Unlock()
	return func() { m.mu.Lock(); delete(m.pools, db); m.mu.Unlock() }
}
func (m *Metrics) RegisterQueue(reader QueueReader) func() {
	m.mu.Lock()
	m.queueGeneration++
	generation := m.queueGeneration
	m.queue = reader
	m.mu.Unlock()
	return func() {
		m.mu.Lock()
		if m.queueGeneration == generation {
			m.queue = nil
		}
		m.mu.Unlock()
	}
}
func (m *Metrics) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{m.jobs, m.retries, m.batches, m.rows, m.revertedBatches, m.revertedRows, m.poolConnections, m.poolWait, m.poolWaitCount, m.scrapeSuccess} {
		ch <- d
	}
}
func (m *Metrics) Collect(ch chan<- prometheus.Metric) {
	m.mu.RLock()
	reader := m.queue
	pools := make(map[string]sql.DBStats)
	for db, name := range m.pools {
		s, current := db.Stats(), pools[name]
		current.InUse += s.InUse
		current.Idle += s.Idle
		current.WaitCount += s.WaitCount
		current.WaitDuration += s.WaitDuration
		pools[name] = current
	}
	m.mu.RUnlock()
	for name, stats := range pools {
		ch <- prometheus.MustNewConstMetric(m.poolConnections, prometheus.GaugeValue, float64(stats.InUse), name, "active")
		ch <- prometheus.MustNewConstMetric(m.poolConnections, prometheus.GaugeValue, float64(stats.Idle), name, "idle")
		ch <- prometheus.MustNewConstMetric(m.poolWait, prometheus.CounterValue, stats.WaitDuration.Seconds(), name)
		ch <- prometheus.MustNewConstMetric(m.poolWaitCount, prometheus.CounterValue, float64(stats.WaitCount), name)
	}
	if reader == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	samples, err := reader(ctx)
	if err != nil {
		ch <- prometheus.MustNewConstMetric(m.scrapeSuccess, prometheus.GaugeValue, 0, "queue")
		return
	}
	ch <- prometheus.MustNewConstMetric(m.scrapeSuccess, prometheus.GaugeValue, 1, "queue")
	counts := map[string]float64{}
	totals := map[string]QueueSample{}
	for _, s := range samples {
		op, status := bounded(s.Operation, operations), bounded(s.Status, jobStatuses)
		if op == "other" || status == "other" {
			continue
		}
		counts[op+":"+status] += s.Jobs
		t := totals[op]
		t.Retries += s.Retries
		t.Batches += s.Batches
		t.Rows += s.Rows
		t.RevertedBatches += s.RevertedBatches
		t.RevertedRows += s.RevertedRows
		totals[op] = t
	}
	for _, op := range operations {
		for _, status := range jobStatuses {
			ch <- prometheus.MustNewConstMetric(m.jobs, prometheus.GaugeValue, counts[op+":"+status], op, status)
		}
		t := totals[op]
		ch <- prometheus.MustNewConstMetric(m.retries, prometheus.CounterValue, t.Retries, op)
		for _, metric := range []struct {
			desc  *prometheus.Desc
			value float64
		}{{m.batches, t.Batches}, {m.rows, t.Rows}, {m.revertedBatches, t.RevertedBatches}, {m.revertedRows, t.RevertedRows}} {
			ch <- prometheus.MustNewConstMetric(metric.desc, prometheus.GaugeValue, metric.value, op)
		}
	}
}
func observe(ctx context.Context, histogram prometheus.Observer, seconds float64) {
	if seconds < 0 {
		seconds = 0
	}
	if exemplar, ok := histogram.(prometheus.ExemplarObserver); ok && trace.SpanContextFromContext(ctx).HasTraceID() {
		exemplar.ObserveWithExemplar(seconds, prometheus.Labels{"trace_id": trace.SpanContextFromContext(ctx).TraceID().String()})
	} else {
		histogram.Observe(seconds)
	}
}
func (m *Metrics) ObserveAttempt(ctx context.Context, operation, status string, elapsed time.Duration) {
	operation, status = bounded(operation, operations), bounded(status, jobStatuses)
	m.attempts.WithLabelValues(operation, status).Inc()
	observe(ctx, m.duration.WithLabelValues(operation, status), elapsed.Seconds())
}
func QueueWait(ctx context.Context, operation string, elapsed time.Duration) {
	observe(ctx, DefaultMetrics.queueWait.WithLabelValues(bounded(operation, operations)), elapsed.Seconds())
}
func LockWait(ctx context.Context, kind string, elapsed time.Duration, err error) {
	status := "succeeded"
	if err != nil {
		status = "failed"
	}
	observe(ctx, DefaultMetrics.lockWait.WithLabelValues(bounded(kind, []string{"session", "transaction"}), status), elapsed.Seconds())
}
func httpMetric(method string, status int, elapsed time.Duration) {
	method = bounded(method, []string{"GET", "HEAD", "POST", "PATCH", "DELETE", "PUT", "OPTIONS"})
	DefaultMetrics.httpDuration.WithLabelValues(method, strconv.Itoa(status/100)+"xx").Observe(elapsed.Seconds())
}

// StartMetrics binds a separate operational listener. Remote binds require a
// configured bearer credential; metrics are never attached to the browser API.
func StartMetrics(ctx context.Context, defaultAddress string) (func(), error) {
	address := os.Getenv("SCHAKKER_METRICS_ADDR")
	if address == "off" {
		return func() {}, nil
	}
	if address == "" {
		address = defaultAddress
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("invalid SCHAKKER_METRICS_ADDR")
	}
	token := os.Getenv("SCHAKKER_METRICS_TOKEN")
	ip := net.ParseIP(host)
	if (ip == nil || !ip.IsLoopback()) && len(token) < 32 {
		return nil, errors.New("non-loopback metrics listener requires a token of at least 32 bytes")
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(405)
			return
		}
		if token != "" && subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")), []byte(token)) != 1 {
			w.WriteHeader(401)
			return
		}
		DefaultMetrics.Handler().ServeHTTP(w, r)
	}))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			Event(ctx, "metrics.listener.failed", err)
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			server.Shutdown(cleanup)
		})
	}, nil
}
