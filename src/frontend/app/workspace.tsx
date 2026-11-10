import { useState, type FormEvent } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import {
  Button,
  DatabasePicker,
  Field,
  Form,
  Heading,
  Hidden,
  initials,
  Pagination,
  Panel,
  permission,
  Results,
  stamp,
  Table,
  url,
  usePage,
} from "./ui";

export function Dashboard() {
  const { data } = usePage();
  return (
    <>
      <Heading
        title={`Welcome, ${data.User.DisplayName}.`}
        text="Ringkasan database dan perubahan yang dapat Anda akses."
      />
      <section className="metrics-grid" aria-label="Ringkasan">
        {[
          [
            "Accessible databases",
            data.Databases?.length || 0,
            "Based on group permissions",
          ],
          ["Editable databases", data.EditableCount, "View + Edit"],
          [
            "Account role",
            "Employee",
            "Database access is granted separately",
          ],
          ["Session expires", stamp(data.Expires), "Idle timeout: 30 minutes"],
        ].map(([label, value, detail]) => (
          <article className="metric-card" key={label}>
            <div>
              <span>{label}</span>
              <strong
                className={
                  typeof value === "string" ? "metric-card__permission" : ""
                }
              >
                {value}
              </strong>
              <small>{detail}</small>
            </div>
          </article>
        ))}
      </section>
      <Panel
        title="Your databases"
        text="Akses efektif menggunakan permission tertinggi dari semua grup."
      >
        <Table
          headers={["Database", "DBMS", "Permission", "Actions"]}
          rows={(data.Databases || []).map((db) => [
            <Link to={url("/explorer", { db: db.ID })}>{db.Name}</Link>,
            db.DBMS,
            permission(db.Permission),
            <Link to={url("/migrations", { db: db.ID })}>Changelog</Link>,
          ])}
          empty="No databases assigned. Contact a platform admin for group access."
        />
      </Panel>
      <Panel
        title="Recent migrations"
        text="Latest attempts across your databases."
      >
        <Table
          headers={["Database", "Migration", "Executor", "Time", "Status"]}
          rows={(data.Recent || []).map((item) => [
            item.DatabaseName,
            <Link to={url("/migrations", { db: item.DatabaseID, id: item.ID })}>
              {item.Description || `Attempt #${item.ID}`}
            </Link>,
            item.Username,
            stamp(item.StartedAt),
            item.Status,
          ])}
          empty="No migration history yet."
        />
      </Panel>
    </>
  );
}

export function Databases() {
  const { data } = usePage();
  const [search, setSearch] = useState("");
  const databases = (data.Databases || []).filter((db) =>
    `${db.Name} ${db.DBMS} ${db.DatabaseName}`
      .toLowerCase()
      .includes(search.toLowerCase()),
  );
  return (
    <>
      <Heading
        title="My Databases"
        text="Database yang dapat Anda akses berdasarkan keanggotaan grup."
      >
        <Link className="button button--secondary" to="/migrations">
          View changelog
        </Link>
      </Heading>
      <section className="toolbar">
        <Field label="Search databases">
          <input
            type="search"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
        </Field>
        <span>{data.Databases?.length || 0} databases</span>
      </section>
      <section className="database-grid" aria-label="Daftar database">
        {databases.map((db) => (
          <article className="database-card" key={db.ID}>
            <div className="database-card__top">
              <span className="db-mark db-mark--blue">{initials(db.Name)}</span>
              <span className="badge badge--neutral">{db.DBMS}</span>
            </div>
            <h2>{db.Name}</h2>
            <p>{db.DatabaseName}</p>
            <div className="database-card__footer">
              <span
                className={`badge badge--${db.Permission === "view_edit" ? "edit" : "view"}`}
              >
                {permission(db.Permission)}
              </span>
              <Link
                className="button button--secondary button--small"
                to={url("/explorer", { db: db.ID })}
              >
                Open explorer
              </Link>
            </div>
          </article>
        ))}
        {!databases.length && (
          <p className="empty-state">
            {data.Databases?.length
              ? "No matching databases."
              : "No databases assigned. Contact your platform admin."}
          </p>
        )}
      </section>
    </>
  );
}

