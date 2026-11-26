import assert from "node:assert/strict";
import { test } from "node:test";
import { renderToStaticMarkup as render } from "react-dom/server";
import { MemoryRouter } from "react-router-dom";
import { PageContext } from "../../../apps/user/frontend/app/ui";
import { Dashboard, Databases, Explorer } from "../../../apps/user/frontend/app/workspace";
import { SQLViewer } from "../../../apps/user/frontend/app/SQLViewer";
import { MigrationEditor } from "../../../apps/user/frontend/app/MigrationEditor";
import { migrationView } from "../../../apps/user/frontend/app/MigrationWorkspace";
const database = { ID: 7, Name: "Employees DB", DBMS: "postgresql", DatabaseName: "employees", Permission: "view_only" };
const data = { Title: "Workspace", User: { DisplayName: "Ada Lovelace" }, CSRF: "csrf", Expires: "2026-11-11T11:00:00Z", Databases: [database], Database: database, CanEdit: false, EditableCount: 0, Recent: [], SQL: "", Description: "", Schemas: [{ Name: "public" }], Tables: [], Schema: "public", Table: "", Detail: null } as any;
function page(component: any, overrides = {}, path = "/dashboard") {
  return render(<MemoryRouter initialEntries={[path]}><PageContext.Provider value={{ data: { ...data, ...overrides }, busy: false, analysisState: { loading: false, error: "" }, submit: async () => true, refresh() {}, expire() {}, acceptCSRF() {} }}>{component}</PageContext.Provider></MemoryRouter>);
}
test("dashboard recent executions link to the exact database and execution", () => {
  const html = page(<Dashboard />, { Recent: [{ id: "execution/7", database_id: 7, database_name: "Employees DB", title: "Add name", actor: "editor", operation: "up_batch", committed_at: "2026-11-11T03:00:00Z" }] });
  assert(html.includes("Welcome, Ada Lovelace.")); assert(html.includes("execution=execution%2F7")); assert(html.includes("db=7")); assert(html.includes("up batch")); assert(html.includes("View Only"));
});
test("database and dashboard empty states explain missing access", () => {
  assert(page(<Databases />, { Databases: [] }).includes("No databases assigned"));
  assert(page(<Dashboard />, { Databases: [], Recent: [] }).includes("No migration history yet"));
});
test("view-only explorer shows data navigation and omits migration creation", () => {
  const html = page(<Explorer />);
  assert(html.includes("Refresh database")); assert(html.includes("Changelog")); assert.equal(html.includes(">New migration<"), false);
  assert(page(<Explorer />, { CanEdit: true }).includes(">New migration<"));
});
test("SQL Viewer exposes a read-only editor without inventing table metadata", () => {
  const html = page(<SQLViewer />);
  assert(html.includes("Read-only SQL query")); assert(html.includes("Run read query")); assert.equal(html.includes("Visual SELECT builder"), false);
  const empty = page(<SQLViewer />, { Database: { ID: 0 }, Databases: [] });
  assert(empty.includes("No databases assigned")); assert.equal(empty.includes("Run read query"), false);
});
test("migration editor limits saved execution workflow to PostgreSQL", () => {
  assert(page(<MigrationEditor />).includes("Up/down approval workflow"));
  assert.equal(page(<MigrationEditor />, { Database: { ...database, DBMS: "mariadb" } }).includes("Up/down approval workflow"), false);
  const empty = page(<MigrationEditor />, { Database: { ID: 0 }, Databases: [] });
  assert(empty.includes("You need View + Edit access")); assert(empty.includes("View changelog"));
});
test("unknown and missing migration views fall back to execution history", () => {
  for (const value of [null, "", "old-history", "../jobs"]) assert.equal(migrationView(value), "history");
  for (const value of ["history", "definitions", "reviews", "jobs"]) assert.equal(migrationView(value), value);
});
