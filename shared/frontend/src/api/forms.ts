import type { Fields } from "./client";
export function collectFormFields(
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
  return fields;
}
