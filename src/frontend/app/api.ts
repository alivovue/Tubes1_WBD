import type { APIResult, Fields } from "./types";

export class APIError extends Error {
  constructor(
    public status: number,
    public result: APIResult,
  ) {
    super(
      result.error || "The operation is temporarily unavailable. Please retry.",
    );
  }
}

export async function api(
  path: string,
  fields?: Fields,
  csrf?: string,
  signal?: AbortSignal,
): Promise<APIResult> {
  const response = await fetch(`/api${path}`, {
    method: fields ? "POST" : "GET",
    credentials: "same-origin",
    headers: fields
      ? { "Content-Type": "application/json", "X-CSRF-Token": csrf || "" }
      : { Accept: "application/json" },
    body: fields ? JSON.stringify(fields) : undefined,
    signal,
  });
  const result = (await response.json().catch(() => ({
    error: "The server returned an unreadable response. Please retry.",
  }))) as APIResult;
  if (!response.ok) throw new APIError(response.status, result);
  return result;
}

export function formFields(
  form: HTMLFormElement,
  submitter: HTMLElement | null,
): Fields {
  const values = new FormData(form);
  if (submitter instanceof HTMLButtonElement && submitter.name)
    values.append(submitter.name, submitter.value);
  const fields: Fields = {};
  for (const key of new Set(values.keys())) {
    const list = values.getAll(key).map(String);
    fields[key] = list.length > 1 ? list : list[0];
  }
  // These fields are arrays even when there is only one item.
  for (const key of [
    "columns",
    "filter_column",
    "filter_operator",
    "filter_type",
    "filter_value",
  ]) {
    if (key in fields && !Array.isArray(fields[key]))
      fields[key] = [fields[key]];
  }
  return fields;
}
