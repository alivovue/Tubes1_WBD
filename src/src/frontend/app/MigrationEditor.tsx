import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { VisualBuilder } from "./VisualBuilder";
import { QueryPlan } from "./QueryPlan";
import type { DiffCell } from "./types";
import {
  Button,
  DatabasePicker,
  Field,
  Form,
  Heading,
  Hidden,
  Notice,
  Panel,
  pretty,
  url,
  usePage,
} from "./ui";

export function MigrationEditor() {
  const { data, analysisState } = usePage();
  const [sql, setSQL] = useState(data.SQL);
  const [description, setDescription] = useState(data.Description);
  useEffect(() => {
    setSQL(data.SQL);
    setDescription(data.Description);
  }, [data]);
  const stale = !!data.Review && data.Review.SQL !== sql;
  const postgres = data.Database.DBMS === "postgresql";
  return (
    <>
      <Heading
        title="Create migration"
        icon="code"
        text="Compose and analyze SQL. Save PostgreSQL migrations in the up/down approval workflow."
      >
        {postgres && (
          <Link
            className="button button--primary"
            to={url("/migrations", { db: data.Database.ID, view: "definitions" })}
            state={{ migrationDraft: { title: description, up_sql: sql } }}
          >
            Up/down approval workflow
          </Link>
        )}
        {!!data.Database.ID && (
          <a className="button button--secondary" href="#query-plan">
            Query Plan
          </a>
        )}
        {!!data.Database.ID && (
          <Link
            className="button button--secondary"
            to={url("/migrations", { db: data.Database.ID })}
          >
            Changelog
          </Link>
        )}
      </Heading>
      <DatabasePicker databases={data.Databases} selected={data.Database.ID} />
      {data.Database.ID ? (
        <>
          <VisualBuilder
            description={description}
            setDescription={setDescription}
          />
          <div className="workflow-editor">
            <Panel
              title={data.Review ? "Review SQL" : "SQL editor"}
              text={`${data.Database.Name} · ${data.Database.DBMS}`}
            >
              <div id="migration-sql-editor" className="workflow-scroll-target">
                <Form className="form-stack">
                  <Hidden name="db" value={data.Database.ID} />
                  {data.Review && (
                    <Hidden name="review" value={pretty(data.Review)} />
                  )}
                  <div className="editor-fields">
                    <Field label="Migration title">
                      <input
                        name="description"
                        value={description}
                        onChange={(event) => setDescription(event.target.value)}
                        maxLength={255}
                        required
                      />
                    </Field>
                  </div>
                  <div className="code-editor">
                    <div className="code-editor__bar">
                      <span>migration.sql</span>
                      <span>{data.Database.DBMS}</span>
                    </div>
                    <div className="code-editor__body">
                      <textarea
                        name="sql"
                        spellCheck={false}
                        aria-label="SQL migration"
                        required
                        value={sql}
                        onChange={(event) => setSQL(event.target.value)}
                        placeholder="ALTER TABLE ...;"
                      />
                    </div>
                  </div>
                  <p className="integration-form muted">
                    The backend manages transactions. Enter migration statements
                    without BEGIN or COMMIT.
                  </p>
                  <div className="editor-actions">
                    <Button action="analyze">
                      {data.Review ? "Re-analyze SQL" : "Analyze migration"}
                    </Button>
                  </div>
                  {data.Review && (
                    <div className="integration-form">
                      {stale && (
                        <Notice error>
                          SQL changed. Analyze again before execution; the
                          previous diff and plans no longer apply.
                        </Notice>
                      )}
                      {postgres ? (
                        <>
                          <Notice>
                            Save this SQL in the approval workflow with SQL down
                            or an irreversible reason. Execution requires
                            approval from every access group's leader.
                          </Notice>
                          <Link
                            className="button button--primary"
                            to={url("/migrations", { db: data.Database.ID, view: "definitions" })}
                            state={{ migrationDraft: { title: description, up_sql: sql } }}
                          >
                            Use SQL in approval draft
                          </Link>
                        </>
                      ) : (
                        <Notice>
                          MariaDB SQL is available for analysis. Saved migrations
                          and execution require PostgreSQL.
                        </Notice>
                      )}
                    </div>
                  )}
                </Form>
              </div>
            </Panel>
          </div>
          {data.Review && !stale && <Analysis />}
          <QueryPlan
            plans={data.Review ? data.Review.Plans || [] : undefined}
            loading={analysisState.loading}
            error={analysisState.error}
            stale={stale}
          />
        </>
      ) : (
        <>
          <p className="empty-state">
            You need View + Edit access to a database to compose migrations.
            View Only users can inspect the changelog.
          </p>
          <Link className="button button--secondary" to="/migrations">
            View changelog
          </Link>
        </>
      )}
    </>
  );
}

