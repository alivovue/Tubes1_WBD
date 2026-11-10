# Existing-installation control-plane cutover

This is an explicit operator procedure, with both applications stopped. It is not a startup hook or a shipped migration command. It transfers the previous split-authentication installation (ordinary accounts and access metadata in `schakker_platform`, administrator credentials in `schakker_admin`) to the independent control plane. Database owner connections are used only by the operator. The applications continue using restricted roles.

If the source still has `users.is_platform_admin`, first complete the earlier administrator-authentication separation with an installation-specific, reviewed owner procedure: move only those admin credentials into `admin_users` preserving their IDs/provenance; revoke their ordinary sessions; remove their memberships; scrub their source hashes and mark them inactive. The archived examples under `src/test/data/adminmigration/` illustrate that historical format for tests only. Do not import such identities into the ordinary control-plane `users` table. The procedure below starts from the resulting `is_legacy_admin` schema. No automatic legacy upgrade is included in either server.

## 1. Freeze and protect the source

Stop both old servers and all writers, including old replicas/jobs. Take complete, restorable backups of **both** databases with `pg_dump --format=custom`, and preserve the old master key. Record database names, backup checksums, row counts and source deployment version. Keep old applications stopped until cutover validation is complete; a maintenance window avoids a distributed transaction or replicated read model.

Use a dedicated owner-controlled directory, here `/secure/cutover`, with directory mode 0700 and file mode 0600 (`umask 077` before creating exports). Files include password hashes and encrypted credentials. Use local PostgreSQL sockets or verified TLS for operator connections; do not put database passwords in command lines or committed files.

Provision the admin-owned metadata tables from [the schema contract](application-databases.md) alongside the existing admin authentication tables. Preserve `admin_users`, administrator sessions/tokens and their existing IDs/provenance. For an existing auth-only database, apply only the new `users`, group, membership, registration and grant definitions; do not recreate the admin authentication tables. Do not start the new admin server yet.

Set `SCHAKKER_ADMIN_MASTER_KEY` to the **exact existing key that encrypted registered connection passwords**. Initially this may have the same value as the old `SCHAKKER_MASTER_KEY`, but they are independently provisioned settings thereafter. Keep the user application's existing signing key. Never regenerate or silently replace either key. Byte-for-byte ciphertext transfer below preserves encrypted credentials and their AES-GCM format.

## 2. Export one stopped-source snapshot

In `psql` connected to the source platform database, set `\set ON_ERROR_STOP on` and run these client-side exports. The same files are retained for recovery; do not re-export a changing source. Migration drafts, executions and sessions stay in the platform database.

```sql
\copy (SELECT id,username,display_name,password_hash,is_active,1::bigint AS auth_version,1::bigint AS rotation_version,created_by_admin_id,legacy_created_by_user_id,created_at,updated_at FROM users WHERE NOT is_legacy_admin ORDER BY id) TO '/secure/cutover/users.csv' WITH (FORMAT csv, HEADER true)
\copy (SELECT GREATEST((SELECT COALESCE(MAX(id),0) FROM users),(SELECT last_value FROM public.users_id_seq)) AS last_id) TO '/secure/cutover/identity_high_water.csv' WITH (FORMAT csv, HEADER true)
\copy (SELECT id,name,description,created_at FROM access_groups ORDER BY id) TO '/secure/cutover/access_groups.csv' WITH (FORMAT csv, HEADER true)
\copy (SELECT m.user_id,m.group_id,m.joined_at FROM user_groups m JOIN users u ON u.id=m.user_id WHERE NOT u.is_legacy_admin ORDER BY m.user_id,m.group_id) TO '/secure/cutover/user_groups.csv' WITH (FORMAT csv, HEADER true)
\copy (SELECT group_id,database_id,access_level,granted_by_admin_id,legacy_granted_by_user_id,granted_at FROM group_database_permissions ORDER BY group_id) TO '/secure/cutover/group_database_permissions.csv' WITH (FORMAT csv, HEADER true)
\copy (SELECT id,display_name,dbms_type,host,port,database_name,db_username,encrypted_password,COALESCE(connection_options,'{}'::jsonb)-'migration_namespace' AS connection_options,is_active,created_by_admin_id,legacy_created_by_user_id,created_at,updated_at FROM managed_databases ORDER BY id) TO '/secure/cutover/managed_databases.csv' WITH (FORMAT csv, HEADER true)
```

