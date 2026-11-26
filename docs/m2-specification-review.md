# M2 v5 and Q&A review

Sources supplied for this review:

- `IF3110 - Milestone 1 - Pengembangan Aplikasi Web (2).pdf`: byte-identical to the previous M1 PDF.
- `IF3110 - Milestone 2 - Pengembangan Aplikasi Web (2).pdf`: version 5, revised 26 November 2026 at 20:19. The change from version 4 is the offline demo scheduling link on page 20; it introduces no additional application feature.
- `QnA Tubes IF3110 .xlsx`: sheets `QnA` (M1) and `QnA 2` (M2). References below use spreadsheet row numbers.

The PDF's revision formatting matters: pages 12–13 cross out the earlier successful-commits-only history rule and replace it with history for **all attempts, successful or failed**. Plain PDF text extraction includes both the deleted and replacement text. The previous implementation followed the deleted wording; this review corrects it while keeping the M2 execution/approval/receipt architecture.

## Requirements and implementation

| Source | Requirement or clarification | Implementation / decision |
| --- | --- | --- |
| M1 Q&A rows 9–12, 14, 16–17 | Frameworks, query libraries, repository layout and development OS are flexible. | Existing Go backends, React SPAs and workspace layout remain suitable. M2 supersedes M1's non-SPA requirement. |
| M1 Q&A rows 13, 15 | A viewer may show tables directly or accept protected read-only queries. | Both exist. SQL Viewer retains backend read-only validation and permissions. |
| M2 pp. 12–13; Q&A row 12, questions 4 and 9 | Preserve every execution attempt, including failures; never overwrite history. Failed rollback must not hide its target or move the active position. | Changelog now combines the verified commit ledger with finished M2 attempt records. Attempts capture their own SQL and identifying metadata. Finished rows cannot be changed or deleted. Filtering removes only successful reversal pairs, before pagination. |
| M2 Q&A row 9 | Compensation may use reversible down SQL restricted to committed rows; full before-images are optional. | Existing per-batch row coverage and before/after evidence are retained for stronger conflict detection. No requirement to remove them. |
| M2 Q&A row 10 | Dataset generation by script is allowed. | Existing `generate_series` benchmark and recorded >1-second unbatched baseline remain applicable. Evidence is in `src/tests/fixtures/postgre/p3-batch-results.json`. |
| M2 Q&A row 11 | A dedicated message broker is optional; asynchronous queue/worker behavior is required. | The existing durable PostgreSQL queue, separate Go worker, leases and bounded retries satisfy the queue/worker mechanism. |
| M2 Q&A row 12, question 1 | Self-approval is allowed; one user may represent several groups. | Supported. Each group retains an explicit decision. A regression covers one leader approving for two assigned groups. |
| M2 Q&A row 12, question 2; row 13, question 3 | Invalidating approvals on authority changes is permitted/recommended. | Existing strict authority-snapshot invalidation is retained, including leader assignment. A fresh review collects current decisions. |
| M2 Q&A row 12, question 3 | Blocking forward after a new migration supersedes the rolled-back branch is allowed. | Existing linear active/forward stacks retain this policy. |
| M2 Q&A row 12, question 4 | SQL remains immutable even after an execution fails and rolls back. | Queued definitions cannot be edited; frozen revisions and attempt snapshots remain immutable. Corrections use a new definition. |
| M2 Q&A row 12, question 5 | Structured stdout audit logs are sufficient; centralized storage is an observability bonus. | Existing structured logging and optional Collector/Loki pipeline are retained. Workflow/attempt records are execution state and history, not a copy of stdout logs. |
| M2 Q&A row 12, question 6 | Syntax/permission validation is sufficient; a dry-run is optional. | Existing SQL policy and permission checks remain; no automatic up/down dry-run was introduced. |
| M2 Q&A row 12, question 7 | Waiting for an explicit execution action after approval is preferred. | Existing approved-review enqueue button remains. |
| M2 Q&A row 12, question 8 | Retries reuse approval for unchanged migration content. | Existing jobs retry without collecting the same approval again, subject to current authority/position validation. History exposes the approved-job retry action and preserves earlier failures. |
| M2 Q&A row 12, question 10 | M2 support for additional DBMSs is optional. | PostgreSQL executes M2 jobs. MariaDB viewer/analysis support remains. |
| M2 Q&A row 13, question 1 | Platform-controlled batch ranges and SQL placeholders are allowed. | Existing bounded BIGINT batch template and durable cursor/checkpoints remain suitable. |
| M2 Q&A row 13, question 2 | A missing leader leaves the submission pending instead of rejecting it. | Submission now returns `pending_review` with every required group represented. Missing/invalid leaders cannot decide or satisfy approval, and enqueue remains blocked. |

## History details

`migration_execution_history` remains the verified commit ledger used by active/forward position and receipt reconciliation. `migration_job_attempts` stores a distinct execution UUID, revision/definition, actor/database, operation, attempted logical version and SQL snapshot for each claim. An open attempt can finish once; finished evidence is append-only. The public history API presents both sources as one paginated timeline. A successful attempt represented by its committed execution is not duplicated, while other retries/reconciliation attempts retain their identities.

Dashboard recent migrations remain a summary of verified commits. Changelog shows all completed attempts; Jobs retains live status and batch progress. Failed ordinary SQL can be retried using the same approved job or followed by another migration. Partial/uncertain work retains its existing safety block until recovery is resolved.

Fresh provisioning uses `src/tests/fixtures/website/schema.sql`, which includes the attempt snapshot columns and write-once protection. Startup validates that the current schema is present.

Regression coverage is in `qna_spec_test.go`, `committed_history_test.go`, `m2_resources_test.go` and the Chromium UI contract suite. It covers failed up/down attempts, approved retries, immutable failure evidence, exact successful reversal filtering before pagination, continuation after failure, missing-leader pending submission, View Only permissions and one leader representing multiple groups.

Verification for this change passed: the integration suite, all eight backend unit-test packages, the user frontend production build, 22 Chromium UI checks and Go vet. The optional large-dataset benchmark was not rerun; its retained evidence remains linked above.
