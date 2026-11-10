import { createContext, useContext, type ReactNode } from "react";
import { Form as SharedForm } from "@schakker/ui";
export {
  Button,
  Check,
  Field,
  Heading,
  Hidden,
  Icon,
  Notice,
  Panel,
  Table,
  stamp,
  pretty,
  permission,
  initials,
  url,
} from "@schakker/ui";
import { formFields } from "./api";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { Table, Field, pretty, permission, url } from "@schakker/ui";
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
export function Form(props: {
  children: ReactNode;
  className?: string;
  confirm?: string;
  onSuccess?: () => void;
}) {
  const { busy, submit } = usePage();
  return (
    <SharedForm
      {...props}
      busy={busy}
      submit={submit}
      collectFields={formFields}
    />
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
