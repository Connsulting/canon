---
id: fbe1c7a
type: technical
title: "Schema Evolution Advisor"
domain: database
created: 2026-03-06T07:49:12Z
depends_on: [4e9a21f]
touched_domains: [database]
---
# Schema Evolution Advisor Draft Spec

## Problem Statement
Canon currently has no built-in analyzer for migration-level schema safety checks. Teams need a deterministic offline command that reviews SQL migration files and flags likely breaking changes before rollout.

## Desired Behavior
Add `canon schema-evolution` to inspect SQL migration files under a repository root, apply deterministic heuristic rules for risky schema changes, and return stable human-readable or JSON findings suitable for CI gating.

## Scope (v1)
In scope:
1. Offline analysis only (no live database access).
2. SQL text heuristics over migration files (no migration-tool-specific parser/AST dependency).
3. Deterministic findings with severity, rule id, file, and statement context.
4. Stable text/JSON output.
5. `--fail-on` severity threshold for non-zero CI exit.

Out of scope:
1. Executing migrations.
2. Database-specific semantic validation against actual schema state.
3. Perfect SQL parsing across all dialect edge cases.

## Assumptions
1. Migration SQL files are available in the repo and can be discovered via filename heuristics.
2. Conservative false positives are acceptable when they prevent missed breaking changes.
3. Deterministic ordering is required for repeatable CI and tests.

## CLI Contract
Command:
- `canon schema-evolution [--root <path>] [--json] [--fail-on <severity>]`

Flags:
1. `--root <path>` repository root to analyze (default: `.`).
2. `--json` emit machine-readable JSON payload.
3. `--fail-on <severity>` fail when highest finding severity meets/exceeds threshold (`low`, `medium`, `high`, `critical`).

Validation:
1. Reject positional arguments.
2. Reject invalid `--fail-on` values.

Exit behavior:
1. Non-zero on validation/analyzer errors.
2. Non-zero when threshold is exceeded.

## Heuristic Rules (v1)
Each matched statement emits one finding:
1. `drop-table`: `DROP TABLE` (high).
2. `drop-column`: `ALTER TABLE ... DROP COLUMN` (high).
3. `rename-column`: `ALTER TABLE ... RENAME COLUMN` (high).
4. `alter-column-type`: `ALTER TABLE ... ALTER COLUMN ... TYPE` (high).
5. `add-not-null-no-default`: `ALTER TABLE ... ADD COLUMN ... NOT NULL` without `DEFAULT` in same statement (medium).
6. `set-not-null`: `ALTER TABLE ... ALTER COLUMN ... SET NOT NULL` (medium).

Severity order: `none < low < medium < high < critical`.

## Discovery and Determinism
1. Discover candidate migration SQL files recursively under root while skipping common non-source directories (`.git`, `.canon`, `vendor`, `node_modules`).
2. File heuristic: include `.sql` files where path or filename indicates migration intent (`migration`, `migrations`, `migrate`, `schema`, `ddl`, or timestamped migration naming).
3. Split SQL into statements by semicolon with quote-aware handling for `'...'`, `"..."`, `` `...` ``, and dollar-quoted blocks (`$$...$$`, `$tag$...$tag$`), and strip `--` and `/* */` comments before matching.
4. Sort findings deterministically by severity desc, then file path, line, rule id, and statement.
5. Summary includes file count, statement count, finding count, highest severity, and severity counts.

## Data Model (v1)
Result includes:
1. Root path, scanned migration file count, scanned statement count.
2. Findings (`rule_id`, `severity`, `file`, `line`, `statement`, `message`).
3. Summary (`total_findings`, `highest_severity`, `findings_by_severity`).
4. Optional threshold fields (`fail_on`, `threshold_exceeded`).

## Risks and Tradeoffs
1. Statement splitting and regex heuristics can misclassify unusual SQL formatting.
2. Conservative matching can produce false positives, but this is preferred for migration safety alerts in v1.
3. Without dialect-aware parsing, v1 favors broad SQL compatibility over deep precision.

## Acceptance Criteria
1. New `schema-evolution` command exists in CLI dispatch/help/docs.
2. Analyzer implementation exists in `internal/canon/schema_evolution.go`.
3. API wrappers are added in `internal/canon/api.go`.
4. Types are added in `internal/canon/types.go`.
5. Unit tests cover heuristics, deterministic ordering, no-finding path, error path, and threshold behavior.
6. CLI tests cover JSON shape, threshold failure, invalid `--fail-on`, and positional-argument rejection.
7. README documents command/options/examples.

## Validation Plan
1. `gofmt -w` on changed Go files.
2. `go test ./...`.
3. `go vet ./...`.
4. Manual positive CLI check:
   - `go run ./cmd/canon schema-evolution --root <fixture-root>`
5. Manual negative CLI check:
   - `go run ./cmd/canon schema-evolution --fail-on urgent`

## AI Enhancements

Schema evolution analyzer with quote-aware splitting and deterministic CI severity gates.
