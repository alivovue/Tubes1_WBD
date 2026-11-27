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
  MigrationDefinition,
  MigrationExecution,
  MigrationJob,
  ReviewResponse,
  ReviewsResponse,
} from "./types";

const databaseQuery = (database: number) =>
  `database_id=${encodeURIComponent(database)}`;
export const migrationPageSize = 50;
const pageQuery = (database: number, offset: number) =>
  `${databaseQuery(database)}&limit=${migrationPageSize + 1}&offset=${offset}`;
export const migrationPaths = {
  definitions: (database: number, offset = 0) =>
    `/migration-definitions?${pageQuery(database, offset)}`,
  definition: (id: string | number) =>
    `/migration-definitions/${encodeURIComponent(id)}`,
  reviews: (database: number, offset = 0) =>
    `/migration-reviews?${pageQuery(database, offset)}`,
  review: (id: string | number) =>
    `/migration-reviews/${encodeURIComponent(id)}`,
  jobs: (database: number, offset = 0) =>
    `/migration-jobs?${pageQuery(database, offset)}`,
  job: (id: string) => `/migration-jobs/${encodeURIComponent(id)}`,
  history: (database: number, hide: boolean, cursor?: string) =>
    `/migrations/history?${databaseQuery(database)}&hide_rollback=${hide}${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`,
};
// Backend lists have no inbox filter. Scan pages until a full matching page and
// one lookahead are available; failures never masquerade as an empty inbox.
export async function loadReviewInbox(
  database: number,
  offset: number,
  signal: AbortSignal,
): Promise<ReviewsResponse> {
  const reviews: MigrationReview[] = [];
  let skipped = 0;
  let CSRF: string | undefined;
  for (let scan = 0; ; scan += 200) {
    const result = await requestResource<ReviewsResponse>(
      `/api/migration-reviews?${databaseQuery(database)}&limit=200&offset=${scan}`,
      { signal },
    );
    CSRF = result.CSRF ?? CSRF;
    for (const review of result.reviews) {
      if (
        review.status !== "pending_review" ||
        !review.required_groups.some(
          (group) => group.can_decide && group.decision === "pending",
        )
      )
        continue;
      if (skipped++ < offset) continue;
      reviews.push(review);
      if (reviews.length > migrationPageSize) return { CSRF, reviews };
    }
    if (result.reviews.length < 200) return { CSRF, reviews };
  }
}
export const migrationsAPI = {
  definitions: (database: number, signal?: AbortSignal) =>
    requestResource<DefinitionsResponse>(
      `/api${migrationPaths.definitions(database)}`,
      { signal },
    ),
  save: (input: DefinitionInput, csrf: string, id?: number) => {
    const { batch: _batch, irreversibility_reason, ...fields } = input;
    // Batch configuration is encoded in the exact SQL reviewed by all leaders.
    const body = { ...fields, irreversible_reason: irreversibility_reason };
    if (id) delete (body as Partial<DefinitionInput>).database_id;
    return requestResource<DefinitionResponse>(
      `/api/migration-definitions${id ? `/${id}` : ""}`,
      { method: id ? "PATCH" : "POST", body, csrf },
    );
  },
  submit: (definition: MigrationDefinition, csrf: string) =>
    requestResource<ReviewResponse>("/api/migration-reviews", {
      method: "POST",
      body: {
        revision_id: definition.revision_id,
        expected_generation: definition.expected_generation,
        action: "up",
      },
      csrf,
    }),
  resubmit: (review: MigrationReview, csrf: string) =>
    requestResource<ReviewResponse>("/api/migration-reviews", {
      method: "POST",
      body: {
        revision_id: review.revision_id,
        expected_generation: review.expected_generation,
        action:
          review.action === "up_batch"
            ? "up"
            : review.action === "cancel_batch"
              ? "batch_cancel"
              : review.action,
        target_execution_id: review.target_execution_id,
        target_job_id: review.target_job_id,
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
      body: {
        group_id: group,
        decision: decision === "approved" ? "approve" : "reject",
        note: reason,
      },
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
  resumeReview: (job: MigrationJob, csrf: string) =>
    requestResource<ReviewResponse>(`/api${migrationPaths.job(job.job_id)}/resume-reviews`, {
      method: "POST",
      body: { expected_generation: job.expected_generation },
      csrf,
    }),
  cancelReview: (job: MigrationJob, csrf: string) =>
    requestResource<ReviewResponse>(
      `/api${migrationPaths.job(job.job_id)}/cancel-reviews`,
      {
        method: "POST",
        body: {
          revision_id: job.revision_id,
          expected_generation: job.expected_generation,
        },
        csrf,
      },
    ),
  executionReview: (
    execution: MigrationExecution,
    operation: "rollback" | "forward",
    csrf: string,
  ) =>
    requestResource<ReviewResponse>(
      `/api/migrations/${encodeURIComponent(execution.id)}/${operation}-reviews`,
      {
        method: "POST",
        body: { expected_generation: execution.expected_generation },
        csrf,
      },
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
