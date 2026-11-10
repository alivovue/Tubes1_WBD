#!/usr/bin/env bash
set -euo pipefail
umask 077

application_mode=user
case "${1:-}" in
    '') ;;
    --admin) application_mode=admin ;;
    *) printf '%s\n' 'Usage: bash ./run.sh [--admin]' >&2; exit 2 ;;
esac

project_directory="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "$project_directory/src"
if [[ -f .env ]]; then
    set -a
    source .env
    set +a
fi
if [[ "$application_mode" == admin && -f "$project_directory/src-admin/.env" ]]; then
    set -a
    source "$project_directory/src-admin/.env"
    set +a
fi
if [[ -n "${SCHAKKER_STATE_DIR:-}" && "$SCHAKKER_STATE_DIR" != /* ]]; then
    export SCHAKKER_STATE_DIR="$project_directory/src/$SCHAKKER_STATE_DIR"
fi
if ! command -v go >/dev/null 2>&1; then
    printf '%s\n' 'Install Go before starting Schakker.' >&2
    exit 1
fi
if ! command -v npm >/dev/null 2>&1; then
    printf '%s\n' 'Install Node.js 22.12+ (with npm) before starting Schakker.' >&2
    exit 1
fi
for frontend_directory in frontend ../src-admin/frontend; do
    (
        cd "$frontend_directory"
        npm ci --no-audit --no-fund
        npm run build
    )
done

ask() {
    local question="$1" default="$2" answer suffix
    if [[ ! -t 0 ]]; then reply="$default"; return; fi
    suffix='[y/N]'
    if [[ "$default" == yes ]]; then suffix='[Y/n]'; fi
    while true; do
        IFS= read -r -p "$question $suffix: " answer
        case "${answer,,}" in
            '') reply="$default"; return ;;
            y|yes) reply=yes; return ;;
            n|no) reply=no; return ;;
            *) printf '%s\n' 'Please answer yes or no.' ;;
        esac
    done
}
ask 'Run unit tests before launching the website?' yes
run_unit_tests="$reply"
ask 'Run PostgreSQL integration tests before launching the website?' yes
run_integration_tests="$reply"

# Only the selected tests own temporary databases; application startup never does.
fixture_state=''
cleanup_fixtures() {
    if [[ -z "$fixture_state" ]]; then return; fi
    if [[ ! -f "$fixture_state/.schakker-fixtures" ]]; then
        rmdir -- "$fixture_state" 2>/dev/null || true
        return
    fi
    if [[ -f "$fixture_state/postgres/postmaster.pid" ]]; then
        local directory control=''
        for directory in "${SCHAKKER_POSTGRES_BIN:-}" "$(pg_config --bindir 2>/dev/null || true)" /usr/lib/postgresql/*/bin; do
            if [[ -n "$directory" && -x "$directory/pg_ctl" ]]; then control="$directory/pg_ctl"; break; fi
        done
        if [[ -n "$control" ]]; then
            "$control" -D "$fixture_state/postgres" -m fast -w -t 15 stop >/dev/null 2>&1 || true
        fi
    fi
    if [[ ! -f "$fixture_state/postgres/postmaster.pid" && ! -f "$fixture_state/mariadb/server.pid" ]]; then
        rm -rf -- "$fixture_state"
    fi
}
trap cleanup_fixtures EXIT

report_directory=''
failures=0
if [[ "$run_unit_tests" == yes || "$run_integration_tests" == yes ]]; then
    mkdir -p test/output
    report_directory="$(mktemp -d "test/output/$(date -u +%Y%m%dT%H%M%SZ).XXXXXX")"
    printf 'Schakker automated tests\n' > "$report_directory/summary.txt"
    printf 'Test reports: %s\n' "$report_directory"
fi
run_suite() {
    local name="$1" package="$2" timeout=5m
    if [[ "$name" == integration ]]; then timeout=10m; fi
    if go test -count=1 -timeout="$timeout" -v "$package" 2>&1 | tee "$report_directory/$name.log"; then
        printf 'PASS %s\n' "$name" >> "$report_directory/summary.txt"
    else
        printf 'FAIL %s\n' "$name" >> "$report_directory/summary.txt"
        failures=$((failures + 1))
    fi
}
if [[ "$run_unit_tests" == yes ]]; then
    run_suite unit ./test/unit/...
    run_suite admin ../src-admin/backend/...
fi
if [[ "$run_integration_tests" == yes ]]; then
    fixture_state="$(mktemp -d "${TMPDIR:-/tmp}/schakker-tests.XXXXXXXX")"
    SCHAKKER_TEST_STATE="$fixture_state" run_suite integration ./test/integration/...
    cleanup_fixtures
    fixture_state=''
fi
if (( failures > 0 )); then
    printf 'Automated tests failed. The website was not started. Reports: %s\n' "$report_directory" >&2
    exit 1
fi

if [[ "$application_mode" == admin ]]; then
    binary="$project_directory/src-admin/bin/admin-server"
    mkdir -p "$project_directory/src-admin/bin"
    go build -o "$binary" ../src-admin
    cd "$project_directory/src-admin"
else
    binary="$project_directory/src/bin/user-server"
    mkdir -p "$project_directory/src/bin"
    go build -o "$binary" .
fi
"$binary"
