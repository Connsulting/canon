# API Contract Verification Draft Spec

## Problem Statement
Automation consumes Canon CLI JSON output, but contract drift can go unnoticed when implementation, tests, and docs evolve independently. We need explicit contract verification so machine consumers can rely on stable JSON fields and failure semantics.

## Desired Behavior
Add contract-focused CLI tests that execute real command flows and strictly decode JSON output for contract stability.

## Scope
In scope:
1. Verify machine-readable JSON contracts for `blame`, `check`, `deps-risk`, and `schema-evolution`.
2. Verify contract failure semantics:
   - invalid flag handling
   - positional argument rejection where defined
   - threshold/non-zero exits where applicable
3. Align README contract documentation with tested behavior.

Out of scope:
1. New product features or new command surfaces.
2. Non-JSON output redesign.
3. Behavioral changes unrelated to contract correctness.

## Assumptions
1. API contracts are CLI JSON payloads used by automation.
2. Contract verification should fail on unknown fields (strict decoding) to catch accidental shape drift.
3. This branch does not expose a `roadmap-entropy` command in `cmd/canon/main.go`; that command is treated as unsupported for this task and verified as such.

## Contract Matrix
1. `canon blame --json ... <query>`
   - Required fields: `query`, `found`
   - Required result item fields when `found=true`: `spec_id`, `title`, `domain`, `confidence`, `created`, `relevant_lines`
   - Negative semantics: invalid flags fail; empty query fails.
2. `canon check --json ...`
   - Required fields: `passed`, `total_specs`, `total_conflicts`, `conflicts`
   - Optional field: `report_paths`
   - Negative semantics: positional args rejected; conflicts return non-zero while still emitting valid JSON.
3. `canon deps-risk --json ...`
   - Required fields: `root`, `go_mod_path`, `go_sum_path`, `go_sum_present`, `dependency_count`, `findings`, `summary`, `threshold_exceeded`
   - `summary` required fields: `total_findings`, `security_findings`, `maintenance_findings`, `highest_severity`, `findings_by_severity`
   - Optional field: `fail_on`
   - Negative semantics: invalid `--fail-on` rejected; positional args rejected; threshold exceed returns non-zero while still emitting valid JSON.
4. `canon schema-evolution --json ...`
   - Required fields: `root`, `migration_file_count`, `statement_count`, `findings`, `summary`, `threshold_exceeded`
   - `summary` required fields: `total_findings`, `highest_severity`, `findings_by_severity`
   - Optional field: `fail_on`
   - Negative semantics: invalid `--fail-on` rejected; positional args rejected; threshold exceed returns non-zero while still emitting valid JSON.
5. `canon roadmap-entropy`
   - Current branch contract: unsupported command surface.
   - Negative semantics: command invocation fails with unknown-command error.

## Acceptance Criteria
1. New contract test file exists at `cmd/canon/api_contract_test.go`.
2. Tests use real CLI command paths (`run(...)`) with `--json` where supported.
3. JSON payloads are strict-decoded with unknown-field rejection.
4. README documents JSON contract guarantees and non-zero behavior for threshold/conflict conditions.
5. Unit tests and vet pass.
6. Manual positive and negative CLI checks are executed for each verified behavior in scope.

## Verification Commands
1. Formatting and static checks:
   - `gofmt -w cmd/canon/api_contract_test.go cmd/canon/main.go cmd/canon/main_test.go internal/canon/types.go`
   - `go test ./...`
   - `go vet ./...`
2. Manual CLI checks (positive and negative):
   - `go run ./cmd/canon blame --root <fixture-root> --json --response-file <response.json> "<query>"`
   - `go run ./cmd/canon blame --root <fixture-root> --json`
   - `go run ./cmd/canon check --root <fixture-root> --json --response-file <response.json>`
   - `go run ./cmd/canon check --root <fixture-root> extra`
   - `go run ./cmd/canon deps-risk --root <fixture-root> --json`
   - `go run ./cmd/canon deps-risk --root <fixture-root> --json --fail-on medium`
   - `go run ./cmd/canon schema-evolution --root <fixture-root> --json`
   - `go run ./cmd/canon schema-evolution --root <fixture-root> --json --fail-on medium`
   - `go run ./cmd/canon roadmap-entropy --json`
