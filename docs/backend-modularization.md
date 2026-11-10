# Backend modularization and shared implementation

This refactor changes source organization and build-time dependencies. The administrator control plane, ordinary runtime database, private authenticated API, identities, cookies, keys, permissions and Milestone 1 engines retain their existing ownership and behavior. Startup still performs no initialization or data migration.

## Audit and extraction decisions

The initial audit found ordinary root backend files of 346 (`server.go`), 234 (`pages.go`), 160 (`actions.go`) and 133 (`sql_viewer.go`) lines. The admin equivalents were 233, 72 and 120 lines. These were preparing JSON for React rather than rendering Go templates; deleting their loaders/actions would have removed working APIs.

| Candidate | Decision |
| --- | --- |
| Token generation, hashing, HMAC signatures, rotation derivation, expiration minimum, login-CSRF signatures | Extract mechanisms to shared auth. Leave identity validation and repository operations in each app. |
| bcrypt hashing, verification, equal-cost missing-account hash | Extract cryptographic primitive. Administrator and ordinary account lookups remain in the control plane; user login continues using the private API. |
| JSON/form decoding, strict JSON parsing, response encoding, HEAD handling, origin checks, headers, scalar ID/offset parsing | Extract to shared HTTP/validation packages. Preserve error sentinels and wire responses. |
| Cookie construction | Share the security attributes; each app supplies its own cookie names and environment policy. |
| Existing-key hex/base64 decoding | Extract a pure parser. Environment names, key files, production settings and secrets remain app-owned. |
| Session configuration defaults and authorization/errors | Retain locally: small, explicit application policies rather than a shared security domain. |
| SQL adapters, explorer/migration services, control-plane DAO, private authority API | Retain in their owners. No database implementation is shared. |
| Repeated UI controls, tables, form submission, icons, formatting, navigation presentation | Extract to `@schakker/ui`; data context, routes, account rules and feature components stay local. |
| Frontend fetch/error/form collection | Extract transport mechanics; local clients retain `/api` routing and their distinct array-field rules. |
| Identical stylesheets | Keep one shared visual theme, preserving the entire existing cascade and media queries. No selector/feature-style purge or visual redesign. |
| Application layouts, login/session/page loading | Retain local route, navigation, content and authentication policy; the user app additionally owns migration-analysis and stale-review state. |
| Cross-application integration tests under `src/` | Move to a test-only workspace module outside both apps, so even test source in either app no longer imports the other app. |

The baseline Go suite passed (including integration). Both frontend builds passed. The ordinary frontend passed all 18 tests when rerun separately after an initial five-second timeout. The admin baseline repeatedly failed its group-access test (the `Analysts Employees` combobox did not become visible after clicking Edit access); the other five tests passed. Final results are recorded below separately from this baseline.

## Directory and dependency structure

```
shared/
  backend/                    module schakker_shared
    auth/                     token/password/CSRF/cookie primitives
    httpapi/                  identity-independent HTTP helpers
    config/                   supplied-value parsing
    validation/               invalid-input sentinel
  frontend/                   npm package @schakker/ui
    src/components/           controls, forms, tables, icons
    src/api/                  JSON transport and form-field collection
    src/hooks/                navigation presentation
    src/utils/                formatting and URLs
    src/styles.css            existing visual theme
src/backend/
  api/
    auth/ dashboard/ explorer/ sqlviewer/ migrations/
    selection/                ordinary database selection/permission filtering
    response/                 unchanged JSON envelope, errors and schema-diff presentation
    router.go                 public API route/auth middleware dispatch
  access/ dbmanagement/ migration/ controlplane/
  server.go                   SPA/assets/public compatibility routing and lifecycle
  startup.go                  dependencies and runtime initialization
src-admin/backend/
  api/
    auth/ users/ groups/ permissions/ connections/
    response/                 unchanged administration JSON envelope and errors
    router.go                 public route/auth/admin middleware dispatch
  access/ connectionconfig/ internalapi/
  server.go                   SPA/assets and lifecycle
  startup.go                  admin-owned configuration and dependencies
src/frontend/app/             ordinary routes and features; typed API/context adapters
src-admin/frontend/app/       administration routes and features; typed API/context adapters
tests/integration/            test-only Go module exercising both apps through HTTP
```

Dependency direction is `src -> shared` and `src-admin -> shared`. Shared code imports neither application. The test-only `schakker_integration` module can construct both applications and still exercises the real private HTTP contract. Each app's unit tests and production import graph are independent of the other app.

The previous page envelopes remain app-owned transport DTOs because their field names/default values are part of existing React API responses. Admin snapshots still include the group/connection selectors on account and connection views. They are not Go-template rendering state. The legacy ordinary HTML redirects and `/Products` JSON compatibility endpoint remain because they are working public behaviors. No Go HTML-template renderer exists in the tracked backend.

## Backend moves and removals

- Ordinary `pages.go`: dashboard history, explorer loading, migration loading, and database selection move to their feature packages.
- Ordinary `actions.go`: migration actions move to `api/migrations`; strict JSON parsing moves to shared HTTP helpers.
- Ordinary `sql_viewer.go`: moves to `api/sqlviewer` with existing read-query validation and response construction.
- Ordinary `schema_diff_view.go`: moves to `api/response/schema_diff.go`; presentation tests use its new Go package path. JSON output is unchanged.
- Admin `pages.go`: group/snapshot presentation moves to `api/groups`, administrator listing to `api/users`. The unreachable POST editor-recovery branch is removed: public POST handlers always return an action response before snapshot loading.
- Admin `actions.go`: account, group, permission and registration handlers move to their respective feature packages.
- Both app-local `backend/httpapi/` copies are removed: pure helpers are shared, authentication endpoints live in each app's `api/auth`.
- Both duplicated frontend component/API implementations, icon files and stylesheets are replaced by shared exports. Local forms remain thin adapters to app-owned data contexts.
- The admin favicon referenced the ordinary frontend source despite an identical admin-owned logo already existing. It now uses its own `/public/sc%20logo.png`, preserving the icon and removing that source/asset dependency.

