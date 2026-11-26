# Repository and application boundaries

All application, library, schema, test, tooling and operations code lives under `src/`. The repository root is the Go and npm workspace. `go.work` includes the two applications, shared backend, schemas, tests and tools. `package.json` includes the user frontend, admin frontend and shared UI package. The root `run.sh`, `run.bat` and `run.ps1` start the complete local development environment through implementations under `src/tools/launchers/`. Individual user, admin and worker launchers are kept in that folder for separately configured services.

| Directory | Responsibility |
| --- | --- |
| `src/apps/user/` | User API, migration worker and React frontend. |
| `src/apps/admin/` | Admin API, private authority API and React frontend. |
| `src/shared/backend/` | Common Go mechanisms without application credentials or database ownership. |
| `src/shared/frontend/` | Shared React session, login, page handling and UI components. |
| `src/schemas/platform/`, `src/schemas/admin/` | Canonical schemas for the two metadata databases. |
| `src/tests/` | Unit/integration/browser checks, disposable database support and sample data. |
| `src/tools/` | Explicit development provisioning, load fixtures and maintenance commands. |
| `src/ops/observability/` | Collector, Loki, Tempo, Prometheus and Grafana configuration. |

Each application retains its own `backend/`, `database/`, `frontend/`, `go.mod` and `.env.example`. Executables live under `cmd/server/`; the user application also owns `cmd/worker/`. Run binaries from their application directory so frontend files and relative state paths resolve correctly. Launchers set this directory automatically. Building with `GOWORK=off` uses explicit local module replacements and does not require the other application's source for a production build.

## Runtime and database ownership

The user server listens on 3000, the admin server on 3001 and the private admin authority on loopback 3002 by default. The migration worker is a separate process owned by the user application. Vite development ports are 5173 and 5174. Each SPA calls `/api` on its own server origin.

`schakker_admin` owns admin accounts/passwords/sessions, authoritative ordinary-user accounts/passwords, groups, membership, permission and encrypted target connection settings. `schakker_platform` owns ordinary-user sessions, migration definitions/revisions/reviews, jobs/attempts/history, receipt namespaces and local identity/database ID snapshots for foreign keys. Each server connects to its own metadata database. Ordinary-user credential and authorization checks go through the private admin API; authority outages fail protected requests. Local identity snapshots contain no authoritative password, admin flag or editable grants.

User and admin identities remain separate even when usernames or numeric IDs match. Their session cookies are `schakker_session` and `schakker_admin_session`, and their signing keys and runtime configuration are independent. Database ownership and service-to-service access restrictions still depend on deployment credentials, network configuration and the private authority contract.

Cookies use HttpOnly, SameSite=Lax and Secure on HTTPS/production. Session defaults remain a 30-minute idle timeout, eight-hour absolute lifetime, 15-minute rotation interval and 30-second grace period. Password/activation changes, permission reductions and connection changes atomically change the user's security version at the authority; older sessions fail their next protected request. Permission increases request token rotation at the next authentication. Changes to an ordinary user's permissions do not revoke admin sessions.

## Shared mechanisms

The shared backend provides PostgreSQL pool setup and SQL error classification (`database`), cryptographic/CSRF/rotation mechanisms (`auth`), HTTP login/session/logout handling (`authhttp`), SPA/static/API serving (`webapp`), HTTP helpers, configuration decoding and observability. Every pool is created from the owning application's supplied DSN. Shared helpers do not store global application connections or read the other application's credentials. Credential verification, authorization policies, DAO queries, transaction/locking behavior, schema validation and startup remain app-specific.

The shared frontend provides session loading/retry, login and refreshed CSRF handling, navigation layout, logout and page loading/submission. Each app supplies its own API client, routes, content and independent React session state.

## Schemas, tools and local state

The `schakker_schemas` module embeds the canonical SQL for explicit provisioning. Tests and development tools import it; production applications validate existing metadata without applying DDL. Sample target data and disposable cluster/role setup live under `src/tests/fixtures/`. Development commands live in their own `schakker_tools` module.

The Go module names (`tubes1_wbd`, `tubes1_wbd_admin`, `schakker_shared`, `schakker_tests`) are preserved across the folder move. Root workspace declarations, module replacement paths, launchers and browser fixtures use the current layout.

Local state, credentials, caches and verification output belong in ignored `.schakker/` directories. Routine launcher reports go to ignored `src/tests/output/`; selected historical evidence remains in `docs/evidence/` and `docs/qa/`. Historical evidence retains the source paths recorded when it was produced. Inactive CI templates are kept in `src/tools/ci-templates/` until explicitly activated.

For setup and runtime configuration see [integration and operation](m2-integration.md); for the current HTTP contract see [frontend API](m2-frontend-api.md).