export function Explorer() {
  const { data, refresh } = usePage();
  const [search, setSearch] = useState("");
  const location = useLocation();
  const navigate = useNavigate();
  const params = Object.fromEntries(new URLSearchParams(location.search));
  const requestedTab = params.tab || "columns-panel";
  const tab = [
    "columns-panel",
    "constraints-panel",
    "indexes-panel",
    "rows-panel",
  ].includes(requestedTab)
    ? requestedTab
    : "columns-panel";
  const fields = {
    db: data.Database.ID,
    schema: data.Schema,
    table: data.Table,
  };
  function sort(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    navigate(
      url("/explorer", {
        ...fields,
        ...(Object.fromEntries(new FormData(event.currentTarget)) as Record<
          string,
          string
        >),
        tab: "rows-panel",
      }),
    );
  }
  return (
    <>
      <Heading
        title={data.Database.Name || "Database Explorer"}
        text={`${data.Database.DBMS || ""} · ${permission(data.Database.Permission)}`}
      >
        <Link className="button button--secondary" to="/databases">
          Change database
        </Link>
        {!!data.Database.ID && (
          <>
            <Button secondary onClick={refresh}>
              Refresh database
            </Button>
            <Link
              className="button button--secondary"
              to={url("/migrations", { db: data.Database.ID })}
            >
              Changelog
            </Link>
          </>
        )}
        {data.CanEdit && (
          <Link
            className="button button--primary"
            to={url("/migrations/new", { db: data.Database.ID })}
          >
            New migration
          </Link>
        )}
      </Heading>
      {data.Database.ID ? (
        <>
          <p className="muted">
            Refresh this view to load changes committed directly in the
            database.
          </p>
          <section className="explorer-shell">
            <aside className="schema-panel" aria-label="Database objects">
              <div className="tree">
                <strong>{data.Database.Name}</strong>
                {data.Schemas?.map((schema) => (
                  <Link
                    className={`tree__schema ${data.Schema === schema.Name ? "is-active" : ""}`}
                    key={schema.Name}
                    to={url("/explorer", {
                      db: data.Database.ID,
                      schema: schema.Name,
                    })}
                  >
                    {schema.Name}
                  </Link>
                ))}
                <Field label="Find table">
                  <input
                    type="search"
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                  />
                </Field>
                <div className="tree__items">
                  {data.Tables?.filter((table) =>
                    table.Name.toLowerCase().includes(search.toLowerCase()),
                  ).map((table) => (
                    <Link
                      className={`tree__item ${data.Table === table.Name ? "is-active" : ""}`}
                      key={table.Name}
                      to={url("/explorer", {
                        db: data.Database.ID,
                        schema: data.Schema,
                        table: table.Name,
                      })}
                    >
                      {table.Name}
                    </Link>
                  ))}
                  {!data.Tables?.length && (
                    <p className="empty-state">No tables in this schema.</p>
                  )}
                  {!!data.Tables?.length &&
                    !data.Tables.some((table) =>
                      table.Name.toLowerCase().includes(search.toLowerCase()),
                    ) && <p>No matching tables.</p>}
                </div>
              </div>
            </aside>
            <div className="object-panel">
              {data.Detail ? (
                <>
                  <header className="object-panel__header">
                    <div>
                      <div>
                        <p>
                          {data.Schema} / {data.Detail.Table.Kind}
                        </p>
                        <h2>{data.Table}</h2>
                      </div>
                    </div>
                  </header>
                  <div
                    className="tabs"
                    role="tablist"
                    aria-label="Table details"
                  >
                    {[
                      ["columns-panel", "Columns"],
                      ["constraints-panel", "Constraints"],
                      ["indexes-panel", "Indexes"],
                      ["rows-panel", "Data"],
                    ].map(([id, label], index, all) => (
                      <button
                        key={id}
                        className={`tab ${tab === id ? "is-active" : ""}`}
                        id={`${id}-tab`}
                        role="tab"
                        aria-selected={tab === id}
                        aria-controls={id}
                        tabIndex={tab === id ? 0 : -1}
                        onClick={() =>
                          navigate(
                            url("/explorer", { ...params, ...fields, tab: id }),
                          )
                        }
                        onKeyDown={(event) => {
                          if (
                            ["ArrowRight", "ArrowLeft", "Home", "End"].includes(
                              event.key,
                            )
                          ) {
                            event.preventDefault();
                            const next =
                              event.key === "Home"
                                ? 0
                                : event.key === "End"
                                  ? all.length - 1
                                  : (index +
                                      (event.key === "ArrowRight" ? 1 : -1) +
                                      all.length) %
                                    all.length;
                            document
                              .getElementById(`${all[next][0]}-tab`)
                              ?.focus();
                            navigate(
                              url("/explorer", {
                                ...params,
                                ...fields,
                                tab: all[next][0],
                              }),
                            );
                          }
                        }}
                      >
                        {label}
                      </button>
                    ))}
                  </div>
                  <section
                    id={tab}
                    role="tabpanel"
                    aria-labelledby={`${tab}-tab`}
                  >
                    {tab === "columns-panel" && (
                      <Table
                        headers={[
                          "Column",
                          "Type",
                          "Nullable",
                          "Default / Generated",
                        ]}
                        rows={(data.Detail.Columns || []).map((column) => [
                          <code>{column.Name}</code>,
                          column.DataType,
                          column.Nullable ? "Yes" : "No",
                          `${column.GeneratedExpression || column.Default || ""}${column.Identity ? ` · Identity ${column.Identity}` : ""}`,
                        ])}
                      />
                    )}
                    {tab === "constraints-panel" && (
                      <Table
                        headers={["Name", "Type", "Definition"]}
                        rows={(data.Detail.Constraints || []).map((item) => [
                          item.Name,
                          item.Type,
                          <>
                            <code>{item.Definition}</code>
                            {item.ReferencedTable && (
                              <small>
                                References {item.ReferencedSchema}.
                                {item.ReferencedTable} (
                                {item.ReferencedColumns?.join(", ")}) · UPDATE{" "}
                                {item.UpdateRule} · DELETE {item.DeleteRule}
                              </small>
                            )}
                          </>,
                        ])}
                        empty="No constraints."
                      />
                    )}
                    {tab === "indexes-panel" && (
                      <Table
                        headers={["Name", "Method", "Unique", "Definition"]}
                        rows={(data.Detail.Indexes || []).map((item) => [
                          item.Name,
                          item.Method,
                          item.Unique ? "Yes" : "No",
                          <code>{item.Definition}</code>,
                        ])}
                        empty="No indexes."
                      />
                    )}
                    {tab === "rows-panel" && (
                      <>
                        <form className="toolbar" onSubmit={sort}>
                          <Field label="Sort column">
                            <select name="sort" defaultValue={data.Sort}>
                              <option value="">Default order</option>
                              {data.Detail.Columns?.map((column) => (
                                <option key={column.Name}>{column.Name}</option>
                              ))}
                            </select>
                          </Field>
                          <Field label="Direction">
                            <select
                              name="direction"
                              defaultValue={data.Direction || "asc"}
                            >
                              <option value="asc">Ascending</option>
                              <option value="desc">Descending</option>
                            </select>
                          </Field>
                          <Button secondary>Apply</Button>
                        </form>
                        {data.Data && <Results data={data.Data} />}
                        <Pagination />
                      </>
                    )}
                  </section>
                </>
              ) : (
                <p className="empty-state">
                  Select a schema with tables to inspect.
                </p>
              )}
            </div>
          </section>
        </>
      ) : (
        <p className="empty-state">
          No databases assigned. Contact your platform admin.
        </p>
      )}
    </>
  );
}