function Analysis() {
  const { data } = usePage();
  const review = data.Review!;
  const diff = review.SchemaDiff;
  return (
    <div id="migration-analysis" className="workflow-scroll-target">
      <Panel
        title="Migration analysis"
        text="Informational preview of the current SQL. Execution rechecks permission, version and schema."
      >
        <div className="analysis-grid">
          {[
            ["Statements", review.Plans?.length || 0],
            ["Base version", review.BaseVersion],
            ["Execution mode", review.Mode],
          ].map(([label, value]) => (
            <article key={label}>
              <span>{label}</span>
              <strong>{value}</strong>
            </article>
          ))}
        </div>
        {review.Warnings?.map((warning, i) => (
          <Notice key={i} error>
            {warning}
          </Notice>
        ))}
        <div className="integration-form">
          <h3>Schema diff</h3>
          {review.SchemaDiffAvailable && diff ? (
            <>
              {diff.Destructive && (
                <Notice error>
                  The candidate includes destructive schema changes. Review the
                  generated SQL carefully.
                </Notice>
              )}
              {data.SchemaDiff && (
                <details className="schema-state-comparison" open>
                  <summary>Compared full schema states</summary>
                  <div
                    className="schema-diff-view"
                    role="group"
                    aria-label="Full schema state comparison"
                  >
                    <div className="schema-diff-view__toolbar">
                      <span>Complete schema diff</span>
                      <span className="schema-diff-view__stats">
                        <strong className="schema-diff-view__stat schema-diff-view__stat--added">
                          +{data.SchemaDiff.Added}
                        </strong>
                        <strong className="schema-diff-view__stat schema-diff-view__stat--removed">
                          −{data.SchemaDiff.Removed}
                        </strong>
                      </span>
                    </div>
                    <div className="schema-diff-view__scroll" tabIndex={0}>
                      <div className="schema-diff-view__table">
                        <div className="schema-diff-view__header-row">
                          <div className="schema-diff-view__header schema-diff-view__header--before">
                            Before · current schema
                          </div>
                          <div className="schema-diff-view__header schema-diff-view__header--after">
                            After · desired schema
                          </div>
                        </div>
                        {data.SchemaDiff.Rows.map((row, i) => (
                          <div className="schema-diff-view__row" key={i}>
                            <DiffLine cell={row.Before} />
                            <DiffLine cell={row.After} />
                          </div>
                        ))}
                      </div>
                    </div>
                  </div>
                </details>
              )}
              <h4>Changed objects</h4>
              <ul className="change-list">
                {diff.Changes?.map((change, i) => (
                  <li key={i}>
                    <strong>{change.Kind}</strong> · {change.Schema}.
                    {change.Table} {change.Name}
                  </li>
                ))}
                {!diff.Changes?.length && <li>No structural changes.</li>}
              </ul>
              <h3>Data-change summary</h3>
              <ul className="change-list">
                {diff.DataChanges?.map((change, i) => (
                  <li key={i}>
                    <strong>{change.Kind}</strong> · {change.Schema}.
                    {change.Table}
                    <pre>
                      Values: {pretty(change.Values)}
                      {"\n"}Filter: {pretty(change.Where)}
                    </pre>
                  </li>
                ))}
                {!diff.DataChanges?.length && (
                  <li>No structured row changes.</li>
                )}
              </ul>
            </>
          ) : (
            <p>{review.SchemaDiffMessage}</p>
          )}
        </div>
      </Panel>
    </div>
  );
}

function DiffLine({ cell }: { cell: DiffCell }) {
  return (
    <div
      className={`schema-diff-view__line schema-diff-view__line--${cell.Kind}`}
    >
      <span className="schema-diff-view__line-number">{cell.Line || ""}</span>
      <span className="schema-diff-view__line-marker">{cell.Marker}</span>
      <code>{cell.Text}</code>
    </div>
  );
}
