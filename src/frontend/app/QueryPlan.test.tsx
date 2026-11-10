import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { QueryPlan } from "./QueryPlan";
import type { PlanNode, StatementPlan } from "./types";

function node(operation: string, extra: Partial<PlanNode> = {}): PlanNode {
  return {
    Operation: operation,
    Relation: "",
    Index: "",
    JoinType: "",
    EstimatedRows: null,
    StartupCost: null,
    TotalCost: null,
    Children: null,
    ...extra,
  };
}

const nativeJSON = [
  {
    Plan: {
      "Node Type": "Nested Loop",
      "Join Type": "Inner",
      "Plan Rows": 4,
      "Startup Cost": 0,
      "Total Cost": 12.4,
      Plans: [
        {
          "Node Type": "Index Scan",
          "Relation Name": "people",
          "Index Name": "people_pkey",
        },
      ],
    },
  },
];
const nestedPlan: StatementPlan = {
  Statement:
    "SELECT * FROM people JOIN orders ON people.id = orders.person_id;",
  DBMS: "postgresql",
  Available: true,
  Message: "",
  JSON: nativeJSON,
  Nodes: [
    node("Nested Loop", {
      JoinType: "Inner",
      EstimatedRows: 4,
      StartupCost: 0,
      TotalCost: 12.4,
      Children: [
        node("Index Scan", {
          Relation: "people",
          Index: "people_pkey",
          EstimatedRows: 0,
          StartupCost: 0.2,
          TotalCost: 3.5,
        }),
        node("Hash", {
          Children: [
            node("Seq Scan", { Relation: "orders", EstimatedRows: 2 }),
          ],
        }),
      ],
    }),
  ],
};

function clipboard(writeText: ReturnType<typeof vi.fn>) {
  const replacement = Object.create(navigator);
  Object.defineProperty(replacement, "clipboard", { value: { writeText } });
  vi.stubGlobal("navigator", replacement);
}

