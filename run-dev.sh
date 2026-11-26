#!/usr/bin/env bash
set -euo pipefail
repository_directory="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
exec bash "$repository_directory/src/tools/launchers/run-dev.sh" "$@"
