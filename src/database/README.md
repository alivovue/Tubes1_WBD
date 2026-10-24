# Schakker platform database

PostgreSQL DDL for `schakker_platform`, the website's internal metadata database.
It contains all nine relations in the supplied schema, including the optional
`migration_drafts` table. All tables are created in the `public` schema.

| Table | Stores |
| --- | --- |
| `users` | Employee accounts and platform-admin flags |
| `access_groups` | Employee access groups |
| `user_groups` | Group memberships |
| `managed_databases` | Connection metadata for separate target databases |
| `group_database_permissions` | Group permissions for each target |
| `sessions` | Session lifecycle timestamps |
| `session_tokens` | Hashed tokens and rotation metadata |
| `migration_drafts` | Unfinished visual or SQL migrations |
| `migration_executions` | Execution attempts and successful version history |

This directory defines storage only. It does not provision employee-managed
databases, connect the Go server to PostgreSQL, seed accounts, or implement
login, access checks, token rotation, encryption, or migration execution.

## Create the database

Requires PostgreSQL 14 or newer and `psql`. Use a PostgreSQL role with `CREATEDB`
permission, replacing the host, port, and username below as needed. From the
repository root:

```sh
cd src
psql -X -h localhost -p 5432 -U postgres -d postgres -f database/init.sql
```

`init.sql` creates an empty UTF-8 `schakker_platform` database, connects to it,
and loads `schema.sql` relative to the script's own directory. Authentication
uses the normal PostgreSQL connection settings; no password is stored here.

This is an initial schema, intended to run once. If the database already exists,
`init.sql` stops without dropping or replacing it. If you have already created
an empty platform database, load only the schema:

```sh
psql -X -h localhost -p 5432 -U postgres -d schakker_platform \
  -v ON_ERROR_STOP=1 -f database/schema.sql
```

All tables, indexes, and comments are created in one transaction. A schema error
rolls back those changes. Database creation itself is a separate operation.

## Schema details

- Numeric IDs use `BIGINT` identity columns; session IDs default to random UUIDs.
- Usernames retain their spelling and have a unique index on `lower(username)`.
- Foreign keys and composite keys enforce the relationships in the supplied
  schema. No cascading deletes are defined.
- Check constraints restrict access levels, draft modes, execution statuses,
  port numbers, and JSON shapes. Migration versions must be positive.
- Each session can have at most one token with `replaced_at IS NULL`.
- Only successful executions have an `applied_version`; successful versions are
  unique per target database. Failed attempts can reuse an attempted version.
- Creation/activity timestamps have defaults where appropriate. `updated_at`
  defaults on insert; future update code must explicitly change it. Session
  expiry times and rotation/grace durations are supplied by future application
  code, with no policy durations chosen here.
- Password hashes, token hashes, and encrypted credentials must be supplied by
  future application code. The schema does not hash or encrypt values itself.

## Verify the schema

Use a separate, empty test database. From `src/`:

```sh
createdb -h localhost -p 5432 -U postgres schakker_platform_test
psql -X -h localhost -p 5432 -U postgres -d schakker_platform_test \
  -v ON_ERROR_STOP=1 -f database/schema.sql -f database/tests.sql
```

The tests exercise inserts, relationships, uniqueness, allowed values, session
rotation storage, and successful/failed migration version records. Test rows
and the temporary test helper are rolled back.