describe("Query Plan", () => {
  it("renders nested operations and native estimates, including zero values", () => {
    render(<QueryPlan plans={[nestedPlan]} />);
    const statement = screen.getByRole("article", { name: "Statement 1" });
    expect(
      within(statement).getByLabelText("SQL for statement 1"),
    ).toHaveTextContent(nestedPlan.Statement);
    expect(screen.getByText("people_pkey")).toBeVisible();
    expect(screen.getByText("Inner")).toBeVisible();
    expect(screen.getByText("12.4")).toBeVisible();
    const root = screen
      .getByRole("button", { name: "Collapse Nested Loop" })
      .closest(".query-plan-node__card")! as HTMLElement;
    expect(
      within(root).getByText("Estimated startup cost").nextElementSibling,
    ).toHaveTextContent(/^0$/);
    expect(
      within(root).getByText("Estimated rows").nextElementSibling,
    ).toHaveTextContent(/^4$/);
    const index = screen
      .getByText("Index Scan")
      .closest(".query-plan-node__card")! as HTMLElement;
    expect(
      within(index).getByText("Estimated rows").nextElementSibling,
    ).toHaveTextContent(/^0$/);
    expect(
      within(index).getByText("Estimated startup cost").nextElementSibling,
    ).toHaveTextContent("0.2");
    expect(screen.getByText("Seq Scan")).not.toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Expand Hash" }));
    expect(screen.getByText("Seq Scan")).toBeVisible();
    expect(screen.getByText("orders")).toBeVisible();
    expect(
      screen.getByText(/Costs use the database planner/),
    ).toHaveTextContent("not milliseconds");
    expect(screen.getByText(/No actual execution timings/)).toBeVisible();
  });

  it("expands and collapses the tree locally without any API requests", () => {
    const fetch = vi.fn();
    vi.stubGlobal("fetch", fetch);
    render(<QueryPlan plans={[nestedPlan]} />);
    fireEvent.click(screen.getByRole("button", { name: "Expand all" }));
    expect(screen.getByText("Seq Scan")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Collapse all" }));
    expect(screen.getByText("Index Scan")).not.toBeVisible();
    expect(screen.getByText("Seq Scan")).not.toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Expand Nested Loop" }));
    expect(screen.getByText("Index Scan")).toBeVisible();
    expect(screen.getByText("Seq Scan")).not.toBeVisible();
    expect(fetch).not.toHaveBeenCalled();
  });

  it("keeps each SQL statement associated with its own plan and tree controls", () => {
    const second: StatementPlan = {
      ...nestedPlan,
      Statement: "SELECT * FROM invoices;",
      Nodes: [
        node("Append", {
          Children: [node("Seq Scan", { Relation: "invoices" })],
        }),
      ],
    };
    render(<QueryPlan plans={[nestedPlan, second]} />);
    const first = screen.getByRole("article", { name: "Statement 1" });
    const other = screen.getByRole("article", { name: "Statement 2" });
    expect(within(first).getByText("people_pkey")).toBeVisible();
    expect(
      within(other).getByLabelText("SQL for statement 2"),
    ).toHaveTextContent(second.Statement);
    fireEvent.click(
      within(other).getByRole("button", { name: "Collapse all" }),
    );
    expect(within(other).getByText("invoices")).not.toBeVisible();
    expect(within(first).getByText("people_pkey")).toBeVisible();
  });

  it("keeps raw JSON secondary and copies the original EXPLAIN data", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    clipboard(writeText);
    const fetch = vi.fn();
    vi.stubGlobal("fetch", fetch);
    render(<QueryPlan plans={[nestedPlan]} />);
    const raw = screen.getByLabelText("Original EXPLAIN JSON");
    expect(raw).not.toBeVisible();
    fireEvent.click(
      screen.getByText("Original EXPLAIN JSON", { selector: "summary" }),
    );
    expect(raw).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Copy JSON" }));
    expect(await screen.findByRole("status")).toHaveTextContent("JSON copied.");
    expect(JSON.parse(writeText.mock.calls[0][0])).toEqual(nativeJSON);
    expect(fetch).not.toHaveBeenCalled();
  });

  it("offers manual copying when clipboard access fails", async () => {
    clipboard(vi.fn().mockRejectedValue(new Error("Denied")));
    render(<QueryPlan plans={[nestedPlan]} />);
    fireEvent.click(
      screen.getByText("Original EXPLAIN JSON", { selector: "summary" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Copy JSON" }));
    expect(await screen.findByRole("status")).toHaveTextContent(
      "copy it manually",
    );
    expect(screen.getByLabelText("Original EXPLAIN JSON")).toBeVisible();
  });

  it("distinguishes unsupported statements from plans unavailable against the schema", () => {
    render(
      <QueryPlan
        plans={[
          {
            ...nestedPlan,
            Available: false,
            UnavailableReason: "unsupported_statement",
            Statement: "ALTER TABLE people ADD COLUMN age integer;",
            Nodes: null,
            JSON: null,
          },
          {
            ...nestedPlan,
            Available: false,
            UnavailableReason: "explain_unavailable",
            Message:
              "Plan unavailable against the current schema; dependent changes may need execution first",
            Nodes: null,
            JSON: null,
          },
        ]}
      />,
    );
    expect(screen.getByText("Unsupported statement")).toBeVisible();
    expect(
      screen.getByText(/does not provide an execution plan/),
    ).toBeVisible();
    expect(screen.getByText("Plan unavailable")).toBeVisible();
    expect(
      screen.getByText(/dependent changes may need execution first/),
    ).toBeVisible();
    expect(screen.queryByText("Original EXPLAIN JSON")).not.toBeInTheDocument();
  });

  it("presents missing fields without inventing metrics and handles missing parsed nodes", () => {
    const { rerender } = render(
      <QueryPlan plans={[{ ...nestedPlan, Nodes: [node("Result")] }]} />,
    );
    expect(screen.getAllByText("Not available")).toHaveLength(3);
    expect(
      screen.queryByRole("button", { name: "Expand all" }),
    ).not.toBeInTheDocument();
    rerender(<QueryPlan plans={[{ ...nestedPlan, Nodes: [] }]} />);
    expect(screen.getByText("Plan unavailable")).toBeVisible();
    expect(screen.getByText(/no recognized plan nodes/)).toBeVisible();
    expect(screen.getByText(/Inspect the original EXPLAIN JSON/)).toBeVisible();
  });

  it("renders MariaDB join and filtering estimates without fabricated cost values", () => {
    render(
      <QueryPlan
        plans={[
          {
            ...nestedPlan,
            DBMS: "mariadb",
            Nodes: [
              node("Block Nested Loop", {
                JoinType: "BNL",
                Children: [
                  node("ref", {
                    Relation: "people",
                    Index: "people_name_idx",
                    EstimatedRows: 3,
                    FilteredPercent: 80,
                  }),
                ],
              }),
            ],
          },
        ]}
      />,
    );
    expect(screen.getByText("mariadb")).toBeVisible();
    expect(screen.getByText("BNL")).toBeVisible();
    expect(screen.getByText("people_name_idx")).toBeVisible();
    expect(
      screen.getByText("Estimated filtered rows (%)").nextElementSibling,
    ).toHaveTextContent("80");
    expect(screen.getAllByText("Not available")).toHaveLength(5);
  });

  it("replaces old plans with loading, error, stale, and empty states", async () => {
    const { rerender } = render(<QueryPlan plans={[nestedPlan]} loading />);
    expect(screen.getByRole("status")).toHaveTextContent(
      "Analyzing query plans",
    );
    expect(screen.queryByText("people_pkey")).not.toBeInTheDocument();
    rerender(
      <QueryPlan
        plans={[nestedPlan]}
        error="Cannot connect to the target database."
      />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Query-plan analysis failed",
    );
    expect(screen.getByRole("alert")).toHaveTextContent("Cannot connect");
    expect(screen.queryByText("people_pkey")).not.toBeInTheDocument();
    rerender(<QueryPlan plans={[nestedPlan]} stale />);
    expect(screen.getByRole("status")).toHaveTextContent("Analyze again");
    expect(screen.queryByText("people_pkey")).not.toBeInTheDocument();
    rerender(<QueryPlan />);
    expect(screen.getByText(/Analyze a migration to preview/)).toBeVisible();
    rerender(<QueryPlan plans={[]} />);
    await waitFor(() =>
      expect(
        screen.getByText(/No statement plans were returned/),
      ).toBeVisible(),
    );
  });
});
