import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import {
  Button,
  Check,
  DatabasePicker,
  Field,
  Form,
  Heading,
  Hidden,
  Notice,
  Panel,
  Results,
  url,
  usePage,
} from "./ui";

export function SQLViewer() {
  const { data } = usePage();
  const navigate = useNavigate();
  const [sql, setSQL] = useState(data.SQL);
  const [filters, setFilters] = useState<number[]>([]);
  const [counter, setCounter] = useState(0);
  const [resultSQL, setResultSQL] = useState<string | null>(null);
  useEffect(() => {
    setSQL(data.SQL);
    if (data.ViewerResult) setResultSQL(data.SQL);
  }, [data]);
  const fields = {
    db: data.Database.ID,
    schema: data.Schema,
    table: data.Table,
  };
  return (
    <>
      <Heading
        title="SQL Viewer"
        text="Build or write a SELECT query, then explore the results."
      >
        <span className="badge badge--view">Read only</span>
      </Heading>
      <DatabasePicker databases={data.Databases} selected={data.Database.ID} />
      {data.Database.ID ? (
        <>
          <div className="toolbar">
            <Field label="Schema">
              <select
                value={data.Schema}
                onChange={(e) =>
                  navigate(
                    url("/sql-viewer", {
                      db: data.Database.ID,
                      schema: e.target.value,
                    }),
                  )
                }
              >
                {data.Schemas?.map((schema) => (
                  <option key={schema.Name}>{schema.Name}</option>
                ))}
              </select>
            </Field>
            <Field label="Table for visual builder">
              <select
                value={data.Table}
                onChange={(e) =>
                  navigate(
                    url("/sql-viewer", { ...fields, table: e.target.value }),
                  )
                }
              >
                {data.Tables?.map((table) => (
                  <option key={table.Name}>{table.Name}</option>
                ))}
              </select>
            </Field>
          </div>
          <Form className="form-stack viewer-form">
            {Object.entries(fields).map(([name, value]) => (
              <Hidden key={name} name={name} value={value} />
            ))}
            {data.Detail && (
              <Panel
                title="Visual SELECT builder"
                text={`${data.Schema}.${data.Table} · Generate SQL, review it in the editor, then run the query.`}
              >
                <div className="integration-form form-stack">
                  <fieldset className="viewer-columns">
                    <legend>Columns · leave empty to select all</legend>
                    <div>
                      {data.Detail.Columns?.map((column) => (
                        <Check
                          key={column.Name}
                          name="columns"
                          value={column.Name}
                        >
                          {column.Name}
                        </Check>
                      ))}
                    </div>
                  </fieldset>
                  <Check name="distinct">
                    DISTINCT · omit duplicate result rows
                  </Check>
                  <div>
                    <h3>Filters · all conditions must match</h3>
                    {filters.map((id) => (
                      <FilterRow
                        key={id}
                        remove={() =>
                          setFilters((current) =>
                            current.filter((value) => value !== id),
                          )
                        }
                      />
                    ))}
                    <Button
                      secondary
                      disabled={filters.length >= 20}
                      onClick={() => {
                        setFilters((current) => [...current, counter]);
                        setCounter((value) => value + 1);
                      }}
                    >
                      Add filter
                    </Button>
                  </div>
                  <div className="form-grid">
                    <Field label="Sort column">
                      <select name="sort">
                        <option value="">No sorting</option>
                        {data.Detail.Columns?.map((column) => (
                          <option key={column.Name}>{column.Name}</option>
                        ))}
                      </select>
                    </Field>
                    <Field label="Direction">
                      <select name="direction">
                        <option value="asc">Ascending</option>
                        <option value="desc">Descending</option>
                      </select>
                    </Field>
                    <Field label="Limit">
                      <input
                        type="number"
                        name="limit"
                        min="1"
                        max="500"
                        defaultValue={100}
                      />
                    </Field>
                    <Field label="Offset">
                      <input
                        type="number"
                        name="offset"
                        min="0"
                        max="1000000"
                        defaultValue={0}
                      />
                    </Field>
                  </div>
                  <Button action="build_query" secondary>
                    Generate SELECT in editor
                  </Button>
                </div>
              </Panel>
            )}
            <Panel
              title="SQL editor"
              text={`${data.Database.Name} · ${data.Database.DBMS}`}
            >
              <div id="viewer-sql-editor" className="workflow-scroll-target">
                <div className="code-editor">
                  <div className="code-editor__bar">
                    <span>query.sql</span>
                    <span>SELECT / WITH</span>
                  </div>
                  <div className="code-editor__body">
                    <textarea
                      name="sql"
                      aria-label="Read-only SQL query"
                      spellCheck={false}
                      value={sql}
                      onChange={(e) => setSQL(e.target.value)}
                      onKeyDown={(event) => {
                        if (
                          (event.ctrlKey || event.metaKey) &&
                          event.key === "Enter"
                        ) {
                          event.preventDefault();
                          const form = event.currentTarget.form;
                          const button = form?.querySelector<HTMLButtonElement>(
                            'button[value="run_query"]',
                          );
                          if (button) form?.requestSubmit(button);
                        }
                      }}
                      placeholder="SELECT * FROM your_table LIMIT 100;"
                    />
                  </div>
                </div>
                <p className="integration-form muted">
                  Run one SELECT query at a time. Joins, read-only CTEs,
                  subqueries and common aggregate functions are supported.
                  Results are limited to 500 rows.
                </p>
                <div className="editor-actions">
                  <Button action="run_query">Run read query</Button>
                </div>
              </div>
            </Panel>
          </Form>
          {data.ViewerResult && (
            <div id="viewer-results" className="workflow-scroll-target">
              <Panel
                title="Query results"
                text={`${data.ViewerResult.Rows?.length || 0} rows · ${Math.round(data.ViewerResult.Duration / 1e6)} ms${data.ViewerResult.HasMore ? " · first 500 rows shown; use LIMIT / OFFSET to read another window" : ""}`}
              >
                {resultSQL !== sql && (
                  <Notice error>
                    SQL changed. Run the query again to refresh these results.
                  </Notice>
                )}
                <Results data={data.ViewerResult} />
              </Panel>
            </div>
          )}
        </>
      ) : (
        <p className="empty-state">
          No databases assigned. Contact your platform admin.
        </p>
      )}
    </>
  );
}

