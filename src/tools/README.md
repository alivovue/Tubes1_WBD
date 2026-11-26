# Development and maintenance tools

Run commands from the repository root:

```bash
# Build the local development launcher; the root run.sh/.bat also does this.
GOWORK=off go -C src/tools build -o "$PWD/.schakker/dev/bin/dev-server" ./cmd/devserver
# Compile every Go module for a CodeQL scan.
bash src/tools/codeql-build.sh
# Run the isolated k6 fixture and report its results; requires k6.
bash src/tools/run-k6.sh
```

`cmd/devserver` provisions isolated local databases and starts the real admin, user and worker binaries. `cmd/loadfixture` provisions disposable databases and services for k6. Both use canonical schemas from `src/schemas/` and database support/sample data from `src/tests/fixtures/`. They are separate from the production application modules.

The root `run.sh`, `run.bat` and `run.ps1` launch the complete local development environment: both frontends, admin backend, user backend, worker and local databases. They keep local state in `.schakker/dev/` and support a real end-to-end check via `bash run.sh --check` or `run.bat -Check`. Stop with Ctrl+C or `bash run.sh --stop` / `run.bat -Stop`.

All launcher implementations live in `launchers/`. For already provisioned databases, its `run.sh` / `run.bat` starts only the user service, `run-admin.sh` / `.bat` starts the admin service, and `run-worker.sh` / `.bat` starts the worker. These individual launchers use the selected application's `.env`. For example, from the repository root use `bash src/tools/launchers/run-worker.sh`.

`codeql-report.py` reads existing SARIF files and creates a review inventory. CI templates live under `ci-templates/`, outside `.github`, and remain inactive.