Session rotation persistence, CSRF identity validity checks, session configuration defaults, app-specific error mapping, connection encryption, route policy, and database roles remain separate intentionally. Sharing their small policy/repository-specific sections would couple the security domains or require a generic service framework.

## Module resolution and deployment

`go.work` includes `src`, `src-admin`, `shared/backend`, and `tests/integration`. Each application requires `schakker_shared v0.0.0` and resolves it with `replace schakker_shared => ../shared/backend`; the integration module has explicit local replacements for the three implementation modules. No app replaces or requires the other app.

An independent source build therefore needs **the selected app plus `shared/backend` in the documented sibling layout**, not the other app or `go.work`:

```bash
cd src-admin                 # or src
GOWORK=off go build -o bin/server .
GOWORK=off go test ./...
```

For deployment, compile with that source subset and copy only the resulting binary, the selected app's `frontend/dist` and its `frontend/public` assets, and its independently supplied configuration. Shared Go/React source, the other app and workspace files are not runtime dependencies. No secrets or mutable runtime configuration are shared. Source error messages now direct frontend installation/build commands to the repository root; public response contracts are unchanged.

Successful integration startup tests use isolated application roots and their own provisioned database roles; the ordinary startup test logs in through the private HTTP API and verifies only an ordinary session is created. The admin binary test builds with `GOWORK=off` and runs without sibling user/shared source directories. A source checkout missing shared code cannot build until that build-time dependency is included; this is explicit rather than relying on production workspace discovery. For source archives without Git metadata, add `-buildvcs=false` to `go build` to disable VCS stamping.

## Frontend installation and builds

The root `package.json` defines npm workspaces for both frontend projects and `shared/frontend`. One root lockfile preserves the existing pinned package versions and transitive lock entries. `@schakker/ui` declares React/ReactDOM peers; both apps pin the same versions and Vite deduplicates them. Application-specific lockfiles and generated duplicate React installations are removed.

```bash
npm ci
npm run build:user
npm run build:admin
npm run typecheck:shared
npm test                     # all workspace tests, sequentially
```

Existing `npm run build`, `npm test` and `npm run dev` scripts can still be invoked from either frontend after the root workspace installation. Each has its own entry point, Vite port, routes, public API client and production bundle. To build only one application from a reduced checkout, include its workspace manifest/source, `shared/frontend`, and the root workspace manifest/lockfile, then select the app and shared package explicitly:

```bash
# Admin; substitute schakker-frontend and build:user for the ordinary app.
npm ci --workspace=schakker-admin-frontend --workspace=@schakker/ui
npm run build:admin
```

Both reduced-checkout dependency installations and frontend builds were verified without the other app present. The user subset used `--ignore-scripts` during installation because the sandbox blocked esbuild’s install-time version check; its production build succeeded, and the full root install and admin subset install also succeeded with install scripts enabled. The root launchers perform workspace installation and build only the selected app.

## Verification

Executed checks:

- The complete Go command below passed, including ordinary/admin unit tests, shared primitives, private-API authorization, session rotation, query plans, SQL viewer, PostgreSQL/MariaDB fixtures and migration/cutover regressions. Integration completed in 244.784 seconds. After the final cleanup, startup/group integration checks passed again in 51.522 seconds, including the new ordinary startup/login check; ordinary/shared unit checks also passed.
- Both Vite production builds and `npm run typecheck:shared` passed. The admin build was repeated after correcting its favicon path.
- Both Go binaries built with `GOWORK=off` from separate source subsets containing only that app and `shared/backend` (`-buildvcs=false` for the source archive). Each frontend installed and built from its corresponding reduced checkout with root workspace selection.
- Actual `go list -deps -test` graphs covered 305 ordinary, 283 admin and 231 shared packages with no forbidden imports. TypeScript resolved-file graphs included shared source and excluded the other app for both frontends.
- All 215 original npm dependency lock records were preserved. React and ReactDOM resolve to one physical installation; both apps and the shared package use 19.2.0. Shared CSS and SVG icons match the original assets byte for byte, including responsive styles.
- `bash -n run.sh run-admin.sh`, shared frontend formatting checks and `git diff --check` passed. PowerShell execution was not available on this Linux host.

Frontend timing limitation: the ordinary suite passed 18/18 separately; admin passed 6/6 and shared transport/form tests passed 5/5. Combined default runs intermittently failed existing frontend tests: a five-second navigation timeout, a query-plan loading assertion, or the admin group-editor assertion recorded in the baseline. No existing frontend tests were skipped or weakened. The final sequential-file run passed all **29 tests** (ordinary 18, admin 6, shared 5) using the command below. Default `npm test` timing remains intermittent on this host; the refactor does not claim to fix that baseline issue.

```bash
npm run test --workspaces --if-present -- --no-file-parallelism --testTimeout=20000
```

Recommended complete checks from the repository root:

```bash
go test -count=1 -timeout=10m ./src/... ./src-admin/... ./shared/backend/... ./tests/integration/...
npm run build
npm run typecheck:shared
npm test
```
