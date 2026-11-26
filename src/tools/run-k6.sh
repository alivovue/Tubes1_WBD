#!/usr/bin/env bash
set -euo pipefail
umask 077
repository_directory="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
if ! command -v k6 >/dev/null 2>&1; then printf '%s\n' 'Install k6 and put it on PATH.' >&2; exit 1; fi
report_directory="${SCHAKKER_K6_OUTPUT:-$(mktemp -d "${TMPDIR:-/tmp}/schakker-k6-report.XXXXXXXX")}"
mkdir -p "$report_directory"
report_directory="$(cd -- "$report_directory" && pwd)"
GOWORK=off go -C "$repository_directory/src/tools" build -o "$report_directory/loadfixture" ./cmd/loadfixture
"$report_directory/loadfixture" -manifest "$report_directory/fixture.json" -listen "${SCHAKKER_K6_LISTEN:-127.0.0.1:3300}" > "$report_directory/fixture.log" 2>&1 &
fixture_process=$!
cleanup() {
    kill -TERM "$fixture_process" 2>/dev/null || true
    wait "$fixture_process" 2>/dev/null || true
}
trap cleanup EXIT INT TERM
for ((attempt=0; attempt<120; attempt++)); do
    if [[ -s "$report_directory/fixture.json" ]]; then break; fi
    if ! kill -0 "$fixture_process" 2>/dev/null; then printf 'Fixture failed; inspect %s/fixture.log\n' "$report_directory" >&2; exit 1; fi
    sleep 1
done
if [[ ! -s "$report_directory/fixture.json" ]]; then printf '%s\n' 'Fixture startup timed out.' >&2; exit 1; fi
status=0
SCHAKKER_K6_FIXTURE="$report_directory/fixture.json" SCHAKKER_K6_SUMMARY="$report_directory/summary.json" k6 run "$repository_directory/src/tests/k6/stress.js" || status=$?
if [[ -f "$report_directory/summary.json" ]]; then
    python3 "$repository_directory/src/tests/k6/report.py" "$report_directory/summary.json" > "$report_directory/results.md"
fi
printf 'Reports: %s\n' "$report_directory"
exit "$status"
