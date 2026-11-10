# Independent Milestone 1 applications

`src-admin/` is a standalone control plane. `src/` is the ordinary database-management application. The public applications have independent Go modules, React projects, dependencies, assets, configuration, session cookies, and database connections. There is no shared application package or runtime directory. `go.work` is only a development/test convenience; either server builds with `GOWORK=off` from its own directory.

```
src-admin/
  main.go                    public and private listeners
  backend/
    access/                  admin authentication, accounts, groups, permissions
    connectionconfig/        metadata validation and credential encryption
    httpapi/                 admin-only session/CSRF/JSON helpers
    internalapi/             authenticated ordinary-user authority API
    startup.go               admin database and owned configuration
  database/{dao,setup}/       control-plane persistence; read-only startup validation
  frontend/{app,public}/      independent administration SPA and assets
  test/unit/                 independent security, session, DAO and registration tests
src/
  main.go
  backend/
    access/                  ordinary sessions; synchronous authority checks
    controlplane/            private HTTP client
    dbmanagement/            existing explorer, SELECT viewer and execution plans
    migration/               existing migration services, engines and history
  database/{dao,platform,setup}/  runtime persistence and read-only startup validation
  frontend/{app,public}/      independent ordinary SPA and assets
  test/                      ordinary tests, cross-application contract tests and fixtures
```

## Database ownership

| Owner | Authoritative data |
| --- | --- |
| `schakker_admin` | `admin_users`, `admin_sessions`, `admin_session_tokens`; ordinary `users` with bcrypt hashes and security versions; `access_groups`, `user_groups`, `group_database_permissions`, `managed_databases` with encrypted credentials and administrative provenance |
| `schakker_platform` | ordinary `sessions`, `session_tokens`, migration drafts/executions/history, receipt namespaces; identity/display snapshots in `users` and registered database IDs in `managed_databases` solely for local foreign keys |

The platform `users` table has no password or administrator flag. Its snapshots are never consulted to authenticate or authorize a request. Old historical actors, including previously scrubbed admin identities, remain as inactive identity snapshots so history keeps its original IDs. Administrator credentials exist only in `admin_users`, which is a separate identity domain even when a username or numeric ID matches an ordinary user.

The applications require externally provisioned schemas, restricted PostgreSQL roles, an initial administrator, encryption/signing keys, and a private service token. Startup never creates databases, schemas, roles, accounts, keys, or seed data. See [schema contracts](application-databases.md) and the [existing-installation cutover](control-plane-cutover.md). Historical schema/upgrade helpers remain under `src/test/data/` solely for regression tests.

## Private API v1

Only the separate admin internal listener serves these endpoints. The public admin router and both React apps have no route or proxy to them. Requests require `Authorization: Bearer <SCHAKKER_INTERNAL_TOKEN>` (at least 32 bytes); no browser cookies, Origin-bearing requests, Fetch Metadata browser requests, CORS, redirects, or public login sessions are accepted as service authority.

| Method and route | Request / response |
| --- | --- |
| `POST /internal/v1/authenticate-user` | `{Username, Password}`; returns `{ID, Username, DisplayName, Active, Version, Rotation}` for an ordinary account only |
| `GET /internal/v1/users/{id}` | Current ordinary identity, active status, revocation version and rotation version |
| `GET /internal/v1/users/{id}/databases` | Active accessible database IDs, names, DBMS and effective permission; no connection secret |
| `GET /internal/v1/users/{id}/databases/{db}/permission` | `{Permission: "" / "view_only" / "view_edit"}` calculated across all current memberships; highest grant wins |
| `GET /internal/v1/users/{id}/databases/{db}/connection?required=view_only\|view_edit` | Current permitted connection metadata, username and decrypted target password; available only after checking that ordinary user's effective permission |

Admin accounts, admin sessions, admin password hashes, and ordinary password hashes are never returned. Connection passwords cross only the private service transport, are kept in user-backend memory, and are excluded from public JSON. Admin storage uses AES-256-GCM with the existing `schakker/target-password/v1` format. Both sides use bounded request bodies and deadlines; private responses are `no-store` and errors omit driver details and secrets.

Use verified HTTPS for production or remote calls. The admin process requires a certificate/key in production or on a non-loopback internal address; the user client rejects HTTP in production and permits development HTTP only to literal loopback IPs. Supply a trusted certificate for the exact configured hostname/IP; the client never disables certificate verification. Restrict the internal port to the user server with deployment networking/firewall rules. Do not reverse-proxy `/internal/` onto the public origin. The loopback development listener defaults to `127.0.0.1:3002`.

## Consistency and availability