Record checksums of the exports. Copy the protected directory to the destination operator host over an encrypted channel if the databases are on different hosts. Administrator auth tables are already owned by the destination and are not part of this transfer.

## 3. Stage, import and validate in one destination transaction

In `psql` connected to the admin database, use `\set ON_ERROR_STOP on`. The stage has the destination's column names and types without credentials being returned to any application client. This block is rerunnable from the same immutable exports while the servers remain stopped. It inserts missing IDs, **never overwrites** a conflicting or subsequently edited row, and aborts on any mismatch. Investigate conflicts instead of changing IDs or using a blind upsert.

```sql
BEGIN;
CREATE SCHEMA IF NOT EXISTS control_cutover;
CREATE TABLE IF NOT EXISTS control_cutover.identity_high_water(last_id BIGINT NOT NULL CHECK(last_id>0));
TRUNCATE control_cutover.identity_high_water;
\copy control_cutover.identity_high_water FROM '/secure/cutover/identity_high_water.csv' WITH (FORMAT csv, HEADER true)
CREATE TABLE IF NOT EXISTS control_cutover.users AS SELECT id,username,display_name,password_hash,is_active,auth_version,rotation_version,created_by_admin_id,legacy_created_by_user_id,created_at,updated_at FROM public.users WITH NO DATA;
CREATE TABLE IF NOT EXISTS control_cutover.access_groups AS SELECT id,name,description,created_at FROM public.access_groups WITH NO DATA;
CREATE TABLE IF NOT EXISTS control_cutover.user_groups AS SELECT user_id,group_id,joined_at FROM public.user_groups WITH NO DATA;
CREATE TABLE IF NOT EXISTS control_cutover.group_database_permissions AS SELECT group_id,database_id,access_level,granted_by_admin_id,legacy_granted_by_user_id,granted_at FROM public.group_database_permissions WITH NO DATA;
CREATE TABLE IF NOT EXISTS control_cutover.managed_databases AS SELECT id,display_name,dbms_type,host,port,database_name,db_username,encrypted_password,connection_options,is_active,created_by_admin_id,legacy_created_by_user_id,created_at,updated_at FROM public.managed_databases WITH NO DATA;
TRUNCATE control_cutover.users,control_cutover.access_groups,control_cutover.user_groups,control_cutover.group_database_permissions,control_cutover.managed_databases;
\copy control_cutover.users(id,username,display_name,password_hash,is_active,auth_version,rotation_version,created_by_admin_id,legacy_created_by_user_id,created_at,updated_at) FROM '/secure/cutover/users.csv' WITH (FORMAT csv, HEADER true)
\copy control_cutover.access_groups(id,name,description,created_at) FROM '/secure/cutover/access_groups.csv' WITH (FORMAT csv, HEADER true)
\copy control_cutover.user_groups(user_id,group_id,joined_at) FROM '/secure/cutover/user_groups.csv' WITH (FORMAT csv, HEADER true)
\copy control_cutover.group_database_permissions(group_id,database_id,access_level,granted_by_admin_id,legacy_granted_by_user_id,granted_at) FROM '/secure/cutover/group_database_permissions.csv' WITH (FORMAT csv, HEADER true)
\copy control_cutover.managed_databases(id,display_name,dbms_type,host,port,database_name,db_username,encrypted_password,connection_options,is_active,created_by_admin_id,legacy_created_by_user_id,created_at,updated_at) FROM '/secure/cutover/managed_databases.csv' WITH (FORMAT csv, HEADER true)
INSERT INTO public.users(id,username,display_name,password_hash,is_active,auth_version,rotation_version,created_by_admin_id,legacy_created_by_user_id,created_at,updated_at) SELECT id,username,display_name,password_hash,is_active,auth_version,rotation_version,created_by_admin_id,legacy_created_by_user_id,created_at,updated_at FROM control_cutover.users ON CONFLICT DO NOTHING;
INSERT INTO public.access_groups(id,name,description,created_at) SELECT id,name,description,created_at FROM control_cutover.access_groups ON CONFLICT DO NOTHING;
INSERT INTO public.managed_databases(id,display_name,dbms_type,host,port,database_name,db_username,encrypted_password,connection_options,is_active,created_by_admin_id,legacy_created_by_user_id,created_at,updated_at) SELECT id,display_name,dbms_type,host,port,database_name,db_username,encrypted_password,connection_options,is_active,created_by_admin_id,legacy_created_by_user_id,created_at,updated_at FROM control_cutover.managed_databases ON CONFLICT DO NOTHING;
INSERT INTO public.user_groups(user_id,group_id,joined_at) SELECT user_id,group_id,joined_at FROM control_cutover.user_groups ON CONFLICT DO NOTHING;
INSERT INTO public.group_database_permissions(group_id,database_id,access_level,granted_by_admin_id,legacy_granted_by_user_id,granted_at) SELECT group_id,database_id,access_level,granted_by_admin_id,legacy_granted_by_user_id,granted_at FROM control_cutover.group_database_permissions ON CONFLICT DO NOTHING;
DO $validate$
BEGIN
 IF (SELECT count(*) FROM control_cutover.identity_high_water)<>1
    OR (SELECT last_id FROM control_cutover.identity_high_water)<(SELECT COALESCE(MAX(id),0) FROM control_cutover.users) THEN
  RAISE EXCEPTION 'Invalid historical identity high-water mark';
 END IF;
 IF EXISTS ((SELECT id,username,display_name,password_hash,is_active,auth_version,rotation_version,created_by_admin_id,legacy_created_by_user_id,created_at,updated_at FROM public.users EXCEPT ALL SELECT id,username,display_name,password_hash,is_active,auth_version,rotation_version,created_by_admin_id,legacy_created_by_user_id,created_at,updated_at FROM control_cutover.users) UNION ALL (SELECT id,username,display_name,password_hash,is_active,auth_version,rotation_version,created_by_admin_id,legacy_created_by_user_id,created_at,updated_at FROM control_cutover.users EXCEPT ALL SELECT id,username,display_name,password_hash,is_active,auth_version,rotation_version,created_by_admin_id,legacy_created_by_user_id,created_at,updated_at FROM public.users)) THEN
  RAISE EXCEPTION 'Control-plane transfer mismatch: users';
 END IF;
 IF EXISTS ((SELECT id,name,description,created_at FROM public.access_groups EXCEPT ALL SELECT id,name,description,created_at FROM control_cutover.access_groups) UNION ALL (SELECT id,name,description,created_at FROM control_cutover.access_groups EXCEPT ALL SELECT id,name,description,created_at FROM public.access_groups)) THEN
  RAISE EXCEPTION 'Control-plane transfer mismatch: access_groups';
 END IF;
 IF EXISTS ((SELECT user_id,group_id,joined_at FROM public.user_groups EXCEPT ALL SELECT user_id,group_id,joined_at FROM control_cutover.user_groups) UNION ALL (SELECT user_id,group_id,joined_at FROM control_cutover.user_groups EXCEPT ALL SELECT user_id,group_id,joined_at FROM public.user_groups)) THEN
  RAISE EXCEPTION 'Control-plane transfer mismatch: user_groups';
 END IF;
 IF EXISTS ((SELECT group_id,database_id,access_level,granted_by_admin_id,legacy_granted_by_user_id,granted_at FROM public.group_database_permissions EXCEPT ALL SELECT group_id,database_id,access_level,granted_by_admin_id,legacy_granted_by_user_id,granted_at FROM control_cutover.group_database_permissions) UNION ALL (SELECT group_id,database_id,access_level,granted_by_admin_id,legacy_granted_by_user_id,granted_at FROM control_cutover.group_database_permissions EXCEPT ALL SELECT group_id,database_id,access_level,granted_by_admin_id,legacy_granted_by_user_id,granted_at FROM public.group_database_permissions)) THEN
  RAISE EXCEPTION 'Control-plane transfer mismatch: group_database_permissions';
 END IF;
 IF EXISTS ((SELECT id,display_name,dbms_type,host,port,database_name,db_username,encrypted_password,connection_options,is_active,created_by_admin_id,legacy_created_by_user_id,created_at,updated_at FROM public.managed_databases EXCEPT ALL SELECT id,display_name,dbms_type,host,port,database_name,db_username,encrypted_password,connection_options,is_active,created_by_admin_id,legacy_created_by_user_id,created_at,updated_at FROM control_cutover.managed_databases) UNION ALL (SELECT id,display_name,dbms_type,host,port,database_name,db_username,encrypted_password,connection_options,is_active,created_by_admin_id,legacy_created_by_user_id,created_at,updated_at FROM control_cutover.managed_databases EXCEPT ALL SELECT id,display_name,dbms_type,host,port,database_name,db_username,encrypted_password,connection_options,is_active,created_by_admin_id,legacy_created_by_user_id,created_at,updated_at FROM public.managed_databases)) THEN
  RAISE EXCEPTION 'Control-plane transfer mismatch: managed_databases';
 END IF;
END
$validate$;
SELECT setval(pg_get_serial_sequence('public.users','id'),GREATEST(COALESCE(MAX(id),1),(SELECT last_id FROM control_cutover.identity_high_water)),TRUE) FROM public.users;
SELECT setval(pg_get_serial_sequence('public.access_groups','id'),COALESCE(MAX(id),1),MAX(id) IS NOT NULL) FROM public.access_groups;
SELECT setval(pg_get_serial_sequence('public.managed_databases','id'),COALESCE(MAX(id),1),MAX(id) IS NOT NULL) FROM public.managed_databases;
COMMIT;
```

