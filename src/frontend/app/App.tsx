import { useNavigation } from "@schakker/ui";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";
import {
  Link,
  Navigate,
  NavLink,
  Outlet,
  Route,
  Routes,
  useLocation,
  useNavigate,
} from "react-router-dom";
import { api, APIError, formFields } from "./api";
import type { APIResult, Fields, PageData } from "./types";
import { Button, Field, Icon, initials, Notice, PageContext } from "./ui";
import { Dashboard, Databases, Explorer, History } from "./workspace";
import { SQLViewer } from "./SQLViewer";
import { MigrationEditor } from "./MigrationEditor";

const SessionContext = createContext<{
  session: APIResult;
  update: (data: APIResult) => void;
  expire: () => void;
} | null>(null);
function useSession() {
  return useContext(SessionContext)!;
}

export function App() {
  const [session, update] = useState<APIResult>({});
  const expire = useCallback(() => update({}), []);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const load = useCallback(() => {
    setLoading(true);
    setError("");
    api("/session")
      .then(update)
      .catch((problem) => setError(problem.message))
      .finally(() => setLoading(false));
  }, []);
  useEffect(load, [load]);
  if (loading)
    return (
      <p className="empty-state" role="status">
        Loading workspace…
      </p>
    );
  if (error)
    return (
      <main className="page">
        <Notice error>{error}</Notice>
        <Button onClick={load}>Retry</Button>
      </main>
    );
  return (
    <SessionContext.Provider value={{ session, update, expire }}>
      <Routes>
        <Route
          path="/"
          element={
            <Navigate to={session.User?.ID ? "/dashboard" : "/login"} replace />
          }
        />
        <Route
          path="/login"
          element={
            session.User?.ID ? <Navigate to="/dashboard" replace /> : <Login />
          }
        />
        <Route
          element={
            session.User?.ID ? <Layout /> : <Navigate to="/login" replace />
          }
        >
          {[
            "dashboard",
            "databases",
            "explorer",
            "sql-viewer",
            "migrations",
            "migrations/new",
          ].map((path) => (
            <Route key={path} path={path} element={<Page />} />
          ))}
        </Route>
        <Route
          path="*"
          element={
            <main className="page">
              <h1>Page not found</h1>
              <Link to="/">Return to workspace</Link>
            </main>
          }
        />
      </Routes>
    </SessionContext.Provider>
  );
}

