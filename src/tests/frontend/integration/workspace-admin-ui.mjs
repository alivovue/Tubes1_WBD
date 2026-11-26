import assert from "node:assert/strict";

const detail = {
  Table: { Name: "employees", Schema: "public", Kind: "table" },
  Columns: [
    { Name: "id", DataType: "bigint", Nullable: false, Default: null },
    { Name: "name", DataType: "text", Nullable: true, Default: null },
    { Name: "salary", DataType: "integer", Nullable: false, Default: "0" },
  ],
  Constraints: [{ Name: "employees_pkey", Type: "p", Definition: "PRIMARY KEY (id)" }],
  Indexes: [{ Name: "employees_salary_idx", Method: "btree", Unique: false, Definition: "CREATE INDEX employees_salary_idx ON employees (salary)" }],
};
const catalog = {
  Schemas: [{ Name: "public" }, { Name: "reporting" }],
  Tables: [{ Name: "employees", Type: "BASE TABLE" }, { Name: "departments", Type: "BASE TABLE" }],
  Schema: "public", Table: "employees", Detail: detail,
  Data: { Columns: detail.Columns, Rows: [["9007199254740993", "Alice", null], ["2", "<script>fixture()</script>", "6000"]] },
  HasMore: true,
};
const plans = [
  { DBMS: "postgresql", Statement: "SELECT name FROM employees", Available: true, Nodes: [{ Operation: "Sort", EstimatedRows: 2, StartupCost: 0, TotalCost: 12.5, Children: [{ Operation: "Seq Scan", Relation: "employees", EstimatedRows: 2, TotalCost: 10 }] }], JSON: { Plan: { "Node Type": "Sort", Plans: [{ "Node Type": "Seq Scan" }] } } },
  { DBMS: "postgresql", Statement: "ALTER TABLE employees ADD note text", Available: false, UnavailableReason: "unsupported_statement" },
];

