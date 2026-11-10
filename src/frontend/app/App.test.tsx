import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, useNavigate } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { App } from "./App";
import type { APIResult, PageData, Review } from "./types";

const user = {
  ID: 1,
  Username: "reader",
  DisplayName: "Test Employee",
  IsAdmin: false,
  Active: true,
};
const database = {
  ID: 7,
  Name: "Employees",
  DBMS: "postgresql",
  DatabaseName: "employees",
  Permission: "view_edit",
  Active: true,
  Host: "localhost",
  Port: 5432,
  SSLMode: "disable",
};
const detail = {
  Table: { Name: "employees", Kind: "table" },
  Columns: [{ Name: "name", DataType: "text", Nullable: false }],
  Constraints: [],
  Indexes: [],
};
const identity = {
  User: user,
  CSRF: "session-csrf",
  Expires: "2026-11-10T12:00:00Z",
};
function page(title: string, extra: Partial<PageData> = {}): PageData {
  return {
    ...identity,
    Title: title,
    Database: database,
    Databases: [database],
    Schemas: [{ Name: "public" }],
    Tables: [{ Name: "employees" }],
    Schema: "public",
    Table: "employees",
    SQL: "",
    Description: "",
    DraftID: 0,
    DraftUpdated: "",
    ViewerSpec: { Limit: 100, Offset: 0 },
    Detail: detail,
    Admin: { Users: [], Groups: [], Databases: [] },
    ...extra,
  } as PageData;
}
function response(value: APIResult, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}
function mockAPI(
  handler: (path: string, init?: RequestInit) => Response | Promise<Response>,
) {
  const fetchMock = vi.fn((input: string | URL | Request, init?: RequestInit) =>
    handler(String(input), init),
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}
function BackButton() {
  const navigate = useNavigate();
  return <button onClick={() => navigate(-1)}>Go back</button>;
}
function open(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
      <BackButton />
    </MemoryRouter>,
  );
}

