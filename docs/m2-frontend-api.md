# Integrated M2 API contract

All paths include `/api`, use same-origin session cookies, and return JSON. Mutations require `Content-Type: application/json` and `X-CSRF-Token`. Existing page envelopes and admin page data keep PascalCase; M2 resources use snake_case. Resource responses are objects, including empty lists, rather than bare DAO records/arrays.

## Changelog and dashboard

The frontend route `/migrations` defaults to M2 execution history, also available with `view=history`. It loads database selection from `/api/databases` and executions from `/api/migrations/history`. The retired M1 `/api/migrations` endpoint returns 404.

`GET /dashboard` returns `Recent`, containing up to eight committed executions across accessible databases, newest commit first. Each entry has `id` (execution UUID), `database_id`, `database_name`, `title`, `actor`, `operation`, `committed_at` and `version`, using the same identity and display values as committed history. Dashboard links use `/migrations?db=<database_id>&execution=<id>`. Completed failed, partial and uncertain attempts also appear in Changelog. Jobs/activity provides live status and progress.

## Definitions

- `GET /migration-definitions?database_id=7&limit=50&offset=0` → `{definitions: [...]}`.
- `GET /migration-definitions/{definition_id}` → `{definition: ...}`.
- `POST /migration-definitions` → `201 {definition: ...}`.
- `PATCH /migration-definitions/{definition_id}` → `{definition: ...}`.

Create body:

```json
{"database_id":7,"title":"Add flag","up_sql":"ALTER TABLE demo ADD flag boolean;","down_sql":"ALTER TABLE demo DROP flag;","irreversible":false,"irreversible_reason":""}
```

PATCH omits `database_id` and adds `expected_revision_id`. Resource `id` is the definition ID; `revision_id` is the immutable revision database ID; `revision` is its display sequence. Do not use these interchangeably. Responses include original SQL, `irreversibility_reason`, `definition_hash`, `status`, `editable`, actor/time, nullable `batch` and current `expected_generation`. Irreversible definitions require empty down SQL and a reason of 10–2000 characters.

The UI's `irreversibility_reason` is mapped to the backend input `irreversible_reason` by `api.ts`. No separate mutable batch configuration is accepted with SQL. Batch settings are encoded in the immutable SQL header and projected back to `batch` for display.

## Reviews

- `POST /migration-reviews` body `{revision_id, action, expected_generation, target_execution_id?, target_job_id?}` → `201 {review: ...}`.
- `GET /migration-reviews?database_id=7` → `{reviews: [...]}`.
- `GET /migration-reviews/{id}` → `{review: ...}`.
- `POST /migration-reviews/{id}/decisions` body `{group_id, decision: "approve"|"reject", note}` → `{review: ...}`. Rejection requires a note.
- `POST /migration-reviews/{id}/enqueue` body `{}` and `Idempotency-Key` header → `202 {job_id,status:"pending",review_id}`. A body `idempotency_key` is also accepted; if both are supplied they must match. Replays return the original durable job.
- `POST /migrations/{execution_uuid}/rollback-reviews` or `/forward-reviews`, body `{expected_generation}` → `201 {review: ...}`.
- `POST /migration-jobs/{job_uuid}/cancel-reviews`, body `{revision_id,expected_generation}` → `201 {review: ...}`.

Request actions are `up`, `rollback`, `forward`, `batch_cancel`. Display actions infer `up_batch` from a signed batch template and use `cancel_batch` for compensation. The client maps these names on resubmission. Fresh cancellation reviews retain the original `target_job_id`.

Review resources include exact SQL/revision/hash, authority revision, expected generation, original targets, `status`, `required_groups`, `job_id`, and actor-specific `can_enqueue`/`can_resubmit`. Groups expose their current review snapshot, assigned leader, decision and actor-specific `can_decide`. Response decisions are `pending|approved|rejected`. All access groups participate, including View Only; View Only leaders can decide but cannot enqueue. A missing leader permits submission with `pending_review` status, while enqueue remains blocked. Assigning a leader changes authority and requires a fresh review snapshot under the current invalidation policy. Self-approval and one user leading several groups are supported; each group has an explicit decision.

