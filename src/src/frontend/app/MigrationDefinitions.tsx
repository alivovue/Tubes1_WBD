import { useState, type FormEvent } from "react";
import { Link, useLocation, useNavigate, useSearchParams } from "react-router-dom";
import { migrationPaths, migrationsAPI } from "./api";
import type {
  DefinitionInput,
  DefinitionsResponse,
  MigrationDefinition,
} from "./types";
import { Button, Field, Notice, Panel, stamp, usePage } from "./ui";
import { MigrationBatch, batchSQL } from "./MigrationBatch";
import {
  MigrationStatus,
  ResourceState,
  SQLDetails,
  useMigrationAction,
  useMigrationResource,
  migrationURL,
} from "./migrationUI";

export function MigrationDefinitions() {
  const { data } = usePage();
  const [params] = useSearchParams();
  const resource = useMigrationResource<DefinitionsResponse>(
    migrationPaths.definitions(data.Database.ID),
  );
  const selected = params.get("definition");
  const copy = params.get("copy");
  const definition = resource.data?.definitions.find(
    (item) => String(item.id) === selected,
  );
  const source = resource.data?.definitions.find(
    (item) => String(item.id) === copy,
  );
  return (
    <>
      <ResourceState {...resource} />
      {resource.data && (
        <div className="migration-layout migration-definition-layout">
          <Panel
            title={
              definition
                ? `Revision ${definition.revision} · ${definition.title}`
                : "New migration draft"
            }
          >
            {(selected && !definition) || (copy && !source) ? (
              <Notice error>
                This definition is unavailable. Refresh or select another draft.
              </Notice>
            ) : (
              <DefinitionEditor
                key={`${definition?.id || `copy-${source?.id || "new"}`}-${definition?.revision || 0}`}
                definition={definition}
                source={source}
                reload={resource.reload}
              />
            )}
          </Panel>
          <Panel
            title="Definitions"
            text="Submitted revisions are frozen. Executed SQL stays immutable."
          >
            <div className="migration-card-list">
              <Link
                className="button button--secondary"
                to={migrationURL("definitions", data.Database.ID)}
              >
                New draft
              </Link>
              {resource.data.definitions.map((item) => (
                <article className="migration-list-entry" key={item.id}>
                  <Link
                    to={migrationURL("definitions", data.Database.ID, {
                      definition: item.id,
                    })}
                  >
                    {item.title}
                  </Link>
                  <p>
                    Revision {item.revision} ·{" "}
                    <MigrationStatus status={item.status} />
                  </p>
                  <small>
                    Updated {stamp(item.updated_at)} · {item.created_by}
                  </small>
                </article>
              ))}
              {!resource.data.definitions.length && (
                <p>No migration definitions yet.</p>
              )}
            </div>
          </Panel>
        </div>
      )}
    </>
  );
}

