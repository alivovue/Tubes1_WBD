import type { ReactNode } from "react";
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