function Login() {
  const { session, update } = useSession();
  const navigate = useNavigate();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [showPassword, setShowPassword] = useState(false);
  useEffect(() => {
    if (!session.CSRF)
      api("/session")
        .then(update)
        .catch((problem) => setError(problem.message));
  }, [session.CSRF, update]);
  async function login(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || !session.CSRF) return;
    setBusy(true);
    setError("");
    try {
      await api("/login", formFields(event.currentTarget, null), session.CSRF);
      const identity = await api("/session");
      update(identity);
      navigate("/dashboard", { replace: true });
    } catch (problem) {
      if (problem instanceof APIError && problem.result.CSRF)
        update(problem.result);
      setError(
        problem instanceof Error
          ? problem.message
          : "Login failed. Please retry.",
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <main className="auth-shell">
      <section className="auth-showcase" aria-labelledby="showcase-title">
        <Brand />
        <div className="auth-showcase__copy">
          <p className="eyebrow">One workspace. Every database.</p>
          <h1 id="showcase-title">
            Ship database changes with clarity and control.
          </h1>
          <p>
            Kelola skema, tinjau SQL, dan telusuri setiap perubahan dari satu
            ruang kerja yang aman.
          </p>
          <ul className="feature-list">
            <li>
              <span>
                <Icon name="check" />
              </span>
              Permission berbasis grup
            </li>
            <li>
              <span>
                <Icon name="check" />
              </span>
              Migration history yang dapat diaudit
            </li>
            <li>
              <span>
                <Icon name="check" />
              </span>
              Eksplorasi database tanpa berpindah alat
            </li>
          </ul>
        </div>
        <div className="code-window">
          <div className="code-window__bar">
            <i />
            <i />
            <i />
            <span>migration_042.sql</span>
          </div>
          <pre>
            {
              "ALTER TABLE employees\n  ADD COLUMN last_login_at TIMESTAMPTZ;\n\nCREATE INDEX idx_employee_login\n  ON employees(last_login_at);"
            }
          </pre>
          <div className="code-window__status">Analyze · Review · Execute</div>
        </div>
        <p className="auth-showcase__footer">
          Schakker Technologies · Internal systems
        </p>
      </section>
      <section className="auth-panel" aria-labelledby="login-title">
        <div className="auth-card">
          <div className="auth-card__mobile-brand">
            <Brand />
          </div>
          <p className="eyebrow">Employee portal</p>
          <h2 id="login-title">Welcome back</h2>
          <p className="auth-card__lead">
            Masuk menggunakan akun internal Schakker Anda.
          </p>
          {error && <Notice error>{error}</Notice>}
          <form className="form-stack" onSubmit={login}>
            <Field label="Username">
              <input name="username" autoComplete="username" required />
            </Field>
            <Field label="Password">
              <span className="input-with-action">
                <input
                  name="password"
                  type={showPassword ? "text" : "password"}
                  autoComplete="current-password"
                  required
                />
                <button
                  type="button"
                  onClick={() => setShowPassword((value) => !value)}
                  aria-label={showPassword ? "Hide password" : "Show password"}
                >
                  {showPassword ? "Hide" : "Show"}
                </button>
              </span>
            </Field>
            <Button disabled={busy || !session.CSRF}>
              {busy ? "Signing in…" : "Masuk ke workspace"}
            </Button>
          </form>
          <p className="security-note">
            Akses terbatas untuk karyawan. Gunakan akun yang dibuat oleh admin.
          </p>
          <p className="auth-help">
            Kesulitan masuk? Hubungi Platform Admin Anda.
          </p>
        </div>
      </section>
    </main>
  );
}

function Brand() {
  return (
    <Link className="brand" to="/dashboard">
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

function Layout() {
  const { session, expire } = useSession();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const location = useLocation();
  const { open, setOpen } = useNavigation(location.pathname, location.search);
  async function logout() {
    setBusy(true);
    setError("");
    try {
      await api("/logout", {}, session.CSRF);
      expire();
    } catch (problem) {
      if (problem instanceof APIError && problem.status === 401) expire();
      else setError((problem as Error).message);
    } finally {
      setBusy(false);
    }
  }
  const nav = (to: string, label: string, icon: string) => (
    <NavLink
      end
      className={({ isActive }) =>
        `nav-item${isActive || (to === "/databases" && location.pathname === "/explorer") ? " is-active" : ""}`
      }
      to={to}
    >
      <Icon name={icon} />
      <span>{label}</span>
    </NavLink>
  );
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
            <Brand />
            <button
              className="icon-button sidebar__close"
              onClick={() => setOpen(false)}
              aria-label="Close navigation"
            >
              <Icon name="close" />
            </button>
          </div>
          <nav className="sidebar__nav">
            <p className="nav-label">Overview</p>
            {nav("/dashboard", "Dashboard", "grid")}
            <p className="nav-label">Workspace</p>
            {nav("/databases", "My Databases", "database")}
            {nav("/sql-viewer", "SQL Viewer", "code")}
            {nav("/migrations/new", "SQL Migration", "code")}
            {nav("/migrations", "Changelog", "history")}
          </nav>
          <div className="sidebar__footer">
            <div className="avatar avatar--small">
              {initials(session.User!.DisplayName)}
            </div>
            <div className="user-summary">
              <strong>{session.User!.DisplayName}</strong>
              <span>Employee</span>
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
                <span>Workspace</span>
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

function Page() {
  const location = useLocation();
  const navigate = useNavigate();
  const { update, expire } = useSession();
  const [data, setData] = useState<PageData | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [revision, setRevision] = useState(0);
  const [analysisState, setAnalysisState] = useState({
    loading: false,
    error: "",
  });
  const currentPath = location.pathname + location.search;
  const apiParams = new URLSearchParams(location.search);
  apiParams.delete("tab");
  const requestPath =
    location.pathname + (apiParams.size ? `?${apiParams}` : "");
  const activePath = useRef(currentPath);
  activePath.current = currentPath;
  const refresh = () => setRevision((value) => value + 1);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError("");
    setData(null);
    setAnalysisState({ loading: false, error: "" });
    api(requestPath, undefined, undefined, controller.signal)
      .then((result) => {
        if (result.location) {
          navigate(result.location, { replace: true });
          return;
        }
        setData(result as PageData);
        update(result);
      })
      .catch((problem) => {
        if (controller.signal.aborted) return;
        if (problem instanceof APIError && problem.status === 401) expire();
        else setError(problem.message);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [requestPath, revision, navigate, update, expire]);
  useEffect(() => {
    if (data?.Title) document.title = `${data.Title} · Schakker Data Platform`;
  }, [data?.Title]);
  useEffect(() => {
    if (data?.ScrollTarget)
      document
        .getElementById(data.ScrollTarget)
        ?.scrollIntoView({ behavior: "smooth", block: "start" });
  }, [data]);
  async function submit(fields: Fields) {
    if (busy || !data) return false;
    setBusy(true);
    setError("");
    const analyzing =
      typeof fields.action === "string" &&
      ["analyze", "analyze_visual", "analyze_desired"].includes(fields.action);
    setAnalysisState({ loading: analyzing, error: "" });
    const submittedPath = currentPath;
    try {
      const result = await api(location.pathname, fields, data.CSRF);
      if (activePath.current !== submittedPath) return false;
      if (result.location) {
        if (result.location === currentPath) refresh();
        else navigate(result.location);
      } else {
        setData(result as PageData);
        update(result);
      }
      return true;
    } catch (problem) {
      if (activePath.current !== submittedPath) return false;
      if (problem instanceof APIError && problem.status === 401) expire();
      else setError((problem as Error).message);
      if (analyzing)
        setAnalysisState({ loading: false, error: (problem as Error).message });
      return false;
    } finally {
      setBusy(false);
      if (analyzing)
        setAnalysisState((current) => ({ ...current, loading: false }));
    }
  }
  if (loading)
    return (
      <p className="empty-state" role="status">
        Loading page…
      </p>
    );
  if (!data)
    return (
      <>
        <Notice error>{error || "Page unavailable."}</Notice>
        <Button onClick={refresh}>Retry</Button>
      </>
    );
  let content: ReactNode;
  switch (location.pathname) {
    case "/dashboard":
      content = <Dashboard />;
      break;
    case "/databases":
      content = <Databases />;
      break;
    case "/explorer":
      content = <Explorer />;
      break;
    case "/sql-viewer":
      content = <SQLViewer />;
      break;
    case "/migrations/new":
      content = <MigrationEditor />;
      break;
    case "/migrations":
      content = <History />;
      break;
  }
  return (
    <PageContext.Provider
      value={{ data, busy, submit, refresh, analysisState }}
    >
      <div aria-busy={busy}>
        {error && <Notice error>{error}</Notice>}
        {data.Message && <Notice>{data.Message}</Notice>}
        {busy && <p role="status">Processing…</p>}
        {content}
      </div>
    </PageContext.Provider>
  );
}