Capability flags guide the UI. Every mutation and worker execution independently revalidates permission, approved content, current authority and active position. Stale approvals are invalidated on revalidation.

## Jobs and execution history

- `GET /migration-jobs?database_id=7&limit=50&offset=0` → `{jobs: [...]}`.
- `GET /migration-jobs/{uuid}` → `{job: ...}`.
- `POST /migration-jobs/{uuid}/retry`, body `{}` → `202 {job: ...}`. Retries retain the same durable job and reconcile target receipts/checkpoints. The original requester retries ordinary failures; another editor can request reconciliation of uncertain outcomes.
- `GET /migrations/history?database_id=7&hide_rollback=false&limit=50&cursor=50` → `{executions: [...],next_cursor: string|null}`.
- `GET /migrations/state?database_id=7` → `{database_id,active_version,generation,active_stack,forward_stack,blocked_reason}`.
- `GET /migrations/activities?database_id=7` retains the low-level operational attempt list.

Job resources include title, definition/revision/review IDs, actor/time, status, attempt limits, nullable `next_retry_at`, committed/reverted batch/row counters, cursor, nullable total rows, classified error and capability flags. Raw driver errors and credentials are never included. Times without evidence are null. Every progress counter is present, including zero for ordinary SQL.

Execution resource `id` and `target_execution_id` are UUID strings. History contains every completed attempt, including `failed`, `partial` and `uncertain` outcomes. Each has its own stored SQL snapshot, original revision, logical version, job/review, actor, `status`, `attempt`, `occurred_at`, nullable `started_at`/`completed_at`, classified `error`, and `can_retry`. Raw driver errors remain private. `committed_at` is null for attempts that are not the verified committed event. `is_committed` distinguishes those events from attempt evidence.

A verified commit retains its existing execution UUID and rollback/forward eligibility. Its successful attempt appears once under that commit ID; failed retries and additional reconciliation attempts retain distinct attempt UUIDs. The committed ledger alone advances the active stack. Finished attempt rows and their snapshots are immutable; an open attempt may only transition once to a finished outcome. Commit duration includes retries; other attempt durations measure that attempt's start/finish.

Hide rollback filters only exact **successful** rollback/target pairs before pagination. Failed up/down attempts and later forward events remain visible. Pagination cursors currently encode an offset; clients should follow the server value. A full last page may lead to an empty final page.

Definitions, reviews and job lists support `limit`/`offset` (default 50, maximum 200). The current SPA displays the most recent page of these lists; execution history also exposes older-page navigation.

## Supported batch template

The UI configures schema, table, BIGINT primary-key column, BIGINT value column, nonzero integer delta, size 1–10000 and optional row estimate. It generates exactly the SQL returned by `engines.BatchSQL`: a `-- schakker:batch {...}` header followed by a parameterized integer UPDATE in both directions. The backend rejects changed headers/statements before review. This supports a constrained, compensatable integer update, rather than arbitrary SQL splitting. Target validation rejects unsafe table shapes, triggers, rules, RLS and unsupported constraints.

Each batch commits data, before/after evidence, receipt and checkpoint in the target database. UI cancellation requests fresh approval; only the worker reverses committed batches, in reverse order, and refuses external-data conflicts.

## Admin leaders

Existing `GET /admin/groups` includes `LeaderUserID`, `LeaderName`, `LeaderValid`, membership and grants. `POST /admin/groups` uses string form values:

```json
{"action":"set_leader","id":"1","leader_user_id":"2"}
```

`set_group_leader` remains accepted. The response redirects to the same group with `editor=leader`. The backend requires an active member and enforces membership/activation invariants.

## Errors

401 means expired/missing session, 403 permission/CSRF denial, 404 missing resource, 409 stale approval/position or blocked job, 422 invalid definition/template, and 503 unavailable authority/worker infrastructure. Malformed JSON, identifiers and other invalid request values use 400. The client retains failed form input and requires refresh after stale state.
