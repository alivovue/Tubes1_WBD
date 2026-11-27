import assert from "node:assert/strict";
import { test } from "node:test";
import { api, migrationPaths, migrationsAPI } from "../../../apps/user/frontend/app/api";
import { api as adminAPI } from "../../../apps/admin/frontend/app/api";
function capture(t: any) {
  const calls: any[] = [];
  t.mock.method(globalThis, "fetch", async (path: string, options: RequestInit) => { calls.push({ path, ...options, body: options.body === undefined ? undefined : JSON.parse(String(options.body)) }); return Response.json({}); });
  return calls;
}
test("each app calls its own relative API and shares no credential origin", async t => {
  const calls = capture(t);
  await api("/dashboard"); await adminAPI("/admin/users", { action: "create_user" }, "admin-token");
  assert.deepEqual(calls.map(c => [c.path, c.credentials]), [["/api/dashboard", "same-origin"], ["/api/admin/users", "same-origin"]]);
  assert.equal(calls[1].headers["X-CSRF-Token"], "admin-token");
});
test("resource paths encode job IDs and opaque history cursors", () => {
  assert.equal(migrationPaths.job("id/with ?&"), "/migration-jobs/id%2Fwith%20%3F%26");
  assert.equal(migrationPaths.history(7, true, "a+/=?"), "/migrations/history?database_id=7&hide_rollback=true&cursor=a%2B%2F%3D%3F");
  assert.equal(migrationPaths.reviews(7), "/migration-reviews?database_id=7&limit=51&offset=0");
  assert.equal(migrationPaths.jobs(7), "/migration-jobs?database_id=7&limit=51&offset=0");
});
test("saving maps recovery fields and preserves optimistic revisions without sending UI-only batch data", async t => {
  const calls = capture(t);
  const input = { database_id: 7, expected_revision_id: 101, title: "Change", up_sql: "UP", down_sql: "DOWN", irreversible: false, irreversibility_reason: "", batch: { batch_size: 50 } } as any;
  await migrationsAPI.save(input, "csrf"); await migrationsAPI.save(input, "csrf", 9);
  assert.deepEqual(calls.map(c => [c.path, c.method]), [["/api/migration-definitions", "POST"], ["/api/migration-definitions/9", "PATCH"]]);
  assert.equal(calls[0].body.database_id, 7); assert.equal(calls[1].body.database_id, undefined);
  assert.equal(calls[1].body.expected_revision_id, 101); assert.equal(calls[1].body.irreversible_reason, "");
  assert.equal(calls[1].body.batch, undefined); assert.equal(calls[1].body.irreversibility_reason, undefined);
  assert.equal(input.database_id, 7);
});
test("submission freezes the revision and current generation", async t => {
  const calls = capture(t);
  await migrationsAPI.submit({ revision_id: 101, expected_generation: 4 } as any, "csrf");
  assert.deepEqual(calls[0].body, { revision_id: 101, expected_generation: 4, action: "up" });
});
for (const [action, expected] of [["up_batch", "up"], ["cancel_batch", "batch_cancel"], ["rollback", "rollback"], ["forward", "forward"]]) {
  test(`resubmit ${action} uses backend action ${expected} and keeps recovery targets`, async t => {
    const calls = capture(t);
    await migrationsAPI.resubmit({ action, revision_id: 8, expected_generation: 3, target_execution_id: "execution-id", target_job_id: "job-id" } as any, "csrf");
    assert.deepEqual(calls[0].body, { action: expected, revision_id: 8, expected_generation: 3, target_execution_id: "execution-id", target_job_id: "job-id" });
  });
}
test("group decisions map approve/reject and retain the note", async t => {
  const calls = capture(t);
  await migrationsAPI.decide(9, 2, "approved", "Reviewed SQL", "csrf");
  await migrationsAPI.decide(9, 3, "rejected", "Unsafe down SQL", "csrf");
  assert.equal(calls[0].path, "/api/migration-reviews/9/decisions");
  assert.deepEqual(calls.map(c => c.body), [{ group_id: 2, decision: "approve", note: "Reviewed SQL" }, { group_id: 3, decision: "reject", note: "Unsafe down SQL" }]);
});
test("enqueue and retry send idempotency keys in headers", async t => {
  const calls = capture(t);
  await migrationsAPI.enqueue(9, "csrf", "enqueue-key"); await migrationsAPI.retry("a/b", "csrf", "retry-key");
  assert.deepEqual(calls.map(c => [c.path, c.headers["Idempotency-Key"], c.body]), [["/api/migration-reviews/9/enqueue", "enqueue-key", {}], ["/api/migration-jobs/a%2Fb/retry", "retry-key", {}]]);
});
test("cancellation, rollback, and forward request reviews against exact targets", async t => {
  const calls = capture(t);
  await migrationsAPI.cancelReview({ job_id: "a/b", revision_id: 8, expected_generation: 4 } as any, "csrf");
  for (const operation of ["rollback", "forward"] as const) await migrationsAPI.executionReview({ id: "e/f", expected_generation: 4 } as any, operation, "csrf");
  assert.deepEqual(calls.map(c => c.path), ["/api/migration-jobs/a%2Fb/cancel-reviews", "/api/migrations/e%2Ff/rollback-reviews", "/api/migrations/e%2Ff/forward-reviews"]);
  assert.deepEqual(calls[0].body, { revision_id: 8, expected_generation: 4 });
  assert.deepEqual(calls[1].body, { expected_generation: 4 });
});
test("fresh resume approval targets the existing job and current generation", async t => {
  const calls = capture(t);
  await migrationsAPI.resumeReview({ job_id: "paused/job", expected_generation: 7 } as any, "csrf");
  assert.equal(calls[0].path, "/api/migration-jobs/paused%2Fjob/resume-reviews");
  assert.deepEqual(calls[0].body, { expected_generation: 7 });
  assert.equal(calls[0].headers["X-CSRF-Token"], "csrf");
});
