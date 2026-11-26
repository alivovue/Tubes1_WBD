# Development and maintenance tools

Run commands from the repository root:

```bash
# Build the local development launcher; run-dev.sh/.bat also does this.
GOWORK=off go -C tools build -o "$PWD/.schakker/dev/bin/dev-server" ./cmd/devserver
# Compile every Go module for a CodeQL scan.
bash src/tools/codeql-build.sh
# Run the isolated k6 fixture and report its results; requires k6.
bash src/tools/run-k6.sh
```

`cmd/devserver` provisions isolated local databases and starts the real admin, user and worker binaries. `cmd/loadfixture` provisions disposable databases and services for k6. Both use canonical schemas from `src/schemas/` and database support/sample data from `src/tests/fixtures/`. They are separate from the production application modules.

The root `run-dev` launchers build both frontends and all development binaries, keep local state in `.schakker/dev/`, and support a real end-to-end check via `bash run-dev.sh --check` or `run-dev.bat -Check`. The root `run`, `run-admin` and `run-worker` launchers use the selected application's `.env` for already provisioned databases.

`codeql-report.py` reads existing SARIF files and creates a review inventory. CI templates live under `ci-templates/`, outside `.github`, and remain inactive.