Authentication, active-account checks, effective permissions, and connections are fetched synchronously without an authorization cache. An unavailable or malformed authority response fails closed; local snapshots cannot authorize operations. The control plane is therefore required for user login and protected requests. Existing migration execution continues to recheck permissions before statements and commit as before. A request already authorized may complete while a concurrent revocation commits; subsequent checks observe the committed revocation. No distributed transaction is implied.

Ordinary sessions store the authority's `auth_version` and `rotation_version`. Password changes, deactivation, group removals, permission reductions and connection edits/disables increment affected users' revocation versions atomically with metadata changes. The user app rejects older sessions on the next request. Privilege increases increment a rotation version, causing token replacement on each affected session's next authentication. Session expiration, bounded grace handoff and absolute lifetime remain enforced locally. A failed runtime token rotation leaves the authority's committed change intact and retries on a later request; it does not require a cross-database rollback.

## Configuration and independent startup

Copy and fill in each application's own `.env.example`. The launcher reads only the selected application's `.env`; direct Go binaries consume environment variables and do not source files.

| Admin only | Meaning |
| --- | --- |
| `SCHAKKER_ADMIN_DATABASE_URL` | Restricted connection to `schakker_admin` only |
| `SCHAKKER_ADMIN_MASTER_KEY` | Existing 32-byte key, hex/base64; no fallback to the user key or filesystem |
| `SCHAKKER_ADMIN_ENV` | `production` enables secure cookies and production TLS requirements |
| `SCHAKKER_ADMIN_PORT` | Public port, default `3001` |
| `SCHAKKER_ADMIN_INTERNAL_ADDR` | Private listen address, default `127.0.0.1:3002` |
| `SCHAKKER_ADMIN_INTERNAL_TLS_CERT`, `SCHAKKER_ADMIN_INTERNAL_TLS_KEY` | Certificate and key for internal HTTPS |
| `SCHAKKER_INTERNAL_TOKEN` | Provisioned private service credential shared with the user server |

| User only | Meaning |
| --- | --- |
| `SCHAKKER_PLATFORM_DATABASE_URL` | Restricted connection to `schakker_platform` only; legacy `SCHAKKER_DATABASE_URL` / `DATABASE_URL` aliases still work |
| `SCHAKKER_MASTER_KEY` | Existing user-session and review-signing key |
| `SCHAKKER_ENV`, `PORT` | User deployment mode and public port (default `3000`) |
| `SCHAKKER_CONTROL_PLANE_URL` | Verified internal API origin, e.g. `https://authority.internal:3002` |
| `SCHAKKER_INTERNAL_TOKEN` | Same provisioned private service credential |
| `SCHAKKER_STATE_DIR` | Optional user-owned directory to read an existing `master.key` in development |

From each application directory, build its own frontend with `npm ci` and `npm run build` in `frontend/`, then run `GOWORK=off go build -o bin/server .` and execute that binary from the application directory with its environment supplied. Start admin before ordinary users need authentication/authorization. Root wrappers remain `bash ./run.sh --admin` and `bash ./run.sh`, or `run-admin.bat` / `run.bat` on Windows. Each wrapper builds only its selected frontend and server. Vite development ports remain `5174` (admin) and `5173` (user), proxying only their respective public API/static paths.

Admin pages remain `/admin/users`, `/admin/administrators`, `/admin/groups`, `/admin/connections`, with `/admin/access` as the existing group-page alias. Ordinary explorer, SQL viewer, query plans and migration pages retain their routes and UI. No Milestone 2 feature is added.

## Connection registration and operational limits

Registration validates and encrypts configuration without opening a target database. The former admin connectivity-test action is removed from UI and backend. The admin build contains no managed-database adapter, catalog reader, SQL executor, query-plan or migration engine. Authorized ordinary operations establish and validate target connections.

An existing registration's DBMS, host, port and database name are immutable, even before its first migration. The independent admin cannot inspect platform history to decide whether retargeting is safe. Correct a target identity by disabling the old registration and registering a distinct target; credentials, TLS settings, display name and active status remain editable. Duplicate targets, including disabled registrations and hostname aliases, remain rejected. No destructive registration deletion is exposed.

Uncertain non-atomic migration outcomes continue to require operator recovery. The former application action required an administrator with ordinary database-edit privileges, a combination the separated identity domains do not grant; the action remains denied. No recovery engine or rollback/forward workflow is redesigned.

## Verification

Run `GOWORK=off go test ./...` from `src-admin/` after building its frontend. From the repository workspace, run `go test ./src/test/unit/... ./src/backend/controlplane/...` and `go test -timeout=10m ./src/test/integration/...`. The cross-application integration harness imports both implementations only for fixture construction and verifies communication through the real private HTTP API. Neither production dependency graph imports the other module. Integration tests include an isolated admin binary launched without a sibling `src/` directory and with workspace support disabled.

Run `npm test` independently in both frontend directories. Database creation, historical upgrade simulations, role provisioning and seed accounts exist only in test support.
