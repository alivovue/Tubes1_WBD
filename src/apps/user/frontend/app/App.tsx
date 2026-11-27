import {
  LoginPage,
  PageFrame,
  SessionProvider,
  SidebarLink,
  WorkspaceShell,
  usePageRequest,
  useSession,
  type LoginContent,
} from "@schakker/ui";
import { useCallback, type ReactNode } from "react";
import { Link, Navigate, Route, Routes, useLocation } from "react-router-dom";
import { api } from "./api";
import type { APIResult, PageData } from "./types";
import { PageContext } from "./ui";
import { Dashboard, Databases, Explorer } from "./workspace";
import { SQLViewer } from "./SQLViewer";
import { MigrationEditor } from "./MigrationEditor";
import { MigrationWorkspace, migrationView } from "./MigrationWorkspace";

const home = "/dashboard";
const loginContent: LoginContent = {
  eyebrow: "One workspace. Every database.",
  title: "Ship database changes with clarity and control.",
  description: "Kelola skema, tinjau SQL, dan telusuri setiap perubahan dari satu ruang kerja yang aman.",
  features: [
    "Permission berbasis grup",
    "Migration history yang dapat diaudit",
    "Eksplorasi database tanpa berpindah alat",
  ],
  portal: "Employee portal",
  icon: "database",
  submitLabel: "Masuk ke workspace",
};

export function App() {
  return (
    <SessionProvider request={api}>
      <AppRoutes />
    </SessionProvider>
  );
}

function AppRoutes() {
  const { session } = useSession();
  return (
    <Routes>
      <Route
        path="/"
        element={<Navigate to={session.User?.ID ? home : "/login"} replace />}
      />
      <Route
        path="/login"
        element={session.User?.ID ? (
          <Navigate to={home} replace />
        ) : (
          <LoginPage home={home} content={loginContent} />
        )}
      />
      <Route element={session.User?.ID ? (
        <WorkspaceShell
          home={home}
          section="Workspace"
          role="Employee"
          navigation={<Navigation />}
        />
      ) : <Navigate to="/login" replace />}>
        {[
          "dashboard", "databases", "explorer", "sql-viewer", "migrations", "migrations/new",
        ].map((path) => (
          <Route key={path} path={path} element={<Page />} />
        ))}
      </Route>
      <Route path="*" element={
        <main className="page">
          <h1>Page not found</h1>
          <Link to="/">Return to workspace</Link>
        </main>
      } />
    </Routes>
  );
}

function Navigation() {
  const location = useLocation();
  const workflowOpen =
    location.pathname === "/migrations" &&
    migrationView(new URLSearchParams(location.search).get("view")) !== "history";
  const nav = (to: string, label: string, icon: string) => {
    const active = to.includes("?view=")
      ? workflowOpen
      : to === "/migrations"
        ? location.pathname === to && !workflowOpen
        : location.pathname === to ||
          (to === "/databases" && location.pathname === "/explorer");
    return <SidebarLink to={to} label={label} icon={icon} active={active} />;
  };
  return (
    <>
      <p className="nav-label">Overview</p>
      {nav("/dashboard", "Dashboard", "grid")}
      <p className="nav-label">Workspace</p>
      {nav("/databases", "My Databases", "database")}
      {nav("/sql-viewer", "SQL Viewer", "code")}
      {nav("/migrations/new", "SQL Migration", "code")}
      {nav("/migrations?view=definitions", "Migration workflow", "history")}
      {nav("/migrations", "Changelog", "history")}
    </>
  );
}

function Page() {
  const location = useLocation();
  const params = new URLSearchParams(location.search);
  const workflow = location.pathname === "/migrations";
  const workflowTitle = migrationView(params.get("view")) === "history"
    ? "Changelog"
    : "Migration workflow";
  const selectedDatabase = workflow ? params.get("db") || "" : "";
  params.delete("tab");
  const requestPath = workflow
    ? "/databases"
    : location.pathname + (params.size ? `?${params}` : "");
  const transform = useCallback((result: APIResult): PageData => {
    if (!workflow) return result as PageData;
    const databases = result.Databases || [];
    const database = selectedDatabase
      ? databases.find((item) => String(item.ID) === selectedDatabase)
      : databases[0];
    if (selectedDatabase && !database)
      throw new Error("You do not have access to the selected database.");
    return {
      ...result,
      Title: workflowTitle,
      Database: database || { ID: 0 },
      CanEdit: database?.Permission === "view_edit",
    } as PageData;
  }, [workflow, workflowTitle, selectedDatabase]);
  const page = usePageRequest<PageData>({
    request: api,
    requestPath,
    titleSuffix: "Schakker Data Platform",
    transform,
  });
  const analyzing = ["analyze", "analyze_visual", "analyze_desired"].includes(
    page.submission.action,
  );
  let content: ReactNode;
  if (page.data) {
    switch (location.pathname) {
      case "/dashboard": content = <Dashboard />; break;
      case "/databases": content = <Databases />; break;
      case "/explorer": content = <Explorer />; break;
      case "/sql-viewer": content = <SQLViewer />; break;
      case "/migrations/new": content = <MigrationEditor />; break;
      case "/migrations": content = <MigrationWorkspace key={page.data.Database.ID} />; break;
    }
  }
  return (
    <PageFrame {...page}>
      {page.data && (
        <PageContext.Provider value={{
          data: page.data,
          busy: page.busy,
          submit: page.submit,
          refresh: page.refresh,
          expire: page.expire,
          acceptCSRF: page.acceptCSRF,
          analysisState: {
            loading: analyzing && page.busy,
            error: analyzing ? page.submission.error : "",
          },
        }}>
          {content}
        </PageContext.Provider>
      )}
    </PageFrame>
  );
}
