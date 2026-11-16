# Central logs, traces, metrics and Grafana

The Go processes keep JSON console logs and console traces. Set `OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318` to also export redacted logs and traces over OTLP/HTTP to the Collector. Signal-specific `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` / `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` can override destinations. This build uses HTTP/protobuf exporters. Credentials and sensitive SQL parameter values are redacted **before** console/OTLP fan-out.

```bash
bash src/observability/init.sh
docker compose -f src/observability/compose.yaml up -d
# Set this in each terminal, or in each application's .env before its launcher:
export OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318
bash run-admin.sh
# Other terminals: bash run.sh; bash run-worker.sh
```

Grafana is at `http://127.0.0.1:3030`, user `admin`; its generated password is in `src/observability/secrets/grafana_password`. The dashboard and Prometheus/Tempo/Loki data sources are provisioned automatically. No anonymous Grafana access is enabled. Service versions are pinned in `compose.yaml`.

The stack uses **host networking** so it can scrape the Go processes' isolated loopback listeners: user 9464, admin 9465, worker 9466 (`/metrics`). Linux supports this directly; Docker Desktop needs host networking enabled. No public metrics route is attached to either SPA API. `SCHAKKER_METRICS_ADDR=off` disables a process's operational listener. A non-loopback bind requires `SCHAKKER_METRICS_TOKEN` of at least 32 bytes; when configured, Prometheus also needs the matching bearer authorization config. Keep tokens outside tracked files.

Without Docker, install the same official portable releases and run:

```bash
python3 src/observability/run-local.py \
  --tools-directory /path/to/extracted/releases \
  --data-directory /path/to/observability-state
```

The local launcher finds the official binaries, provisions Grafana against the repository dashboard, keeps all state/logs in the selected directory and terminates its children on Ctrl+C. Its versions used for verification are recorded in `docs/evidence/bonuses/observability.json`. Backend readiness may initially return 503 while the single-node rings become ready; wait until `/ready` returns 200.

## Metrics and interpretation

- `schakker_jobs{operation,status}` reads durable queue state, including zero statuses and partial/uncertain work. API and worker export the same database counts, so the dashboard uses `max` before summing to avoid counting each job twice.
- `schakker_job_retries_total{operation}` reads persisted attempt numbers, so worker restarts preserve retry evidence.
- `schakker_queue_wait_seconds` measures creation→first claim. `schakker_database_lock_wait_seconds` measures acquisition, including connection checkout. `schakker_migration_duration_seconds` measures each worker attempt.
- `schakker_migration_attempts_total` counts finished worker attempts; retries are separate observations. Histograms/counters reset when that process restarts.
- `schakker_migration_batches`, `schakker_migration_rows` and reverted equivalents expose persisted progress projections, including partial jobs. These are gauges because recovery can rebuild a lost projection; they are not exact-once event counters.
- `schakker_database_pool_connections{pool,state}` and pool wait counters use Go `sql.DB.Stats()` for long-lived platform/admin pools. Short-lived target connections are not retained in a global registry.
- `schakker_http_request_duration_seconds` provides API latency by method/status class. `schakker_metrics_collection_success` makes failed queue scrapes visible rather than presenting stale counts as fresh.

Metric labels use fixed operation/status/pool/method enums. Job, actor, database and trace IDs never become metric labels. Trace IDs are permitted only as OpenMetrics exemplars. Loki indexes `service.name`; identifiers remain structured log metadata. `observability.json` records the actual index labels seen in the test deployment.

## Correlate a real operation

Open the Schakker dashboard and use a migration-duration exemplar to open its trace in Tempo. Follow its trace ID to the matching Loki `worker.finished` event; the reverse derived-field link is also provisioned. The data-source config includes trace→logs links and the Prometheus exemplar mapping.

A concrete verified example, including its trace ID, 164-span trace, metric exemplar and successful log match, is saved in `docs/evidence/bonuses/observability.json`. Loki query shape:

```logql
{service_name="schakker-load-fixture"} | trace_id="<trace-id>"
```

Durations, queue state, logs and traces are separate signals; a metric sample alone is not proof that a migration committed. The durable job/history and target receipt remain authoritative.

Primary implementation references: [OpenTelemetry Go exporters](https://opentelemetry.io/docs/languages/go/exporters/), [Loki OTLP ingestion](https://grafana.com/docs/loki/latest/send-data/otel/), [Grafana Loki correlation](https://grafana.com/docs/grafana/latest/datasources/loki/configure/).