function DefinitionEditor({
  definition,
  source,
  reload,
}: {
  definition?: MigrationDefinition;
  source?: MigrationDefinition;
  reload: () => void;
}) {
  const { data } = usePage();
  const navigate = useNavigate();
  const location = useLocation();
  const carried = location.state?.migrationDraft as
    | { title?: string; up_sql?: string }
    | undefined;
  const original = definition || source;
  const baseline: DefinitionInput = {
    database_id: data.Database.ID,
    title: original?.title ?? carried?.title ?? "",
    up_sql: original?.up_sql ?? carried?.up_sql ?? "",
    down_sql: original?.down_sql || "",
    irreversible: original?.irreversible || false,
    irreversibility_reason: original?.irreversibility_reason || "",
    batch: original?.batch || null,
    ...(definition ? { expected_revision_id: definition.revision_id } : {}),
  };
  const [input, setInput] = useState<DefinitionInput>(baseline);
  const [confirmed, setConfirmed] = useState(false);
  const action = useMigrationAction();
  const editable =
    data.CanEdit &&
    (!definition || (definition.editable && definition.status === "draft"));
  const dirty = JSON.stringify(input) !== JSON.stringify(baseline);
  const postgres = data.Database.DBMS === "postgresql";
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!editable) return;
    const result = await action.run((csrf) =>
      migrationsAPI.save(input, csrf, definition?.id),
    );
    if (result) {
      reload();
      navigate(
        migrationURL("definitions", data.Database.ID, {
          definition: result.definition.id,
        }),
      );
    }
  }
  async function submit() {
    if (!definition || dirty || !confirmed) return;
    const result = await action.run((csrf) =>
      migrationsAPI.submit(definition, csrf),
    );
    if (result)
      navigate(
        migrationURL("reviews", data.Database.ID, { review: result.review.id }),
      );
  }
  return (
    <form className="form-stack integration-form" onSubmit={save}>
      {action.error && <Notice error>{action.error}</Notice>}
      {source && (
        <Notice>
          Creating a new draft from definition #{source.id}, revision{" "}
          {source.revision}. Saving creates a separate definition for a new
          approval cycle.
        </Notice>
      )}
      {!data.CanEdit && (
        <Notice>
          View Only access allows inspection and assigned leader decisions.
          Creating, editing and submitting requires View + Edit.
        </Notice>
      )}
      {definition && !editable && (
        <Notice>
          This revision is frozen. Its original SQL is reused for rollback and
          forward.
        </Notice>
      )}
      <fieldset className="form-fields" disabled={action.busy || !editable}>
        <Field label="Migration title">
          <input
            required
            maxLength={255}
            value={input.title}
            onChange={(event) =>
              setInput({ ...input, title: event.target.value })
            }
          />
        </Field>
        <Field label="SQL up">
          <textarea
            className="migration-sql-input"
            spellCheck={false}
            required
            rows={10}
            wrap="off"
            value={input.up_sql}
            readOnly={!!input.batch}
            onChange={(event) =>
              setInput({ ...input, up_sql: event.target.value })
            }
            placeholder="ALTER TABLE ...;"
          />
        </Field>
        <label className="check-control">
          <input
            type="checkbox"
            checked={input.irreversible}
            disabled={!!input.batch}
            onChange={(event) =>
              setInput({
                ...input,
                irreversible: event.target.checked,
                down_sql: event.target.checked ? "" : input.down_sql,
                irreversibility_reason: event.target.checked
                  ? input.irreversibility_reason
                  : "",
              })
            }
          />
          <span>This migration is irreversible</span>
        </label>
        {input.irreversible ? (
          <Field label="Why is this migration irreversible?">
            <textarea
              required
              maxLength={2000}
              rows={3}
              value={input.irreversibility_reason}
              onChange={(event) =>
                setInput({
                  ...input,
                  irreversibility_reason: event.target.value,
                })
              }
            />
          </Field>
        ) : (
          <Field label="SQL down">
            <textarea
              className="migration-sql-input"
              spellCheck={false}
              required
              rows={7}
              wrap="off"
              value={input.down_sql}
              readOnly={!!input.batch}
              onChange={(event) =>
                setInput({ ...input, down_sql: event.target.value })
              }
              placeholder="SQL that safely reverses this migration"
            />
          </Field>
        )}
        <p className="muted">
          The worker owns transactions. Enter SQL without BEGIN or COMMIT.
        </p>
        {postgres && (
          <MigrationBatch
            value={input.batch}
            onChange={(batch) =>
              setInput({
                ...input,
                batch,
                ...(batch
                  ? {
                      ...batchSQL(batch),
                      irreversible: false,
                      irreversibility_reason: "",
                    }
                  : {}),
              })
            }
            disabled={!editable}
          />
        )}
        {!postgres && (
          <Notice>
            Approval, rollback and batch workflows require PostgreSQL. Existing
            MariaDB tools remain available in the SQL editor.
          </Notice>
        )}
        {editable && (
          <Button
            disabled={!postgres || action.busy || (!!definition && !dirty)}
          >
            {action.busy ? "Saving…" : "Save draft"}
          </Button>
        )}
      </fieldset>
      {definition && (
        <>
          {!editable && data.CanEdit && (
            <Link
              className="button button--secondary"
              to={migrationURL("definitions", data.Database.ID, {
                copy: definition.id,
              })}
            >
              Copy to new draft
            </Link>
          )}
          <SQLDetails
            up={definition.up_sql}
            down={definition.down_sql}
            irreversible={definition.irreversible}
            reason={definition.irreversibility_reason}
          />
          <p className="muted">
            Definition #{definition.id} · Revision {definition.revision} · Hash{" "}
            <code>{definition.definition_hash}</code>
          </p>
          {editable && (
            <>
              {dirty && (
                <Notice>Save your changes before submitting for review.</Notice>
              )}
              <label className="check-control">
                <input
                  type="checkbox"
                  checked={confirmed}
                  disabled={dirty || action.busy}
                  onChange={(event) => setConfirmed(event.target.checked)}
                />
                <span>
                  Freeze revision {definition.revision} and request approval
                  from every group with access to {data.Database.Name}.
                </span>
              </label>
              <Button
                onClick={submit}
                disabled={dirty || !confirmed || action.busy || !postgres}
              >
                Submit for approval
              </Button>
            </>
          )}
        </>
      )}
    </form>
  );
}
