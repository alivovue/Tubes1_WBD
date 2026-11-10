import {
  createContext,
  useContext,
  type FormEvent,
  type ReactNode,
} from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { formFields } from "./api";
import icons from "./icons.svg?no-inline";
import type { Database, Fields, PageData, Rows } from "./types";

export const PageContext = createContext<{
  data: PageData;
  busy: boolean;
  analysisState: { loading: boolean; error: string };
  submit: (fields: Fields) => Promise<boolean>;
  refresh: () => void;
} | null>(null);
export function usePage() {
  const value = useContext(PageContext);
  if (!value) throw new Error("Page context is unavailable");
  return value;
}
export const stamp = (value: string) =>
  value ? new Date(value).toLocaleString() : "—";
export const pretty = (value: unknown) => JSON.stringify(value, null, 2);
export const permission = (value: string) =>
  value === "view_edit"
    ? "View + Edit"
    : value === "view_only"
      ? "View Only"
      : "No Access";
export const initials = (value: string) =>
  value
    .trim()
    .split(/\s+/)
    .slice(0, 2)
    .map((word) => word[0])
    .join("")
    .toUpperCase();
export function url(
  path: string,
  fields: Record<string, string | number | undefined> = {},
) {
  const params = new URLSearchParams();
  Object.entries(fields).forEach(([key, value]) => {
    if (value !== undefined && value !== "") params.set(key, String(value));
  });
  return path + (params.size ? `?${params}` : "");
}
export function Notice({
  children,
  error = false,
}: {
  children: ReactNode;
  error?: boolean;
}) {
  return (
    <div
      className={`notice${error ? " notice--warning" : ""}`}
      role={error ? "alert" : "status"}
    >
      <div>{children}</div>
    </div>
  );
}
export function Heading({
  title,
  text,
  children,
}: {
  title: string;
  text?: string;
  children?: ReactNode;
}) {
  return (
    <section className="page-heading">
      <div>
        <h1>{title}</h1>
        {text && <p>{text}</p>}
      </div>
      <div className="heading-actions">{children}</div>
    </section>
  );
}
export function Panel({
  title,
  text,
  children,
}: {
  title?: string;
  text?: string;
  children: ReactNode;
}) {
  return (
    <section className="panel">
      {title && (
        <header className="panel__header">
          <div>
            <h2>{title}</h2>
            {text && <p>{text}</p>}
          </div>
        </header>
      )}
      {children}
    </section>
  );
}
export function Field({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  return (
    <label className="field">
      <span>{label}</span>
      {children}
    </label>
  );
}
export function Check({
  name,
  children,
  checked,
  value,
}: {
  name: string;
  children: ReactNode;
  checked?: boolean;
  value?: string;
}) {
  return (
    <label className="check-control">
      <input
        type="checkbox"
        name={name}
        value={value}
        defaultChecked={checked}
      />
      <span>{children}</span>
    </label>
  );
}
export function Hidden({
  name,
  value,
}: {
  name: string;
  value: string | number;
}) {
  return <input type="hidden" name={name} value={value} />;
}
export function Button({
  children,
  action,
  danger = false,
  secondary = false,
  disabled = false,
  onClick,
}: {
  children: ReactNode;
  action?: string;
  danger?: boolean;
  secondary?: boolean;
  disabled?: boolean;
  onClick?: () => void;
}) {
  return (
    <button
      type={onClick ? "button" : "submit"}
      name={action ? "action" : undefined}
      value={action}
      disabled={disabled}
      onClick={onClick}
      className={`button button--${danger ? "danger" : secondary ? "secondary" : "primary"}`}
    >
      {children}
    </button>
  );
}
export function Form({
  children,
  className = "form-stack integration-form",
  confirm,
  onSuccess,
}: {
  children: ReactNode;
  className?: string;
  confirm?: string;
  onSuccess?: () => void;
}) {
  const { busy, submit } = usePage();
  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || (confirm && !window.confirm(confirm))) return;
    const form = event.currentTarget;
    const fields = formFields(
      form,
      (event.nativeEvent as SubmitEvent).submitter,
    );
    if (await submit(fields)) onSuccess?.();
  }
  return (
    <form onSubmit={onSubmit} className={className}>
      <fieldset className="form-fields" disabled={busy}>
        {children}
      </fieldset>
    </form>
  );
}
export function DatabasePicker({
  databases,
  selected,
}: {
  databases: Database[] | null;
  selected: number;
}) {
  const navigate = useNavigate();
  const { pathname } = useLocation();
  return (
    <div className="toolbar">
      <Field label="Target database">
        <select
          value={selected || ""}
          onChange={(e) => navigate(url(pathname, { db: e.target.value }))}
          disabled={!databases?.length}
        >
          {!databases?.length && (
            <option value="">No databases assigned</option>
          )}
          {databases?.map((db) => (
            <option key={db.ID} value={db.ID}>
              {db.Name} · {permission(db.Permission)}
            </option>
          ))}
        </select>
      </Field>
    </div>
  );
}
export function Table({
  headers,
  rows,
  empty = "No items.",
}: {
  headers: ReactNode[];
  rows: ReactNode[][];
  empty?: string;
}) {
  return (
    <div className="table-wrap" tabIndex={0} aria-label="Scrollable table">
      <table className="data-table">
        <thead>
          <tr>
            {headers.map((head, i) => (
              <th key={i} scope="col">
                {head}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.length ? (
            rows.map((row, i) => (
              <tr key={i}>
                {row.map((cell, j) => (
                  <td key={j}>{cell}</td>
                ))}
              </tr>
            ))
          ) : (
            <tr>
              <td colSpan={Math.max(1, headers.length)}>{empty}</td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}
export function Results({ data }: { data: Rows }) {
  return (
    <Table
      headers={(data.Columns || []).map((column) => (
        <>
          {column.Name}
          <small className="viewer-result-type">{column.DataType}</small>
        </>
      ))}
      rows={(data.Rows || []).map((row) =>
        row.map((cell) =>
          cell === null
            ? "NULL"
            : typeof cell === "object"
              ? pretty(cell)
              : String(cell),
        ),
      )}
      empty="No matching rows."
    />
  );
}
export function Pagination() {
  const { data } = usePage();
  const location = useLocation();
  const params = Object.fromEntries(new URLSearchParams(location.search));
  return (
    <footer className="pagination">
      <span>Offset {data.Offset}</span>
      <div>
        {data.Offset > 0 && (
          <Link
            className="button button--secondary"
            to={url(location.pathname, { ...params, offset: data.Previous })}
          >
            Previous
          </Link>
        )}
        {data.HasMore && (
          <Link
            className="button button--secondary"
            to={url(location.pathname, { ...params, offset: data.Next })}
          >
            Next
          </Link>
        )}
      </div>
    </footer>
  );
}

export function Icon({ name }: { name: string }) {
  return (
    <svg aria-hidden="true">
      <use href={`${icons}#icon-${name}`} />
    </svg>
  );
}
