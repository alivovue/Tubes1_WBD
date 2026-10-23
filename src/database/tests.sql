-- Run after schema.sql in a dedicated test database. Fixture rows are rolled back.
\set ON_ERROR_STOP on
BEGIN;
SET LOCAL search_path = pg_catalog, public;

CREATE FUNCTION pg_temp.expect_sqlstate(command TEXT, expected_state TEXT)
RETURNS VOID
LANGUAGE plpgsql
AS $helper$
BEGIN
    BEGIN
        EXECUTE command;
    EXCEPTION WHEN OTHERS THEN
        IF SQLSTATE = expected_state THEN
            RETURN;
        END IF;
        RAISE EXCEPTION 'Expected SQLSTATE %, received %: %', expected_state, SQLSTATE, SQLERRM;
    END;
    RAISE EXCEPTION 'Expected SQLSTATE %, but statement succeeded', expected_state;
END;
$helper$;

DO $tests$
DECLARE
    admin_id       BIGINT;
    employee_id    BIGINT;
    developer_id   BIGINT;
    analyst_id     BIGINT;
    target_id      BIGINT;
    other_target   BIGINT;
    session_id     UUID;
    draft_id       BIGINT;
    execution_id   BIGINT;
BEGIN
    INSERT INTO public.users (username, display_name, password_hash, is_platform_admin)
    VALUES ('__schema_admin__', 'Schema Admin', 'test-only-hash', TRUE)
    RETURNING id INTO admin_id;

    INSERT INTO public.users (username, display_name, password_hash, created_by)
    VALUES ('__schema_employee__', 'Schema Employee', 'test-only-hash', admin_id)
    RETURNING id INTO employee_id;

    IF NOT EXISTS (
        SELECT 1 FROM public.users
        WHERE id = employee_id AND NOT is_platform_admin AND is_active
          AND created_at IS NOT NULL AND updated_at IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'User defaults are incorrect';
    END IF;

    PERFORM pg_temp.expect_sqlstate(
        'INSERT INTO public.users (username, display_name, password_hash)
         VALUES (''__SCHEMA_EMPLOYEE__'', ''Duplicate'', ''test-only-hash'')', '23505');
    PERFORM pg_temp.expect_sqlstate(
        'INSERT INTO public.users (username, display_name, password_hash)
         VALUES (''__missing_hash__'', ''Missing Hash'', NULL)', '23502');
    PERFORM pg_temp.expect_sqlstate(
        'INSERT INTO public.users (username, display_name, password_hash, created_by)
         VALUES (''__bad_creator__'', ''Bad Creator'', ''test-only-hash'', -1)', '23503');

    INSERT INTO public.access_groups (name) VALUES ('__schema_developers__') RETURNING id INTO developer_id;
    INSERT INTO public.access_groups (name) VALUES ('__schema_analysts__') RETURNING id INTO analyst_id;
    INSERT INTO public.user_groups (user_id, group_id)
    VALUES (employee_id, developer_id), (employee_id, analyst_id), (admin_id, developer_id);

    PERFORM pg_temp.expect_sqlstate(format(
        'INSERT INTO public.user_groups (user_id, group_id) VALUES (%L, %L)', employee_id, developer_id), '23505');
    PERFORM pg_temp.expect_sqlstate(format(
        'INSERT INTO public.user_groups (user_id, group_id) VALUES (-1, %L)', developer_id), '23503');

    -- These are metadata fixtures, not connections or real target databases.
    INSERT INTO public.managed_databases (
        display_name, host, database_name, db_username, encrypted_password, created_by
    ) VALUES (
        '__schema_target__', 'localhost', 'fixture_target', 'fixture_user', decode('deadbeef', 'hex'), admin_id
    ) RETURNING id INTO target_id;
    INSERT INTO public.managed_databases (
        display_name, host, database_name, db_username, encrypted_password
    ) VALUES (
        '__schema_other_target__', 'localhost', 'other_fixture_target', 'fixture_user', decode('cafe', 'hex')
    ) RETURNING id INTO other_target;

    IF NOT EXISTS (
        SELECT 1 FROM public.managed_databases
        WHERE id = target_id AND dbms_type = 'postgresql' AND port = 5432
          AND connection_options = '{}'::JSONB AND is_active
    ) THEN
        RAISE EXCEPTION 'Managed-database defaults are incorrect';
    END IF;
    PERFORM pg_temp.expect_sqlstate(format(
        'UPDATE public.managed_databases SET port = 0 WHERE id = %L', target_id), '23514');
    PERFORM pg_temp.expect_sqlstate(format(
        'UPDATE public.managed_databases SET port = 65536 WHERE id = %L', target_id), '23514');
    PERFORM pg_temp.expect_sqlstate(format(
        'UPDATE public.managed_databases SET connection_options = ''[]''::JSONB WHERE id = %L', target_id), '23514');
    UPDATE public.managed_databases SET connection_options = NULL WHERE id = other_target;

    INSERT INTO public.group_database_permissions (group_id, database_id, access_level, granted_by)
    VALUES (developer_id, target_id, 'view_edit', admin_id), (analyst_id, target_id, 'view_only', admin_id);
    PERFORM pg_temp.expect_sqlstate(format(
        'UPDATE public.group_database_permissions SET access_level = ''admin'' WHERE group_id = %L', developer_id), '23514');
    PERFORM pg_temp.expect_sqlstate(format(
        'INSERT INTO public.group_database_permissions (group_id, database_id, access_level)
         VALUES (%L, %L, ''view_only'')', developer_id, target_id), '23505');
    PERFORM pg_temp.expect_sqlstate(format(
        'INSERT INTO public.group_database_permissions (group_id, database_id, access_level)
         VALUES (%L, -1, ''view_only'')', developer_id), '23503');

    INSERT INTO public.sessions (user_id, idle_expires_at, absolute_expires_at)
    VALUES (employee_id, CURRENT_TIMESTAMP + INTERVAL '30 minutes', CURRENT_TIMESTAMP + INTERVAL '8 hours')
    RETURNING id INTO session_id;
    INSERT INTO public.session_tokens (session_id, token_hash) VALUES (session_id, decode('01', 'hex'));
    PERFORM pg_temp.expect_sqlstate(format(
        'INSERT INTO public.session_tokens (session_id, token_hash) VALUES (%L, decode(''02'', ''hex''))', session_id), '23505');

    -- Retire the current token, then store a new current token alongside its predecessor.
    UPDATE public.session_tokens
    SET replaced_at = CURRENT_TIMESTAMP, grace_expires_at = CURRENT_TIMESTAMP + INTERVAL '30 seconds'
    WHERE token_hash = decode('01', 'hex');
    INSERT INTO public.session_tokens (session_id, token_hash) VALUES (session_id, decode('02', 'hex'));
    PERFORM pg_temp.expect_sqlstate(format(
        'INSERT INTO public.session_tokens (session_id, token_hash, replaced_at)
         VALUES (%L, decode(''01'', ''hex''), CURRENT_TIMESTAMP)', session_id), '23505');
    PERFORM pg_temp.expect_sqlstate(
        'INSERT INTO public.session_tokens (session_id, token_hash)
         VALUES (''00000000-0000-0000-0000-000000000000'', decode(''03'', ''hex''))', '23503');

    INSERT INTO public.migration_drafts (database_id, created_by, title, input_mode, operations)
    VALUES (target_id, employee_id, 'Visual fixture', 'visual', '[{"type":"add_column"}]'::JSONB)
    RETURNING id INTO draft_id;
    INSERT INTO public.migration_drafts (database_id, created_by, title, input_mode, sql_text)
    VALUES (target_id, employee_id, 'SQL fixture', 'sql', 'SELECT 1;');
    PERFORM pg_temp.expect_sqlstate(format(
        'UPDATE public.migration_drafts SET input_mode = ''terminal'' WHERE id = %L', draft_id), '23514');
    PERFORM pg_temp.expect_sqlstate(format(
        'UPDATE public.migration_drafts SET operations = ''{}''::JSONB WHERE id = %L', draft_id), '23514');

    INSERT INTO public.migration_executions (database_id, draft_id, executed_by, sql_text, attempted_version, status)
    VALUES (target_id, draft_id, employee_id, 'SELECT 1;', 1, 'running')
    RETURNING id INTO execution_id;
    UPDATE public.migration_executions
    SET status = 'failed', error_message = 'Fixture failure', finished_at = CURRENT_TIMESTAMP
    WHERE id = execution_id;

    -- A failed attempt does not consume the successful version number.
    INSERT INTO public.migration_executions (
        database_id, draft_id, executed_by, sql_text, attempted_version, applied_version, status, finished_at
    ) VALUES (target_id, draft_id, employee_id, 'SELECT 1;', 1, 1, 'succeeded', CURRENT_TIMESTAMP);
    INSERT INTO public.migration_executions (database_id, executed_by, sql_text, attempted_version, status)
    VALUES (target_id, employee_id, 'SELECT 1;', 1, 'failed');

    PERFORM pg_temp.expect_sqlstate(format(
        'INSERT INTO public.migration_executions
         (database_id, executed_by, sql_text, attempted_version, applied_version, status)
         VALUES (%L, %L, ''SELECT 1;'', 1, 1, ''succeeded'')', target_id, employee_id), '23505');

    -- The same successful version is allowed on a different target database.
    INSERT INTO public.migration_executions (
        database_id, executed_by, sql_text, attempted_version, applied_version, status
    ) VALUES (other_target, employee_id, 'SELECT 1;', 1, 1, 'succeeded');

    PERFORM pg_temp.expect_sqlstate(format(
        'INSERT INTO public.migration_executions (database_id, executed_by, sql_text, attempted_version, status)
         VALUES (%L, %L, ''SELECT 1;'', 2, ''succeeded'')', target_id, employee_id), '23514');
    PERFORM pg_temp.expect_sqlstate(format(
        'INSERT INTO public.migration_executions
         (database_id, executed_by, sql_text, attempted_version, applied_version, status)
         VALUES (%L, %L, ''SELECT 1;'', 2, 2, ''failed'')', target_id, employee_id), '23514');
    PERFORM pg_temp.expect_sqlstate(format(
        'INSERT INTO public.migration_executions
         (database_id, executed_by, sql_text, attempted_version, applied_version, status)
         VALUES (%L, %L, ''SELECT 1;'', 2, 2, ''running'')', target_id, employee_id), '23514');
    PERFORM pg_temp.expect_sqlstate(format(
        'INSERT INTO public.migration_executions (database_id, executed_by, sql_text, attempted_version, status)
         VALUES (%L, %L, ''SELECT 1;'', 2, ''cancelled'')', target_id, employee_id), '23514');
    PERFORM pg_temp.expect_sqlstate(format(
        'INSERT INTO public.migration_executions (database_id, executed_by, sql_text, attempted_version, status)
         VALUES (%L, %L, ''SELECT 1;'', 0, ''running'')', target_id, employee_id), '23514');
    PERFORM pg_temp.expect_sqlstate(format(
        'DELETE FROM public.users WHERE id = %L', admin_id), '23503');

    RAISE NOTICE 'All schema assertions passed.';
END;
$tests$;

ROLLBACK;
