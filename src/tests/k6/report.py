#!/usr/bin/env python3
"""Render measured k6 per-phase data; never invent missing samples."""
import json
from pathlib import Path
import sys

data = json.loads(Path(sys.argv[1]).read_text())
metrics = data['metrics']
metadata = data['schakker']
print('| Phase | Duration | API p50 / p95 (ms) | API req/s | API errors | Queue p95 (ms) | Migration p95 (ms) | Completed jobs | Job/s |')
print('| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |')
def values(metric, phase):
    return metrics.get(f'{metric}{{phase:{phase}}}', {}).get('values', {})
def number(value):
    return 'n/a' if value is None else f'{value:.2f}'
for phase, duration in zip(metadata['phases'], metadata['stage_seconds']):
    if phase == 'recovery':
        duration = max(duration, data.get('state', {}).get('testRunDurationMs', 0) / 1000 - sum(metadata['stage_seconds'][:-1]))
    api = values('api_latency_ms', phase)
    requests = values('api_requests', phase).get('count', 0)
    completed = values('jobs_completed', phase).get('count', 0)
    error = values('api_errors', phase).get('rate')
    print(f"| {phase} | {duration:.2f}s | {number(api.get('p(50)'))} / {number(api.get('p(95)'))} | {requests/duration:.2f} | {number(None if error is None else 100*error)}% | {number(values('queue_wait_ms',phase).get('p(95)'))} | {number(values('migration_duration_ms',phase).get('p(95)'))} | {completed} | {completed/duration:.2f} |")
print('\nThreshold results:')
for name, metric in metrics.items():
    for expression, result in metric.get('thresholds', {}).items():
        print(f"- {name}: {expression}: {'PASS' if result['ok'] else 'FAIL'}")
