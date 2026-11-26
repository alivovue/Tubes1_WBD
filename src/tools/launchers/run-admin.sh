#!/usr/bin/env bash
set -euo pipefail
launchers_directory="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
exec bash "$launchers_directory/run.sh" --admin "$@"
