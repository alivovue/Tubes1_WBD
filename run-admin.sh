#!/usr/bin/env bash
set -euo pipefail
project_directory="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
exec bash "$project_directory/run.sh" --admin
