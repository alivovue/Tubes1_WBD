# Person 1 — mandatory frontend work

This page records the original frontend delivery; [M2 integration](m2-integration.md) describes the current merged application.

Scope follows section 6.1 / FE-01–FE-05 of the supplied Milestone 2 implementation plan. Bonus work (CodeQL, k6, Grafana and early submission) is excluded. No Go migration, authority, queue, worker or database schema code was changed.

## Implemented screens

| Task | UI delivered |
| --- | --- |
| FE-01 | Existing user/admin SPA routes and M1 tools retained; shared layouts contain SQL/table scrolling; mobile navigation remains available. |
| FE-02 | SQL up/down editor, irreversible reason, draft creation/editing with expected revision, frozen SQL inspection, copy to a new draft, submission confirmation. |
| FE-03 | Review inbox, explicit decisions per required group (including View Only), missing leaders, approval counts, rejection/invalidation, fresh review, approved enqueue; admin active-member leader selector. |
| FE-04 | Polling job detail, job activity separate from committed history, execution details, exact execution-ID rollback pairing and Hide rollback. |
| FE-05 | Rollback/forward review requests, PostgreSQL batch configuration, committed/reverted rows and batches, cursor, checkpoint retry, cancellation review, partial/uncertain/compensation states. |

The navigation entry **Migration workflow** opens `/migrations?view=definitions`. The four views are `definitions`, `reviews`, `jobs` and `history`; all support `db=<database-id>`. Existing `/migrations/new` (SQL analysis and M1 execution) and `/migrations` (M1 history) remain available, with links into the new workflow. Query parameters reuse Go's existing HTML routes, so deep links do not need new page handlers.

Selection parameters: `definition`, `copy`, `review`, `job`, `execution`; committed history also uses `hide_rollback=true` and a server-provided `cursor`. Changing databases preserves the workflow view and drops the old selection.

## Integration status

The three implementations are now connected. The current transport contract is [m2-frontend-api.md](m2-frontend-api.md), and startup, validation and limitations are in [m2-integration.md](m2-integration.md). The original screenshots below document the initial frontend work; rerun the browser suite for current evidence. Browser fixtures remain synthetic, while Go integration tests exercise the real authority, databases, queue and worker.

The SQL analysis editor retains M1 tools. Its synchronous HTTP execute action is rejected; migrations run through reviewed durable jobs.

## Verification and evidence

From `src/`:

```powershell
npm ci
npm run build
npm run typecheck:shared
npm run verify:person1
```

The last command launches an installed Chromium browser against an ephemeral loopback server with synthetic API responses. It needs Node 22 and a local Edge, Chrome or Chromium executable. Set `SCHAKKER_QA_BROWSER` if automatic browser discovery does not match your machine. No frontend test dependencies are added; it uses Node's HTTP/WebSocket APIs and Chromium DevTools.

The script saves a machine-readable [report](qa/person1/report.json) and screenshots under `docs/qa/person1/`. The fixtures contain only invented users, database names and SQL. They are outside the production frontend and cannot be activated in a real app.

Checks cover existing SPA routes; reversible/irreversible validation; optimistic draft editing; batch configuration; separate View Only leader approval; enqueue; partial progress, retry and cancellation; rollback/forward review requests; exact-pair filtering; HTTP 403/409/422/503, loading/empty/missing states; permission presentation; active-member leader selection; login/logout; and layout at 1440×1000, 390×844 and 375×812.

Screenshots: [user desktop](qa/person1/user-1440.png), [user 390px](qa/person1/user-390.png), [user 375px](qa/person1/user-375.png), [admin desktop](qa/person1/admin-1440.png), [admin 390px](qa/person1/admin-390.png), [admin 375px](qa/person1/admin-375.png), [partial batch](qa/person1/user-partial-desktop.png).

Recorded results on 11 November 2026:

| Check | Result |
| --- | --- |
| `npm run build` | Passed for user and admin production builds (including TypeScript). |
| `npm run typecheck:shared` | Passed. |
| `go test ./unit/...` from `src/tests/` | Passed in all seven unit-test packages. |
| `npm run verify:person1` | All 19 browser checks passed in Edge 157, with zero browser runtime errors. |
| `git diff --check` | Passed. |

Browser fixtures verify client behavior; they do not prove real leader authority, SQL safety, durable execution, concurrency, receipts or database recovery. Real authority, queue/worker and recovery behavior is exercised separately by the Go integration suite; see [current integration status and limitations](m2-integration.md).

## Demo with the integrated services

1. In admin **Groups**, add active members, choose **Designate leader**, and assign database grants. Try an invalid/deactivated leader and verify the API rejects it.
2. As an editor, open **Migration workflow → Drafts & SQL**. Enter up/down SQL, or enable irreversible and supply a reason. Save, edit, save again, then submit the exact revision.
3. Sign in as every required group leader. Open **Approval inbox**, inspect the frozen SQL and record each group's decision, including View Only groups. Change authority during a review and verify invalidation.
4. As an editor, queue the approved review. Follow **Jobs & activity** while navigating away and back; verify progress survives a reload.
5. Open **Committed history**, inspect exact SQL/actor/date/job IDs, request rollback on the last reversible migration, approve and queue it. Toggle **Hide rollback**, then forward the original revision with a fresh review. Verify subsequent migrations are not forwarded automatically.
6. Submit a supported batch UPDATE. Interrupt after a committed batch and verify rows/checkpoint remain visible. Resume without repeating the committed work; separately request and approve compensation, and inspect partially reverted/reverted states.
7. Repeat user/admin navigation at the recorded mobile widths. Confirm the page stays within the viewport while wide SQL and tables scroll inside their containers.

Do not mark real M2 execution or security acceptance complete using the UI fixture report alone.
