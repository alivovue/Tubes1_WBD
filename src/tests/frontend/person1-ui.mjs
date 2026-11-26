// UI contract checks against isolated, synthetic API fixtures. No database access.
// Requires Node 22 and a local Chromium browser; no test framework/dependency.
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { createServer } from "node:http";
import { existsSync } from "node:fs";
import { mkdtemp, readFile, mkdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { basename, dirname, extname, join, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const evidence = process.env.SCHAKKER_QA_OUTPUT || join(root, "docs/qa/person1");
const userDist = join(root, "src/src/frontend/dist");
const adminDist = join(root, "src/src-admin/frontend/dist");
const browserPath =
  process.env.SCHAKKER_QA_BROWSER ||
  [
    "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe",
    "C:/Program Files/Google/Chrome/Application/chrome.exe",
    "/usr/bin/chromium",
    "/usr/bin/chromium-browser",
    "/usr/bin/google-chrome",
  ].find(existsSync);
assert(
  browserPath,
  "Set SCHAKKER_QA_BROWSER to a local Chromium browser executable.",
);
assert(
  existsSync(join(userDist, "index.html")) &&
    existsSync(join(adminDist, "index.html")),
  "Run npm run build first.",
);
await mkdir(evidence, { recursive: true });

const person = {
  ID: 1,
  Username: "demo.leader",
  DisplayName: "Demo Leader",
  Active: true,
};
const member = {
  ID: 2,
  Username: "demo.member",
  DisplayName: "Demo Member",
  Active: true,
};
const inactive = {
  ID: 3,
  Username: "demo.inactive",
  DisplayName: "Inactive Member",
  Active: false,
};
const database = {
  ID: 7,
  Name: "Disposable demo database",
  DBMS: "postgresql",
  DatabaseName: "demo",
  Host: "localhost",
  Port: 5432,
  SSLMode: "disable",
  Active: true,
  Permission: "view_edit",
};
const date = "2026-11-11T03:00:00Z";
const definition = {
  id: 1,
  revision_id: 101,
  expected_generation: 0,
  database_id: 7,
  title: "Add demo flag",
  revision: 1,
  status: "draft",
  editable: true,
  up_sql: "ALTER TABLE demo ADD COLUMN enabled boolean;",
  down_sql: "ALTER TABLE demo DROP COLUMN enabled;",
  irreversible: false,
  irreversibility_reason: "",
  batch: null,
  definition_hash: "synthetic-revision-hash",
  created_by: person.Username,
  updated_at: date,
};
const groupApproval = (id, name, permission) => ({
  group_id: id,
  group_name: name,
  permission,
  leader_user_id: 1,
  leader_name: person.DisplayName,
  decision: "pending",
  can_decide: true,
});
const makeReview = (id, source, action = "up", target = null) => ({
  id,
  database_id: 7,
  definition_id: source.id,
  revision_id: source.revision_id,
  expected_generation: 0,
  target_job_id: null,
  revision: source.revision,
  title: source.title,
  action,
  target_execution_id: target,
  status: "pending_review",
  requested_by: person.Username,
  requested_at: date,
  definition_hash: source.definition_hash,
  authority_revision: "synthetic-authority-1",
  up_sql: source.up_sql,
  down_sql: source.down_sql,
  irreversible: source.irreversible,
  irreversibility_reason: source.irreversibility_reason,
  batch: source.batch,
  required_groups: [
    groupApproval(1, "Editors", "view_edit"),
    groupApproval(2, "Auditors", "view_only"),
  ],
  can_enqueue: false,
  can_resubmit: false,
  job_id: null,
});
const makeJob = () => ({
  job_id: "demo-job-1",
  database_id: 7,
  definition_id: 1,
  revision_id: 101,
  expected_generation: 0,
  review_id: 1,
  title: "Batch demo update",
  operation: "up_batch",
  status: "pending",
  requested_by: person.Username,
  created_at: date,
  started_at: null,
  completed_at: null,
  attempt: 1,
  max_attempts: 3,
  next_retry_at: null,
  committed_batches: 0,
  committed_rows: 0,
  reverted_batches: 0,
  reverted_rows: 0,
  total_rows: 5000,
  last_cursor: null,
  retryable: false,
  can_cancel: false,
  blocked_reason: null,
  error: null,
});
const makeEvent = (id, operation, target = null) => ({
  id: String(id),
  database_id: 7,
  definition_id: 1,
  revision_id: 101,
  expected_generation: 1,
  revision: 1,
  version: 1,
  title: "Add demo flag",
  operation,
  sql: operation === "rollback" ? definition.down_sql : definition.up_sql,
  actor: person.Username,
  status: "succeeded",
  is_committed: true,
  attempt: 1,
  occurred_at: date,
  started_at: date,
  completed_at: date,
  committed_at: date,
  can_retry: false,
  error: "",
  duration_ms: 125,
  job_id: `history-job-${id}`,
  review_id: id,
  target_execution_id: target == null ? null : String(target),
  irreversible: false,
  irreversibility_reason: "",
  can_rollback: id === 3,
  can_forward: false,
});
const state = {
  definitions: [structuredClone(definition)],
  reviews: [],
  jobs: [],
  history: [
    makeEvent(1, "up"),
    makeEvent(2, "rollback", 1),
    makeEvent(3, "forward", 1),
  ],
  leader: null,
  permission: "view_edit",
  noDatabases: false,
  failure: null,
  delay: 0,
  loggedOut: false,
};
const requests = [];
const jobReads = [];
function page(admin = false) {
  const db = { ...database, Permission: state.permission };
  return {
    Title: admin ? "Groups" : "Workspace",
    Active: "dashboard",
    Section: "Workspace",
    User: person,
    CSRF: "synthetic-csrf",
    Message: "",
    Now: date,
    Expires: "2026-11-11T11:00:00Z",
    Databases: state.noDatabases
      ? []
      : [db, { ...db, ID: 8, Name: "Second demo database" }],
    Database: state.noDatabases ? { ID: 0 } : db,
    CanEdit: state.permission === "view_edit",
    EditableCount: 1,
    Recent: state.noDatabases ? [] : state.history.filter((event) => event.is_committed).slice(-1).map((event) => ({
      id: event.id,
      database_id: event.database_id,
      database_name: db.Name,
      title: event.title,
      actor: event.actor,
      operation: event.operation,
      committed_at: event.committed_at,
      version: event.version,
    })),
    Schemas: [{ Name: "public" }],
    Tables: [],
    Schema: "public",
    Table: "",
    Detail: null,
    Data: null,
    Drafts: [],
    DraftID: 0,
    DraftUpdated: "",
    Description: "",
    SQL: "",
    OperationsJSON: "",
    Review: null,
    ViewerResult: null,
    ViewerSpec: { Limit: 100, Offset: 0 },
    Offset: 0,
    Previous: 0,
    Next: 50,
    HasMore: false,
    DBMS: ["postgresql", "mariadb"],
    Administrators: [person],
    Admin: { Users: [person, member, inactive], Groups: [], Databases: [db] },
    Groups: [
      {
        ID: 1,
        Name: "Editors",
        Description: "Synthetic UI fixture",
        LeaderUserID: state.leader,
        Members: [person, member, inactive],
        Grants: [{ Database: db, Permission: "view_edit" }],
        Editor: "",
        Focus: false,
      },
    ],
  };
}
const json = (res, status, body) => {
  res.writeHead(status, { "Content-Type": "application/json" });
  res.end(JSON.stringify(body));
};
const server = createServer(async (req, res) => {
  try {
    const url = new URL(req.url, "http://localhost");
    const path = url.pathname;
    if (path.startsWith("/api/")) {
      const chunks = [];
      for await (const chunk of req) chunks.push(chunk);
      const body = chunks.length
        ? JSON.parse(Buffer.concat(chunks).toString())
        : {};
      requests.push({
        method: req.method,
        path,
        body,
        csrf: req.headers["x-csrf-token"],
        key: req.headers["idempotency-key"],
      });
      if (state.delay)
        await new Promise((done) => setTimeout(done, state.delay));
      if (
        state.failure &&
        path.startsWith(state.failure.path) &&
        (!state.failure.method || req.method === state.failure.method)
      )
        return json(res, state.failure.status, {
          error: `Synthetic ${state.failure.status} response`,
        });
      if (path === "/api/session")
        return json(
          res,
          200,
          state.loggedOut ? { CSRF: "synthetic-csrf" } : page(),
        );
      if (path === "/api/logout") {
        state.loggedOut = true;
        return json(res, 200, {});
      }
      if (path === "/api/login") {
        state.loggedOut = false;
        return json(res, 200, {});
      }
      if (
        req.method !== "GET" &&
        req.headers["x-csrf-token"] !== "synthetic-csrf"
      )
        return json(res, 403, { error: "Missing CSRF" });
      if (path === "/api/migration-definitions" && req.method === "GET")
        return json(res, 200, { definitions: state.definitions });
      if (
        path.startsWith("/api/migration-definitions") &&
        req.method !== "GET"
      ) {
        let saved;
        if (req.method === "PATCH") {
          saved = state.definitions.find(
            (item) => String(item.id) === path.split("/").at(-1),
          );
          if (!saved || saved.revision_id !== body.expected_revision_id)
            return json(res, 409, { error: "Stale draft revision" });
          Object.assign(saved, body, { revision: saved.revision + 1, revision_id: saved.revision_id + 1 });
        } else {
          saved = {
            ...structuredClone(definition),
            ...body,
            id: state.definitions.length + 1,
            revision_id: 100 + state.definitions.length + 1,
          };
          state.definitions.push(saved);
        }
        saved.irreversibility_reason = body.irreversible_reason;
        if (body.up_sql.startsWith("-- schakker:batch ")) {
          const spec = JSON.parse(body.up_sql.split("\n")[0].slice("-- schakker:batch ".length));
          saved.batch = { template: "update_by_key", schema: spec.schema, table: spec.table, key_column: spec.key, value_column: spec.column, delta: spec.delta, batch_size: spec.size, expected_rows: spec.expected_rows };
        } else saved.batch = null;
        return json(res, 200, { definition: saved });
      }
      if (path === "/api/migration-reviews" && req.method === "GET")
        return json(res, 200, { reviews: state.reviews });
      if (path === "/api/migration-reviews" && req.method === "POST") {
        const source = state.definitions.find(
          (item) => item.revision_id === body.revision_id,
        );
        source.editable = false;
        source.status = "submitted";
        const review = makeReview(
          state.reviews.length + 1,
          source,
          body.action,
          body.target_execution_id,
        );
        state.reviews.push(review);
        return json(res, 200, { review });
      }
      if (/\/migration-reviews\/\d+\/decisions$/.test(path)) {
        const review = state.reviews.find(
          (item) => String(item.id) === path.split("/")[3],
        );
        Object.assign(
          review.required_groups.find(
            (group) => group.group_id === body.group_id,
          ),
          {
            decision: body.decision === "approve" ? "approved" : "rejected",
            reason: body.note,
            decided_by: person.Username,
            decided_at: date,
            can_decide: false,
          },
        );
        review.status =
          body.decision === "reject"
            ? "rejected"
            : review.required_groups.every(
                  (group) => group.decision === "approved",
                )
              ? "approved"
              : "pending_review";
        review.can_enqueue = review.status === "approved";
        review.can_resubmit = review.status === "rejected";
        return json(res, 200, { review });
      }
      if (/\/migration-reviews\/\d+\/enqueue$/.test(path)) {
        const review = state.reviews.find(
          (item) => String(item.id) === path.split("/")[3],
        );
        const job = {
          ...makeJob(),
          review_id: review.id,
          definition_id: review.definition_id,
        };
        state.jobs = [job];
        review.job_id = job.job_id;
        return json(res, 202, {
          job_id: job.job_id,
          status: job.status,
          review_id: review.id,
        });
      }
      if (path === "/api/migration-jobs")
        return json(res, 200, { jobs: state.jobs });
      if (/\/migration-jobs\/[^/]+$/.test(path)) {
        const job = state.jobs.find(
          (item) => item.job_id === decodeURIComponent(path.split("/").at(-1)),
        );
        jobReads.push(Date.now());
        return json(
          res,
          job ? 200 : 404,
          job ? { job } : { error: "Job does not exist" },
        );
      }
      if (path.endsWith("/retry")) {
        const job = state.jobs[0];
        Object.assign(job, {
          status: "running",
          retryable: false,
          error: null,
          attempt: job.attempt + 1,
        });
        return json(res, 202, { job });
      }
      if (/\/(cancel|rollback|forward)-reviews$/.test(path)) {
        const operation = path.endsWith("cancel-reviews")
          ? "cancel_batch"
          : path.endsWith("rollback-reviews")
            ? "rollback"
            : "forward";
        const review = makeReview(
          state.reviews.length + 1,
          definition,
          operation,
          operation === "cancel_batch" ? null : path.split("/")[3],
        );
        state.reviews.push(review);
        return json(res, 200, { review });
      }
      if (path === "/api/migrations/history") {
        let executions = state.history;
        if (url.searchParams.get("hide_rollback") === "true") {
          const hidden = new Set(
            executions
              .filter((event) => event.is_committed && event.status === "succeeded" && event.operation === "rollback")
              .flatMap((event) => [event.id, event.target_execution_id]),
          );
          executions = executions.filter((event) => !hidden.has(event.id));
        }
        return json(res, 200, { executions, next_cursor: null });
      }
      if (path === "/api/admin/groups" && req.method === "POST") {
        if (body.action === "set_leader")
          state.leader = Number(body.leader_user_id);
        return json(res, 200, page(true));
      }
      return json(res, 200, page(path.includes("/admin/")));
    }
    if (path.startsWith("/assets/")) {
      for (const dist of [userDist, adminDist]) {
        const target = resolve(dist, `.${path}`);
        if (target.startsWith(dist + sep) && existsSync(target)) {
          const mime =
            {
              ".js": "text/javascript",
              ".css": "text/css",
              ".svg": "image/svg+xml",
              ".png": "image/png",
            }[extname(target)] || "application/octet-stream";
          res.writeHead(200, { "Content-Type": mime });
          res.end(await readFile(target));
          return;
        }
      }
      res.writeHead(404);
      res.end();
      return;
    }
    if (path.startsWith("/public/")) {
      res.writeHead(200, { "Content-Type": "image/png" });
      res.end(
        await readFile(join(root, "src/src/frontend/public/sc logo.png")),
      );
      return;
    }
    res.writeHead(200, { "Content-Type": "text/html" });
    res.end(
      await readFile(
        join(
          path.startsWith("/admin") || url.searchParams.get("admin") === "1"
            ? adminDist
            : userDist,
          "index.html",
        ),
      ),
    );
  } catch (error) {
    json(res, 500, { error: error.message });
  }
});
await new Promise((done) => server.listen(0, "127.0.0.1", done));
const origin = `http://127.0.0.1:${server.address().port}`;
const profile = await mkdtemp(join(tmpdir(), "schakker-ui-"));
const browser = spawn(
  browserPath,
  [
    "--headless=new",
    "--disable-gpu",
    "--no-first-run",
    "--no-default-browser-check",
    "--remote-debugging-port=0",
    `--user-data-dir=${profile}`,
    "about:blank",
  ],
  { stdio: ["ignore", "ignore", "pipe"], windowsHide: true },
);
let browserStartup = "";
browser.stderr.on("data", (chunk) => { browserStartup = (browserStartup + chunk).slice(-4000); });
let socket;
const results = [];
const browserErrors = [];
const delay = (ms) => new Promise((done) => setTimeout(done, ms));
try {
  const portFile = join(profile, "DevToolsActivePort");
  for (let i = 0; !existsSync(portFile) && i < 300 && browser.exitCode == null; i++) await delay(100);
  assert(existsSync(portFile), `Browser did not start: ${browserStartup}`);
  const port = (await readFile(portFile, "utf8")).split("\n")[0];
  const target = await fetch(`http://127.0.0.1:${port}/json/new?about:blank`, {
    method: "PUT",
  }).then((response) => response.json());
  socket = new WebSocket(target.webSocketDebuggerUrl);
  await new Promise((done, reject) => {
    socket.addEventListener("open", done, { once: true });
    socket.addEventListener("error", reject, { once: true });
  });
  let sequence = 0;
  const pending = new Map();
  socket.addEventListener("message", ({ data }) => {
    const message = JSON.parse(data);
    if (message.id) {
      const operation = pending.get(message.id);
      pending.delete(message.id);
      if (message.error)
        operation.reject(new Error(JSON.stringify(message.error)));
      else operation.resolve(message.result);
    }
    if (message.method === "Runtime.exceptionThrown")
      browserErrors.push(message.params.exceptionDetails.text);
    if (
      message.method === "Runtime.consoleAPICalled" &&
      message.params.type === "error"
    )
      browserErrors.push(
        message.params.args
          .map((arg) => arg.description || arg.value)
          .join(" "),
      );
  });
  const cdp = (method, params = {}) =>
    new Promise((resolve, reject) => {
      const id = ++sequence;
      pending.set(id, { resolve, reject });
      socket.send(JSON.stringify({ id, method, params }));
    });
  const evaluate = async (expression) => {
    const result = await cdp("Runtime.evaluate", {
      expression,
      awaitPromise: true,
      returnByValue: true,
    });
    if (result.exceptionDetails)
      throw new Error(
        result.exceptionDetails.exception?.description ||
          result.exceptionDetails.text,
      );
    return result.result.value;
  };
  const wait = async (expression, message = expression) => {
    for (let i = 0; i < 80; i++) {
      if (await evaluate(expression)) return;
      await delay(100);
    }
    await writeFile(join(evidence, "timeout.json"), JSON.stringify({
      expected: message,
      page: await evaluate("({url: location.href, text: document.body?.innerText})"),
      errors: browserErrors,
    }, null, 2));
    throw new Error(`Timed out: ${message}`);
  };
  const text = (value) =>
    `!!document.body?.innerText.includes(${JSON.stringify(value)})`;
  const navigate = async (path, expected) => {
    await cdp("Page.navigate", { url: origin + path });
    await wait(text(expected));
  };
  const click = async (value) =>
    evaluate(
      `(() => { const el = [...document.querySelectorAll('button, a, summary')].find(el => el.textContent.trim() === ${JSON.stringify(value)} && el.getClientRects().length); if (!el) throw new Error('Missing control: ' + ${JSON.stringify(value)}); if(el.disabled) throw new Error('Disabled control: ' + ${JSON.stringify(value)}); el.click(); })()`,
    );
  const field = async (label, value) =>
    evaluate(
      `(() => { const label = [...document.querySelectorAll('label')].find(el => el.querySelector('span')?.textContent === ${JSON.stringify(label)}); const el = label?.querySelector('input,textarea,select'); if (!el) throw new Error('Missing field'); Object.getOwnPropertyDescriptor(Object.getPrototypeOf(el), 'value').set.call(el, ${JSON.stringify(value)}); el.dispatchEvent(new Event(el.tagName === 'SELECT' ? 'change' : 'input', {bubbles:true})); })()`,
    );
  const check = async (label) =>
    evaluate(
      `(() => { const el = [...document.querySelectorAll('label')].find(el => el.textContent.includes(${JSON.stringify(label)}))?.querySelector('input[type=checkbox]'); if (!el || el.disabled) throw new Error('Missing/enabled checkbox'); el.click(); })()`,
    );
  const viewport = (width, height) =>
    cdp("Emulation.setDeviceMetricsOverride", {
      width,
      height,
      deviceScaleFactor: 1,
      mobile: false,
    });
  const overflow = async () =>
    assert.equal(
      await evaluate("document.documentElement.scrollWidth <= innerWidth + 1"),
      true,
      `Page overflow: ${await evaluate("document.documentElement.scrollWidth + ' > ' + innerWidth")}`,
    );
  const screenshot = async (name) => {
    const result = await cdp("Page.captureScreenshot", {
      format: "png",
      captureBeyondViewport: false,
    });
    await writeFile(
      join(evidence, `${name}.png`),
      Buffer.from(result.data, "base64"),
    );
  };
  async function test(name, action) {
    await action();
    results.push({ name, passed: true });
    console.log(`PASS ${name}`);
  }
  await cdp("Page.enable");
  await cdp("Runtime.enable");
  await viewport(1440, 1000);

  await test("Application routes render in both SPAs", async () => {
    for (const [path, expected] of [
      ["/dashboard", "Welcome,"],
      ["/databases", "My Databases"],
      ["/explorer?db=7", "Select a schema with tables to inspect."],
      ["/sql-viewer?db=7", "SQL Viewer"],
      ["/migrations/new?db=7", "Create migration"],
      ["/migrations?db=7", "Changelog"],
      ["/admin/users", "Employees"],
      ["/admin/administrators", "Administrators"],
      ["/admin/connections", "Database Connections"],
      ["/admin/groups", "Designate leader"],
    ])
      await navigate(path, expected);
  });
  await test("Default Changelog and dashboard links use M2 history", async () => {
    const start = requests.length;
    await navigate("/migrations?db=7", "Migration execution history");
    await wait("document.querySelector('tbody')?.children.length === 3");
    assert(requests.slice(start).some((request) => request.path === "/api/migrations/history"));
    assert.equal(requests.slice(start).some((request) => request.path === "/api/migrations"), false);
    await navigate("/dashboard", "Latest committed executions across your databases.");
    await wait(text("Add demo flag"));
    await click("Add demo flag");
    await wait(text("Execution #3"));
    assert.equal(await evaluate("new URLSearchParams(location.search).get('execution')"), "3");
    assert.equal(await evaluate("document.querySelector('h1').textContent"), "Changelog");
  });
  await test("Failed rollback stays in history and retries its existing approved job", async () => {
    const savedHistory = state.history;
    const savedJobs = state.jobs;
    try {
      state.history = [...savedHistory, {
        ...makeEvent(4, "rollback", 3),
        id: "failed-down",
        status: "failed",
        is_committed: false,
        committed_at: null,
        can_rollback: false,
        can_retry: true,
        error: "database_rejected",
        job_id: "qna-failed-job",
      }];
      state.jobs = [{ ...makeJob(), job_id: "qna-failed-job", status: "failed", retryable: true }];
      await navigate("/migrations?db=7&hide_rollback=true", "Migration execution history");
      await wait("document.querySelector('tbody')?.children.length === 2");
      await click("#failed-down · Add demo flag");
      await wait(text("Retry approved job"));
      assert.equal(await evaluate("[...document.querySelectorAll('button')].some(el => el.textContent.trim() === 'Request rollback review')"), false);
      await check("Retry approved job for execution");
      await click("Retry approved job");
      await wait("new URLSearchParams(location.search).get('job') === 'qna-failed-job'");
      assert(requests.some(request => request.path === "/api/migration-jobs/qna-failed-job/retry" && request.method === "POST"));
      await navigate("/dashboard", "Welcome,");
    } finally {
      state.history = savedHistory;
      state.jobs = savedJobs;
    }
  });
  await test("Draft requires SQL down or irreversibility reason; batch settings persist", async () => {
    await navigate("/migrations?view=definitions&db=7", "New migration draft");
    await field("Migration title", "Batch synthetic update");
    await field("SQL up", "UPDATE demo SET enabled = true;");
    assert.equal(
      await evaluate("document.querySelector('form').checkValidity()"),
      false,
    );
    await check("This migration is irreversible");
    assert.equal(
      await evaluate("document.querySelector('form').checkValidity()"),
      false,
    );
    await field("Why is this migration irreversible?", "Synthetic reason");
    assert.equal(
      await evaluate("document.querySelector('form').checkValidity()"),
      true,
    );
    await check("This migration is irreversible");
    await field("SQL down", "UPDATE demo SET enabled = false;");
    await check("Use a PostgreSQL batch UPDATE");
    await field("Table", "demo");
    await field("Rows per batch", "500");
    await click("Save draft");
    await wait(text("Definition #2"));
    const save = requests.findLast(
      (request) =>
        request.path === "/api/migration-definitions" &&
        request.method === "POST",
    );
    const batch = JSON.parse(save.body.up_sql.split("\n")[0].slice("-- schakker:batch ".length));
    assert.equal(batch.size, 500);
    assert.equal(batch.column, "value");
    assert.equal(batch.delta, 1);
    assert(save.body.down_sql.includes('"value" - $1'));
    assert.equal(save.csrf, "synthetic-csrf");
  });
  await test("Draft edits use optimistic revision and block unsaved submission", async () => {
    await field("Amount to add per row", "2");
    assert.equal(
      await evaluate(
        "[...document.querySelectorAll('button')].find(el => el.textContent === 'Submit for approval').disabled",
      ),
      true,
    );
    await click("Save draft");
    await wait(text("Revision 2 ·"));
    assert.equal(
      requests.findLast((request) => request.method === "PATCH").body
        .expected_revision_id,
      102,
    );
    await check("Freeze revision");
    await click("Submit for approval");
    await wait(text("Review #1"));
    assert.equal(state.definitions[1].editable, false);
  });
  await test("Both access groups require explicit leader decisions, including View Only", async () => {
    await check("as leader of Editors");
    await click("Record approval for Editors");
    await wait(text("1 / 2 group approvals"));
    assert.equal(await evaluate(text("Queue approved migration")), false);
    await check("as leader of Auditors");
    await click("Record approval for Auditors");
    await wait(text("2 / 2 group approvals"));
    assert.equal(state.reviews[0].status, "approved");
    await check("Queue the approved");
    await click("Queue approved migration");
    await wait(text("Job details"));
    await wait(text("demo-job-1"));
    assert(
      requests.findLast((request) => request.path.endsWith("/enqueue")).key,
    );
  });
  await test("Partial jobs preserve committed progress; resume uses an idempotent API action", async () => {
    Object.assign(state.jobs[0], {
      status: "partial",
      committed_batches: 2,
      committed_rows: 2000,
      last_cursor: "2000",
      retryable: true,
      can_cancel: true,
      error: "Synthetic interruption after batch 2",
    });
    await navigate(
      "/migrations?view=jobs&db=7&job=demo-job-1",
      "Committed batches remain applied",
    );
    await overflow();
    await screenshot("user-partial-desktop");
    await click("Resume / retry from checkpoint");
    await wait(text("running"));
    assert.equal(state.jobs[0].committed_rows, 2000);
    assert(requests.findLast((request) => request.path.endsWith("/retry")).key);
    Object.assign(state.jobs[0], {
      status: "succeeded",
      completed_at: date,
      committed_batches: 5,
      committed_rows: 5000,
    });
    await wait(text("succeeded"));
    const count = jobReads.length;
    await delay(2300);
    assert.equal(jobReads.length, count);
  });
  await test("Batch cancellation requests a fresh review", async () => {
    Object.assign(state.jobs[0], { status: "partial", can_cancel: true });
    await navigate(
      "/migrations?view=jobs&db=7&job=demo-job-1",
      "Request cancellation review",
    );
    await check("Request approval to compensate");
    await click("Request cancellation review");
    await wait(text("Review #2"));
    assert.equal(state.reviews[1].action, "cancel_batch");
  });
  await test("Hide rollback uses exact execution pairs and preserves a later forward", async () => {
    await navigate(
      "/migrations?view=history&db=7",
      "Migration execution history",
    );
    assert.equal(
      await evaluate("document.querySelector('tbody').children.length"),
      3,
    );
    await check("Hide rollback");
    await wait("document.querySelector('tbody')?.children.length === 1");
    assert.equal(
      await evaluate(
        "document.querySelector('tbody').innerText.includes('#3')",
      ),
      true,
    );
  });
  await test("Rollback and forward each create a new review for the exact execution", async () => {
    await navigate(
      "/migrations?view=history&db=7&execution=3",
      "Request rollback review",
    );
    await check("Request review for execution");
    await click("Request rollback review");
    await wait(text("Review #3"));
    assert.equal(state.reviews[2].target_execution_id, "3");
    state.history[0].can_forward = true;
    await navigate(
      "/migrations?view=history&db=7&execution=1",
      "Request forward review",
    );
    await check("Request review for execution");
    await click("Request forward review");
    await wait(text("Review #4"));
    assert.equal(state.reviews[3].action, "forward");
    state.history[0].can_forward = false;
  });
  await test("403, 409, 422, 503, loading, empty and missing resources have usable states", async () => {
    for (const status of [403, 409, 422, 503]) {
      state.failure = { path: "/api/migration-definitions", status };
      await navigate(
        "/migrations?view=definitions&db=7",
        `Synthetic ${status} response`,
      );
      assert.equal(
        await evaluate("!!document.querySelector('[role=alert]')"),
        true,
      );
    }
    state.failure = null;
    state.delay = 300;
    await navigate("/migrations?view=definitions&db=7", "Loading migrations");
    await wait(text("New migration draft"));
    state.delay = 0;
    const saved = state.definitions;
    state.definitions = [];
    await navigate(
      "/migrations?view=definitions&db=7",
      "No migration definitions yet",
    );
    state.definitions = saved;
    await navigate(
      "/migrations?view=jobs&db=7&job=missing",
      "Job does not exist",
    );
    state.noDatabases = true;
    await navigate(
      "/migrations?view=definitions",
      "No database access is assigned",
    );
    state.noDatabases = false;
  });
  await test("View Only users can inspect and decide as leaders but cannot submit or enqueue", async () => {
    state.permission = "view_only";
    await navigate("/migrations?view=definitions&db=7", "View Only access");
    assert.equal(
      await evaluate(
        "document.querySelector('input:not([type=hidden])').matches(':disabled')",
      ),
      true,
    );
    assert.equal(await evaluate(text("Save draft")), false);
    const review = makeReview(50, definition);
    state.reviews.push(review);
    await navigate(
      "/migrations?view=reviews&db=7&review=50",
      "Record approval for Auditors",
    );
    state.permission = "view_edit";
  });
  await test("Rejected and invalidated reviews can request a fresh approval cycle", async () => {
    for (const status of ["rejected", "invalidated"]) {
      const review = {
        ...makeReview(60, definition),
        status,
        reason: "Synthetic authority/content change",
        can_resubmit: true,
      };
      state.reviews.push(review);
      await navigate(
        "/migrations?view=reviews&db=7&review=60",
        "Request fresh review",
      );
      const nextID = state.reviews.length + 1;
      await click("Request fresh review");
      await wait(text(`Review #${nextID}`));
      assert.equal(state.reviews.at(-1).status, "pending_review");
      state.reviews = state.reviews.filter((item) => item.id !== 60);
    }
  });
  await test("Frozen SQL can be copied into a separate draft without altering its revision", async () => {
    await navigate(
      "/migrations?view=definitions&db=7&definition=2",
      "Copy to new draft",
    );
    await click("Copy to new draft");
    await wait(text("Creating a new draft from definition #2"));
    await field("Migration title", "Separate copied draft");
    await click("Save draft");
    await wait(text("Definition #3"));
    assert.equal(state.definitions[1].status, "submitted");
    assert.equal(state.definitions[2].status, "draft");
  });
  await test("A missing leader blocks enqueue even when an inconsistent server flag permits it", async () => {
    const review = {
      ...makeReview(70, definition),
      status: "approved",
      can_enqueue: true,
    };
    review.required_groups = review.required_groups.map((group) => ({
      ...group,
      leader_user_id: null,
      leader_name: null,
      decision: "approved",
    }));
    state.reviews.push(review);
    await navigate(
      "/migrations?view=reviews&db=7&review=70",
      "Missing active leader",
    );
    assert.equal(
      await evaluate(
        "[...document.querySelectorAll('button')].find(el=>el.textContent==='Queue approved migration').disabled",
      ),
      true,
    );
  });
  await test("Stale mutation errors retain unsaved SQL and title", async () => {
    await navigate(
      "/migrations?view=definitions&db=7&definition=3",
      "SQL down",
    );
    await field("Migration title", "Keep unsaved title");
    state.failure = {
      path: "/api/migration-definitions/3",
      method: "PATCH",
      status: 409,
    };
    await click("Save draft");
    await wait(text("Synthetic 409 response"));
    assert.equal(
      await evaluate("document.querySelector('form input').value"),
      "Keep unsaved title",
    );
    state.failure = null;
  });
  await test("Compensation, failure and uncertain job states remain visible; leaving stops polling", async () => {
    for (const status of [
      "failed",
      "uncertain",
      "reverting",
      "reverted",
      "partially_reverted",
    ]) {
      Object.assign(state.jobs[0], {
        status,
        reverted_batches: 1,
        reverted_rows: 1000,
        retryable: false,
        can_cancel: false,
      });
      await navigate(
        "/migrations?view=jobs&db=7&job=demo-job-1",
        status.replaceAll("_", " "),
      );
      await wait(text("Reverted batches / rows"));
    }
    Object.assign(state.jobs[0], { status: "running" });
    await navigate("/migrations?view=jobs&db=7&job=demo-job-1", "running");
    await navigate("/databases", "My Databases");
    const count = jobReads.length;
    await delay(2300);
    assert.equal(jobReads.length, count);
  });
  await test("Responsive layouts and internal scrolling at 1440, 390 and 375 pixels", async () => {
    for (const [width, height] of [
      [1440, 1000],
      [390, 844],
      [375, 812],
    ]) {
      await viewport(width, height);
      await navigate(
        "/migrations?view=definitions&db=7&definition=2",
        "This revision is frozen",
      );
      await overflow();
      await screenshot(`user-${width}`);
      for (const view of ["reviews", "jobs", "history"]) {
        await navigate(
          `/migrations?view=${view}&db=7`,
          view === "reviews"
            ? "Review #"
            : view === "jobs"
              ? "Job activity"
              : "Migration execution history",
        );
        await overflow();
      }
      for (const [path, expected] of [
        ["/dashboard", "Welcome,"],
        ["/databases", "My Databases"],
        ["/explorer?db=7", "Select a schema with tables to inspect."],
        ["/sql-viewer?db=7", "SQL Viewer"],
        ["/migrations/new?db=7", "Create migration"],
        ["/migrations?db=7", "Changelog"],
        ["/admin/users", "Employees"],
        ["/admin/administrators", "Administrators"],
        ["/admin/connections", "Database Connections"],
      ]) {
        await navigate(path, expected);
        await overflow();
      }
      await navigate("/admin/groups", "Designate leader");
      await overflow();
      await screenshot(`admin-${width}`);
      if (width < 960) {
        await evaluate(
          "document.querySelector('[aria-label=\"Open navigation\"]').click()",
        );
        assert.equal(
          await evaluate("document.body.classList.contains('sidebar-is-open')"),
          true,
        );
        await evaluate(
          "document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}))",
        );
      }
    }
  });
  await test("Admin leader selection excludes inactive members and surfaces backend validation", async () => {
    await viewport(1440, 1000);
    await navigate("/admin/groups", "Designate leader");
    await click("Designate leader");
    assert.equal(
      await evaluate(
        "document.querySelector('select[name=leader_user_id]').options.length",
      ),
      3,
    );
    await field("Leader for Editors", "2");
    state.failure = { path: "/api/admin/groups", method: "POST", status: 422 };
    await click("Save leader");
    await wait(text("Synthetic 422 response"));
    state.failure = null;
    await click("Save leader");
    await wait(
      "[...document.querySelectorAll('.group-members p')].some(el=>el.textContent.includes('Demo Member'))",
    );
    assert.equal(
      requests.findLast((request) => request.body.action === "set_leader").body
        .leader_user_id,
      "2",
    );
  });
  await test("Login and logout remain usable in both SPAs", async () => {
    for (const admin of [false, true]) {
      state.loggedOut = true;
      await navigate(admin ? "/login?admin=1" : "/login", "Username");
      await field("Username", "demo.leader");
      await field("Password", "SyntheticPassword123!");
      await evaluate("document.querySelector('form').requestSubmit()");
      await wait(text(admin ? "Employees" : "Welcome,"));
      await evaluate("document.querySelector('[aria-label=Logout]').click()");
      await wait(text("Username"));
    }
    state.loggedOut = false;
  });
  await test("SQL analysis carries SQL and title into a reviewed draft", async () => {
    await navigate("/migrations/new?db=7", "Create migration");
    await field("Description / draft title", "From analysis");
    await evaluate(`(() => {
      const input = document.querySelector('textarea[aria-label="SQL migration"]');
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set.call(input, 'ALTER TABLE demo ADD reviewed boolean;');
      input.dispatchEvent(new Event('input', { bubbles: true }));
    })()`);
    assert.equal(await evaluate(text("Execute migration")), false);
    await click("Up/down approval workflow");
    await wait(text("New migration draft"));
    await wait(`document.querySelector('textarea')?.value === 'ALTER TABLE demo ADD reviewed boolean;'`);
    assert.equal(await evaluate(`document.querySelector('input:not([type=hidden])').value`), "From analysis");
  });
  await test("No browser runtime errors or direct migration execution from new screens", async () => {
    assert.deepEqual(browserErrors, []);
    assert.equal(
      requests.some((request) => request.body.action === "execute"),
      false,
    );
  });
  await writeFile(
    join(evidence, "report.json"),
    JSON.stringify(
      {
        browser: await cdp("Browser.getVersion"),
        scope:
          "Synthetic API fixture UI checks; backend authorization and execution are not exercised.",
        viewport_widths: [1440, 390, 375],
        results,
      },
      null,
      2,
    ) + "\n",
  );
  console.log(
    `${results.length} UI checks passed. Screenshots and report: ${evidence}`,
  );
} finally {
  socket?.close();
  browser.kill();
  await new Promise((done) => server.close(done));
  // Only remove the unique browser profile created by this script under tmpdir.
  if (
    profile.startsWith(resolve(tmpdir()) + sep) &&
    basename(profile).startsWith("schakker-ui-")
  ) {
    await delay(800);
    await rm(profile, {
      recursive: true,
      force: true,
      maxRetries: 5,
      retryDelay: 300,
    }).catch(() => {});
  }
}
