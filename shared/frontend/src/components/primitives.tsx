import type { ReactNode } from "react";
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
