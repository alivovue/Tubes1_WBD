import { useState, type ReactNode } from "react";
import { Link, Outlet, useLocation } from "react-router-dom";
import { APIError } from "../api/client";
import { Icon } from "../components/Icon";
import { Notice } from "../components/primitives";
import { useNavigation } from "../hooks/useNavigation";
import { initials } from "../utils/format";
import { useSession } from "./session";

export function WorkspaceBrand({ home }: { home: string }) {
  return (
    <Link className="brand" to={home}>
      <img
        className="brand__mark"
        src="/public/sc%20logo.png"
        width="34"
        height="34"
        alt=""
      />
      <span>
        <strong>Schakker.</strong>
        <small>Data Platform</small>
      </span>
    </Link>
  );
}

export function SidebarLink({
  to,
  label,
  icon,
  active,
}: {
  to: string;
  label: string;
  icon: string;
  active: boolean;
}) {
  return (
    <Link
      className={`nav-item${active ? " is-active" : ""}`}
      to={to}
      aria-current={active ? "page" : undefined}
    >
      <Icon name={icon} />
      <span>{label}</span>
    </Link>
  );
}

export function WorkspaceShell({
  home,
  section,
  role,
  navigation,
}: {
  home: string;
  section: string;
  role: string;
  navigation: ReactNode;
}) {
  const { session, expire, request } = useSession();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const location = useLocation();
  const { open, setOpen } = useNavigation(location.pathname, location.search);
  async function logout() {
    setBusy(true);
    setError("");
    try {
      await request("/logout", {}, session.CSRF);
      expire();
    } catch (problem) {
      if (problem instanceof APIError && problem.status === 401) expire();
      else setError((problem as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <a className="skip-link" href="#main-content">
        Lewati ke konten utama
      </a>
      <div className={`app-shell${open ? " sidebar-open" : ""}`}>
        <aside
          className={`sidebar${open ? " is-open" : ""}`}
          aria-label="Navigasi aplikasi"
        >
          <div className="sidebar__header">
            <WorkspaceBrand home={home} />
            <button
              className="icon-button sidebar__close"
              onClick={() => setOpen(false)}
              aria-label="Close navigation"
            >
              <Icon name="close" />
            </button>
          </div>
          <nav className="sidebar__nav">{navigation}</nav>
          <div className="sidebar__footer">
            <div className="avatar avatar--small">
              {initials(session.User!.DisplayName)}
            </div>
            <div className="user-summary">
              <strong>{session.User!.DisplayName}</strong>
              <span>{role}</span>
            </div>
            <button
              className="icon-button"
              onClick={logout}
              disabled={busy}
              aria-label="Logout"
            >
              <Icon name="logout" />
            </button>
          </div>
        </aside>
        <button
          className="sidebar-backdrop"
          type="button"
          onClick={() => setOpen(false)}
          aria-label="Close navigation"
        />
        <div className="app-stage">
          <header className="topbar">
            <div className="topbar__left">
              <button
                className="icon-button topbar__menu"
                onClick={() => setOpen((value) => !value)}
                aria-expanded={open}
                aria-label="Open navigation"
              >
                <Icon name="menu" />
              </button>
              <nav className="breadcrumb" aria-label="Breadcrumb">
                <span>{section}</span>
                <strong>
                  {session.Title ||
                    location.pathname.split("/").filter(Boolean).at(-1)}
                </strong>
              </nav>
            </div>
            <div className="avatar">{initials(session.User!.DisplayName)}</div>
          </header>
          <main className="page" id="main-content" tabIndex={-1}>
            {error && <Notice error>{error}</Notice>}
            <Outlet />
          </main>
        </div>
      </div>
    </>
  );
}
