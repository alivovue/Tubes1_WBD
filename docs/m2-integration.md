# M2 integration and operation

The merged application connects both React SPAs to the Go authority, workflow, durable queue and PostgreSQL worker. API projections translate persistence records into the client resource contract without letting browser flags or submitted SQL authorize execution.

## Run locally without team credentials

For a working local development environment, run this from the repository root in PowerShell:

```powershell
.\run-dev.bat
```

Git Bash on Windows can use `bash ./run-dev.sh`; it delegates to the same PowerShell launcher. Linux/WSL uses `bash ./run-dev.sh` with Go, Node/npm and PostgreSQL server tools installed in that environment. Windows does not require WSL. Shell scripts use LF line endings, enforced by `.gitattributes`.

The development launcher builds both SPAs and the actual admin, user API and worker binaries. It creates a private PostgreSQL cluster on an available loopback port, separate admin/platform/target databases, restricted application roles, persistent random keys and an initial administrator. It registers a demo target with `employees` and `demo_counters` tables and creates two access groups with separate editor and View Only leaders. Then it runs all three application processes. Public development listeners bind to `127.0.0.1`; the application ports are 3000 and 3001, authority 3002, and metrics 9464–9466.

Open:

- User application: http://localhost:3000 — username `dev.editor`.
- Administrator: http://localhost:3001 — username `dev.admin`.
- View Only approval leader: http://localhost:3000 — username `dev.reader`.

The generated password for these local accounts is stored privately in `src/.schakker/dev/credentials.txt`. Read it with:

```powershell
notepad .\src\.schakker\dev\credentials.txt
```

Use the same hostname consistently during a session: `localhost` and `127.0.0.1` have separate cookies. The local data, keys and history persist under the Git-ignored `src/.schakker/dev/` directory. Keep this directory to retain the same database and encryption keys on restart. Service activity is in `admin.log`, `user.log`, `worker.log` and `setup.log` in that directory. The private cluster is stopped with the applications; the installed Windows PostgreSQL service is separate.

Press Ctrl+C in the launcher terminal to stop, or use another terminal:

```powershell
.\run-dev.bat -Stop
```

Git Bash also accepts `bash ./run-dev.sh --stop`. Use `-SkipBuild` on Windows (or `--skip-build` in Bash) to reuse the last compiled binaries. Rebuild after source changes. `-Check` / `--check` starts the real services, authenticates all three accounts, submits a reversible migration, collects both leader approvals, waits for the worker commit, then approves and executes rollback and stops. Check history remains in the local development database.

This mode uses its own private configuration and leaves the existing application `.env` files and team databases alone. To connect to team databases, use the standard three-process launchers below with the actual team URLs, original keys and shared service token. Copying `.env.example` alone leaves the required values empty. Standard launchers now detect an empty database URL before installing/building and point to the local launcher.

Verified on Windows on 22 November 2026: PowerShell and Git Bash launch paths; restart with persistent accounts/data; stop command; all eight backend unit-test packages; five focused real PostgreSQL integration tests for startup/schema/SPA migration/batching; `go vet` for the development command; and real browser admin/editor login with the actual Groups and migration workspace pages. A real two-leader approval → enqueue → worker commit → approved rollback completed on the local cluster. The browser pages loaded without runtime exceptions. Linux/WSL was not executed in this Windows verification.

## Start all three processes

Provision two separate application databases, restricted runtime roles, an initial administrator, existing master keys and a shared internal service token outside the application. Use `src/src/.env.example` and `src/src-admin/.env.example`; both `.env` files are ignored by Git. The complete fresh schemas are `src/tests/fixtures/website/schema.sql` and `src/tests/fixtures/admin/schema.sql`. These scripts create tables and should only be applied to empty application databases with suitable ownership/grants. They do not bootstrap the initial administrator.

Existing databases need an explicit, reviewed upgrade. `src/src/database/dao/jobs_upgrade.sql` only upgrades the P2 queue schema to P3's columns; it is not an M1-to-M2 migration. Startup and launchers validate externally provisioned schemas and do not modify them.

From the repository root, run each command in a separate terminal:

```bash
bash ./run-admin.sh
bash ./run.sh
bash ./run-worker.sh
```

Windows uses `run-admin.bat`, `run.bat`, `run-worker.bat`, or `run.ps1 -Admin`, `run.ps1`, `run.ps1 -Worker`. The worker loads the user application's `.env`, builds `src/src/cmd/worker`, and runs from the user application directory. It does not build the frontend or start a web listener. A direct binary reads exported environment variables only.

The admin private authority defaults to `127.0.0.1:3002`; public apps default to 3000/3001. Production authority uses trusted HTTPS; loopback HTTP is permitted for development. The worker needs the same platform URL, signing key, control-plane URL and internal token as the user API. Optional settings: concurrency 1–32 (default 2), lease 3–300 seconds (default 30), job timeout 10–86400 seconds (default 1800).

## Demonstrate a migration

