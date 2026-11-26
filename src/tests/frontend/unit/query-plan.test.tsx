import assert from "node:assert/strict";
import { test } from "node:test";
import { renderToStaticMarkup as render } from "react-dom/server";
import { QueryPlan } from "../../../apps/user/frontend/app/QueryPlan";
for (const [props, expected] of [
  [{}, "Analyze a migration to preview"],
  [{ loading: true }, "Analyzing query plans"],
  [{ error: "Timeout" }, "Query-plan analysis failed"],
  [{ stale: true }, "Analyze again to refresh"],
  [{ plans: [] }, "No statement plans were returned"],
] as const) test(`query-plan state: ${expected}`, () => { assert(render(<QueryPlan {...props as any} />).includes(expected)); });
test("query-plan loading and errors take precedence over stale or previously available plans", () => {
  const html = render(<QueryPlan loading error="Timeout" stale plans={[]} />);
  assert(html.includes('aria-busy="true"')); assert.equal(html.includes("Timeout"), false); assert.equal(html.includes("No statement plans"), false);
});
test("available plan trees show estimates, nested operators and original explain JSON", () => {
  const html = render(<QueryPlan plans={[{ DBMS: "postgresql", Statement: "SELECT '<script>'", Available: true, Nodes: [{ Operation: "Nested Loop", JoinType: "Inner", EstimatedRows: 0, StartupCost: 0, TotalCost: 12.5, Children: [{ Operation: "Index Scan", Relation: "employees", Index: "employees_pkey", FilteredPercent: 50, EstimatedRows: null }] }], JSON: { Plan: { "Node Type": "Nested Loop" } } } as any]} />);
  for (const text of ["Nested Loop", "Index Scan", "employees_pkey", "Estimated rows", "Not available", "Expand all", "Collapse all", "Original EXPLAIN JSON", "&lt;script&gt;"]) assert(html.includes(text));
  assert.equal(html.includes("<script>"), false); assert(html.includes("Estimates only"));
});
test("unsupported statements and unfamiliar output have distinct explanations", () => {
  const html = render(<QueryPlan plans={[
    { Statement: "CREATE TABLE demo(id int)", Available: false, UnavailableReason: "unsupported_statement" },
    { Statement: "SELECT 1", Available: true, Nodes: [], JSON: { unfamiliar: true } },
    { Statement: "SELECT 2", Available: false, Message: "Planner unavailable" },
  ] as any} />);
  for (const text of ["Unsupported statement", "does not provide an execution plan", "Inspect the original EXPLAIN JSON", "Planner unavailable", "Statement 3"]) assert(html.includes(text));
});
test("non-finite estimates display as unavailable instead of invalid numeric output", () => {
  const html = render(<QueryPlan plans={[{ Statement: "SELECT 1", Available: true, Nodes: [{ Operation: "Scan", EstimatedRows: NaN, StartupCost: Infinity, TotalCost: -Infinity }] } as any]} />);
  assert.equal((html.match(/Not available/g) || []).length, 3);
});
