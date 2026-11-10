import { useId, useState } from "react";
import type { PlanNode, StatementPlan } from "./types";
import { Icon, Notice, pretty } from "./ui";

type QueryPlanProps = {
  plans?: StatementPlan[] | null;
  loading?: boolean;
  error?: string;
  stale?: boolean;
};

export function QueryPlan({ plans, loading, error, stale }: QueryPlanProps) {
  return (
    <section
      className="panel query-plan workflow-scroll-target"
      id="query-plan"
      aria-labelledby="query-plan-title"
      aria-busy={loading || false}
      tabIndex={-1}
    >
      <header className="panel__header">
        <div>
          <p className="eyebrow">SQL analysis</p>
          <h2 id="query-plan-title">Query Plan</h2>
          <p>
            Explore how the database planner expects to process each statement.
          </p>
        </div>
        <span className="badge badge--view">Estimates only</span>
      </header>
      <div className="integration-form">
        <p className="muted">
          Row counts and costs are planner estimates. Costs use the database
          planner’s own units, not milliseconds. No actual execution timings
          were collected.
        </p>
        {loading ? (
          <p className="query-plan__state" role="status">
            Analyzing query plans…
          </p>
        ) : error ? (
          <Notice error>
            <strong>Query-plan analysis failed.</strong> {error} Correct the SQL
            or retry analysis.
          </Notice>
        ) : stale ? (
          <Notice>
            SQL changed. Analyze again to refresh the query plans.
          </Notice>
        ) : plans == null ? (
          <p className="query-plan__state">
            Analyze a migration to preview its query plans.
          </p>
        ) : plans.length === 0 ? (
          <p className="query-plan__state">
            No statement plans were returned for this analysis.
          </p>
        ) : (
          <div className="query-plan__statements">
            {plans.map((plan, index) => (
              <StatementPlanCard key={index} plan={plan} index={index} />
            ))}
          </div>
        )}
      </div>
    </section>
  );
}

function StatementPlanCard({
  plan,
  index,
}: {
  plan: StatementPlan;
  index: number;
}) {
  const id = useId();
  const hasNodes = plan.Available && !!plan.Nodes?.length;
  const unsupported =
    !plan.Available && plan.UnavailableReason === "unsupported_statement";
  return (
    <article className="query-plan-statement" aria-labelledby={`${id}-title`}>
      <header className="query-plan-statement__header">
        <h3 id={`${id}-title`}>Statement {index + 1}</h3>
        <div className="heading-actions">
          {plan.DBMS && (
            <span className="badge badge--neutral">{plan.DBMS}</span>
          )}
          <span className={`badge badge--${hasNodes ? "success" : "warning"}`}>
            {hasNodes
              ? "Plan available"
              : unsupported
                ? "Unsupported statement"
                : "Plan unavailable"}
          </span>
        </div>
      </header>
      <pre
        className="query-plan-statement__sql"
        aria-label={`SQL for statement ${index + 1}`}
      >
        <code>{plan.Statement}</code>
      </pre>
      {hasNodes ? (
        <PlanTree nodes={plan.Nodes!} statement={index + 1} />
      ) : (
        <div className="query-plan__state">
          <p>
            {unsupported
              ? "The database does not provide an execution plan for this statement type."
              : plan.Message ||
                "The database returned no recognized plan nodes for this statement."}
          </p>
          {plan.Available && (
            <p className="muted">
              Inspect the original EXPLAIN JSON below for the returned database
              output.
            </p>
          )}
        </div>
      )}
      {plan.JSON != null && <OriginalJSON value={plan.JSON} />}
    </article>
  );
}

