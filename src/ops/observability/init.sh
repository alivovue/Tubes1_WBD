#!/usr/bin/env bash
set -euo pipefail
umask 077
observability_directory="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
mkdir -p "$observability_directory/secrets"
if [[ ! -s "$observability_directory/secrets/grafana_password" ]]; then
    python3 -c 'import secrets; print(secrets.token_urlsafe(32))' > "$observability_directory/secrets/grafana_password"
fi
printf '%s\n' 'Grafana credential prepared in observability/secrets/grafana_password.'
printf '%s\n' 'Start: docker compose -f src/ops/observability/compose.yaml up -d'
