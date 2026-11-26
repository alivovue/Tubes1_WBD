import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react";
import type { APIResponse, Fields } from "../api/client";
import { Button, Notice } from "../components/primitives";

export type SessionData = APIResponse & {
  Title?: string;
  User?: { ID: number; DisplayName: string };
};

export type FrontendAPI<T extends SessionData = SessionData> = (
  path: string,
  fields?: Fields,
  csrf?: string,
  signal?: AbortSignal,
) => Promise<T>;

const SessionContext = createContext<{
  session: SessionData;
  update: (data: SessionData) => void;
  expire: () => void;
  request: FrontendAPI;
} | null>(null);

export function useSession() {
  const session = useContext(SessionContext);
  if (!session) throw new Error("Session context is unavailable");
  return session;
}

// Every mounted provider owns its own state and uses its application's API.
export function SessionProvider({
  request,
  children,
}: {
  request: FrontendAPI;
  children: ReactNode;
}) {
  const [session, update] = useState<SessionData>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [revision, setRevision] = useState(0);
  const expire = useCallback(() => update({}), []);
  const retry = () => setRevision((value) => value + 1);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError("");
    request("/session", undefined, undefined, controller.signal)
      .then((result) => {
        if (!controller.signal.aborted) update(result);
      })
      .catch((problem: Error) => {
        if (!controller.signal.aborted) setError(problem.message);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [request, revision]);
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
        <Button onClick={retry}>Retry</Button>
      </main>
    );
  return (
    <SessionContext.Provider value={{ session, update, expire, request }}>
      {children}
    </SessionContext.Provider>
  );
}
