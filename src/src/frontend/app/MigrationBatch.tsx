import type { BatchConfig, MigrationJob } from "./types";
import { Field, Notice } from "./ui";

export const defaultBatch: BatchConfig = {
  template: "update_by_key",
  schema: "public",
  table: "",
  key_column: "id",
  value_column: "value",
  delta: 1,
  batch_size: 1000,
};

// Match engines.BatchSQL exactly: both directions and configuration are signed.
export function batchSQL(value: BatchConfig) {
  const spec = {
    schema: value.schema,
    table: value.table,
    key: value.key_column,
    column: value.value_column,
    delta: value.delta,
    size: value.batch_size,
    expected_rows: value.expected_rows || 0,
  };
  const quote = (name: string) => `"${name.replaceAll('"', '""')}"`;
  const header = `-- schakker:batch ${JSON.stringify(spec)}\n`;
  const base = `UPDATE ${quote(spec.schema)}.${quote(spec.table)} SET ${quote(spec.column)} = ${quote(spec.column)} `;
  const tail = ` $1 WHERE ${quote(spec.key)} = ANY($2::bigint[]);`;
  return {
    up_sql: header + base + "+" + tail,
    down_sql: header + base + "-" + tail,
  };
}

export function MigrationBatch({
  value,
  onChange,
  disabled,
}: {
  value: BatchConfig | null;
  onChange: (value: BatchConfig | null) => void;
  disabled: boolean;
}) {
  return (
    <div className="migration-batch-config">
      <label className="check-control">
        <input
          type="checkbox"
          checked={!!value}
          disabled={disabled}
          onChange={(event) =>
            onChange(event.target.checked ? { ...defaultBatch } : null)
          }
        />
        <span>Use a PostgreSQL batch UPDATE</span>
      </label>
      {value && (
        <>
          <p className="muted">
            Add a fixed integer amount to a BIGINT column using a BIGINT primary
            key. The generated SQL and settings are frozen together for approval.
          </p>
          <div className="form-grid">
            {(
              [
                ["schema", "Schema"],
                ["table", "Table"],
                ["key_column", "Unique key column"],
                ["value_column", "Value column (BIGINT)"],
              ] as const
            ).map(([key, label]) => (
              <Field key={key} label={label}>
                <input
                  value={value[key]}
                  required
                  disabled={disabled}
                  maxLength={63}
                  pattern="[A-Za-z_][A-Za-z0-9_]*"
                  onChange={(event) =>
                    onChange({ ...value, [key]: event.target.value })
                  }
                />
              </Field>
            ))}
            <Field label="Rows per batch">
              <input
                type="number"
                min={1}
                max={10000}
                step={1}
                required
                disabled={disabled}
                value={value.batch_size || ""}
                onChange={(event) =>
                  onChange({ ...value, batch_size: Number(event.target.value) })
                }
              />
            </Field>
            <Field label="Amount to add per row">
              <input
                type="number"
                required
                step={1}
                disabled={disabled}
                value={value.delta || ""}
                min={-Number.MAX_SAFE_INTEGER}
                max={Number.MAX_SAFE_INTEGER}
                onChange={(event) =>
                  onChange({ ...value, delta: Number(event.target.value) })
                }
              />
            </Field>
            <Field label="Expected rows (optional)">
              <input
                type="number"
                min={0}
                max={Number.MAX_SAFE_INTEGER}
                step={1}
                disabled={disabled}
                value={value.expected_rows ?? ""}
                onChange={(event) =>
                  onChange({
                    ...value,
                    expected_rows: event.target.value
                      ? Number(event.target.value)
                      : undefined,
                  })
                }
              />
            </Field>
          </div>
          <Notice>
            Each completed batch is committed separately. A later failure
            retains earlier changes. Safe cancellation needs an approved
            compensation review.
          </Notice>
        </>
      )}
    </div>
  );
}

export function BatchProgress({ job }: { job: MigrationJob }) {
  const reverting =
    ["reverting", "reverted", "partially_reverted"].includes(job.status) ||
    job.operation === "cancel_batch";
  const completed = reverting ? job.reverted_rows : job.committed_rows;
  const total = reverting ? job.committed_rows : job.total_rows;
  return (
    <section
      className="migration-progress"
      aria-label={`Progress for ${job.job_id}`}
    >
      {total != null && total > 0 ? (
        <>
          <label htmlFor={`progress-${job.job_id}`}>
            {reverting ? "Compensation" : "Committed rows"}:{" "}
            {completed.toLocaleString()} / {total.toLocaleString()}
          </label>
          <progress
            id={`progress-${job.job_id}`}
            max={total}
            value={Math.min(completed, total)}
          />
        </>
      ) : (
        <p>
          {completed.toLocaleString()} rows{" "}
          {reverting ? "reverted" : "committed"}. Total rows unavailable.
        </p>
      )}
      <dl className="detail-grid">
        <div>
          <dt>Committed batches</dt>
          <dd>{job.committed_batches}</dd>
        </div>
        <div>
          <dt>Committed rows</dt>
          <dd>{job.committed_rows.toLocaleString()}</dd>
        </div>
        <div>
          <dt>Reverted batches / rows</dt>
          <dd>
            {job.reverted_batches} / {job.reverted_rows.toLocaleString()}
          </dd>
        </div>
        <div>
          <dt>Last committed cursor</dt>
          <dd>
            <code>{job.last_cursor ?? "Not started"}</code>
          </dd>
        </div>
      </dl>
      {["partial", "partially_reverted", "uncertain"].includes(job.status) && (
        <Notice error>
          {job.status === "uncertain"
            ? "The commit outcome is unconfirmed. Further work may be blocked until the worker reconciles the target receipt."
            : "Committed batches remain applied until compensation is confirmed. Retry resumes from the saved checkpoint."}
        </Notice>
      )}
    </section>
  );
}