export async function runFeatureChecks(h) {
  const { test, state, requests, page, navigate, evaluate, wait, text, click, field, check, viewport, delay } = h;
  const set = (selector, value, index = 0) => evaluate(`(() => {
    const el = document.querySelectorAll(${JSON.stringify(selector)})[${index}];
    if (!el) throw new Error('Missing input: ' + ${JSON.stringify(selector)});
    Object.getOwnPropertyDescriptor(Object.getPrototypeOf(el), 'value').set.call(el, ${JSON.stringify(value)});
    el.dispatchEvent(new Event(el.tagName === 'SELECT' ? 'change' : 'input', { bubbles: true }));
  })()`);
  const submit = selector => evaluate(`document.querySelector(${JSON.stringify(selector)}).requestSubmit()`);
  const untilRequest = (start, predicate) => {
    const candidates = requests.slice(start).filter(predicate);
    assert(candidates.length, "Expected a matching application API request");
    return candidates.at(-1);
  };
  async function scoped(overrides, action) {
    const saved = { pageData: state.pageData, respond: state.respond, permission: state.permission, failure: state.failure };
    Object.assign(state, overrides);
    try { await action(); } finally { Object.assign(state, saved); }
  }

  await test("Database cards filter by name and show an empty search state", async () => {
    await navigate("/databases", "My Databases");
    await field("Search databases", "SECOND");
    assert.equal(await evaluate("document.querySelectorAll('.database-card').length"), 1);
    await field("Search databases", "no-match");
    await wait(text("No matching databases."));
    await field("Search databases", "");
    assert.equal(await evaluate("document.querySelectorAll('.database-card').length"), 2);
  });
  await test("Explorer displays columns constraints indexes rows and preserves sorting during pagination", async () => {
    await scoped({ pageData: catalog }, async () => {
      const start = requests.length;
      state.respond = (req, url) => {
        if (req.method !== "GET" || url.pathname !== "/api/explorer") return;
        const offset = Number(url.searchParams.get("offset") || 0);
        return { body: { ...page(), Offset: offset, Previous: Math.max(0, offset - 50), Next: offset + 50, HasMore: offset === 0, Sort: url.searchParams.get("sort") || "", Direction: url.searchParams.get("direction") || "asc" } };
      };
      await navigate("/explorer?db=7", "employees");
      await wait(text("bigint"));
      await click("Constraints"); await wait(text("employees_pkey"));
      await click("Indexes"); await wait(text("employees_salary_idx"));
      await click("Data"); await wait(text("9007199254740993"));
      assert(await evaluate("document.body.innerText.includes('NULL')"));
      assert.equal(await evaluate("document.querySelectorAll('main script').length"), 0);
      await field("Sort column", "salary"); await field("Direction", "desc"); await click("Apply");
      await wait("new URLSearchParams(location.search).get('sort') === 'salary' && !document.querySelector('.empty-state[role=status]')");
      await wait("[...document.querySelectorAll('.pagination a')].some(a => a.textContent.trim() === 'Next')");
      await click("Next"); await wait(text("Offset 50"));
      const req = untilRequest(start, request => request.path === "/api/explorer" && request.query.offset === "50");
      assert.equal(req.query.sort, "salary"); assert.equal(req.query.direction, "desc"); assert.equal(await evaluate("new URLSearchParams(location.search).get('tab')"), "rows-panel");
      await click("Previous"); await wait(text("Offset 0"));
      const beforeRefresh = requests.length;
      await click("Refresh database"); await wait("!document.querySelector('.empty-state[role=status]')");
      assert(requests.slice(beforeRefresh).some(request => request.path === "/api/explorer"));
    });
  });
  await test("SQL Viewer submits typed array filters, renders safe results, and marks edited SQL stale", async () => {
    await scoped({ pageData: catalog }, async () => {
      const generated = 'SELECT "name" FROM "public"."employees" WHERE "salary" >= 6000 ORDER BY "salary" DESC LIMIT 10 OFFSET 0;';
      state.respond = (req, url, body) => {
        if (req.method !== "POST" || url.pathname !== "/api/sql-viewer") return;
        return { body: { ...page(), SQL: body.action === "build_query" ? generated : body.sql, ViewerResult: body.action === "run_query" ? { Columns: [{ Name: "name", DataType: "text" }], Rows: [[null], ["<script>fixture()</script>"], ["9007199254740993"]], Duration: 1000000, HasMore: false } : null } };
      };
      await navigate("/sql-viewer?db=7", "Visual SELECT builder");
      const start = requests.length;
      await check("name"); await check("DISTINCT"); await click("Add filter");
      await field("Column", "salary"); await field("Operator", ">="); await field("Value type", "number"); await field("Value", "6000");
      await field("Sort column", "salary"); await field("Direction", "desc"); await field("Limit", "10");
      await click("Generate SELECT in editor");
      await wait(`document.querySelector('textarea').value === ${JSON.stringify(generated)}`);
      const build = untilRequest(start, request => request.body.action === "build_query");
      for (const [key, value] of Object.entries({ columns: ["name"], filter_column: ["salary"], filter_operator: [">="], filter_type: ["number"], filter_value: ["6000"], distinct: "on", sort: "salary", direction: "desc", limit: "10" })) assert.deepEqual(build.body[key], value);
      await click("Run read query"); await wait(text("Query results"));
      await wait(text("9007199254740993")); assert(await evaluate("document.body.innerText.includes('NULL')"));
      assert.equal(await evaluate("document.querySelectorAll('main script').length"), 0);
      await set("textarea[name=sql]", "SELECT name FROM employees;"); await wait(text("SQL changed. Run the query again"));
      const beforeKeyboard = requests.length;
      await evaluate("document.querySelector('textarea').dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', ctrlKey: true, bubbles: true }))");
      await wait("!document.querySelector('main [aria-busy=true]')");
      assert.equal(untilRequest(beforeKeyboard, request => request.body.action === "run_query").body.sql, "SELECT name FROM employees;");
    });
  });
  await test("SQL Viewer null filters need no value and the filter count stays bounded", async () => {
    await scoped({ pageData: catalog }, async () => {
      await navigate("/sql-viewer?db=7", "Visual SELECT builder");
      await click("Add filter"); await field("Operator", "IS NULL");
      assert.equal(await evaluate("document.querySelector('[name=filter_value]').readOnly"), true);
      await click("Remove");
      assert.equal(await evaluate("document.querySelectorAll('.viewer-filter-row').length"), 0);
      for (let index = 0; index < 20; index++) await click("Add filter");
      assert.equal(await evaluate("[...document.querySelectorAll('button')].find(b => b.textContent.trim() === 'Add filter').disabled"), true);
      assert.equal(await evaluate("document.querySelectorAll('.viewer-filter-row').length"), 20);
    });
  });
  await test("SQL form collection preserves repeated column and filter order with the clicked action", async () => {
    await scoped({ pageData: catalog }, async () => {
      await navigate("/sql-viewer?db=7", "Visual SELECT builder");
      await check("id"); await check("name");
      for (let index = 0; index < 2; index++) await click("Add filter");
      for (const [index, column, operator, value] of [[0, "salary", ">=", "5000"], [1, "id", "<=", "10"]]) {
        await set(".viewer-filter-row [name=filter_column]", column, index);
        await set(".viewer-filter-row [name=filter_operator]", operator, index);
        await set(".viewer-filter-row [name=filter_type]", "number", index);
        await set(".viewer-filter-row [name=filter_value]", value, index);
      }
      const start = requests.length; await click("Generate SELECT in editor");
      await wait("!document.querySelector('main [aria-busy=true]')");
      const request = untilRequest(start, request => request.body.action === "build_query");
      assert.deepEqual(request.body.columns, ["id", "name"]);
      assert.deepEqual(request.body.filter_column, ["salary", "id"]);
      assert.deepEqual(request.body.filter_operator, [">=", "<="]);
      assert.deepEqual(request.body.filter_type, ["number", "number"]);
      assert.deepEqual(request.body.filter_value, ["5000", "10"]);
    });
  });
  await test("Visual migration builder validates, edits, reorders and serializes every operation kind", async () => {
    await navigate("/migrations/new?db=7", "Visual change set");
    await click("Add operation to change set"); await wait(text("Enter the schema and table name"));
    await field("Table", "new_table"); await field("Column name", "id"); await field("SQL type", "bigint"); await check("Primary key");
    await click("Add operation to change set");
    for (const kind of ["add_column", "alter_column", "drop_column"]) {
      await field("Operation", kind); await field("Column name", "note"); if (kind !== "drop_column") await field("SQL type", "text");
      await click("Add operation to change set");
    }
    for (const kind of ["add_index", "drop_index"]) {
      await field("Operation", kind); await field("Index name", "note_idx"); if (kind === "add_index") { await field("Columns (comma separated)", "note, id"); await check("Unique index"); }
      await click("Add operation to change set");
    }
    await field("Operation", "insert_row"); await field("Column", "name"); await field("Value", "New employee"); await click("Add operation to change set");
    await field("Operation", "update_row");
    await set(".builder-row input", "salary");
    await evaluate(`(() => {
      const rows = [...document.querySelectorAll('.builder-row')];
      const set = (el,value) => {Object.getOwnPropertyDescriptor(Object.getPrototypeOf(el),'value').set.call(el,value);el.dispatchEvent(new Event(el.tagName === 'SELECT' ? 'change' : 'input',{bubbles:true}));};
      set(rows[0].querySelectorAll('input')[1],'6000'); set(rows[0].querySelector('select'),'number');
      set(rows[1].querySelectorAll('input')[0],'id'); set(rows[1].querySelectorAll('input')[1],'1'); set(rows[1].querySelector('select'),'number');
    })()`);
    await click("Add operation to change set");
    await field("Operation", "drop_table"); await click("Add operation to change set");
    const operations = await evaluate("JSON.parse(document.querySelector('input[name=operations]').value)");
    assert.deepEqual(operations.map(op => op.Kind), ["create_table", "add_column", "alter_column", "drop_column", "add_index", "drop_index", "insert_row", "update_row", "drop_table"]);
    assert.equal(operations[0].Definition.Columns[0].Nullable, false); assert.deepEqual(operations[0].Definition.Constraints[0].Columns, ["id"]);
    assert.deepEqual(operations[7].Values, { salary: 6000 }); assert.deepEqual(operations[7].Where, { id: 1 });
    assert.equal(operations[4].Index.Unique, true); assert.deepEqual(operations[4].Index.Columns, [{ Name: "note" }, { Name: "id" }]);
    await evaluate("document.querySelector('.operation-list li button:nth-child(3)').click()");
    await wait(text("Save operation changes"));
    assert.equal(await evaluate("document.querySelector('[value=analyze_visual]').disabled"), true);
    await field("Table", "renamed_table"); await click("Save operation changes");
    await evaluate("document.querySelector('.operation-list li button:nth-child(2)').click()");
    await evaluate("document.querySelector('.operation-list li:last-child button:nth-child(4)').click()");
    const changed = await evaluate("JSON.parse(document.querySelector('input[name=operations]').value)");
    assert.equal(changed[1].Table, "renamed_table"); assert.equal(changed.length, 8);
    await field("Description", "All visual operations");
    const start = requests.length; await click("Analyze change set"); await wait("!document.querySelector('main [aria-busy=true]')");
    assert.deepEqual(JSON.parse(untilRequest(start, request => request.body.action === "analyze_visual").body.operations), changed);
  });
  await test("Visual row values reject unsafe precision and missing filters while retaining typed null and booleans", async () => {
    await navigate("/migrations/new?db=7", "Visual change set"); await field("Operation", "insert_row"); await field("Table", "employees");
    await field("Column", "id"); await field("Value type", "number"); await field("Value", "9007199254740993");
    await click("Add operation to change set"); await wait(text("exceeds browser precision"));
    await field("Value type", "text"); await click("Add operation to change set");
    await field("Column", "active"); await field("Value type", "boolean"); await field("Value", "not-boolean");
    await click("Add operation to change set"); await wait(text("Enter true or false"));
    await field("Value", " FALSE "); await click("Add operation to change set");
    await field("Column", "note"); await field("Value type", "null"); await click("Add operation to change set");
    const operations = await evaluate("JSON.parse(document.querySelector('[name=operations]').value)");
    assert.equal(operations[0].Values.id, "9007199254740993"); assert.equal(operations[1].Values.active, false); assert.equal(operations[2].Values.note, null);
    await field("Operation", "update_row"); await click("Add operation to change set"); await wait(text("Every value and filter needs a column name"));
    assert.equal(await evaluate("JSON.parse(document.querySelector('[name=operations]').value).length"), 3);
  });
  await test("Analyzed query plans expand collapse expose original JSON and become stale after SQL edits", async () => {
    await scoped({}, async () => {
      state.respond = (req, url, body) => req.method === "POST" && url.pathname === "/api/migrations/new" ? { body: { ...page(), SQL: body.sql, Description: body.description, Review: { SQL: body.sql, Plans: plans, BaseVersion: 0, Mode: "transactional", Warnings: ["Preview only"], SchemaDiffAvailable: true, SchemaDiff: { Destructive: true, Changes: [{ Kind: "add_column", Schema: "public", Table: "employees", Name: "note" }], DataChanges: [{ Kind: "update_row", Schema: "public", Table: "employees", Values: { salary: 6000 }, Where: { id: 1 } }] } }, SchemaDiff: { Added: 1, Removed: 1, Rows: [{ Before: { Kind: "removed", Line: 1, Marker: "−", Text: "old_column" }, After: { Kind: "added", Line: 1, Marker: "+", Text: "new_column" } }] } } } : undefined;
      await navigate("/migrations/new?db=7", "Create migration"); await field("Migration title", "Inspect plan"); await set("textarea[name=sql]", "SELECT name FROM employees;"); await click("Analyze migration");
      await wait(text("Plan available")); await wait(text("Seq Scan")); await wait(text("Unsupported statement"));
      assert(await evaluate("document.body.innerText.includes('old_column') && document.body.innerText.includes('new_column')"));
      assert(await evaluate("document.body.innerText.includes('destructive schema changes') && document.body.innerText.includes('Data-change summary')"));
      await click("Collapse all"); assert.equal(await evaluate("document.querySelector('.query-plan-tree__children').hidden"), true);
      await click("Expand all"); assert.equal(await evaluate("document.querySelector('.query-plan-tree__children').hidden"), false);
      await evaluate("document.querySelector('[aria-label=\"Collapse Sort\"]').click()"); assert.equal(await evaluate("document.querySelector('.query-plan-tree__children').hidden"), true);
      await click("Original EXPLAIN JSON"); await wait(text('"Node Type"'));
      await evaluate("Object.defineProperty(navigator, 'clipboard', { configurable:true, value:{writeText:async value => {window.copiedPlan=value}} })");
      await click("Copy JSON"); await wait(text("JSON copied.")); assert.equal(JSON.parse(await evaluate("window.copiedPlan")).Plan["Node Type"], "Sort");
      await evaluate("Object.defineProperty(navigator, 'clipboard', { configurable:true, value:undefined })"); await click("Copy JSON"); await wait(text("Copy unavailable"));
      await set("textarea[name=sql]", "SELECT id FROM employees;"); await wait(text("SQL changed. Analyze again"));
      assert.equal(await evaluate("document.querySelectorAll('.query-plan-statement').length"), 0);
    });
  });
  await test("Employee and administrator forms submit separate create and update actions", async () => {
    for (const administrator of [false, true]) {
      await navigate(administrator ? "/admin/administrators" : "/admin/users", administrator ? "Add administrator" : "Add employee");
      const start = requests.length;
      await set("form input[name=username]", "new.account"); await set("form input[name=display]", "New Account"); await set("form input[name=password]", "SyntheticPassword123!");
      await submit("form"); await wait("!document.querySelector('main [aria-busy=true]')");
      const create = untilRequest(start, request => request.method === "POST");
      assert.equal(create.body.action, administrator ? "create_admin" : "create_user"); assert.equal(create.csrf, "synthetic-csrf");
      await click("Edit account"); await set(".inline-editor [name=display]", "Renamed Account"); await set(".inline-editor [name=password]", "ChangedPassword123!");
      await evaluate("document.querySelector('.inline-editor [name=active]').click()"); await submit(".inline-editor"); await wait("!document.querySelector('main [aria-busy=true]')");
      const update = untilRequest(start, request => request.body.action === (administrator ? "update_admin" : "update_user"));
      assert.equal(update.body.display, "Renamed Account"); assert.equal(update.body.password, "ChangedPassword123!"); assert.equal(update.body.active, undefined); assert.equal(update.body.id, "1");
    }
  });
  await test("Admin connection forms preserve endpoint identity and confirm disabling", async () => {
    await navigate("/admin/connections", "Database Connections"); const start = requests.length;
    for (const [selector, value] of [["[name=name]", "New database"], ["[name=host]", "db.local"], ["[name=database_name]", "analytics"], ["[name=username]", "viewer"], ["[name=password]", "SyntheticPassword123!"]]) await set(`form ${selector}`, value);
    await evaluate("document.querySelector('form').requestSubmit(document.querySelector('form [value=save_connection]'))"); await wait("!document.querySelector('main [aria-busy=true]')");
    const created = untilRequest(start, request => request.body.action === "save_connection"); assert.equal(created.body.host, "db.local"); assert.equal(created.body.active, "on");
    await click("Connection settings");
    assert.equal(await evaluate("document.querySelector('.connection-card [name=host]').readOnly"), true);
    assert.equal(await evaluate("document.querySelector('.connection-card select[name=dbms]').disabled"), true);
    await set(".connection-card [name=name]", "Renamed database"); await evaluate("document.querySelector('.connection-card details form').requestSubmit(document.querySelector('.connection-card details [value=save_connection]'))"); await wait("!document.querySelector('main [aria-busy=true]')");
    const edited = untilRequest(start, request => request.body.name === "Renamed database"); assert.equal(edited.body.dbms, "postgresql"); assert.equal(edited.body.password, ""); assert.equal(edited.body.id, "7");
    await evaluate("window.confirm = () => false"); const before = requests.length; await click("Disable connection"); await delay(100); assert.equal(requests.length, before);
    await evaluate("window.confirm = () => true"); await click("Disable connection"); await wait("!document.querySelector('main [aria-busy=true]')");
    assert.equal(untilRequest(before, request => request.method === "POST").body.action, "disable_connection");
  });
  await test("Group forms submit membership grants information and confirmed deletion", async () => {
    await navigate("/admin/groups", "Designate leader"); let start = requests.length;
    await field("Name", "New group"); await field("Description", "New description"); await click("Create group"); await wait("!document.querySelector('main [aria-busy=true]')");
    assert.equal(untilRequest(start, request => request.method === "POST").body.action, "save_group");
    await click("Edit membership"); await field("Employee", "2"); await field("Membership", "no"); await click("Update membership"); await wait("!document.querySelector('main [aria-busy=true]')");
    const membership = untilRequest(start, request => request.body.action === "membership"); assert.equal(membership.body.user, "2"); assert.equal(membership.body.member, "no");
    await click("Edit access"); await set("select[name=permission]", ""); await click("Save access"); await wait("!document.querySelector('main [aria-busy=true]')");
    const grant = untilRequest(start, request => request.body.action === "set_grants"); assert.deepEqual(grant.body.db, ["7"]); assert.deepEqual(grant.body.permission, [""]);
    await click("Edit group"); await set("#group-information-1 input[name=name]", "Updated group"); await click("Save group"); await wait("!document.querySelector('main [aria-busy=true]')");
    assert(untilRequest(start, request => request.body.name === "Updated group").body.id === "1");
    await evaluate("window.confirm = () => false"); start = requests.length; await click("Delete group"); await delay(100); assert.equal(requests.length, start);
    await evaluate("window.confirm = () => true"); await click("Delete group"); await wait("!document.querySelector('main [aria-busy=true]')"); assert.equal(untilRequest(start, request => request.method === "POST").body.action, "delete_group");
  });
  await test("Mobile navigation closes on Escape and route changes in both applications", async () => {
    await viewport(390, 900);
    try { for (const admin of [false, true]) {
      await navigate(admin ? "/admin/users" : "/dashboard", admin ? "Employees" : "Welcome,");
      await evaluate("document.querySelector('[aria-label=\"Open navigation\"]').click()"); await wait("document.body.classList.contains('sidebar-is-open')");
      await evaluate("document.dispatchEvent(new KeyboardEvent('keydown', {key:'Escape',bubbles:true}))"); await wait("!document.body.classList.contains('sidebar-is-open')");
      await evaluate("document.querySelector('[aria-label=\"Open navigation\"]').click()"); await click(admin ? "Groups" : "My Databases"); await wait("!document.body.classList.contains('sidebar-is-open')");
      await wait("document.activeElement?.id === 'main-content'");
    } } finally { await viewport(1440, 1000); }
  });
}
