import { useCallback, useEffect, useRef, useState } from "react";
import { APIError, requestResource } from "@schakker/ui";
import type { ResourceResponse } from "./types";
import { Button, Notice, usePage, url } from "./ui";

export function migrationURL(
  view: string,
  database: number,
  extra: Record<string, string | number> = {},
) {
  return url("/migrations", { db: database, view, ...extra });
}

export function migrationError(problem: unknown): string {
  if (!(problem instanceof APIError))
    return problem instanceof Error
      ? problem.message
      : "The request failed. Please retry.";
  const context: Record<number, string> = {
    403: "Your current permissions do not allow this action.",
    404: "This migration resource is unavailable. The migration service may not be enabled yet.",
    409: "This migration or its approvals changed. Refresh and review the latest state.",
    422: "Check the migration fields and recovery requirements.",
    503: "The required service is unavailable. Try again when it is restored.",
  };
  return [context[problem.status], problem.message].filter(Boolean).join(" ");
}

// One request at a time; cleanup cancels requests and timers on navigation.
// Polls pause in a hidden tab and resume immediately when it becomes visible.
export function useMigrationResource<T extends ResourceResponse>(
  path: string | null,
  poll = 0,
  complete?: (result: T) => boolean,
) {
  const { expire, acceptCSRF } = usePage();
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [revision, setRevision] = useState(0);
  const reload = useCallback(() => setRevision((value) => value + 1), []);
  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;
    let inFlight = false;
    let stopped = false;
    setData(null);
    setError("");
    setLoading(!!path);
    async function load() {
      if (!path || inFlight || stopped || controller.signal.aborted) return;
      if (timer) clearTimeout(timer);
      inFlight = true;
      let delay = poll;
      try {
        const result = await requestResource<T>(`/api${path}`, {
          signal: controller.signal,
        });
        if (controller.signal.aborted) return;
        acceptCSRF(result.CSRF);
        setData(result);
        setError("");
        if (complete?.(result)) stopped = true;
      } catch (problem) {
        if (controller.signal.aborted) return;
        if (problem instanceof APIError) {
          acceptCSRF(problem.result.CSRF);
          if (problem.status === 401) expire();
          if ([401, 403, 404, 422].includes(problem.status)) stopped = true;
        }
        setError(migrationError(problem));
        delay = Math.max(poll, 10000);
      } finally {
        inFlight = false;
        if (!controller.signal.aborted) {
          setLoading(false);
          if (poll && !stopped && !document.hidden)
            timer = setTimeout(load, delay);
        }
      }
    }
    const visible = () => {
      if (!document.hidden) void load();
      else if (timer) clearTimeout(timer);
    };
    void load();
    if (poll) document.addEventListener("visibilitychange", visible);
    return () => {
      controller.abort();
      if (timer) clearTimeout(timer);
      document.removeEventListener("visibilitychange", visible);
    };
  }, [path, poll, revision, expire, acceptCSRF, complete]);
  return { data, error, loading, reload };
}

export function useMigrationAction() {
  const { data, expire, acceptCSRF } = usePage();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const active = useRef(false);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  async function run<T extends ResourceResponse>(
    action: (csrf: string) => Promise<T>,
  ): Promise<T | null> {
    if (active.current || !data.CSRF) return null;
    active.current = true;
    setBusy(true);
    setError("");
    try {
      const result = await action(data.CSRF);
      if (!mounted.current) return null;
      acceptCSRF(result.CSRF);
      return result;
    } catch (problem) {
      if (!mounted.current) return null;
      if (problem instanceof APIError) {
        acceptCSRF(problem.result.CSRF);
        if (problem.status === 401) expire();
      }
      setError(migrationError(problem));
      return null;
    } finally {
      active.current = false;
      if (mounted.current) setBusy(false);
    }
  }
  return { busy, error, run };
}

export function ResourceState({
  loading,
  error,
  reload,
}: {
  loading: boolean;
  error: string;
  reload: () => void;
}) {
  return (
    <>
      {loading && (
        <p className="empty-state" role="status">
          Loading migrations…
        </p>
      )}
      {error && (
        <Notice error>
          <p>{error}</p>
          <Button onClick={reload} secondary>
            Refresh
          </Button>
        </Notice>
      )}
    </>
  );
}

export function MigrationStatus({ status }: { status: string }) {
  const tone = ["succeeded", "approved", "reverted"].includes(status)
    ? "success"
    : [
          "failed",
          "rejected",
          "uncertain",
          "invalidated",
          "partial",
          "partially_reverted",
        ].includes(status)
      ? "warning"
      : "neutral";
  return (
    <span className={`badge badge--${tone}`}>
      {status.replaceAll("_", " ")}
    </span>
  );
}

export function SQLDetails({
  up,
  down,
  irreversible,
  reason,
}: {
  up: string;
  down?: string;
  irreversible?: boolean;
  reason?: string;
}) {
  return (
    <details className="migration-sql-details">
      <summary>Inspect immutable SQL</summary>
      <h4>SQL up</h4>
      <pre tabIndex={0}>{up}</pre>
      {irreversible ? (
        <Notice error>Irreversible: {reason}</Notice>
      ) : (
        <>
          <h4>SQL down</h4>
          <pre tabIndex={0}>{down || "No down SQL available."}</pre>
        </>
      )}
    </details>
  );
}