1. Register a disposable PostgreSQL connection as administrator. Assign access groups and active group-member leaders. Include a View Only group to verify its leader participates.
2. Log in as an editor, open Migration workflow, select the target, and save reversible SQL up/down. The UI can generate the supported BIGINT increment batch template.
3. Freeze and submit the revision. Log in as each assigned leader and record a separate decision for every group.
4. As editor, enqueue the approved review. The API returns a durable job ID before any target SQL runs. Without a worker the job stays pending.
5. Watch Jobs for attempt/progress state, then inspect Committed history. Request rollback of the last active reversible event, approve it and enqueue. Forward requires another review and reapplies only the selected original revision.

Database-wide locks, leases and target receipts prevent concurrent duplicate commits. Independent targets can run concurrently. Failed attempts remain in operational activity; only verified commits advance the active stack/history. Full transport details are in [m2-frontend-api.md](m2-frontend-api.md).

## Partial work and recovery

For failed work, inspect classified errors and progress in Jobs. Safe retry keeps the same job and resumes after committed target checkpoints. An uncertain result must reconcile its target receipt before executing anything else. Only the original requester retries ordinary failures; another authorized editor can request receipt-only reconciliation after access changes.

For a paused partial batch, request cancellation review from its job detail. Every required leader must approve again. Enqueue the cancellation review; the worker compensates only committed batches in reverse order. A compensation conflict preserves the remaining progress and blocks unsafe continuation. Fix the external cause and use the supported retry/fresh-review flow. Do not clear leases, checkpoints, blocked state or receipts by hand to bypass an unresolved outcome.

## Verification

From the repository root:

```bash
npm --prefix src run build
npm --prefix src run typecheck:shared
go -C src/tests test -count=1 -timeout=12m ./...
# Requires a local Chromium browser; synthetic API fixtures, no database writes:
npm --prefix src run verify:person1
```

Set `TMPDIR` to a spacious writable directory when the root disk is nearly full. Go integration fixtures are isolated temporary databases and are removed after the suite. `SCHAKKER_POSTGRES_BIN` selects local PostgreSQL server tools; MariaDB tools are optional. Browser evidence defaults to `docs/qa/person1`; `SCHAKKER_QA_OUTPUT` can redirect new evidence to a temporary directory.

`m2_resources_test.go` follows the actual SPA payloads through real approval, enqueue, worker, rollback, forward and history pagination. Existing worker tests cover interrupted/lost-ack commits, leases/concurrency, checkpoint resume, safe/unsafe compensation and separate-database execution. `groups_page_test.go` exercises the SPA leader action. Browser tests cover the UI and mobile layouts against synthetic fixtures; they complement the real Go tests.

## Integration verification — 16 November 2026

- Both production frontend builds and the shared TypeScript check passed.
- User server, admin server and dedicated worker binaries built independently with `GOWORK=off`.
- All eight backend unit-test packages passed; the complete integration suite passed (495 seconds).
- Focused real API tests passed for definition/revision separation, all-group approval, idempotency headers, worker execution, UUID rollback/forward, history filtering/pagination, batch-template integrity, leader assignment and partial-batch cancellation.
- Go vet, Bash launcher syntax and `git diff --check` passed.
- All 20 Chromium UI checks passed, including SQL analysis → approval draft and desktop/mobile layouts. Generated evidence is in `/home/audric/go-tmp/schakker-integration/browser/` for this verification run; the original tracked screenshots were preserved.
- Windows launchers were reviewed but not executed on this Linux host. This integration checkpoint preceded the bonus work; see [M2 bonuses](m2-bonuses.md) for the subsequent real stack, k6 and CodeQL results.

## Remaining scope limits

- Existing M1 synchronous target history is not automatically imported into the reviewed active stack; the backend blocks starting an M2 chain over unresolved/committed M1 history. An explicit legacy-history/schema upgrade is still required for that transition.
- M2 execution is PostgreSQL only. MariaDB's existing viewer/analysis code is retained. New migrations execute only through immutable definitions, leader approval, the durable queue and the worker. The synchronous execution service, signed analysis review, legacy reconciliation service/button, and browser `execute`/`resolve`/`reconcile` actions have been removed; retired actions return 400 as unsupported actions. Execution recovery uses M2 job retry and worker receipt/checkpoint reconciliation.
- Existing M1 history remains readable. Tests for existing history seed historical records directly; transaction and receipt-protection tests execute approved M2 jobs through the worker.
- The batch template updates one BIGINT column by a fixed nonzero integer using a BIGINT primary key. Arbitrary batch SQL and unsupported table shapes are rejected.
- Submitted/frozen revisions are inspected or copied into new drafts in the UI. Original executed SQL is preserved.
- JSON logs and stdout OpenTelemetry traces are wired. SQL/parameter values are redacted and SQL hashes retained; this is not evidence of full literal-SQL logging compliance.
- Collector/Prometheus/Grafana, k6 and CodeQL bonus automation and measured evidence are now provided in [M2 bonuses](m2-bonuses.md). The existing mandatory large-dataset batch benchmark evidence is under `src/tests/fixtures/postgre/p3-batch-results.json`; running that optional benchmark requires `SCHAKKER_RUN_BATCH_BENCHMARK=1` and is separate from both the normal suite and the smaller k6 fixture.

This integration is not a sign-off on every item in the external implementation proposal. The limitations above remain visible so further milestone work can be scoped explicitly.