function PlanTree({
  nodes,
  statement,
}: {
  nodes: PlanNode[];
  statement: number;
}) {
  const id = useId();
  const [expanded, setExpanded] = useState(
    () => new Set(nodes.map((_, index) => String(index))),
  );
  const paths: string[] = [];
  function visit(items: PlanNode[], parent = "") {
    items.forEach((node, index) => {
      const path = parent ? `${parent}-${index}` : String(index);
      if (node.Children?.length) {
        paths.push(path);
        visit(node.Children, path);
      }
    });
  }
  visit(nodes);
  function toggle(path: string) {
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
  }
  function renderNode(node: PlanNode, path: string) {
    const children = node.Children || [];
    const open = expanded.has(path);
    const operation = node.Operation || "Unknown operation";
    return (
      <li className="query-plan-node" key={path}>
        <div className="query-plan-node__card">
          {children.length ? (
            <button
              className="query-plan-node__heading query-plan-node__toggle"
              type="button"
              aria-expanded={open}
              aria-controls={`${id}-${path}`}
              aria-label={`${open ? "Collapse" : "Expand"} ${operation}`}
              onClick={() => toggle(path)}
            >
              <Icon name="chevron" />
              <strong>{operation}</strong>
              <span className="query-plan-node__count">
                {children.length} child{" "}
                {children.length === 1 ? "node" : "nodes"}
              </span>
            </button>
          ) : (
            <div className="query-plan-node__heading">
              <Icon name="code" />
              <strong>{operation}</strong>
            </div>
          )}
          <dl className="query-plan-node__metrics">
            {node.Relation && <Metric label="Relation" value={node.Relation} />}
            {node.Index && <Metric label="Index" value={node.Index} />}
            {node.JoinType && (
              <Metric label="Join type" value={node.JoinType} />
            )}
            <Metric
              label="Estimated rows"
              value={estimate(node.EstimatedRows)}
            />
            <Metric
              label="Estimated startup cost"
              value={estimate(node.StartupCost)}
            />
            <Metric
              label="Estimated total cost"
              value={estimate(node.TotalCost)}
            />
            {node.FilteredPercent != null && (
              <Metric
                label="Estimated filtered rows (%)"
                value={estimate(node.FilteredPercent)}
              />
            )}
          </dl>
        </div>
        {!!children.length && (
          <ul
            id={`${id}-${path}`}
            className="query-plan-tree__children"
            hidden={!open}
          >
            {children.map((child, index) =>
              renderNode(child, `${path}-${index}`),
            )}
          </ul>
        )}
      </li>
    );
  }
  return (
    <>
      {!!paths.length && (
        <div
          className="query-plan-tree__actions"
          role="group"
          aria-label={`Tree controls for statement ${statement}`}
        >
          <button
            className="button button--secondary button--small"
            type="button"
            onClick={() => setExpanded(new Set(paths))}
          >
            Expand all
          </button>
          <button
            className="button button--secondary button--small"
            type="button"
            onClick={() => setExpanded(new Set())}
          >
            Collapse all
          </button>
        </div>
      )}
      <div
        className="query-plan-tree__scroll"
        tabIndex={0}
        aria-label={`Scrollable query plan for statement ${statement}`}
      >
        <ul
          className="query-plan-tree"
          aria-label={`Query plan nodes for statement ${statement}`}
        >
          {nodes.map((node, index) => renderNode(node, String(index)))}
        </ul>
      </div>
    </>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>{value}</dd>
    </div>
  );
}

const numberFormat = new Intl.NumberFormat(undefined, {
  maximumSignificantDigits: 15,
});
function estimate(value: number | null | undefined) {
  return value != null && Number.isFinite(value)
    ? numberFormat.format(value)
    : "Not available";
}

function OriginalJSON({ value }: { value: unknown }) {
  const [status, setStatus] = useState("");
  const json = pretty(value);
  async function copy() {
    try {
      if (!navigator.clipboard?.writeText)
        throw new Error("Clipboard unavailable");
      await navigator.clipboard.writeText(json);
      setStatus("JSON copied.");
    } catch {
      setStatus(
        "Copy unavailable. Select the JSON below and copy it manually.",
      );
    }
  }
  return (
    <details className="query-plan-json">
      <summary>Original EXPLAIN JSON</summary>
      <div className="query-plan-json__actions">
        <button
          className="button button--secondary button--small"
          type="button"
          onClick={copy}
        >
          Copy JSON
        </button>
        {status && <span role="status">{status}</span>}
      </div>
      <pre tabIndex={0} aria-label="Original EXPLAIN JSON">
        <code>{json}</code>
      </pre>
    </details>
  );
}
