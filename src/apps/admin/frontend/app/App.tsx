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
import type { ReactNode } from "react";
import { Link, Navigate, Route, Routes, useLocation } from "react-router-dom";
import { api } from "./api";
import type { PageData } from "./types";
import { PageContext } from "./ui";
import { Connections, Groups, Users } from "./admin";

const home = "/admin/users";
const loginContent: LoginContent = {
  eyebrow: "Platform access administration.",
  title: "Manage platform access with confidence.",
  portal: "Administration portal",
  icon: "users",
  submitLabel: "Masuk ke administrasi",
  people: true,
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
          section="Administration"
          role="Administrator"
          navigation={<Navigation />}
        />
      ) : <Navigate to="/login" replace />}>
        {[
          "admin/users", "admin/administrators", "admin/groups", "admin/connections",
        ].map((path) => (
          <Route key={path} path={path} element={<Page />} />
        ))}
        <Route path="admin/access" element={<Navigate to="/admin/groups" replace />} />
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
  const { pathname } = useLocation();
  return (
    <>
      <p className="nav-label">Administration</p>
      {[
        ["/admin/users", "Employees", "users"],
        ["/admin/administrators", "Administrators", "shield"],
        ["/admin/groups", "Groups", "users"],
        ["/admin/connections", "DB Connections", "server"],
      ].map(([to, label, icon]) => (
        <SidebarLink
          key={to}
          to={to}
          label={label}
          icon={icon}
          active={pathname === to}
        />
      ))}
    </>
  );
}

function Page() {
  const { pathname, search } = useLocation();
  const params = new URLSearchParams(search);
  params.delete("tab");
  const requestPath = pathname + (params.size ? `?${params}` : "");
  const page = usePageRequest<PageData>({
    request: api,
    requestPath,
    titleSuffix: "Schakker Administration",
  });
  let content: ReactNode;
  switch (pathname) {
    case "/admin/administrators": content = <Users administrators />; break;
    case "/admin/users": content = <Users />; break;
    case "/admin/groups": content = <Groups />; break;
    case "/admin/connections": content = <Connections />; break;
  }
  return (
    <PageFrame {...page}>
      {page.data && (
        <PageContext.Provider value={{ data: page.data, busy: page.busy, submit: page.submit, refresh: page.refresh }}>
          {content}
        </PageContext.Provider>
      )}
    </PageFrame>
  );
}
