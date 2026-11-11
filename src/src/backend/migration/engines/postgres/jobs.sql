CREATE SCHEMA IF NOT EXISTS _schakker;
CREATE TABLE IF NOT EXISTS _schakker.job_receipts (
 namespace text NOT NULL, job_id uuid NOT NULL, execution_id uuid NOT NULL,
 action_hash text NOT NULL, sql_hash text NOT NULL, operation text NOT NULL,
 committed_at timestamptz NOT NULL DEFAULT clock_timestamp(), progress jsonb NOT NULL,
 PRIMARY KEY(namespace,job_id), UNIQUE(namespace,execution_id)
);
CREATE TABLE IF NOT EXISTS _schakker.batch_jobs (
 namespace text NOT NULL, job_id uuid NOT NULL, action_hash text NOT NULL,
 spec jsonb NOT NULL, batches bigint NOT NULL DEFAULT 0, rows_count bigint NOT NULL DEFAULT 0,
 last_cursor bigint, total_rows bigint NOT NULL, reverted_batches bigint NOT NULL DEFAULT 0,
 reverted_rows bigint NOT NULL DEFAULT 0, state text NOT NULL,
 PRIMARY KEY(namespace,job_id)
);
CREATE TABLE IF NOT EXISTS _schakker.batch_undo (
 namespace text NOT NULL, job_id uuid NOT NULL, row_id bigint NOT NULL,
 batch_no bigint, before_value bigint, after_value bigint, after_xmin text,
 reverted boolean NOT NULL DEFAULT false,
 PRIMARY KEY(namespace,job_id,row_id),
 FOREIGN KEY(namespace,job_id) REFERENCES _schakker.batch_jobs(namespace,job_id)
);
CREATE INDEX IF NOT EXISTS batch_undo_pending ON _schakker.batch_undo(namespace,job_id,row_id) WHERE batch_no IS NULL;
CREATE INDEX IF NOT EXISTS batch_undo_reverse ON _schakker.batch_undo(namespace,job_id,batch_no DESC) WHERE batch_no IS NOT NULL AND NOT reverted;
CREATE TABLE IF NOT EXISTS _schakker.batch_receipts (
 namespace text NOT NULL, job_id uuid NOT NULL, batch_no bigint NOT NULL,
 direction text NOT NULL CHECK(direction IN('up','down')), rows_count bigint NOT NULL,
 cursor bigint, committed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(namespace,job_id,batch_no,direction),
 FOREIGN KEY(namespace,job_id) REFERENCES _schakker.batch_jobs(namespace,job_id)
);
CREATE OR REPLACE FUNCTION _schakker.execute_batch_down(statement text,delta bigint,ids bigint[])
RETURNS bigint LANGUAGE plpgsql AS $$
DECLARE changed bigint;
BEGIN
 EXECUTE statement USING delta,ids;
 GET DIAGNOSTICS changed=ROW_COUNT;
 RETURN changed;
END $$;
