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
