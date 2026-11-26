# Real approved-job stress test

Run `bash src/tools/run-k6.sh` from the repository root with Go, PostgreSQL server tools, Python 3 and k6 on PATH. The launcher creates a disposable fixture, runs k6, renders results and stops/removes the fixture databases. It never reads application `.env` files or uses a production connection URL. Set `TMPDIR` to a spacious directory when needed. Existing listeners on 3300/9464 must be stopped or overridden with `SCHAKKER_K6_LISTEN` / `SCHAKKER_METRICS_ADDR`.

To export this test's telemetry to the local observability stack:

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318 bash src/tools/run-k6.sh
```

The fixture creates real independent admin/platform/target PostgreSQL databases, active test users, an editor group and a View Only approval group. Both group decisions go through the normal workflow policy. Every third migration uses the supported 1000-row, 100-row-per-batch BIGINT template. Others execute ordinary reversible SQL. The same production queue and four-concurrency worker execute all jobs.

The normal workload is **5 target VUs**: 2 API readers, 1 shared-target publisher and 2 publishers across two other databases. The load ramp targets 12 VUs and the stress ramp targets 24. Recovery ramps back to 5 and holds there. Default phase durations are 10/15/20/30 seconds; `SCHAKKER_K6_STAGE_SECONDS` supplies four positive seconds for another run. A graceful drain can extend recovery, and the report includes that duration.

Each shared-target publisher sends **two concurrent enqueue requests** with the same approval and idempotency key, verifies one returned job ID, and counts its completion once. Different-target publishers run concurrently on two other databases. Current approval policy binds the active generation, so a test-only per-target lease prepares fresh reviews serially; expected fixture contention (409) is counted separately. The broker does not execute migrations or bypass leader decisions. SQL runs only through the normal public enqueue endpoint and worker.

Scripts poll the real status API until committed success, terminal failure, or timeout. HTTP 202 is counted as accepted work, never as execution success. Completion-phase tags reflect when work finishes, not just when it was submitted. Credentials are temporary random values in a mode-0600 manifest removed when the fixture exits. Never commit that manifest or raw fixture logs.

Metrics include API p50/p95, request throughput/error rate, queue wait, migration execution time, end-to-end completion time, unique jobs accepted/completed, completion throughput, failures and fixture contention. Built-in URL tags are disabled; custom tags use only phase/workload names. API measurements exclude test-broker requests. Queue wait is creation→first start; migration time is first start→final completion and includes retry/lock effects.

The initial service objective is **API p95 <750 ms and error rate <1%**, with job failures <1%. These are test objectives for this local setup, not universal capacity guarantees. A threshold failure under stress is retained as evidence and must be explained. The script also emits per-phase submetrics, with broad diagnostic latency limits; the global API/error objectives remain the release thresholds. `report.py` writes missing samples as `n/a` rather than making up zero latency.

`SCHAKKER_K6_OUTPUT` selects the launcher report directory; otherwise it creates one under TMPDIR. Exit status preserves k6 threshold failures. Detailed measured results are linked from the repository README and `docs/m2-bonuses.md`.
