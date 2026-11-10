import { requestJSON, collectFormFields } from "@schakker/ui";
export { APIError } from "@schakker/ui";
import type { APIResult, Fields } from "./types";
// This application's client always targets its own public API origin.
export function api(
  path: string,
  fields?: Fields,
  csrf?: string,
  signal?: AbortSignal,
): Promise<APIResult> {
  return requestJSON<APIResult>(`/api${path}`, fields, csrf, signal);
}
export function formFields(
  form: HTMLFormElement,
  submitter: HTMLElement | null,
): Fields {
  const fields = collectFormFields(form, submitter);
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
