import assert from "node:assert/strict";
import { test } from "node:test";
import { APIError, requestJSON, requestResource } from "../../../shared/frontend/src/api/client";

function fixture(t: any, response: Response | Error) {
  const calls: { path: string; options: RequestInit }[] = [];
  t.mock.method(globalThis, "fetch", async (path: string, options: RequestInit) => {
    calls.push({ path, options });
    if (response instanceof Error) throw response;
    return response;
  });
  return calls;
}
test("GET keeps credentials on the current origin and forwards cancellation", async t => {
  const calls = fixture(t, Response.json({ value: 42 }));
  const controller = new AbortController();
  assert.deepEqual(await requestJSON("/api/session", undefined, undefined, controller.signal), { value: 42 });
  assert.equal(calls[0].path, "/api/session");
  assert.equal(calls[0].options.method, "GET");
  assert.equal(calls[0].options.credentials, "same-origin");
  assert.equal(calls[0].options.signal, controller.signal);
  assert.equal(calls[0].options.body, undefined);
  assert.deepEqual(calls[0].options.headers, { Accept: "application/json" });
});
test("form actions preserve arrays and include CSRF", async t => {
  const calls = fixture(t, Response.json({ location: "/admin/groups" }));
  const fields = { action: "set_grants", db: ["7", "8"], permission: ["view_only", ""] };
  await requestJSON("/api/admin/groups", fields, "admin-csrf");
  assert.equal(calls[0].options.method, "POST");
  assert.deepEqual(JSON.parse(String(calls[0].options.body)), fields);
  assert.equal(calls[0].options.headers["X-CSRF-Token"], "admin-csrf");
});
test("typed mutations retain booleans, nulls, revision IDs and idempotency headers", async t => {
  const calls = fixture(t, Response.json({}));
  const body = { irreversible: false, batch: { size: 20 }, expected_revision_id: 101, target: null };
  await requestResource("/api/migration-definitions/1", { method: "PATCH", body, csrf: "user-csrf", idempotencyKey: "enqueue-once" });
  assert.equal(calls[0].options.method, "PATCH");
  assert.deepEqual(JSON.parse(String(calls[0].options.body)), body);
  assert.equal(calls[0].options.headers["Content-Type"], "application/json");
  assert.equal(calls[0].options.headers["Idempotency-Key"], "enqueue-once");
});
for (const status of [401, 403, 404, 409, 422, 503]) {
  test(`HTTP ${status} preserves status, CSRF and the server's useful error`, async t => {
    fixture(t, Response.json({ error: "Review changed", CSRF: "replacement" }, { status }));
    await assert.rejects(requestResource("/api/migration-reviews"), (error: APIError) => error instanceof APIError && error.status === status && error.message === "Review changed" && error.result.CSRF === "replacement");
  });
}
for (const body of ["<html>proxy error</html>", "null", "[]", '"text"', "42"]) {
  test(`unreadable successful response becomes a 502: ${body}`, async t => {
    fixture(t, new Response(body));
    await assert.rejects(requestResource("/api/session"), (error: APIError) => error instanceof APIError && error.status === 502 && error.message.includes("unreadable"));
  });
}
test("unreadable failed response retains its HTTP status", async t => {
  fixture(t, new Response("bad gateway", { status: 503 }));
  await assert.rejects(requestResource("/api/session"), (error: APIError) => error.status === 503);
});
test("network failures and aborts propagate without becoming HTTP errors", async t => {
  const error = new DOMException("Cancelled", "AbortError");
  fixture(t, error);
  await assert.rejects(requestResource("/api/session"), value => value === error);
});
test("missing server error uses a useful fallback", () => {
  assert.match(new APIError(503, {}).message, /temporarily unavailable/);
});
