# CodeQL security review

Actual local CodeQL **2.27.4**, Go queries **1.6.14**, JavaScript queries **2.5.0**, and both `security-extended` suites produced the retained SARIF. The first scans reported **20 findings**: 12 Go and 8 JavaScript. Nine allocation findings were fixed in local commit **7694875** (full SHA in the manifest; inspect with `git show 7694875`). A fresh Go database and rescan report **3 remaining Go findings**; the unchanged JavaScript scan reports 8. All 11 remaining findings have explicit review decisions below. No rule or finding was suppressed to obtain these counts.

## Scope and reproducibility

- Go: **129 production source files**, covering the user API, dedicated worker, admin API and shared backend. The CLI baseline lists 202 Go files; its build extraction did **not** analyze the other 73 files in the test module (test cases, fixtures and load-fixture support). Test compilation is not claimed as SAST coverage. Both Go scans cover the same 129 files.
- JavaScript/TypeScript: **42 source files**, comprising 11 `.ts`, 23 `.tsx`, 5 `.js`, 1 `.mjs` and 2 HTML entry points. This includes both SPAs, shared frontend, browser harness and k6 scripts. The Go invocation's incidental HTML extraction does not substitute for this separate JavaScript scan.
- Dependencies, generated frontend output, binaries, runtime state and test output are excluded by [config](../temp/codeql/config.yml). Python/YAML/shell, runtime configuration, database privileges and deployed infrastructure are outside these language scans.
- The [manifest](evidence/bonuses/codeql-manifest.json) records every analyzed source SHA-256, exact source counts, unextracted files and SARIF checksums. Both final scans match the current relevant source byte for byte. The initial Go snapshot differs only in the two fixed production files. This evidence describes the analyzed working tree, which also includes bonus changes outside the fix commit.

[Before-fix Go SARIF](evidence/bonuses/codeql-go-before.sarif), [after-fix Go SARIF](evidence/bonuses/codeql-go-after.sarif), [JavaScript SARIF](evidence/bonuses/codeql-javascript.sarif) and the [complete machine-readable review](evidence/bonuses/codeql-review.json) retain the original findings and dataflow locations. Only `%SRCROOT%` was normalized to `file:///workspace/schakker/` for portable artifact paths; findings were not edited. Go/JS analyses completed successfully without extraction-error notifications.

The [workflow template](../temp/workflows/codeql.yml) is intentionally **inactive**, stored outside `.github` at the user's request. It retains push, pull request, weekly and manual scan definitions with SARIF upload/artifacts, but pushing the template does not run GitHub Actions. A hosted run would require explicitly activating it later by copying it into `.github/workflows/`. Local scans below remain genuine scan evidence; no hosted run is claimed.

With the official CodeQL CLI and language query packs installed, run from the repository root (choose a new writable directory for `SCAN_DIR`):

```bash
SCAN_DIR=/path/to/new/local-scan
mkdir -p "$SCAN_DIR"
codeql database create "$SCAN_DIR/go" --language=go \
  --source-root="$PWD" --command='./scripts/codeql-build.sh' --threads=2 --ram=4096
codeql database analyze "$SCAN_DIR/go" \
  codeql/go-queries:codeql-suites/go-security-extended.qls \
  --format=sarif-latest --output="$SCAN_DIR/go.sarif" --threads=2 --ram=4096
codeql database create "$SCAN_DIR/javascript" --language=javascript-typescript \
  --build-mode=none --source-root="$PWD" \
  --codescanning-config=temp/codeql/config.yml --threads=2 --ram=4096
codeql database analyze "$SCAN_DIR/javascript" \
  codeql/javascript-queries:codeql-suites/javascript-security-extended.qls \
  --format=sarif-latest --output="$SCAN_DIR/javascript.sarif" --threads=2 --ram=4096
python3 scripts/codeql-report.py "$SCAN_DIR/go.sarif" "$SCAN_DIR/javascript.sarif" \
  --output "$SCAN_DIR/review-pending.json"
```

The build script invokes Go **inside each module directory**. The local extractor skipped invocations beginning with `go -C`, so that form is deliberately avoided. Cache-enabled extraction was verified against the full 129-file production coverage. Always inspect coverage and extraction diagnostics, not just the process exit code.

## Decisions and residual risks

The SQL finding identifies the platform's intentional SQL execution sink. Approved migration DDL cannot become a parameter placeholder. The worker revalidates authority, immutable revision/hash and required group decisions; the engine parses the statement and protects its receipt metadata. The residual ability of authorized editors/leaders to approve destructive target SQL is accepted as part of the product. Restricted target roles remain an operational requirement. This disposition does not establish that every possible SQL-policy bypass has been ruled out.

