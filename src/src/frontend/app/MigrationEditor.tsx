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
  stamp,
  url,
  usePage,
} from "./ui";

export function MigrationEditor() {
  const { data, analysisState } = usePage();
  const [sql, setSQL] = useState(data.SQL);
  const [description, setDescription] = useState(data.Description);
  const [confirmed, setConfirmed] = useState(false);
  const [accepted, setAccepted] = useState(false);
  useEffect(() => {
    setSQL(data.SQL);
    setDescription(data.Description);
    setConfirmed(false);
    setAccepted(false);
  }, [data]);
  const stale = !!data.Review && data.Review.SQL !== sql;
  return (
    <>
      <Heading
        title="Create migration"
        text="Compose → Analyze → Review → Execute. Only Execute applies changes."
      >
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
          <div className="migration-layout workflow-editor">
            <Panel
              title={data.Review ? "Review SQL" : "SQL editor"}
              text={`${data.Database.Name} · ${data.Database.DBMS}`}
            >
              <div id="migration-sql-editor" className="workflow-scroll-target">
                <Form className="form-stack">
                  <Hidden name="db" value={data.Database.ID} />
                  <Hidden name="draft" value={data.DraftID || ""} />
                  <Hidden name="draft_updated" value={data.DraftUpdated} />
                  {data.Review && (
                    <Hidden name="review" value={pretty(data.Review)} />
                  )}
                  <div className="editor-fields">
                    <Field label="Description / draft title">
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
                        onChange={(event) => {
                          setSQL(event.target.value);
                          setConfirmed(false);
                          setAccepted(false);
                        }}
                        placeholder="ALTER TABLE ...;"
                      />
                    </div>
                  </div>
                  <p className="integration-form muted">
                    The backend manages transactions. Enter migration statements
                    without BEGIN or COMMIT.
                    {!!data.DraftID &&
                      " Save edited draft SQL before analysis."}
                  </p>
                  <div className="editor-actions">
                    <Button action="save_draft" secondary>
                      Save draft
                    </Button>
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
                      {!data.Review.Transactional && (
                        <label className="check-control">
                          <input
                            type="checkbox"
                            name="accept_non_atomic"
                            checked={accepted}
                            onChange={(event) =>
                              setAccepted(event.target.checked)
                            }
                          />
                          <span>
                            I accept non-atomic execution. Statements may commit
                            individually and a failure can leave partial
                            changes.
                          </span>
                        </label>
                      )}
                      <label className="check-control">
                        <input
                          type="checkbox"
                          name="confirm"
                          checked={confirmed}
                          onChange={(event) =>
                            setConfirmed(event.target.checked)
                          }
                        />
                        <span>
                          I reviewed the SQL and approve applying it to{" "}
                          {data.Database.Name}.
                        </span>
                      </label>
                      <Button
                        action="execute"
                        danger
                        disabled={
                          stale ||
                          !confirmed ||
                          (!data.Review.Transactional && !accepted)
                        }
                      >
                        Execute migration
                      </Button>
                    </div>
                  )}
                </Form>
              </div>
            </Panel>
            <aside className="panel guidance-panel saved-drafts">
              <header className="panel__header">
                <h2>Saved drafts</h2>
              </header>
              <div className="integration-form">
                {data.Drafts?.map((draft) => (
                  <article className="draft-entry" key={draft.ID}>
                    <Link
                      to={url("/migrations/new", {
                        db: data.Database.ID,
                        draft: draft.ID,
                      })}
                    >
                      {draft.Title}
                    </Link>
                    <small>{stamp(draft.UpdatedAt)}</small>
                    <Form confirm="Delete this draft?">
                      <Hidden name="db" value={data.Database.ID} />
                      <Hidden name="draft" value={draft.ID} />
                      <Button action="delete_draft" secondary>
                        Delete
                      </Button>
                    </Form>
                  </article>
                ))}
                {!data.Drafts?.length && (
                  <p>No saved drafts for this database.</p>
                )}
              </div>
            </aside>
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