export function History() {
  const { data } = usePage();
  const execution = data.Execution;
  return (
    <>
      <Heading
        title="Changelog"
        text="Linear history of execution attempts for each database."
      >
        {data.CanEdit && (
          <Link
            className="button button--primary"
            to={url("/migrations/new", { db: data.Database.ID })}
          >
            Create migration
          </Link>
        )}
      </Heading>
      <DatabasePicker databases={data.Databases} selected={data.Database.ID} />
      {data.Database.ID ? (
        <>
          {execution && (
            <Panel
              title={`Attempt #${execution.ID} · ${execution.Status}`}
              text={execution.Description}
            >
              <div className="integration-form">
                <dl className="detail-grid">
                  {[
                    [
                      "Applied version",
                      execution.AppliedVersion
                        ? `v${execution.AppliedVersion}`
                        : "—",
                    ],
                    ["Attempted version", execution.AttemptedVersion],
                    ["Executor", execution.Username],
                    ["Started", stamp(execution.StartedAt)],
                    ["Mode", execution.Mode],
                    [
                      "Committed statements",
                      `${execution.CompletedStatements} / ${execution.TotalStatements}`,
                    ],
                  ].map(([label, value]) => (
                    <div key={label}>
                      <dt>{label}</dt>
                      <dd>{value}</dd>
                    </div>
                  ))}
                </dl>
                {execution.ErrorMessage && (
                  <p className="notice notice--warning" role="alert">
                    {execution.ErrorMessage}
                  </p>
                )}
                <Field label="Executed SQL">
                  <textarea
                    className="review-editor"
                    readOnly
                    value={execution.SQL}
                  />
                </Field>
                {data.CanEdit && execution.Status === "running" && (
                  <>
                    <Form>
                      <Hidden name="db" value={data.Database.ID} />
                      <Button action="reconcile" secondary>
                        Reconcile from target receipt
                      </Button>
                    </Form>
                  </>
                )}
              </div>
            </Panel>
          )}
          <Panel>
            <Table
              headers={[
                "Version / Attempt",
                "Description",
                "Executor",
                "Time",
                "Status",
                "Progress",
              ]}
              rows={(data.History || []).map((item) => [
                <Link
                  to={url("/migrations", {
                    db: data.Database.ID,
                    id: item.ID,
                    offset: data.Offset,
                  })}
                >
                  {item.AppliedVersion
                    ? `v${item.AppliedVersion}`
                    : `Attempt #${item.ID}`}
                </Link>,
                item.Description,
                item.Username,
                stamp(item.StartedAt),
                <span
                  className={`badge badge--${item.Status === "succeeded" ? "success" : item.Status === "failed" ? "danger" : "warning"}`}
                >
                  {item.Status}
                </span>,
                <>
                  {item.CompletedStatements} / {item.TotalStatements}
                  <small>{item.Mode}</small>
                </>,
              ])}
              empty="No migration history yet."
            />
            <Pagination />
          </Panel>
        </>
      ) : (
        <p className="empty-state">No databases assigned.</p>
      )}
    </>
  );
}
