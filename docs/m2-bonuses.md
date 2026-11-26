# Milestone 2 bonuses

This implements the four bonus areas in the supplied M2 specification and Section 8 of the implementation plan. The documents define requirements; they do not establish that an external submission or hosted workflow has happened.

| Bonus | Implementation and measured evidence | Remaining external action |
| --- | --- | --- |
| Observability | OTLP logs **and** traces → Collector → Loki/Tempo; isolated Prometheus metrics; provisioned Grafana dashboard with 12 panels and correlation links. [Setup](../src/observability/README.md), [correlation records](evidence/bonuses/observability.json), [actual dashboard screenshot](evidence/bonuses/grafana.png). | Run the stack alongside the team's provisioned application databases for the demo. |
| k6 | Authenticated API and real approved migrations on shared/different PostgreSQL targets, ramps above normal then recovery, polling through committed completion, thresholds and per-stage analysis. [Scripts/setup](../src/tests/k6/README.md), [raw measured summary](evidence/bonuses/k6.json), [generated table](evidence/bonuses/k6.md). | None for the recorded local run; API latency threshold failure is retained. |
| CodeQL | Inactive GitHub Actions template for Go and JS/TS in `temp/workflows/`, actual local security-extended scans, every finding reviewed, allocation fix commit and post-fix scan. [Security review](m2-security-review.md). | Hosted automation is intentionally inactive at the user's request; local evidence is retained. |
| Early submission | [Deadline and receipt checklist](m2-early-submission.md). Cutoff: **23 November 2026, 13:10 WIB**. | Submit the repository through the course form and retain the actual timestamp/receipt. Code cannot earn this bonus by itself. |

## Observability verification

The official standalone Collector, Loki, Tempo, Prometheus and Grafana releases were actually started with the tracked configs using `run-local.py`; Docker was unavailable on this host. The Compose alternative uses the same pinned versions/configs but was not executed here. [Environment](evidence/bonuses/environment.json) and [backend versions/correlation](evidence/bonuses/observability.json) record the tested versions.

The provisioned dashboard queried real queue status, retries, queue/lock wait histograms, attempt duration, committed/compensated batch and row projections, and platform/admin connection pools. Queue counts are deduplicated across API/worker exporters. Durable progress remains distinct from process-local histograms/counters. Scrape failures have a separate metric.

The recorded trace has 164 spans covering API → approval/enqueue → queue → worker → lock → SQL/commit. Its trace ID matches a Loki `worker.finished` log and a migration-duration OpenMetrics exemplar. Representative actual log/span records are retained alongside the screenshot. Loki's observed index labels were only `service_name` and its internal `__stream_shard__`; identity/job/SQL values are excluded from metric labels. Credentials/parameters are redacted before both log exporters. Pool checkout wait and the actual OTLP protobuf log/trace path also have automated tests.

## k6 results and interpretation

The simultaneous scenarios use 2 API VUs, 1 shared-database publisher and 2 different-database publishers as the defined normal load (5 target VUs). The next ramps target 12 and 24 VUs. Recovery returns to 5 over ten seconds, holds for twenty seconds and allows accepted jobs to drain. Tests use three isolated targets, real admin authority and both editor/View Only leader approvals. Every third migration is a 1000-row batch; this is a stress fixture, separate from the mandatory >1-second large-dataset benchmark.

The final 75-second schedule plus 2.79-second drain completed **55 of 55 accepted unique jobs**, with no API errors or job failures. Duplicate enqueue responses must identify the same job and count once. The global **p95 API <750 ms objective failed**, while API error <1% and job failure <1% passed. k6 returned its nonzero threshold-failure exit code; the runner preserves it.

The [repository README](../README.md#hasil-stress-test-bonus) contains the measured phase table and analysis requested by the specification. The [machine-readable summary](evidence/bonuses/k6.json) retains all thresholds, including broad per-phase diagnostics that do not replace the global objectives. Re-render it with:

```bash
python3 src/tests/k6/report.py docs/evidence/bonuses/k6.json
```

Normal API p95 was 367 ms; load reached 810 ms and stress 1758 ms, while request throughput decreased from 31.4 to 19.6/s. Job completion throughput fell from 0.90 to 0.50/s. The recovery median returned near normal (67 versus 55 ms), and job throughput rose to 0.76/s, but recovery p95 remained 1051 ms. This demonstrates partial recovery, not return to baseline or stable capacity.

Completion counts are tagged **when jobs finish**, so recovery includes work submitted earlier. Queue and execution durations are attached to those completion samples. Recovery throughput uses its actual elapsed time including drain. The approval fixture serializes fresh review creation per target because approval generation changes after commits; broker contention is measured separately and is not a public API error. This constrains the workload's interpretation: it tests duplicate enqueue plus different-target concurrency, not arbitrary numbers of independently preapproved same-target revisions.

All Go processes, central telemetry services and local PostgreSQL share an AMD Ryzen 7 7840U development workstation with other activity. Short phases, fixture preparation and instrumentation can affect results. The measurements locate an observed latency limit for this run; they do not prove CPU, database lock or pool contention as the sole cause. A production capacity claim would require longer repeated runs on an isolated host.

## Verification scope

Both frontend production builds, shared TypeScript checking, all eight backend unit-test packages, Go vet, OTLP correlation/redaction tests and the schema-diff allocation regression passed. The full PostgreSQL integration suite passed after the changes in **428.159 seconds**; [verification records](evidence/bonuses/verification.json) retain the commands and actual results. The standalone admin binary test gives its cold Go build a separate deadline and uses an ephemeral metrics listener so optional instrumentation cannot conflict with another local process.

These bonuses preserve the existing approval and worker execution path. The literal-SQL logging limitation remains documented in [M2 integration](m2-integration.md).
