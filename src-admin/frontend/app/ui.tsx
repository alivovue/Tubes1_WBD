import {
  createContext,
  useContext,
  type FormEvent,
  type ReactNode,
} from "react";
import { formFields } from "./api";
import icons from "../../../src/frontend/app/icons.svg?no-inline";
import type { Fields, PageData } from "./types";

export const PageContext = createContext<{
  data: PageData;
  busy: boolean;
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
export function Icon({ name }: { name: string }) {
  return (
    <svg aria-hidden="true">
      <use href={`${icons}#icon-${name}`} />
    </svg>
  );
}
