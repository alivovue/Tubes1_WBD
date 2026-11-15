# M2 frontend API contract — P1 → P2/P3

This is the concrete client contract used by the new UI, based on the plan's proposed APIs. It is a handoff for backend implementation, not a claim that those endpoints already exist. TypeScript definitions live in `src/src/frontend/app/types.ts`; calls live in `api.ts`.

## Transport

All paths below include `/api`. JSON resource DTOs use **snake_case**; existing M1 page data and admin group DTOs retain Go's PascalCase names. Use actual JSON booleans/numbers/nested objects, rather than M1's string-valued form decoder. Responses may include `CSRF` and failures return `{ "error": "human-readable explanation" }`.

Requests include same-origin cookies. Mutations send `Content-Type: application/json` and `X-CSRF-Token`; enqueue/retry also send `Idempotency-Key`. Return JSON for errors as well as success. Enqueue returns `202` promptly, before SQL runs. Never treat accepted/enqueued as a successful migration.

## Definitions (P2)

| Method / path | Body / response |
| --- | --- |
| `GET /migration-definitions?database_id=7` | `{ definitions: MigrationDefinition[] }`; include authorized frozen/executed revisions for inspection/copy. |
| `POST /migration-definitions` | `DefinitionInput` → `{ definition: MigrationDefinition }`. |
| `PATCH /migration-definitions/{id}` | `DefinitionInput` with `expected_revision` → `{ definition: MigrationDefinition }`; return 409 on conflict. |

```json
{
  "database_id": 7,
  "title": "Enable a demo flag",
  "up_sql": "UPDATE public.demo SET enabled = true;",
  "down_sql": "UPDATE public.demo SET enabled = false;",
  "irreversible": false,
  "irreversibility_reason": "",
  "batch": {
    "template": "update_by_key",
    "schema": "public",
    "table": "demo",
    "key_column": "id",
    "batch_size": 1000,
    "expected_rows": 5000
  }
}
```

Use `batch: null` for ordinary SQL. `expected_rows` is optional. PostgreSQL identifiers are distinct fields, and the backend validates/quotes them. The backend must define and validate the supported UPDATE template, stable unique BIGINT key, fixed target/high-water mark and compensation semantics; the client never splits arbitrary SQL. `irreversible=true` requires a nonempty reason and empty down SQL; otherwise require down SQL. The example is synthetic and does not establish that a real data update is safely reversible.

`MigrationDefinition` adds `id`, `revision`, `status` (`draft|submitted|executed`), `editable`, `definition_hash`, `created_by` and ISO `updated_at`. `editable` is false for a frozen/executed revision. Copying it creates a separate draft through POST and preserves the original.

## Reviews (P2)

| Method / path | Body / response |
| --- | --- |
| `POST /migration-reviews` | `{ database_id, definition_id, revision, action, target_execution_id? }` → `{ review: MigrationReview }`. Draft submission sends `up` or `up_batch`; fresh reviews reuse the exact action/target. |
| `GET /migration-reviews?database_id=7` | `{ reviews: MigrationReview[] }`; include authorized pending and completed decisions. |
| `POST /migration-reviews/{id}/decisions` | `{ group_id, decision: "approved"|"rejected", reason }` → `{ review }`. Rejection requires a reason. |
| `POST /migration-reviews/{id}/enqueue` | `{}` + idempotency key → `202 { job_id, status: "pending", review_id }`. |
| `POST /migrations/{execution_id}/rollback-reviews` | `{}` → `{ review }`. Exact execution, last-active reversible only. |
| `POST /migrations/{execution_id}/forward-reviews` | `{}` → `{ review }`. Original up SQL and linear forward eligibility. |
| `POST /migration-jobs/{job_id}/cancel-reviews` | `{}` → `{ review }`. Approve compensation of committed batches; do not directly cancel/undo work. |

`MigrationReview` includes `id`, `database_id`, `definition_id`, `revision`, `title`, `action` (`up|up_batch|rollback|forward|cancel_batch`), nullable `target_execution_id`, `status` (`pending_review|approved|rejected|invalidated`), `requested_by`, ISO `requested_at`, `definition_hash`, `authority_revision`, original `up_sql`/`down_sql`, `irreversible`, `irreversibility_reason`, nullable `batch`, `reason?`, `can_enqueue`, `can_resubmit`, nullable `job_id`, and `required_groups`.

Each required group contains `group_id`, `group_name`, `permission` (`view_only|view_edit`), nullable `leader_user_id`/`leader_name`, `decision` (`pending|approved|rejected`), `decided_by?`, `decided_at?`, `reason?`, and `can_decide`. Resolve all current database access groups, including View Only. `can_decide` is specific to the current authenticated user and group. No required groups or a missing leader blocks queueing. A leader of multiple groups records separate decisions.