function FilterRow({ remove }: { remove: () => void }) {
  const { data } = usePage();
  const [operator, setOperator] = useState("=");
  return (
    <div className="viewer-filter-row">
      <Field label="Column">
        <select name="filter_column">
          <option value="">Choose column</option>
          {data.Detail?.Columns?.map((column) => (
            <option key={column.Name}>{column.Name}</option>
          ))}
        </select>
      </Field>
      <Field label="Operator">
        <select
          name="filter_operator"
          value={operator}
          onChange={(event) => setOperator(event.target.value)}
        >
          {[
            "=",
            "!=",
            "<",
            "<=",
            ">",
            ">=",
            "LIKE",
            "NOT LIKE",
            "IS NULL",
            "IS NOT NULL",
          ].map((value) => (
            <option key={value}>{value}</option>
          ))}
        </select>
      </Field>
      <Field label="Value type">
        <select name="filter_type">
          <option value="text">Text</option>
          <option value="number">Number</option>
          <option value="boolean">Boolean (true/false)</option>
        </select>
      </Field>
      <Field label="Value">
        <input
          name="filter_value"
          readOnly={operator.includes("NULL")}
          placeholder={
            operator.includes("NULL") ? "No value needed" : "Filter value"
          }
          maxLength={8192}
        />
      </Field>
      <Button secondary onClick={remove}>
        Remove
      </Button>
    </div>
  );
}
