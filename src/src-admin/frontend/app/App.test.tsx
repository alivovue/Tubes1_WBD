import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, it, expect, vi } from "vitest";
import { App } from "./App";
import type { PageData, APIResult } from "./types";
const user = {
  ID: 1,
  Username: "admin",
  DisplayName: "Platform Admin",
  Active: true,
};
const identity = {
  User: user,
  CSRF: "session-csrf",
  Expires: "2026-11-10T12:00:00Z",
};
const database = {
  ID: 7,
  Name: "Employees",
  DBMS: "postgresql",
  DatabaseName: "employees",
  Permission: "",
  Active: true,
  Host: "localhost",
  Port: 5432,
  SSLMode: "disable",
};
function page(title: string, extra: Partial<PageData> = {}): PageData {
  return {
    ...identity,
    Title: title,
    Admin: { Users: [], Groups: [], Databases: [] },
    Groups: [],
    DBMS: ["postgresql", "mariadb"],
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
  const requests = vi.fn((input: string | URL | Request, init?: RequestInit) =>
    handler(String(input), init),
  );
  vi.stubGlobal("fetch", requests);
  return requests;
}
function open(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

describe("Admin application", () => {
  it("submits all group grants in one JSON action with a CSRF header", async () => {
    const admin = { ...user };
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
      await screen.findByRole("combobox", { name: "Analysts Employees" }),
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

  it("navigates only among accounts, groups, and connection configuration", async () => {
    mockAPI((path) =>
      path === "/api/session"
        ? response(identity)
        : response(page(path.includes("groups") ? "Groups" : "Employees")),
    );
    open("/admin/users");
    await screen.findByRole("heading", { name: "Employees" });
    for (const label of [
      "Dashboard",
      "My Databases",
      "SQL Viewer",
      "SQL Migration",
      "Changelog",
      "Query Plan",
    ]) {
      expect(
        screen.queryByRole("link", { name: label }),
      ).not.toBeInTheDocument();
    }
    fireEvent.click(screen.getByRole("link", { name: "Groups" }));
    await screen.findByRole("heading", { name: "Groups" });
    expect(
      screen.getByText(/Group membership never grants administrators/),
    ).toBeVisible();
  });
  it("manages administrators separately from ordinary accounts without a promotion control", async () => {
    let submitted: { path: string; fields: Record<string, string> } | undefined;
    mockAPI((path, init) => {
      if (path === "/api/session") return response(identity);
      if (init?.method === "POST") {
        submitted = { path, fields: JSON.parse(String(init.body)) };
        return response({ error: "Validation example" }, 400);
      }
      return response(
        page("Accounts", {
          Admin: {
            Users: [
              {
                ...user,
                ID: 2,
                Username: "ordinary",
                DisplayName: "Ordinary employee",
              },
            ],
            Groups: [],
            Databases: [],
          },
          Administrators: [
            { ...user, DisplayName: "Independent administrator" },
          ],
        }),
      );
    });
    open("/admin/users");
    await screen.findByRole("heading", { name: "Employees" });
    expect(screen.getByText("Ordinary employee")).toBeVisible();
    expect(
      screen.queryByRole("checkbox", { name: "Platform Admin" }),
    ).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("link", { name: "Administrators" }));
    await screen.findByRole("heading", { name: "Administrators" });
    expect(screen.getByText("Independent administrator")).toBeVisible();
    expect(screen.queryByText("Ordinary employee")).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Username"), {
      target: { value: "new.admin" },
    });
    fireEvent.change(screen.getAllByLabelText("Display name")[0], {
      target: { value: "New administrator" },
    });
    fireEvent.change(screen.getByLabelText("Initial password"), {
      target: { value: "Independent-admin-password-123" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Create administrator" }),
    );
    await screen.findByRole("alert");
    expect(submitted?.path).toBe("/api/admin/administrators");
    expect(submitted?.fields).toMatchObject({
      action: "create_admin",
      username: "new.admin",
    });
    expect(submitted?.fields).not.toHaveProperty("admin");
  });

  it("does not load managed-database routes", async () => {
    const requests = mockAPI(() => response(identity));
    open("/explorer?db=7");
    await screen.findByRole("heading", { name: "Page not found" });
    expect(requests.mock.calls.map(([path]) => path)).toEqual(["/api/session"]);
  });
  it("shows an ordinary account's denied admin session", async () => {
    mockAPI(() =>
      response(
        {
          error:
            "Only platform administrators can sign in to this application.",
        },
        403,
      ),
    );
    open("/admin/users");
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Only platform administrators",
    );
    expect(
      screen.queryByRole("heading", { name: "Add employee" }),
    ).not.toBeInTheDocument();
  });
  it("keeps admin login failures and retries with a replacement CSRF token", async () => {
    let loggedIn = false,
      attempts = 0;
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
        return response({ location: "/admin/users" });
      }
      return response(page("Employees"));
    });
    open("/login");
    await screen.findByRole("heading", { name: "Welcome back" });
    fireEvent.change(screen.getByLabelText("Username"), {
      target: { value: "admin" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "test-password-value" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Masuk ke administrasi" }),
    );
    await screen.findByRole("alert");
    expect(screen.getByLabelText("Username")).toHaveValue("admin");
    fireEvent.click(
      screen.getByRole("button", { name: "Masuk ke administrasi" }),
    );
    await screen.findByRole("heading", { name: "Employees" });
    expect(
      requests.mock.calls.filter(([path]) => path === "/api/login")[1][1]
        ?.headers,
    ).toMatchObject({ "X-CSRF-Token": "replacement-csrf" });
  });
});
