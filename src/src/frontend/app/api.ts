import { requestJSON, requestResource, collectFormFields } from "@schakker/ui";
export { APIError } from "@schakker/ui";
import type { APIResult, Fields } from "./types";
import type {
  DefinitionInput,
  DefinitionResponse,
  DefinitionsResponse,
  EnqueueResponse,
  JobResponse,
  MigrationReview,
  ReviewResponse,
} from "./types";

const databaseQuery = (database: number) =>
  `database_id=${encodeURIComponent(database)}`;
export const migrationPaths = {
  definitions: (database: number) =>
    `/migration-definitions?${databaseQuery(database)}`,
  reviews: (database: number) =>
    `/migration-reviews?${databaseQuery(database)}`,
  jobs: (database: number) => `/migration-jobs?${databaseQuery(database)}`,
  job: (id: string) => `/migration-jobs/${encodeURIComponent(id)}`,
  history: (database: number, hide: boolean, cursor?: string) =>
    `/migrations/history?${databaseQuery(database)}&hide_rollback=${hide}${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`,
};
export const migrationsAPI = {
  definitions: (database: number, signal?: AbortSignal) =>
    requestResource<DefinitionsResponse>(
      `/api${migrationPaths.definitions(database)}`,
      { signal },
    ),
  save: (input: DefinitionInput, csrf: string, id?: number) =>
    requestResource<DefinitionResponse>(
      `/api/migration-definitions${id ? `/${id}` : ""}`,
      { method: id ? "PATCH" : "POST", body: input, csrf },
    ),
  submit: (
    definition: {
      id: number;
      revision: number;
      database_id: number;
      batch: unknown;
    },
    csrf: string,
  ) =>
    requestResource<ReviewResponse>("/api/migration-reviews", {
      method: "POST",
      body: {
        database_id: definition.database_id,
        definition_id: definition.id,
        revision: definition.revision,
        action: definition.batch ? "up_batch" : "up",
      },
      csrf,
    }),
  resubmit: (review: MigrationReview, csrf: string) =>
    requestResource<ReviewResponse>("/api/migration-reviews", {
      method: "POST",
      body: {
        database_id: review.database_id,
        definition_id: review.definition_id,
        revision: review.revision,
        action: review.action,
        target_execution_id: review.target_execution_id,
      },
      csrf,
    }),
  decide: (
    id: number,
    group: number,
    decision: "approved" | "rejected",
    reason: string,
    csrf: string,
  ) =>
    requestResource<ReviewResponse>(`/api/migration-reviews/${id}/decisions`, {
      method: "POST",
      body: { group_id: group, decision, reason },
      csrf,
    }),
  enqueue: (id: number, csrf: string, key: string) =>
    requestResource<EnqueueResponse>(`/api/migration-reviews/${id}/enqueue`, {
      method: "POST",
      body: {},
      csrf,
      idempotencyKey: key,
    }),
  retry: (id: string, csrf: string, key: string) =>
    requestResource<JobResponse>(`/api${migrationPaths.job(id)}/retry`, {
      method: "POST",
      body: {},
      csrf,
      idempotencyKey: key,
    }),
  cancelReview: (id: string, csrf: string) =>
    requestResource<ReviewResponse>(
      `/api${migrationPaths.job(id)}/cancel-reviews`,
      { method: "POST", body: {}, csrf },
    ),
  executionReview: (
    execution: number,
    operation: "rollback" | "forward",
    csrf: string,
  ) =>
    requestResource<ReviewResponse>(
      `/api/migrations/${execution}/${operation}-reviews`,
      { method: "POST", body: {}, csrf },
    ),
};
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