The two log-injection alerts reach structured `slog` JSON handlers. JSON encoding escapes embedded control characters and preserves values as fields; production OTLP export receives the same redacted attributes. The six browser code-construction alerts use complete `JSON.stringify` string literals in direct DevTools evaluations with repository-controlled test values, without HTML/script-element embedding. The file/network alerts concern a private Chrome profile and a fixed JSON timeout diagnostic. These are reviewed trust-boundary decisions, not broad claims that those patterns are always safe.

The allocation findings were valid hardening opportunities: bound LCS dimensions before `+1`, guard the cell product by division against two million cells, use a linear fallback for large/asymmetric inputs, and remove summed preallocation capacities. Commit **7694875** includes the 12000-line asymmetric regression; `go test ./unit/src/...` passed. The fresh rescan contains **zero** `go/allocation-size-overflow` findings with unchanged source coverage.

## Every reported finding

Locations below refer to the original retained SARIF, before the allocation fix moved lines. Severity includes the CodeQL numeric security score and SARIF level. Full messages, dataflows, impact, reasoning and fix/rescan references are in `codeql-review.json`.

| ID | Rule / severity | Original location | Potential impact | Decision and reason |
| --- | --- | --- | --- | --- |
| G01 | `go/sql-injection` / 8.8 (error) | [src/src/backend/migration/engines/postgres/execution.go:60:39](../src/src/backend/migration/engines/postgres/execution.go#L60) | An authorized migration can change or destroy target data; bypassed authority or an overprivileged target role would magnify the impact. | Accepted by design: approved target SQL execution is intentional; permission/revision/leader revalidation and restricted target roles constrain this sink. |
| G02 | `go/allocation-size-overflow` / 8.1 (warning) | [src/src/backend/api/response/schema_diff.go:56:37](../src/src/backend/api/response/schema_diff.go#L56) | Integer overflow or excessive LCS allocation could panic a schema-diff request; summed capacities also lacked explicit bounds. | Fixed in **7694875**: bounded dimensions/product and append-based growth; absent from the fresh Go rescan. |
| G03 | `go/allocation-size-overflow` / 8.1 (warning) | [src/src/backend/api/response/schema_diff.go:55:13](../src/src/backend/api/response/schema_diff.go#L55) | Integer overflow or excessive LCS allocation could panic a schema-diff request; summed capacities also lacked explicit bounds. | Fixed in **7694875**: bounded dimensions/product and append-based growth; absent from the fresh Go rescan. |
| G04 | `go/allocation-size-overflow` / 8.1 (warning) | [src/src/backend/api/response/schema_diff.go:56:22](../src/src/backend/api/response/schema_diff.go#L56) | Integer overflow or excessive LCS allocation could panic a schema-diff request; summed capacities also lacked explicit bounds. | Fixed in **7694875**: bounded dimensions/product and append-based growth; absent from the fresh Go rescan. |
| G05 | `go/allocation-size-overflow` / 8.1 (warning) | [src/src/backend/api/response/schema_diff.go:74:47](../src/src/backend/api/response/schema_diff.go#L74) | Integer overflow or excessive LCS allocation could panic a schema-diff request; summed capacities also lacked explicit bounds. | Fixed in **7694875**: bounded dimensions/product and append-based growth; absent from the fresh Go rescan. |
| G06 | `go/allocation-size-overflow` / 8.1 (warning) | [src/src/backend/api/response/schema_diff.go:74:59](../src/src/backend/api/response/schema_diff.go#L74) | Integer overflow or excessive LCS allocation could panic a schema-diff request; summed capacities also lacked explicit bounds. | Fixed in **7694875**: bounded dimensions/product and append-based growth; absent from the fresh Go rescan. |
| G07 | `go/allocation-size-overflow` / 8.1 (warning) | [src/src/backend/api/response/schema_diff.go:166:47](../src/src/backend/api/response/schema_diff.go#L166) | Integer overflow or excessive LCS allocation could panic a schema-diff request; summed capacities also lacked explicit bounds. | Fixed in **7694875**: bounded dimensions/product and append-based growth; absent from the fresh Go rescan. |
| G08 | `go/allocation-size-overflow` / 8.1 (warning) | [src/src/backend/api/response/schema_diff.go:166:59](../src/src/backend/api/response/schema_diff.go#L166) | Integer overflow or excessive LCS allocation could panic a schema-diff request; summed capacities also lacked explicit bounds. | Fixed in **7694875**: bounded dimensions/product and append-based growth; absent from the fresh Go rescan. |
| G09 | `go/allocation-size-overflow` / 8.1 (warning) | [src/src/backend/migration/diff/compare.go:572:31](../src/src/backend/migration/diff/compare.go#L572) | Integer overflow or excessive LCS allocation could panic a schema-diff request; summed capacities also lacked explicit bounds. | Fixed in **7694875**: bounded dimensions/product and append-based growth; absent from the fresh Go rescan. |
| G10 | `go/allocation-size-overflow` / 8.1 (warning) | [src/src/backend/migration/diff/compare.go:572:48](../src/src/backend/migration/diff/compare.go#L572) | Integer overflow or excessive LCS allocation could panic a schema-diff request; summed capacities also lacked explicit bounds. | Fixed in **7694875**: bounded dimensions/product and append-based growth; absent from the fresh Go rescan. |
| G11 | `go/log-injection` / 6.1 (error) | [src/src/backend/migration/workflow.go:95:94](../src/src/backend/migration/workflow.go#L95) | Raw control characters in text logging could forge records or hide audit events. | False positive in configured sinks: slog JSON encodes dynamic fields and escapes control characters; production fan-out redacts first. |
| G12 | `go/log-injection` / 6.1 (error) | [src/shared/backend/observability/telemetry.go:218:40](../src/shared/backend/observability/telemetry.go#L218) | Raw control characters in text logging could forge records or hide audit events. | False positive in configured sinks: slog JSON encodes dynamic fields and escapes control characters; production fan-out redacts first. |
| J01 | `js/bad-code-sanitization` / 6.1 (error) | [src/tests/frontend/person1-ui.mjs:584:120](../src/tests/frontend/person1-ui.mjs#L584) | Unsafe JavaScript string construction could execute unintended code in the test browser. | False positive: complete JSON.stringify literals go directly to CDP, with fixed test inputs and no HTML context. |
| J02 | `js/bad-code-sanitization` / 6.1 (error) | [src/tests/frontend/person1-ui.mjs:584:224](../src/tests/frontend/person1-ui.mjs#L584) | Unsafe JavaScript string construction could execute unintended code in the test browser. | False positive: complete JSON.stringify literals go directly to CDP, with fixed test inputs and no HTML context. |
| J03 | `js/bad-code-sanitization` / 6.1 (error) | [src/tests/frontend/person1-ui.mjs:584:306](../src/tests/frontend/person1-ui.mjs#L584) | Unsafe JavaScript string construction could execute unintended code in the test browser. | False positive: complete JSON.stringify literals go directly to CDP, with fixed test inputs and no HTML context. |
| J04 | `js/bad-code-sanitization` / 6.1 (error) | [src/tests/frontend/person1-ui.mjs:588:126](../src/tests/frontend/person1-ui.mjs#L588) | Unsafe JavaScript string construction could execute unintended code in the test browser. | False positive: complete JSON.stringify literals go directly to CDP, with fixed test inputs and no HTML context. |
| J05 | `js/bad-code-sanitization` / 6.1 (error) | [src/tests/frontend/person1-ui.mjs:588:335](../src/tests/frontend/person1-ui.mjs#L588) | Unsafe JavaScript string construction could execute unintended code in the test browser. | False positive: complete JSON.stringify literals go directly to CDP, with fixed test inputs and no HTML context. |
| J06 | `js/bad-code-sanitization` / 6.1 (error) | [src/tests/frontend/person1-ui.mjs:592:105](../src/tests/frontend/person1-ui.mjs#L592) | Unsafe JavaScript string construction could execute unintended code in the test browser. | False positive: complete JSON.stringify literals go directly to CDP, with fixed test inputs and no HTML context. |
| J07 | `js/file-access-to-http` / 6.5 (warning) | [src/tests/frontend/person1-ui.mjs:514:30](../src/tests/frontend/person1-ui.mjs#L514) | An attacker-controlled port file could redirect a test browser control request to another local port. | Accepted test flow: trusted Chrome child writes its private profile port; request host stays loopback. |
| J08 | `js/http-to-file-access` / 6.3 (warning) | [src/tests/frontend/person1-ui.mjs:569:53](../src/tests/frontend/person1-ui.mjs#L569) | Network-controlled filesystem paths or executable file contents could overwrite files or introduce code. | False positive: browser response is inert JSON in a fixed diagnostic filename, not a caller-controlled path or executable. |

A successful or clean SAST scan is not proof of application security. These results cover selected static queries and the recorded source snapshots; authentication, authorization, SQL policy, operational configuration and recovery still require their runtime tests and review.
