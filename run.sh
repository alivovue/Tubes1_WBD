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
application_directory="$project_directory/src"
if [[ "$application_mode" == admin ]]; then application_directory="$project_directory/src-admin"; fi
cd "$application_directory"
if [[ -f .env ]]; then
    set -a
    source .env
    set +a
fi
if [[ "$application_mode" == user && -n "${SCHAKKER_STATE_DIR:-}" && "$SCHAKKER_STATE_DIR" != /* ]]; then
    export SCHAKKER_STATE_DIR="$application_directory/$SCHAKKER_STATE_DIR"
fi
if ! command -v go >/dev/null 2>&1; then
    printf '%s\n' 'Install Go before starting Schakker.' >&2
    exit 1
fi
if ! command -v npm >/dev/null 2>&1; then
    printf '%s\n' 'Install Node.js 22.12+ (with npm) before starting Schakker.' >&2
    exit 1
fi
(
    cd "$project_directory"
    frontend_workspace=schakker-frontend
    if [[ "$application_mode" == admin ]]; then frontend_workspace=schakker-admin-frontend; fi
    npm ci --workspace="$frontend_workspace" --workspace=@schakker/ui --no-audit --no-fund
    npm run build --workspace="$frontend_workspace"
)

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
run_integration_tests=no
if [[ "$application_mode" == user ]]; then
    ask 'Run PostgreSQL integration tests before launching the website?' yes
    run_integration_tests="$reply"
fi

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
    local name="$1" package="$2" directory="${3:-$application_directory}" timeout=5m
    if [[ "$name" == integration ]]; then timeout=10m; fi
    if GOWORK=off go -C "$directory" test -count=1 -timeout="$timeout" -v "$package" 2>&1 | tee "$report_directory/$name.log"; then
        printf 'PASS %s\n' "$name" >> "$report_directory/summary.txt"
    else
        printf 'FAIL %s\n' "$name" >> "$report_directory/summary.txt"
        failures=$((failures + 1))
    fi
}
if [[ "$run_unit_tests" == yes ]]; then
    run_suite shared ./... "$project_directory/shared/backend"
    if [[ "$application_mode" == admin ]]; then
        run_suite unit ./...
    else
        run_suite unit ./test/unit/...
        run_suite authority ./backend/controlplane/...
    fi
fi
if [[ "$run_integration_tests" == yes ]]; then
    fixture_state="$(mktemp -d "${TMPDIR:-/tmp}/schakker-tests.XXXXXXXX")"
    SCHAKKER_TEST_STATE="$fixture_state" run_suite integration ./... "$project_directory/tests/integration"
    cleanup_fixtures
    fixture_state=''
fi
if (( failures > 0 )); then
    printf 'Automated tests failed. The website was not started. Reports: %s\n' "$report_directory" >&2
    exit 1
fi

mkdir -p "$application_directory/bin"
binary="$application_directory/bin/user-server"
if [[ "$application_mode" == admin ]]; then binary="$application_directory/bin/admin-server"; fi
GOWORK=off go build -o "$binary" .
"$binary"
