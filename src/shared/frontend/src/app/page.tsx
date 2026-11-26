import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { APIError, type Fields } from "../api/client";
import { Button, Notice } from "../components/primitives";
import { useSession, type FrontendAPI, type SessionData } from "./session";

export type PageEnvelope = SessionData & {
  Title: string;
  CSRF: string;
  Message: string;
  ScrollTarget?: string;
};
export type PageResult<T extends PageEnvelope> = Partial<T> & SessionData;

// App-specific adapters select the endpoint and shape its page data. Request
// cancellation, session expiry and form state have one implementation.
export function usePageRequest<T extends PageEnvelope>({
  request,
  requestPath,
  titleSuffix,
  transform,
}: {
  request: FrontendAPI<PageResult<T>>;
  requestPath: string;
  titleSuffix: string;
  transform?: (result: PageResult<T>) => T;
}) {
  const location = useLocation();
  const navigate = useNavigate();
  const { update, expire } = useSession();
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [revision, setRevision] = useState(0);
  const [submission, setSubmission] = useState({ action: "", error: "" });
  const mutation = useRef(0);
  const inFlight = useRef(false);
  const currentPath = location.pathname + location.search;
  const activePath = useRef(currentPath);
  activePath.current = currentPath;
  const refresh = useCallback(() => setRevision((value) => value + 1), []);
  const acceptCSRF = useCallback((csrf?: string) => {
    if (csrf)
      setData((current) =>
        current && current.CSRF !== csrf ? { ...current, CSRF: csrf } : current,
      );
  }, []);
  useEffect(() => {
    mutation.current++;
    inFlight.current = false;
    setBusy(false);
    setSubmission({ action: "", error: "" });
    return () => {
      mutation.current++;
    };
  }, [currentPath, revision]);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError("");
    setData(null);
    request(requestPath, undefined, undefined, controller.signal)
      .then((result) => {
        if (controller.signal.aborted) return;
        if (result.location) {
          navigate(result.location, { replace: true });
          return;
        }
        const page = transform ? transform(result) : (result as T);
        setData(page);
        update(page);
      })
      .catch((problem: Error) => {
        if (controller.signal.aborted) return;
        if (problem instanceof APIError && problem.status === 401) expire();
        else setError(problem.message);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [request, requestPath, revision, navigate, update, expire, transform]);
  useEffect(() => {
    if (data?.Title) document.title = `${data.Title} · ${titleSuffix}`;
  }, [data?.Title, titleSuffix]);
  useEffect(() => {
    if (data?.ScrollTarget)
      document
        .getElementById(data.ScrollTarget)
        ?.scrollIntoView({ behavior: "smooth", block: "start" });
  }, [data]);
  async function submit(fields: Fields) {
    if (inFlight.current || !data) return false;
    inFlight.current = true;
    const token = ++mutation.current;
    const submittedPath = currentPath;
    const isCurrent = () =>
      mutation.current === token && activePath.current === submittedPath;
    const action = typeof fields.action === "string" ? fields.action : "";
    setBusy(true);
    setError("");
    setSubmission({ action, error: "" });
    try {
      const result = await request(location.pathname, fields, data.CSRF);
      if (!isCurrent()) return false;
      if (result.location) {
        if (result.location === currentPath) refresh();
        else navigate(result.location);
      } else {
        setData(result as T);
        update(result);
      }
      return true;
    } catch (problem) {
      if (!isCurrent()) return false;
      const message = (problem as Error).message;
      if (problem instanceof APIError && problem.status === 401) expire();
      else setError(message);
      setSubmission({ action, error: message });
      return false;
    } finally {
      if (isCurrent()) {
        inFlight.current = false;
        setBusy(false);
      }
    }
  }
  return {
    data, error, loading, busy, submit, refresh, submission, expire, acceptCSRF,
  };
}

export function PageFrame({
  data,
  loading,
  error,
  busy,
  refresh,
  children,
}: {
  data: PageEnvelope | null;
  loading: boolean;
  error: string;
  busy: boolean;
  refresh: () => void;
  children: ReactNode;
}) {
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
  return (
    <div aria-busy={busy}>
      {error && <Notice error>{error}</Notice>}
      {data.Message && <Notice>{data.Message}</Notice>}
      {busy && <p role="status">Processing…</p>}
      {children}
    </div>
  );
}