The equality checks compare every transferred field in both directions, including IDs, timestamps, password hashes, provenance and ciphertext. Target foreign keys validate memberships and grants. The ordinary-user identity sequence advances above the source high-water mark, including excluded historical administrator IDs and previously allocated IDs. This prevents a future ordinary account from taking the ID of a historical actor retained in platform migration history. Existing nonmatching destination metadata is a conflict, not something this procedure deletes. Save the successful validation output and counts with the maintenance record.

After destination commit, temporarily launch the **new** admin in a maintenance environment with its public listener inaccessible, its internal listener restricted to verification tooling, and administrative writes disabled operationally. Keep both old servers and the ordinary server stopped. Before modifying the source, verify that the admin encryption key decrypts the transferred ciphertext correctly using a private API integration check against a known permitted ordinary account. Do this over the private verified TLS connection, without printing or logging the returned password; check only a success/failure assertion in operator tooling. Compare credentials to the known managed-account credential through a secure operator channel. Do not expose the API publicly. If this check fails, leave the old platform data intact and restore the correct key. Stop the maintenance admin before the source cutover; restart it for normal operation only in step 5.

## 4. Commit the platform runtime cutover

Only after all transfer comparisons and key verification succeed, run this transaction in `psql` on the platform database. It preserves IDs, identity/display history, receipt namespaces, migration drafts and migration executions. Existing ordinary session rows receive the initial authority versions; historical administrator sessions are revoked first.