The client only offers enqueue when all groups are approved, `can_enqueue=true`, and the caller has View + Edit. Rejected/invalidated reviews offer a fresh review only if `can_resubmit=true`; revising content creates a new draft. These flags do not replace backend checks.

## Jobs/activity (P3)

| Method / path | Response |
| --- | --- |
| `GET /migration-jobs?database_id=7` | `{ jobs: MigrationJob[] }`; operational activity includes failed attempts and partial/uncertain jobs. |
| `GET /migration-jobs/{id}` | `{ job: MigrationJob }`. |
| `POST /migration-jobs/{id}/retry` | `{}` + idempotency key → `202 { job: MigrationJob }`; validate safe checkpoint/receipt recovery and approval rules. |

`MigrationJob` fields:

```json
{
  "job_id": "synthetic-job-id",
  "database_id": 7,
  "definition_id": 1,
  "review_id": 1,
  "title": "Demo batch",
  "operation": "up_batch",
  "status": "partial",
  "requested_by": "demo.leader",
  "created_at": "2026-11-11T03:00:00Z",
  "started_at": "2026-11-11T03:00:01Z",
  "completed_at": null,
  "attempt": 1,
  "max_attempts": 3,
  "next_retry_at": null,
  "committed_batches": 2,
  "committed_rows": 2000,
  "reverted_batches": 0,
  "reverted_rows": 0,
  "total_rows": 5000,
  "last_cursor": "2000",
  "retryable": true,
  "can_cancel": true,
  "blocked_reason": null,
  "error": "Connection interrupted after a committed checkpoint"
}
```

Statuses: `pending|running|succeeded|failed|partial|uncertain|reverting|reverted|partially_reverted`. Numeric progress fields are always present, including zeros for ordinary jobs. Unknown totals/cursors/times are JSON null. `retryable` means the server permits this actor's safe retry; `can_cancel` permits requesting review, never direct compensation. A blocked database disables retry. Unknown commit outcomes need receipt reconciliation before retry is authorized.

Lists/reviews poll every 5 seconds; selected job detail every 2 seconds. Polling uses one request at a time, pauses in hidden tabs, aborts on navigation, and stops detail polling in terminal/intervention states without `next_retry_at`. Transient failures back off to 10 seconds; 401/403/404/422 need manual refresh. Failures do not erase previously observed progress; stale data disables mutation controls. No worker runs in the browser.

## Committed history (P2)

`GET /migrations/history?database_id=7&hide_rollback=false&cursor=<opaque>` → `{ executions: MigrationExecution[], next_cursor: string|null }`.

Every execution is committed; failures/retries belong to jobs/activity. Required fields: `id`, `database_id`, `definition_id`, `revision`, `version`, `title`, `operation`, exact executed `sql`, `actor`, ISO `committed_at`, `duration_ms`, `job_id`, `review_id`, nullable `target_execution_id`, `irreversible`, `irreversibility_reason`, `can_rollback`, `can_forward`, optional `action_blocked_reason`.

For `hide_rollback=true`, filter a successful rollback and the execution identified by its **exact** `target_execution_id` across the complete history **before pagination**. Never hide by definition/version/title or remove later forward executions. Do not delete stored records. The client applies a defensive exact-pair filter only to received events; it cannot resolve a pair outside a returned page. Action flags reflect current active/forward state, not the highest historical version.

## Admin group leaders (P2)

Extend existing `GET /api/admin/groups` group DTOs with `LeaderUserID: number|null` and optional `LeadershipError: string`. Group `Members` already exposes `ID`, `Username`, `DisplayName`, `Active`.

The leader form reuses existing `POST /api/admin/groups` and CSRF, with M1 string fields:

```json
{ "action": "set_leader", "id": "1", "leader_user_id": "2" }
```

Return updated page data or the existing `{ location }` redirect format, preferably preserving `group=<id>&editor=leader`. Validate exactly one designated active member per group and return a useful error if membership/activation changed. Extend the backend group editor allowlist to recognize `leader` when preserving an open panel after redirect. Group/user/membership updates must enforce leader invariants and invalidate affected review authority on the server.

## Failure and acceptance contract

Use 401 for an expired identity, 403 for denied actions, 409 for stale state/approval or a blocked database, 422 for invalid/unsafe input, 503 for unavailable authority/infrastructure, and 404 for missing resources. The UI explains those conditions, retains form input after failed mutations and offers explicit refresh. Revalidate every mutation and execution on the server. Never accept client-provided approval flags or mutable SQL as authorization.

Real API integration is complete only after the demo in [m2-person1.md](m2-person1.md) passes against P2/P3's services, with denial, authority changes and worker failures included.
