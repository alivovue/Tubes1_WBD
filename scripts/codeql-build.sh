#!/usr/bin/env bash
set -euo pipefail
repository_directory="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
export GOWORK=off
# Go extraction observes module-local build invocations and reads their sources.
# The SARIF source coverage is checked separately; compiler cache hits are safe.
for module in shared/backend src src-admin; do
    # Run from the module directory: the CodeQL Go tracer does not extract
    # sources when the Go invocation starts with the global -C option.
    (cd "$repository_directory/src/$module" && go build -p=2 ./...)
done
# Compile tests and their support code; never start fixtures during SAST.
(cd "$repository_directory/src/tests" && go test -p=2 -run '^$' -exec=true ./...)