```sql
BEGIN;
-- A completed marker makes a retry an explicit no-op, rather than reinterpreting
-- a runtime-only schema as an authoritative source.
CREATE TABLE IF NOT EXISTS public.control_plane_cutover (
 singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton),
 completed_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
DO $cutover$
BEGIN
 IF EXISTS(SELECT 1 FROM public.control_plane_cutover WHERE singleton) THEN
  RETURN;
 END IF;
 -- Refuse to proceed if this is not the expected pre-cutover source format.
 IF NOT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='users' AND column_name='is_legacy_admin') THEN
  RAISE EXCEPTION 'Unexpected source schema; inspect before cutover';
 END IF;
 UPDATE public.sessions SET revoked_at=COALESCE(revoked_at,CURRENT_TIMESTAMP)
 WHERE user_id IN(SELECT id FROM public.users WHERE is_legacy_admin);
 DELETE FROM public.session_tokens WHERE session_id IN
  (SELECT s.id FROM public.sessions s JOIN public.users u ON u.id=s.user_id WHERE u.is_legacy_admin);
 UPDATE public.users SET is_active=FALSE WHERE is_legacy_admin;
 ALTER TABLE public.sessions ADD COLUMN IF NOT EXISTS authority_version BIGINT NOT NULL DEFAULT 1;
 ALTER TABLE public.sessions ADD COLUMN IF NOT EXISTS rotation_version BIGINT NOT NULL DEFAULT 1;
 DROP TABLE public.group_database_permissions,public.user_groups,public.access_groups;
 DROP INDEX IF EXISTS public.users_username_lower_key;
 ALTER TABLE public.users DROP COLUMN password_hash,DROP COLUMN is_legacy_admin,
  DROP COLUMN created_by_admin_id,DROP COLUMN legacy_created_by_user_id;
 UPDATE public.managed_databases SET connection_options=jsonb_strip_nulls(
  jsonb_build_object('migration_namespace',connection_options->>'migration_namespace'));
 ALTER TABLE public.managed_databases DROP COLUMN display_name,DROP COLUMN dbms_type,
  DROP COLUMN host,DROP COLUMN port,DROP COLUMN database_name,DROP COLUMN db_username,
  DROP COLUMN encrypted_password,DROP COLUMN is_active,DROP COLUMN created_by_admin_id,
  DROP COLUMN legacy_created_by_user_id,DROP COLUMN created_at,DROP COLUMN updated_at;
 INSERT INTO public.control_plane_cutover(singleton)VALUES(TRUE);
END
$cutover$;
COMMIT;
```

