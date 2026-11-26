import assert from "node:assert/strict";
import { test } from "node:test";
import { renderToStaticMarkup as render } from "react-dom/server";
import { APIError } from "../../../shared/frontend/src/api/client";
import { batchSQL, BatchProgress, defaultBatch, MigrationBatch } from "../../../apps/user/frontend/app/MigrationBatch";
import { hideRollbackPairs } from "../../../apps/user/frontend/app/MigrationHistory";
import { migrationError, migrationURL, MigrationStatus, SQLDetails } from "../../../apps/user/frontend/app/migrationUI";
import { Results } from "../../../apps/user/frontend/app/ui";

test("batch SQL signs configuration into both parameterized directions", () => {
  const sql = batchSQL({ ...defaultBatch, table: "counters", delta: -7, batch_size: 50, expected_rows: 100 });
  const header = '-- schakker:batch {"schema":"public","table":"counters","key":"id","column":"value","delta":-7,"size":50,"expected_rows":100}\n';
  assert.equal(sql.up_sql, header + 'UPDATE "public"."counters" SET "value" = "value" + $1 WHERE "id" = ANY($2::bigint[]);');
  assert.equal(sql.down_sql, header + 'UPDATE "public"."counters" SET "value" = "value" - $1 WHERE "id" = ANY($2::bigint[]);');
});
test("batch SQL escapes identifiers and binds an omitted expected row count to zero", () => {
  const sql = batchSQL({ ...defaultBatch, table: 'counter"table' });
  assert.match(sql.up_sql, /"counter""table"/); assert.match(sql.down_sql, /"expected_rows":0/);
});
test("batch configuration exposes bounds and freezes every input when disabled", () => {
  const html = render(<MigrationBatch value={{ ...defaultBatch, table: "demo" }} onChange={() => {}} disabled />);
  assert.match(html, /max="10000"/);
  const inputs = html.match(/<input[^>]+>/g)!;
  assert(inputs.every(input => input.includes('disabled=""')));
});
const event = (id: string, operation: string, target: string | null = null, overrides = {}) => ({ id, operation, target_execution_id: target, is_committed: true, status: "succeeded", ...overrides }) as any;
test("history hides successful committed rollback pairs while retaining later forward and failed attempts", () => {
  const events = [event("up", "up"), event("down", "rollback", "up"), event("forward", "forward", "up"), event("failed", "rollback", "forward", { status: "failed", is_committed: false })];
  const original = structuredClone(events);
  assert.deepEqual(hideRollbackPairs(events).map(e => e.id), ["forward", "failed"]);
  assert.deepEqual(events, original);
});
test("uncommitted, failed or targetless rollback events cannot hide an execution", () => {
  const events = [event("up", "up"), event("partial", "rollback", "up", { is_committed: false }), event("failed", "rollback", "up", { status: "failed" }), event("no-target", "rollback")];
  assert.deepEqual(hideRollbackPairs(events), events);
});
for (const [status, context] of [[403, "permissions"], [404, "unavailable"], [409, "changed"], [422, "fields"], [503, "unavailable"]] as const) {
  test(`migration error ${status} adds actionable context and preserves the server message`, () => {
    const text = migrationError(new APIError(status, { error: "Specific explanation" }));
    assert(text.includes(context)); assert(text.includes("Specific explanation"));
  });
}
test("unknown errors remain useful and migration links preserve encoded selection", () => {
  assert.equal(migrationError(new Error("Offline")), "Offline");
  assert.match(migrationError(null), /request failed/);
  assert.equal(migrationURL("jobs", 7, { job: "a/b" }), "/migrations?db=7&view=jobs&job=a%2Fb");
});
for (const [status, tone] of [["approved", "success"], ["partially_reverted", "warning"], ["uncertain", "warning"], ["pending_review", "neutral"]]) {
  test(`migration status ${status} has readable text and ${tone} tone`, () => {
    const html = render(<MigrationStatus status={status} />); assert(html.includes(`badge--${tone}`)); assert(html.includes(status.replaceAll("_", " ")));
  });
}
test("immutable SQL details escape SQL and display irreversible reasons", () => {
  const html = render(<SQLDetails up="SELECT '<script>'" irreversible reason="No inverse" />);
  assert(html.includes("&lt;script&gt;")); assert(html.includes("Irreversible: No inverse")); assert.equal(html.includes("SQL down"), false);
  assert(render(<SQLDetails up="UP" down="DOWN" />).includes("DOWN"));
});
const job = { job_id: "job-1", operation: "up_batch", status: "running", committed_batches: 2, committed_rows: 100, reverted_batches: 1, reverted_rows: 50, total_rows: 500, last_cursor: "100" } as any;
test("batch progress uses committed rows and safely caps the visible progress bar", () => {
  assert.match(render(<BatchProgress job={job} />), /max="500" value="100"/);
  assert.match(render(<BatchProgress job={{ ...job, total_rows: 80 }} />), /max="80" value="80"/);
});
test("compensation progress uses reverted rows out of committed rows", () => {
  const html = render(<BatchProgress job={{ ...job, operation: "cancel_batch", status: "partially_reverted" }} />);
  assert.match(html, /max="100" value="50"/); assert(html.includes("Compensation")); assert(html.includes("saved checkpoint"));
});
test("unknown totals and uncertain commits display recovery guidance", () => {
  const html = render(<BatchProgress job={{ ...job, status: "uncertain", total_rows: null, last_cursor: null }} />);
  assert(html.includes("Total rows unavailable")); assert(html.includes("Not started")); assert(html.includes("commit outcome is unconfirmed"));
});
test("query results retain null, false, zero, JSON and HTML as safe display values", () => {
  const html = render(<Results data={{ Columns: [{ Name: "value", DataType: "text" }], Rows: [[null], [false], [0], [{ x: "<img>" }]] } as any} />);
  for (const text of ["NULL", "false", ">0<", "&lt;img&gt;", "viewer-result-type"]) assert(html.includes(text));
  assert.equal(html.includes("<img>"), false);
  assert(render(<Results data={{ Columns: [], Rows: [] } as any} />).includes("No matching rows"));
});
