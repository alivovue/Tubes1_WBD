# Database schemas

`platform/schema.sql` defines the user application's metadata database; `admin/schema.sql` defines the administration database. These are the canonical schema files. Each directory embeds its SQL in a small Go package in the `schakker_schemas` module so tests and development tools use the same definitions without copying files or depending on the current working directory.

Provision these schemas explicitly into separate empty PostgreSQL databases with suitable ownership and runtime grants. Normal server and worker startup only validates existing schemas; neither application imports this module or applies DDL automatically. Initial accounts, restricted roles and secrets must also be provisioned explicitly. The local development tool handles its own isolated databases.

`platform/jobs_upgrade.sql` is the existing P2-to-P3 queue upgrade for an older M2 database. It is not an M1 import or a general schema upgrade; fresh databases use `platform/schema.sql`.

Sample target data and disposable database setup remain under `src/tests/fixtures/`. The development launcher is under `src/tools/cmd/devserver/`.
