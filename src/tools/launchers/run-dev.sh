#!/usr/bin/env bash
set -euo pipefail
repository_directory="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../../.." && pwd)"
check=false
stop=false
skip_build=false
windows_arguments=()
for argument in "$@"; do
    case "$argument" in
        --check|-Check) check=true; windows_arguments+=(-Check) ;;
        --stop|-Stop) stop=true; windows_arguments+=(-Stop) ;;
        --skip-build|-SkipBuild) skip_build=true; windows_arguments+=(-SkipBuild) ;;
        *) printf '%s\n' 'Usage: bash ./run.sh [--check|--stop|--skip-build]' >&2; exit 2 ;;
    esac
done
case "$(uname -s)" in
    MINGW*|MSYS*|CYGWIN*)
        MSYS_NO_PATHCONV=1 exec powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "$(cygpath -w "$repository_directory/src/tools/launchers/run-dev.ps1")" "${windows_arguments[@]}"
        ;;
esac
cd "$repository_directory"
state="$PWD/.schakker/dev"
if $stop; then exec "$state/bin/dev-server" -root "$repository_directory" -stop; fi
if ! $skip_build; then
    if [[ ! -x node_modules/.bin/vite ]]; then npm ci --no-audit --no-fund; fi
    npm run build
    mkdir -p "$state/bin"
    GOWORK=off go -C src/apps/user build -o "$state/bin/user-server" ./cmd/server
    GOWORK=off go -C src/apps/admin build -o "$state/bin/admin-server" ./cmd/server
    GOWORK=off go -C src/apps/user build -o "$state/bin/migration-worker" ./cmd/worker
    GOWORK=off go -C src/tools build -o "$state/bin/dev-server" ./cmd/devserver
fi
arguments=(-root "$repository_directory")
if $check; then arguments+=(-check); fi
exec "$state/bin/dev-server" "${arguments[@]}"
