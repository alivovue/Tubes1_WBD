| Phase | Duration | API p50 / p95 (ms) | API req/s | API errors | Queue p95 (ms) | Migration p95 (ms) | Completed jobs | Job/s |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| normal | 10.00s | 70.75 / 140.78 | 34.20 | 0.00% | 287.00 | 4479.40 | 9 | 0.90 |
| load | 15.00s | 108.18 / 397.81 | 30.00 | 0.00% | 314.00 | 5248.60 | 12 | 0.80 |
| stress | 20.00s | 303.86 / 1081.84 | 23.05 | 0.00% | 311.40 | 10059.20 | 9 | 0.45 |
| recovery | 34.40s | 115.67 / 916.13 | 20.17 | 0.00% | 258.25 | 10042.70 | 22 | 0.64 |

Threshold results:
- queue_wait_ms{phase:load}: p(95)<120000: PASS
- api_requests{phase:stress}: count>=0: PASS
- api_requests{phase:recovery}: count>=0: PASS
- api_latency_ms{phase:load}: p(95)<10000: PASS
- migration_duration_ms{phase:load}: p(95)<120000: PASS
- api_requests{phase:normal}: count>=0: PASS
- queue_wait_ms{phase:stress}: p(95)<120000: PASS
- api_errors: rate<0.01: PASS
- api_latency_ms: p(95)<750: FAIL
- migration_duration_ms{phase:normal}: p(95)<120000: PASS
- api_errors{phase:load}: rate<1: PASS
- api_errors{phase:stress}: rate<1: PASS
- api_errors{phase:normal}: rate<1: PASS
- jobs_completed{phase:normal}: count>=0: PASS
- queue_wait_ms{phase:normal}: p(95)<120000: PASS
- jobs_completed{phase:load}: count>=0: PASS
- api_errors{phase:recovery}: rate<1: PASS
- jobs_completed{phase:recovery}: count>=0: PASS
- jobs_completed{phase:stress}: count>=0: PASS
- api_latency_ms{phase:normal}: p(95)<10000: PASS
- migration_duration_ms{phase:stress}: p(95)<120000: PASS
- migration_duration_ms{phase:recovery}: p(95)<120000: PASS
- job_failures: rate<0.01: PASS
- jobs_completed: count>0: PASS
- api_latency_ms{phase:stress}: p(95)<10000: PASS
- queue_wait_ms{phase:recovery}: p(95)<120000: PASS
- api_requests{phase:load}: count>=0: PASS
- api_latency_ms{phase:recovery}: p(95)<10000: PASS