describe("SPA workflows", () => {
  it("navigates between screens and restores history without bootstrapping another document", async () => {
    const requests = mockAPI((path) => {
      if (path === "/api/session") return response(identity);
      if (path === "/api/dashboard") return response(page("Dashboard"));
      if (path === "/api/databases") return response(page("My Databases"));
      if (path === "/api/explorer?db=7")
        return response(page("Database Explorer"));
      throw new Error(`Unexpected request ${path}`);
    });
    open("/dashboard");
    await screen.findByRole("heading", { name: "Welcome, Test Employee." });
    fireEvent.click(screen.getByRole("link", { name: "My Databases" }));
    await screen.findByRole("heading", { name: "My Databases" });
    fireEvent.change(
      screen.getByRole("searchbox", { name: "Search databases" }),
      { target: { value: "unknown" } },
    );
    expect(screen.getByText("No matching databases.")).toBeInTheDocument();
    fireEvent.change(
      screen.getByRole("searchbox", { name: "Search databases" }),
      { target: { value: "" } },
    );
    fireEvent.click(screen.getByRole("link", { name: "Open explorer" }));
    await screen.findByRole("heading", { name: "employees" });
    fireEvent.click(screen.getByRole("button", { name: "Go back" }));
    await screen.findByRole("heading", { name: "My Databases" });
    expect(
      requests.mock.calls.filter(([path]) => path === "/api/session"),
    ).toHaveLength(1);
  });

  it("returns an expired session to login and obtains a fresh login challenge", async () => {
    let sessionCalls = 0;
    mockAPI((path) =>
      path === "/api/session"
        ? response(
            sessionCalls++ === 0 ? identity : { CSRF: "fresh-login-csrf" },
          )
        : response(
            { error: "Your session expired. Please sign in again." },
            401,
          ),
    );
    open("/databases");
    await screen.findByRole("heading", { name: "Welcome back" });
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Masuk ke workspace" }),
      ).toBeEnabled(),
    );
    expect(sessionCalls).toBe(2);
  });

  it("keeps failed login input and uses the replacement CSRF token on retry", async () => {
    let loggedIn = false;
    let attempts = 0;
    const requests = mockAPI((path, init) => {
      if (path === "/api/session")
        return response(loggedIn ? identity : { CSRF: "login-csrf" });
      if (path === "/api/login") {
        if (attempts++ === 0)
          return response(
            {
              error: "Incorrect username or password.",
              CSRF: "replacement-csrf",
            },
            401,
          );
        loggedIn = true;
        return response({ location: "/dashboard" });
      }
      return response(page("Dashboard"));
    });
    open("/login");
    await screen.findByRole("heading", { name: "Welcome back" });
    fireEvent.change(screen.getByLabelText("Username"), {
      target: { value: "reader" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "password-value" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Masuk ke workspace" }));
    await screen.findByRole("alert");
    expect(screen.getByLabelText("Username")).toHaveValue("reader");
    fireEvent.click(screen.getByRole("button", { name: "Masuk ke workspace" }));
    await screen.findByRole("heading", { name: "Welcome, Test Employee." });
    const retry = requests.mock.calls.filter(
      ([path]) => path === "/api/login",
    )[1];
    expect(retry[1]?.headers).toMatchObject({
      "X-CSRF-Token": "replacement-csrf",
    });
  });

  it("generates SELECT without execution and renders query values as text", async () => {
    const actions: Record<string, unknown>[] = [];
    mockAPI((path, init) => {
      if (path === "/api/session") return response(identity);
      const fields = init?.body ? JSON.parse(String(init.body)) : null;
      if (fields) actions.push(fields);
      return response(
        page(
          "SQL Viewer",
          fields?.action === "build_query"
            ? { SQL: "SELECT name FROM employees LIMIT 100;" }
            : fields?.action === "run_query"
              ? {
                  SQL: fields.sql,
                  ViewerResult: {
                    Columns: [{ Name: "name", DataType: "text" }],
                    Rows: [["<script>alert(1)</script>"]],
                    HasMore: false,
                    Duration: 1000000,
                  },
                }
              : {},
        ),
      );
    });
    const { container } = open("/sql-viewer?db=7");
    await screen.findByRole("heading", { name: "Visual SELECT builder" });
    fireEvent.click(screen.getByRole("checkbox", { name: "name" }));
    fireEvent.click(
      screen.getByRole("button", { name: "Generate SELECT in editor" }),
    );
    await waitFor(() =>
      expect(screen.getByLabelText("Read-only SQL query")).toHaveValue(
        "SELECT name FROM employees LIMIT 100;",
      ),
    );
    expect(actions).toHaveLength(1);
    expect(actions[0]).toMatchObject({
      action: "build_query",
      columns: ["name"],
    });
    fireEvent.click(screen.getByRole("button", { name: "Run read query" }));
    expect(
      await screen.findByText("<script>alert(1)</script>"),
    ).toBeInTheDocument();
    expect(container.querySelector("script")).toBeNull();
    fireEvent.change(screen.getByLabelText("Read-only SQL query"), {
      target: { value: "SELECT 2;" },
    });
    expect(screen.getByRole("alert")).toHaveTextContent("SQL changed");
  });

  it("requires a fresh migration review and explicit non-atomic acceptance", async () => {
    const review: Review = {
      SQL: "ALTER TABLE employees ADD COLUMN note text;",
      BaseVersion: 0,
      Mode: "non_atomic",
      Transactional: false,
      Warnings: [],
      Plans: [],
      SchemaDiffAvailable: false,
      SchemaDiffMessage: "Preview unavailable.",
      SchemaDiff: null,
    };
    const actions: string[] = [];
    mockAPI((path, init) => {
      if (path === "/api/session") return response(identity);
      const fields = init?.body ? JSON.parse(String(init.body)) : null;
      if (fields) actions.push(fields.action);
      return response(
        page("SQL Migration", {
          SQL: fields?.sql || "",
          Description: fields?.description || "",
          Review: fields ? review : null,
        }),
      );
    });
    open("/migrations/new?db=7");
    await screen.findByRole("heading", { name: "Create migration" });
    fireEvent.change(screen.getByLabelText("Description / draft title"), {
      target: { value: "Add note" },
    });
    fireEvent.change(screen.getByLabelText("SQL migration"), {
      target: { value: review.SQL },
    });
    fireEvent.click(screen.getByRole("button", { name: "Analyze migration" }));
    const execute = await screen.findByRole("button", {
      name: "Execute migration",
    });
    expect(execute).toBeDisabled();
    fireEvent.click(
      screen.getByRole("checkbox", { name: /I reviewed the SQL/ }),
    );
    expect(execute).toBeDisabled();
    fireEvent.click(
      screen.getByRole("checkbox", { name: /I accept non-atomic execution/ }),
    );
    expect(execute).toBeEnabled();
    fireEvent.change(screen.getByLabelText("SQL migration"), {
      target: { value: review.SQL + "\nSELECT 1;" },
    });
    expect(execute).toBeDisabled();
    expect(screen.getByRole("alert")).toHaveTextContent("Analyze again");
    expect(actions).toEqual(["analyze"]);
  });

  it("builds and reorders visual operations before generating migration SQL", async () => {
    const actions: Record<string, string>[] = [];
    const generatedSQL = "ALTER TABLE employees ADD COLUMN tag text;";
    mockAPI((path, init) => {
      if (path === "/api/session") return response(identity);
      const fields = init?.body ? JSON.parse(String(init.body)) : null;
      if (fields) actions.push(fields);
      return response(
        page(
          "SQL Migration",
          fields ? { SQL: generatedSQL, Description: fields.description } : {},
        ),
      );
    });
    open("/migrations/new?db=7");
    await screen.findByRole("heading", { name: "Visual change set" });
    fireEvent.change(screen.getByLabelText("Description"), {
      target: { value: "Add tags" },
    });
    fireEvent.change(screen.getByRole("combobox", { name: "Operation" }), {
      target: { value: "add_column" },
    });
    fireEvent.change(screen.getByLabelText("Table"), {
      target: { value: "employees" },
    });
    fireEvent.change(screen.getByLabelText("Column name"), {
      target: { value: "tag" },
    });
    fireEvent.change(screen.getByLabelText("SQL type"), {
      target: { value: "text" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Add operation to change set" }),
    );
    fireEvent.change(screen.getByLabelText("Column name"), {
      target: { value: "label" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Add operation to change set" }),
    );
    fireEvent.click(screen.getAllByRole("button", { name: "Up" })[1]);
    expect(actions).toHaveLength(0);
    fireEvent.click(screen.getByRole("button", { name: "Analyze change set" }));
    await waitFor(() =>
      expect(screen.getByLabelText("SQL migration")).toHaveValue(generatedSQL),
    );
    expect(actions).toHaveLength(1);
    expect(actions[0].action).toBe("analyze_visual");
    const operations = JSON.parse(actions[0].operations);
    expect(
      operations.map(
        (operation: { Column: { Name: string } }) => operation.Column.Name,
      ),
    ).toEqual(["label", "tag"]);
  });

  it("retains page errors and lets the user retry a failed load", async () => {
    let attempts = 0;
    mockAPI((path) =>
      path === "/api/session"
        ? response(identity)
        : attempts++ === 0
          ? response({ error: "Cannot connect to the target database." }, 502)
          : response(page("Database Explorer")),
    );
    open("/explorer?db=7");
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Cannot connect",
    );
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await screen.findByRole("heading", { name: "employees" });
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("submits all group grants in one JSON action with a CSRF header", async () => {
    const admin = { ...user, IsAdmin: true };
    let submitted: RequestInit | undefined;
    mockAPI((path, init) => {
      if (path === "/api/session")
        return response({ ...identity, User: admin });
      if (init?.method === "POST") {
        submitted = init;
        return response(
          { error: "Check the supplied values and required fields." },
          400,
        );
      }
      return response(
        page("Groups", {
          User: admin,
          Groups: [
            {
              ID: 2,
              Name: "Analysts",
              Description: "",
              Editor: "",
              Focus: false,
              Members: [],
              Grants: [{ Database: database, Permission: "view_only" }],
            },
          ],
        }),
      );
    });
    open("/admin/groups");
    await screen.findByRole("heading", { name: "Analysts" });
    fireEvent.click(screen.getByRole("button", { name: "Edit access" }));
    fireEvent.change(
      screen.getByRole("combobox", { name: "Analysts Employees" }),
      { target: { value: "view_edit" } },
    );
    fireEvent.click(screen.getByRole("button", { name: "Save access" }));
    await screen.findByRole("alert");
    expect(JSON.parse(String(submitted?.body))).toMatchObject({
      action: "set_grants",
      db: ["7"],
      permission: ["view_edit"],
    });
    expect(submitted?.headers).toMatchObject({
      "X-CSRF-Token": "session-csrf",
    });
    expect(
      screen.getByRole("combobox", { name: "Analysts Employees" }),
    ).toHaveValue("view_edit");
  });

  it("shows backend permission errors on direct admin navigation", async () => {
    mockAPI((path) =>
      path === "/api/session"
        ? response(identity)
        : response(
            { error: "You do not have permission for this action." },
            403,
          ),
    );
    open("/admin/users");
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "You do not have permission",
    );
    expect(
      screen.queryByRole("heading", { name: "Add employee" }),
    ).not.toBeInTheDocument();
  });
});
