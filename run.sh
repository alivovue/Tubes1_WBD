#!/usr/bin/env bash
set -euo pipefail
umask 077

project_directory="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "$project_directory/src"
if [[ -f .env ]]; then
    set -a
    source .env
    set +a
fi
if ! command -v go >/dev/null 2>&1; then
    printf '%s\n' 'Install Go before starting Schakker.' >&2
    exit 1
fi

ask() {
    local question="$1" default="$2" answer suffix
    if [[ ! -t 0 ]]; then
        reply="$default"
        return
    fi
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

state_directory="${SCHAKKER_STATE_DIR:-.schakker}"
mkdir -p "$state_directory"
chmod 700 "$state_directory"
state_directory="$(cd "$state_directory" && pwd)"
fixture_state=''
retain_fixtures=no

cleanup_fixtures() {
    if [[ -z "$fixture_state" || "$retain_fixtures" == yes ]]; then return; fi
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
    if [[ ! -f "$fixture_state/postgres/postmaster.pid" ]]; then
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
    local name="$1" package="$2"
    if go test -count=1 -timeout=5m -v "$package" 2>&1 | tee "$report_directory/$name.log"; then
        printf 'PASS %s\n' "$name" >> "$report_directory/summary.txt"
    else
        printf 'FAIL %s\n' "$name" >> "$report_directory/summary.txt"
        failures=$((failures + 1))
    fi
}
if [[ "$run_unit_tests" == yes ]]; then run_suite unit ./test/unit/...; fi
if [[ "$run_integration_tests" == yes ]]; then
    fixture_state="$(mktemp -d "$state_directory/fixtures.XXXXXXXX")"
    SCHAKKER_TEST_STATE="$fixture_state" run_suite integration ./test/integration/...
fi
if (( failures > 0 )); then
    printf 'Automated tests failed. The website was not started. Reports: %s\n' "$report_directory" >&2
    exit 1
fi

ask 'Keep and use the mock website database for the website run?' no
export SCHAKKER_USE_MOCK_WEBSITE="$reply"
ask 'Keep and use the mock managed database for the website run?' no
export SCHAKKER_USE_MOCK_MANAGED="$reply"
if [[ "$SCHAKKER_USE_MOCK_WEBSITE" == yes || "$SCHAKKER_USE_MOCK_MANAGED" == yes ]]; then
    if [[ -z "$fixture_state" && -f "$state_directory/kept-fixtures" ]]; then
        if IFS= read -r previous_fixtures < "$state_directory/kept-fixtures"; then
            case "$previous_fixtures" in
                "$state_directory"/fixtures.*)
                    if [[ -f "$previous_fixtures/.schakker-fixtures" ]]; then fixture_state="$previous_fixtures"; fi
                    ;;
            esac
        fi
    fi
    if [[ -z "$fixture_state" ]]; then fixture_state="$(mktemp -d "$state_directory/fixtures.XXXXXXXX")"; fi
    retain_fixtures=yes
    printf '%s\n' "$fixture_state" > "$state_directory/kept-fixtures"
    export SCHAKKER_FIXTURE_STATE="$fixture_state"
    printf 'Kept mock database files: %s\n' "$fixture_state"
else
    cleanup_fixtures
    fixture_state=''
    unset SCHAKKER_FIXTURE_STATE
fi

binary="$state_directory/server"
go build -o "$binary" .
if "$binary"; then exit 0; else status=$?; fi
if [[ "$status" != 3 ]]; then exit "$status"; fi
if [[ ! -t 0 ]]; then
    printf '%s\n' 'First startup needs a terminal to choose admin credentials, or set SCHAKKER_ADMIN_USERNAME and SCHAKKER_ADMIN_PASSWORD.' >&2
    exit 3
fi
printf '\n%s\n' 'First-time admin setup. Your password is stored as a hash in the website database.'
if [[ "$SCHAKKER_USE_MOCK_WEBSITE" == yes ]]; then
    printf '%s\n' 'The mock.writer and mock.reader accounts will use the password you choose here.'
fi
IFS= read -r -p 'Admin username [admin]: ' initial_username
initial_username="${initial_username:-admin}"
IFS= read -r -p 'Display name [Platform Admin]: ' initial_display
initial_display="${initial_display:-Platform Admin}"
while true; do
    IFS= read -r -s -p 'Admin password (12–72 bytes): ' initial_password
    printf '\n'
    IFS= read -r -s -p 'Confirm password: ' initial_confirmation
    printf '\n'
    password_bytes="$(printf '%s' "$initial_password" | wc -c)"
    if [[ "$initial_password" == "$initial_confirmation" ]] && (( password_bytes >= 12 && password_bytes <= 72 )); then break; fi
    printf '%s\n' 'Passwords must match and contain 12–72 bytes. Try again.'
done
export SCHAKKER_ADMIN_USERNAME="$initial_username"
export SCHAKKER_ADMIN_NAME="$initial_display"
export SCHAKKER_ADMIN_PASSWORD="$initial_password"
unset initial_password initial_confirmation
"$binary"
