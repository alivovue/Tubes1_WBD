import { useState } from "react";
import type { Column, Operation } from "./types";
import {
  Button,
  Field,
  Form,
  Hidden,
  Notice,
  Panel,
  pretty,
  usePage,
} from "./ui";

type BuilderColumn = Column & { Primary: boolean };
type Pair = { name: string; value: string; type: string };
const newColumn = (): BuilderColumn => ({
  Name: "",
  DataType: "",
  Nullable: true,
  Default: "",
  Primary: false,
});
const newPair = (): Pair => ({ name: "", value: "", type: "text" });
const kinds = [
  "create_table",
  "drop_table",
  "add_column",
  "alter_column",
  "drop_column",
  "add_index",
  "drop_index",
  "insert_row",
  "update_row",
];

export function VisualBuilder({
  description,
  setDescription,
}: {
  description: string;
  setDescription: (value: string) => void;
}) {
  const { data } = usePage();
  const [operations, setOperations] = useState<Operation[]>([]);
  const [editing, setEditing] = useState(-1);
  const [error, setError] = useState("");
  const [kind, setKind] = useState("create_table");
  const [schema, setSchema] = useState(data.Schemas?.[0]?.Name || "");
  const [table, setTable] = useState("");
  const [columns, setColumns] = useState<BuilderColumn[]>([newColumn()]);
  const [column, setColumn] = useState<Column>(newColumn());
  const [indexName, setIndexName] = useState("");
  const [indexColumns, setIndexColumns] = useState("");
  const [unique, setUnique] = useState(false);
  const [values, setValues] = useState<Pair[]>([newPair()]);
  const [where, setWhere] = useState<Pair[]>([newPair()]);
  function edit(index: number) {
    const op = operations[index];
    setEditing(index);
    setKind(op.Kind);
    setSchema(op.Schema);
    setTable(op.Table);
    setColumn(op.Column || newColumn());
    setColumns(
      op.Definition?.Columns.map((item) => ({
        ...item,
        Primary: !!op.Definition?.Constraints.some(
          (constraint) =>
            constraint.Type === "p" && constraint.Columns?.includes(item.Name),
        ),
      })) || [newColumn()],
    );
    setIndexName(op.Index?.Name || "");
    setIndexColumns(
      op.Index?.Columns?.map((item) => item.Name).join(", ") || "",
    );
    setUnique(!!op.Index?.Unique);
    const pairs = (entries: Record<string, unknown> = {}) =>
      Object.entries(entries).map(([name, value]) => ({
        name,
        value:
          value == null
            ? ""
            : typeof value === "object"
              ? pretty(value)
              : String(value),
        type:
          value === null
            ? "null"
            : typeof value === "number"
              ? "number"
              : typeof value === "boolean"
                ? "boolean"
                : "text",
      }));
    setValues(pairs(op.Values));
    setWhere(pairs(op.Where));
  }
  function add() {
    try {
      const op: Operation = {
        Kind: kind,
        Schema: schema.trim(),
        Table: table.trim(),
      };
      if (!op.Schema || !op.Table)
        throw new Error("Enter the schema and table name.");
      if (kind === "create_table") {
        if (
          !columns.length ||
          columns.some((item) => !item.Name.trim() || !item.DataType.trim())
        )
          throw new Error(
            "Every table needs columns with names and SQL types.",
          );
        const primary = columns
          .filter((item) => item.Primary)
          .map((item) => item.Name.trim());
        op.Definition = {
          Columns: columns.map(({ Primary, ...item }) => ({
            ...item,
            Name: item.Name.trim(),
            DataType: item.DataType.trim(),
            Default: item.Default?.trim() || null,
            Nullable: Primary ? false : item.Nullable,
          })),
          Constraints: primary.length
            ? [{ Name: `${op.Table}_pkey`, Type: "p", Columns: primary }]
            : [],
        };
      }
      if (kind.endsWith("_column")) {
        if (!column.Name.trim()) throw new Error("Enter a column name.");
        if (kind !== "drop_column" && !column.DataType.trim())
          throw new Error("Enter the SQL type.");
        op.Column = {
          Name: column.Name.trim(),
          DataType: column.DataType.trim(),
          Default: column.Default?.trim() || null,
          Nullable: column.Nullable,
        };
      }
      if (kind.endsWith("_index")) {
        if (!indexName.trim()) throw new Error("Enter an index name.");
        const names = indexColumns
          .split(",")
          .map((name) => ({ Name: name.trim() }));
        if (kind === "add_index" && names.some((item) => !item.Name))
          throw new Error("Enter index columns.");
        op.Index = {
          Name: indexName.trim(),
          Method: "btree",
          Unique: unique,
          Columns: kind === "add_index" ? names : [],
        };
      }
      if (kind === "insert_row" || kind === "update_row") {
        op.Values = parsePairs(values);
        if (!Object.keys(op.Values).length)
          throw new Error("Add at least one row value.");
      }
      if (kind === "update_row") {
        op.Where = parsePairs(where);
        if (!Object.keys(op.Where).length)
          throw new Error("Updates require at least one filter.");
      }
      setOperations((current) =>
        editing < 0
          ? [...current, op]
          : current.map((item, i) => (i === editing ? op : item)),
      );
      setEditing(-1);
      setError("");
    } catch (problem) {
      setError((problem as Error).message);
    }
  }
  function move(index: number, direction: number) {
    setOperations((current) => {
      const result = [...current];
      [result[index], result[index + direction]] = [
        result[index + direction],
        result[index],
      ];
      return result;
    });
    setEditing(-1);
  }
  return (
    <Panel
      title="Visual change set"
      text="Accumulate and reorder operations. Analyze generates editable SQL, compares schema states and previews row changes."
    >
      <Form>
        <Hidden name="db" value={data.Database.ID} />
        <Hidden name="operations" value={pretty(operations)} />
        <Field label="Description">
          <input
            name="description"
            value={description}
            onChange={(event) => setDescription(event.target.value)}
            required
            maxLength={255}
          />
        </Field>
        <div className="form-grid">
          <Field label="Operation">
            <select
              value={kind}
              onChange={(event) => setKind(event.target.value)}
            >
              {kinds.map((value) => (
                <option key={value} value={value}>
                  {value.replaceAll("_", " ")}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Schema">
            <input
              value={schema}
              list="schema-names"
              onChange={(event) => setSchema(event.target.value)}
            />
          </Field>
          <Field label="Table">
            <input
              value={table}
              onChange={(event) => setTable(event.target.value)}
            />
          </Field>
        </div>
        <datalist id="schema-names">
          {data.Schemas?.map((item) => (
            <option key={item.Name} value={item.Name} />
          ))}
        </datalist>
        {kind === "create_table" && (
          <div>
            <h3>Columns</h3>
            {columns.map((item, i) => (
              <div className="builder-row form-grid" key={i}>
                <ColumnFields
                  column={item}
                  update={(change) =>
                    setColumns((current) =>
                      current.map((value, j) =>
                        i === j ? { ...value, ...change } : value,
                      ),
                    )
                  }
                />
                <label className="check-control">
                  <input
                    type="checkbox"
                    checked={item.Primary}
                    onChange={(event) =>
                      setColumns((current) =>
                        current.map((value, j) =>
                          i === j
                            ? { ...value, Primary: event.target.checked }
                            : value,
                        ),
                      )
                    }
                  />
                  <span>Primary key</span>
                </label>
                <Button
                  secondary
                  onClick={() =>
                    setColumns((current) => current.filter((_, j) => i !== j))
                  }
                >
                  Remove column
                </Button>
              </div>
            ))}
            <Button
              secondary
              onClick={() => setColumns((current) => [...current, newColumn()])}
            >
              Add column
            </Button>
          </div>
        )}
        {kind.endsWith("_column") && (
          <div className="form-grid">
            <ColumnFields
              column={column}
              update={(change) =>
                setColumn((current) => ({ ...current, ...change }))
              }
              nameOnly={kind === "drop_column"}
            />
          </div>
        )}
        {kind.endsWith("_index") && (
          <div className="form-grid">
            <Field label="Index name">
              <input
                value={indexName}
                onChange={(event) => setIndexName(event.target.value)}
              />
            </Field>
            {kind === "add_index" && (
              <>
                <Field label="Columns (comma separated)">
                  <input
                    value={indexColumns}
                    onChange={(event) => setIndexColumns(event.target.value)}
                  />
                </Field>
                <label className="check-control">
                  <input
                    type="checkbox"
                    checked={unique}
                    onChange={(event) => setUnique(event.target.checked)}
                  />
                  <span>Unique index</span>
                </label>
              </>
            )}
          </div>
        )}
        {["insert_row", "update_row"].includes(kind) && (
          <PairFields label="Row values" pairs={values} update={setValues} />
        )}
        {kind === "update_row" && (
          <PairFields
            label="Filter · all conditions must match"
            pairs={where}
            update={setWhere}
          />
        )}
        {error && <Notice error>{error}</Notice>}
        <Button secondary onClick={add}>
          {editing < 0
            ? "Add operation to change set"
            : "Save operation changes"}
        </Button>
        <ol className="operation-list">
          {operations.map((op, i) => (
            <li key={i}>
              <strong>
                {i + 1}. {op.Kind} · {op.Schema}.{op.Table}
              </strong>
              <div className="heading-actions">
                <Button
                  secondary
                  disabled={i === 0}
                  onClick={() => move(i, -1)}
                >
                  Up
                </Button>
                <Button
                  secondary
                  disabled={i === operations.length - 1}
                  onClick={() => move(i, 1)}
                >
                  Down
                </Button>
                <Button secondary onClick={() => edit(i)}>
                  Edit
                </Button>
                <Button
                  secondary
                  onClick={() => {
                    setOperations((current) =>
                      current.filter((_, j) => i !== j),
                    );
                    setEditing(-1);
                  }}
                >
                  Remove
                </Button>
              </div>
            </li>
          ))}
        </ol>
        {editing >= 0 && (
          <Notice>Save the edited operation before analysis.</Notice>
        )}
        <Button
          action="analyze_visual"
          disabled={!operations.length || editing >= 0}
        >
          Analyze change set
        </Button>
      </Form>
    </Panel>
  );
}

function ColumnFields({
  column,
  update,
  nameOnly = false,
}: {
  column: Column;
  update: (change: Partial<Column>) => void;
  nameOnly?: boolean;
}) {
  return (
    <>
      <Field label="Column name">
        <input
          value={column.Name}
          onChange={(event) => update({ Name: event.target.value })}
        />
      </Field>
      {!nameOnly && (
        <>
          <Field label="SQL type">
            <input
              value={column.DataType}
              onChange={(event) => update({ DataType: event.target.value })}
              placeholder="integer / varchar(255)"
            />
          </Field>
          <Field label="Default SQL expression">
            <input
              value={column.Default || ""}
              onChange={(event) => update({ Default: event.target.value })}
            />
          </Field>
          <label className="check-control">
            <input
              type="checkbox"
              checked={column.Nullable}
              onChange={(event) => update({ Nullable: event.target.checked })}
            />
            <span>Nullable</span>
          </label>
        </>
      )}
    </>
  );
}
function PairFields({
  label,
  pairs,
  update,
}: {
  label: string;
  pairs: Pair[];
  update: (pairs: Pair[]) => void;
}) {
  return (
    <div>
      <h3>{label}</h3>
      {pairs.map((pair, i) => (
        <div className="builder-row form-grid" key={i}>
          <Field label="Column">
            <input
              value={pair.name}
              onChange={(event) =>
                update(
                  pairs.map((value, j) =>
                    i === j ? { ...value, name: event.target.value } : value,
                  ),
                )
              }
            />
          </Field>
          <Field label="Value">
            <input
              value={pair.value}
              onChange={(event) =>
                update(
                  pairs.map((value, j) =>
                    i === j ? { ...value, value: event.target.value } : value,
                  ),
                )
              }
            />
          </Field>
          <Field label="Value type">
            <select
              value={pair.type}
              onChange={(event) =>
                update(
                  pairs.map((value, j) =>
                    i === j ? { ...value, type: event.target.value } : value,
                  ),
                )
              }
            >
              {["text", "number", "boolean", "null"].map((type) => (
                <option key={type}>{type}</option>
              ))}
            </select>
          </Field>
          <Button
            secondary
            onClick={() => update(pairs.filter((_, j) => i !== j))}
          >
            Remove value
          </Button>
        </div>
      ))}
      <Button secondary onClick={() => update([...pairs, newPair()])}>
        Add value
      </Button>
    </div>
  );
}
function parsePairs(pairs: Pair[]): Record<string, unknown> {
  const entries: [string, unknown][] = [];
  for (const pair of pairs) {
    const name = pair.name.trim();
    if (!name) throw new Error("Every value and filter needs a column name.");
    if (entries.some(([key]) => key === name))
      throw new Error("Each column may occur only once.");
    let value: unknown = pair.value;
    if (pair.type === "null") value = null;
    if (pair.type === "number") {
      const number = Number(pair.value);
      if (!pair.value.trim() || !Number.isFinite(number))
        throw new Error("Enter a valid finite number.");
      if (Number.isInteger(number) && !Number.isSafeInteger(number))
        throw new Error(
          "This integer exceeds browser precision. Enter it as text to preserve the value.",
        );
      value = number;
    }
    if (pair.type === "boolean") {
      if (!["true", "false"].includes(pair.value.trim().toLowerCase()))
        throw new Error("Enter true or false for boolean values.");
      value = pair.value.trim().toLowerCase() === "true";
    }
    entries.push([name, value]);
  }
  return Object.fromEntries(entries);
}
