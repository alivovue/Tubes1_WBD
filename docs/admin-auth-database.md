# Administrator authentication databases

The application uses two externally provisioned PostgreSQL databases, which may
share one PostgreSQL instance:

| Database | Contents | Application connection |
| --- | --- | --- |
| `schakker_platform` | Ordinary accounts, sessions/tokens, groups, memberships, grants, registered connections, migration drafts/history | Both servers, using separate restricted roles |
| `schakker_admin` | Administrator accounts and admin sessions/tokens | Admin server only |

The ordinary server never connects to the admin database. Admin identity comes
from the admin authentication database, and cannot be granted by editing an
ordinary account. Administrators who need managed-data access use independently
provisioned ordinary accounts with their own credentials, sessions and grants.

## Provisioning responsibility

Provision both databases and their required schemas, restricted PostgreSQL roles,
the initial administrator account with a bcrypt password hash, and the shared
32-byte master key outside the application. Neither server nor launcher includes
schema initialization, role creation, seed data, administrator bootstrap, data
upgrade commands, automatic PostgreSQL startup, or key generation.

Startup validates existing schemas and runtime roles without modifying data.
Missing configuration or schemas produce a startup error. The application does
not manage the lifecycle of external PostgreSQL processes. Account, session,
permission and migration writes requested through normal application features
continue to work.

## Runtime configuration

The ordinary server needs:

```dotenv
SCHAKKER_PLATFORM_DATABASE_URL='postgres://schakker_user:PRIVATE_PASSWORD@HOST/schakker_platform?sslmode=verify-full'
SCHAKKER_MASTER_KEY='SHARED_32_BYTE_KEY_ENCODED_AS_HEX_OR_BASE64'
PORT=3000
```

The admin server needs:

```dotenv
SCHAKKER_PLATFORM_DATABASE_URL='postgres://schakker_administration:PRIVATE_PASSWORD@HOST/schakker_platform?sslmode=verify-full'
SCHAKKER_ADMIN_DATABASE_URL='postgres://schakker_admin_auth:PRIVATE_PASSWORD@HOST/schakker_admin?sslmode=verify-full'
SCHAKKER_MASTER_KEY='SHARED_32_BYTE_KEY_ENCODED_AS_HEX_OR_BASE64'
ADMIN_PORT=3001
```

Both launchers load `src/.env`; admin additionally loads `src-admin/.env` as an
override. The platform URL may also use the legacy `SCHAKKER_DATABASE_URL` or
`DATABASE_URL` alias. No alias substitutes for the admin URL.

In development, an existing `master.key` may supply the key from `src/.schakker/`
or `SCHAKKER_STATE_DIR`; startup reads it without creating or changing files.
Production requires the key in `SCHAKKER_MASTER_KEY`. Keep the existing encryption
key when connecting to existing encrypted registrations.

## Expected schema and access

The platform connection expects the ordinary `users`, `sessions`, `session_tokens`,
`access_groups`, `user_groups`, `group_database_permissions`, `managed_databases`,
`migration_drafts` and `migration_executions` tables. The admin connection expects
only the independent `admin_users`, `admin_sessions` and `admin_session_tokens`
authentication tables.

Platform account/authentication queries exclude `is_legacy_admin` historical rows.
Admin authentication excludes `migration_pending` rows. Existing provenance and
historical attribution columns remain compatible with previously provisioned
schemas, including `created_by_admin_id` and `granted_by_admin_id`. The application
does not transform or upgrade old rows or schemas.

Use restricted, non-owner runtime roles without superuser, CREATEDB, CREATEROLE,
replication, BYPASSRLS or inherited elevated privileges. The ordinary role needs
platform reads, session and migration writes, connection namespace updates, and
`users.updated_at` update permission for PostgreSQL's login row lock. It must not
connect to the admin database or administer accounts/groups/grants.

The platform administration role manages ordinary accounts, groups, memberships,
grants, connection metadata, and affected ordinary session tokens. It needs no
ordinary password-hash reads or migration-SQL access. Its execution-history access
is limited to checking database IDs before repointing a connection. The admin-auth
role accesses admin account/session tables in the admin database only. Role and
ACL provisioning is external to the application.

## Account management and sessions

Existing administrators manage admin accounts at `/admin/administrators` and
ordinary accounts at `/admin/users`. Admin account management requires an existing
authenticated administrator; startup never creates the first account. At least one
administrator must remain active when changing account status through the UI.

| Domain | Cookie | Authentication tables |
| --- | --- | --- |
| Ordinary | `schakker_session` | `users`, `sessions`, `session_tokens` |
| Admin | `schakker_admin_session` | `admin_users`, `admin_sessions`, `admin_session_tokens` |

Matching usernames or numeric IDs do not make credentials or tokens interchangeable.
Login challenges have separate HttpOnly cookies; admin CSRF and rotation keys have
an independent derivation domain. Idle timeout, absolute expiry, rotation, grace,
logout and revocation continue to apply within each database.

## Testing

Database initialization, local PostgreSQL lifecycle helpers, fixture seeding,
bootstrap helpers and archived upgrade scenarios are retained exclusively under
`src/test/data/`. Test schemas are embedded by test fixture packages. Their package
paths are absent from both server dependency graphs.

Unit and integration tests retain isolated setup. Runtime integration tests verify
that missing schemas remain empty, bootstrap environment variables create no
accounts, existing provisioned accounts still authenticate, and startup creates no
local state or key files. Normal authorization, administration, session and SQL
migration coverage remains in place.

Launchers can run the tests before starting the application, then clean up the
temporary test cluster. They do not retain fixtures as application data.
