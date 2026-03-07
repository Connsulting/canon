---
id: 8cfdb59
type: feature
title: "Auto DRY AI Response Parsing Refactor (Draft)"
domain: general
created: 2026-03-07T08:50:34Z
depends_on: [c5e7d91]
touched_domains: [general]
---
# Auto DRY AI Response Parsing Refactor (Draft)

## Problem Statement
Multiple command flows implement near-identical logic for:
1. Validating and resolving `--response-file` paths.
2. Reading response-file bytes.
3. Decoding AI JSON with fallback extraction from mixed prose output.

This duplication increases maintenance cost and makes future fixes/error-handling improvements easy to miss in one flow.

## Scope Confirmation
This refactor is intentionally narrow and behavior-preserving:
1. Deduplicate AI response-file path loading and resilient JSON extraction only.
2. Migrate these flows to the shared helper: `init`, `ingest`, `check`, `blame`, `render`, `gc`, `semantic-diff`.
3. Preserve each flow's current user-facing error text and per-flow post-processing (normalization/sorting/defaulting).

## Non-Goals
1. No CLI flag changes.
2. No business-rule changes in conflict detection, rendering, blame ranking, or semantic diff logic.
3. No changes to unrelated local worktree edits.

## Design
Add a new internal helper module that provides:
1. Response-file path normalization/read helper.
2. Generic JSON decode helper that first attempts direct decode and then extracts the first valid JSON object from mixed text.

Callers remain responsible for mapping helper errors to existing command-specific error strings.

## Acceptance Criteria
1. Shared helper is used by all seven targeted flows.
2. Existing user-visible behavior is unchanged (including existing error message text at command boundaries).
3. Unit tests cover helper behavior:
1. Empty response-file path rejection.
2. Relative and absolute path handling.
3. Direct JSON decode.
4. Wrapped/prose JSON extraction.
5. Invalid JSON error propagation.
4. `gofmt` passes on all touched Go files.
5. Targeted tests and `go test ./...` pass, or unrelated pre-existing failures are documented.
6. Manual CLI checks pass:
1. Positive: `--ai from-response` with valid response file.
2. Negative: `--ai from-response` without `--response-file` fails as expected.

## Risks and Tradeoffs
1. Risk: helper changes decoding semantics if extraction behavior differs from prior `first "{" + last "}"` approach.
2. Mitigation: keep command-level error wrappers stable and add focused tests for wrapped JSON + invalid payloads.

## Rollback Plan
1. Revert helper-caller migration commit.
2. Restore prior per-command parsing functions.
3. Re-run existing command tests to ensure pre-refactor behavior.
