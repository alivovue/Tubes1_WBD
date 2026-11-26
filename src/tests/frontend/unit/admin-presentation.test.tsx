import assert from "node:assert/strict";
import { test } from "node:test";
import { renderToStaticMarkup as render } from "react-dom/server";
import { MemoryRouter } from "react-router-dom";
import { Connections, Groups, Users } from "../../../apps/admin/frontend/app/admin";
import { PageContext } from "../../../apps/admin/frontend/app/ui";
const employee = { ID: 1, Username: "employee-only", DisplayName: "Employee", Active: true };
const admin = { ID: 2, Username: "admin-only", DisplayName: "Administrator", Active: true };
const database = { ID: 7, Name: "Target", Host: "db.local", Port: 5432, DatabaseName: "app", DBMS: "postgresql", SSLMode: "require", Active: true };
const data = { Admin: { Users: [employee], Databases: [database] }, Administrators: [admin], DBMS: ["postgresql", "mariadb"], Groups: [] } as any;
function page(children: any, overrides = {}) {
  return render(<MemoryRouter><PageContext.Provider value={{ data: { ...data, ...overrides }, busy: false, submit: async () => true, refresh() {} }}>{children}</PageContext.Provider></MemoryRouter>);
}
test("ordinary and administrator account forms use distinct actions and account lists", () => {
  const employees = page(<Users />), admins = page(<Users administrators />);
  assert(employees.includes('value="create_user"')); assert(employees.includes('value="update_user"')); assert(employees.includes("employee-only")); assert.equal(employees.includes("admin-only"), false);
  assert(admins.includes('value="create_admin"')); assert(admins.includes('value="update_admin"')); assert(admins.includes("admin-only")); assert.equal(admins.includes("employee-only"), false);
  assert(admins.includes('minLength="12"')); assert(admins.includes('maxLength="72"'));
});
test("account empty states distinguish employees and administrators", () => {
  assert(page(<Users />, { Admin: { Users: [] } }).includes("No employee accounts"));
  assert(page(<Users administrators />, { Administrators: [] }).includes("No administrator accounts"));
});
test("saved connection identity is immutable and credential updates start blank", () => {
  const html = page(<Connections />);
  assert.match(html, /<input(?=[^>]*name="host")(?=[^>]*readOnly="")(?=[^>]*value="db.local")[^>]*>/);
  assert.match(html, /<input(?=[^>]*name="database_name")(?=[^>]*readOnly="")[^>]*>/);
  assert(html.includes("Username (blank keeps current)")); assert(html.includes("Password (blank keeps current)"));
  assert.equal(/name="password"[^>]*value=/.test(html), false);
  assert(html.includes('value="disable_connection"'));
});
test("disabled connections can be edited without offering another disable action", () => {
  const html = page(<Connections />, { Admin: { ...data.Admin, Databases: [{ ...database, Active: false }] } });
  assert(html.includes("Disabled")); assert.equal(html.includes('value="disable_connection"'), false);
});
test("group leaders must be active members and access selectors preserve explicit revocation", () => {
  const group = { ID: 3, Name: "Editors", LeaderUserID: 1, Members: [employee, { ...admin, Active: false }], Grants: [{ Database: database, Permission: "view_only" }], Editor: "leader" };
  const html = page(<Groups />, { Groups: [group] });
  assert(html.includes('value="1">Employee (employee-only)</option>'));
  assert.equal(html.includes('value="2">Administrator (admin-only)</option>'), false);
  assert(html.includes('value="">No Access</option>')); assert(html.includes("View Only"));
  assert(html.includes('value="set_grants"')); assert(html.includes('value="membership"')); assert(html.includes('value="delete_group"'));
});
test("groups without active members block leader submission with an explanation", () => {
  const html = page(<Groups />, { Groups: [{ ID: 3, Name: "Empty", Members: [], Grants: [], Editor: "leader" }] });
  assert(html.includes("Add an active member before designating a leader")); assert.match(html, /disabled=""[^>]*>Save leader/);
  assert(page(<Groups />).includes("Create a group to start assigning"));
});
