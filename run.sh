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

state_directory="${SCHAKKER_STATE_DIR:-.schakker}"
mkdir -p "$state_directory"
chmod 700 "$state_directory"
binary="$state_directory/server"
go build -o "$binary" .

if "$binary"; then
    exit 0
else
    status=$?
fi

if [[ "$status" != 3 ]]; then
    exit "$status"
fi

if [[ ! -t 0 ]]; then
    printf '%s\n' 'First startup needs a terminal to choose admin credentials, or set SCHAKKER_ADMIN_USERNAME and SCHAKKER_ADMIN_PASSWORD.' >&2
    exit 3
fi

printf '\n%s\n' 'First-time admin setup. Your password is stored as a hash in the website database.'
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
    if [[ "$initial_password" == "$initial_confirmation" ]] && (( password_bytes >= 12 && password_bytes <= 72 )); then
        break
    fi
    printf '%s\n' 'Passwords must match and contain 12–72 bytes. Try again.'
done

export SCHAKKER_ADMIN_USERNAME="$initial_username"
export SCHAKKER_ADMIN_NAME="$initial_display"
export SCHAKKER_ADMIN_PASSWORD="$initial_password"
unset initial_password initial_confirmation
exec "$binary"
