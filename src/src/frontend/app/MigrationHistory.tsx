import { useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { migrationPaths, migrationsAPI } from "./api";
import type { HistoryResponse, MigrationExecution } from "./types";
import { Button, Notice, Panel, stamp, Table, usePage } from "./ui";
import {
  ResourceState,
  migrationURL,
  useMigrationAction,
  useMigrationResource,
} from "./migrationUI";

// Defensive presentation filter for a complete received page. The server applies
// this across the full history before pagination, including cross-page pairs.
export function hideRollbackPairs(executions: MigrationExecution[]) {
  const hidden = new Set<string>();
  for (const event of executions) {
    if (event.operation === "rollback" && event.target_execution_id != null) {
      hidden.add(event.id);
      hidden.add(event.target_execution_id);
    }
  }
  return executions.filter((event) => !hidden.has(event.id));
}

export function MigrationHistory() {
  const { data } = usePage();
  const [params, setParams] = useSearchParams();
  const hide = params.get("hide_rollback") === "true";
  const cursor = params.get("cursor") || undefined;
  const resource = useMigrationResource<HistoryResponse>(
    migrationPaths.history(data.Database.ID, hide, cursor),
  );
  const selected = params.get("execution");
  const events = resource.data?.executions || [];
  const visible = hide ? hideRollbackPairs(events) : events;
  const execution = visible.find((event) => String(event.id) === selected);
  function filter(checked: boolean) {
    const next = new URLSearchParams(params);
    next.set("hide_rollback", String(checked));
    next.delete("cursor");
    next.delete("execution");
    setParams(next);
  }
  return (
    <>
      <div className="toolbar">
        <label className="check-control">
          <input
            type="checkbox"
            checked={hide}
            onChange={(event) => filter(event.target.checked)}
          />
          <span>Hide rollback</span>
        </label>
        <Button secondary onClick={resource.reload}>
          Refresh history
        </Button>
        <Link to={migrationURL("jobs", data.Database.ID)}>
          View failures and job attempts
        </Link>
      </div>
      <p className="muted">
        Only committed executions appear here. Hide rollback removes each
        successful rollback and its exact target execution from this view.
        Stored history is preserved; later forward executions remain visible.
      </p>
      <ResourceState {...resource} />
      {resource.data && (
        <>
          {selected && !execution && (
            <Notice>
              The selected execution is unavailable on this page or is hidden by
              the rollback filter.
            </Notice>
          )}
          {execution && (
            <ExecutionDetail
              key={execution.id}
              execution={execution}
              stale={!!resource.error}
            />
          )}
          <Panel title="Committed execution history">
            <Table
              headers={[
                "Execution / migration",
                "Version / revision",
                "Action / pairing",
                "Actor",
                "Committed",
                "Duration",
                "Job",
              ]}
              rows={visible.map((event) => [
                <Link
                  to={migrationURL("history", data.Database.ID, {
                    execution: event.id,
                    hide_rollback: String(hide),
                    ...(cursor ? { cursor } : {}),
                  })}
                >
                  #{event.id} · {event.title}
                </Link>,
                `v${event.version} / r${event.revision}`,
                <>
                  {event.operation.replaceAll("_", " ")}
                  {event.target_execution_id != null && (
                    <small className="migration-secondary">
                      Reverses / references execution #
                      {event.target_execution_id}
                    </small>
                  )}
                </>,
                event.actor,
                stamp(event.committed_at),
                event.duration_ms == null ? "Unavailable" : `${event.duration_ms.toLocaleString()} ms`,
                <Link
                  to={migrationURL("jobs", data.Database.ID, {
                    job: event.job_id,
                  })}
                >
                  {event.job_id}
                </Link>,
              ])}
              empty="No committed executions match this view."
            />
          </Panel>
          <div className="toolbar">
            {cursor && (
              <Link
                className="button button--secondary"
                to={migrationURL("history", data.Database.ID, {
                  hide_rollback: String(hide),
                })}
              >
                Latest executions
              </Link>
            )}
            {resource.data.next_cursor && (
              <Link
                className="button button--secondary"
                to={migrationURL("history", data.Database.ID, {
                  hide_rollback: String(hide),
                  cursor: resource.data.next_cursor,
                })}
              >
                Older executions
              </Link>
            )}
          </div>
        </>
      )}
    </>
  );
}

function ExecutionDetail({
  execution,
  stale,
}: {
  execution: MigrationExecution;
  stale: boolean;
}) {
  const { data } = usePage();
  const navigate = useNavigate();
  const action = useMigrationAction();
  const [confirmed, setConfirmed] = useState(false);
  async function request(operation: "rollback" | "forward") {
    if (!confirmed) return;
    const result = await action.run((csrf) =>
      migrationsAPI.executionReview(execution, operation, csrf),
    );
    if (result)
      navigate(
        migrationURL("reviews", data.Database.ID, { review: result.review.id }),
      );
  }
  return (
    <Panel
      title={`Execution #${execution.id} · ${execution.title}`}
      text={`${execution.operation.replaceAll("_", " ")} committed by ${execution.actor} on ${stamp(execution.committed_at)}`}
    >
      <div className="integration-form form-stack">
        <p>
          Definition #{execution.definition_id} · Revision {execution.revision}{" "}
          · Version {execution.version} · Review #{execution.review_id}
        </p>
        {execution.target_execution_id != null && (
          <p>Exact target execution #{execution.target_execution_id}</p>
        )}
        <h3>Exact executed SQL</h3>
        <pre tabIndex={0}>{execution.sql}</pre>
        {execution.irreversible && (
          <Notice error>
            Irreversible: {execution.irreversibility_reason}. Rollback is
            unavailable.
          </Notice>
        )}
        {execution.action_blocked_reason && (
          <Notice>{execution.action_blocked_reason}</Notice>
        )}
        {action.error && <Notice error>{action.error}</Notice>}
        {data.CanEdit &&
          ((execution.can_rollback && !execution.irreversible) ||
            execution.can_forward) && (
            <>
              <Notice>
                Rollback uses the original SQL down for the last active
                reversible migration. Forward reapplies only the original SQL up
                for this revision. Every action requires a new leader review and
                a new background job.
              </Notice>
              <label className="check-control">
                <input
                  type="checkbox"
                  checked={confirmed}
                  disabled={stale || action.busy}
                  onChange={(event) => setConfirmed(event.target.checked)}
                />
                <span>
                  Request review for execution #{execution.id} on{" "}
                  {data.Database.Name}.
                </span>
              </label>
              <div className="heading-actions">
                {execution.can_rollback && !execution.irreversible && (
                  <Button
                    danger
                    onClick={() => request("rollback")}
                    disabled={!confirmed || stale || action.busy}
                  >
                    Request rollback review
                  </Button>
                )}
                {execution.can_forward && (
                  <Button
                    onClick={() => request("forward")}
                    disabled={!confirmed || stale || action.busy}
                  >
                    Request forward review
                  </Button>
                )}
              </div>
            </>
          )}
      </div>
    </Panel>
  );
}