The marker and schema/data changes commit together in the platform database. Do not delete or reassign historical actors to repair references: runtime identity rows, database IDs and all existing migration FK values remain intact. Custom views or triggers that depend on removed authoritative columns require installation-specific review; a failing transaction rolls back rather than using `CASCADE` to destroy unrelated objects.

## 5. Restrict roles, configure and verify

Remove the old admin role's ability to connect to `schakker_platform`; remove the ordinary role's ability to connect to `schakker_admin`. Revoke `PUBLIC` database CONNECT privileges and grant each application only its own database/schema permissions described in the schema contract. The admin role owns metadata only; the ordinary role owns runtime writes only. Keep the operator owner credentials out of server configuration.

Configure the new admin environment, independent public/internal ports, TLS certificate/key, existing admin encryption key and private service token. Configure the user environment with its own database/signing key and the private authority origin/token. Start the new admin, verify its public login, then start the user server. Verify ordinary login, a View Only read, View + Edit migration analysis/execution on an approved test target, permission revocation, and rejection of admin cookies by ordinary routes. Verify existing drafts/history and receipt namespaces before reopening access.

Retain protected backups and immutable exports according to the installation's recovery policy. Remove the temporary `control_cutover` staging schema only after the complete deployment has been accepted; it contains credential material. It is not queried by either application.

## Recovery and reruns

- **Export or destination import interrupted:** the source remains authoritative and intact. Keep both servers stopped. Rerun the destination transaction from the same export files. A failed transaction leaves no partially committed metadata.
- **Destination committed, platform not cut over:** rerun the destination comparison block; matching records are retained without resetting credentials or IDs. Then rerun the platform transaction.
- **Platform commit outcome uncertain:** inspect `control_plane_cutover`. Marker present means the entire runtime cutover committed; the platform block is a no-op on retry. Marker absent means the transaction did not commit. Keep the servers stopped while establishing the result.
- **Failure after platform cutover:** do not restart an old server against the reduced runtime schema. Correct deployment configuration and start the new pair, or restore **both matching pre-cutover database backups and the original keys** before restarting the old pair. Restoring only one database can lose attribution or permissions. Never rerun old exports against a live edited authority: the equality guard deliberately rejects that state.

This procedure uses separate committed transactions with validation and an explicit maintenance window. It makes no claim of atomic commit across databases and requires no cross-database foreign keys, shared Go packages, foreign data wrappers, or message queue.
