---
id: e4b8a6f
type: feature
title: "Canon Init Agentic Crawl Mode"
domain: init
created: 2026-04-11T10:41:15Z
depends_on: [6a7c9d4, a94f2c1]
touched_domains: [ai-runtime, canon-cli, init]
---
# Canon Init Agentic Crawl Draft Spec

## Problem Statement
`canon init` currently decomposes repositories from a bounded, preassembled prompt context. That works for small projects, but it does not scale well to larger repositories because the provider only sees a subset of file contents at once. Users want `canon init` to behave more like an interactive repo-analysis session: start from a lightweight inventory, inspect the repository iteratively, and then generate specs from discovered evidence.

## Desired Behavior
Add a new `canon init` crawl mode that lets the AI provider start from a seed inventory and inspect repository files directly during decomposition instead of relying solely on one bundled prompt snapshot.

## Scope
In scope:
1. Add `--crawl snapshot|agentic` to `canon init`.
2. Preserve current bundled-prompt behavior as `snapshot`.
3. Add `agentic` mode that:
   - builds a lightweight seed inventory from the local scan
   - instructs the provider to inspect additional repository files directly
   - focuses on key components, features, infrastructure, workflows, and runtime wiring
4. Keep existing `--response-file` replay behavior.
5. Update user-visible init output so agentic runs describe the seed inventory instead of implying only those files are considered.
6. Add unit and CLI tests for the new mode and invalid mode rejection.

Out of scope:
1. Full multi-phase crawl transcripts or resumable crawl sessions.
2. Persisting a provider-side exploration log under `.canon/`.
3. Replacing the existing `snapshot` mode.
4. Perfect subsystem discovery for every provider/runtime combination.

## Assumptions
1. The provider runtime can inspect repository files when invoked in the target root.
2. A small inventory plus iterative repo reads scales better than one large bundled context.
3. The local filesystem scan still provides useful structure, ignore filtering, and seed metadata even when the provider performs deeper repo inspection.

## CLI Contract
Command:
- `canon init [options]`

New flag:
1. `--crawl <mode>`
   - `snapshot`: existing bounded prompt mode
   - `agentic`: inventory + direct repo inspection mode
   - default: `snapshot`

Interaction with existing flags:
1. `--context-limit` remains the size budget for the initial bundled context in `snapshot` mode.
2. `--context-limit` becomes the seed inventory budget in `agentic` mode.
3. `--response-file` remains valid and bypasses live provider execution regardless of crawl mode.

Validation:
1. Reject unsupported crawl modes.

## Agentic Mode Requirements
1. Build a seed inventory from scan metadata, not a full inline dump of many file bodies.
2. Seed inventory should include:
   - directory tree
   - high-signal file paths
   - README excerpt when available
   - scan statistics
3. Prompt must explicitly instruct the provider to:
   - inspect repository files directly as needed
   - avoid modifying files
   - describe current behavior only
   - cover key components and user-facing features, not only top-level summaries
4. User-facing output should clarify that the seed inventory is only a starting point for agentic inspection.

## Acceptance Criteria
1. `canon init --crawl snapshot` preserves existing behavior.
2. `canon init --crawl agentic` uses a distinct prompt path designed for iterative repo inspection.
3. Agentic prompt includes seed inventory and explicit “inspect repo directly” instructions.
4. Invalid crawl modes are rejected by the CLI.
5. README/help text documents the new flag and clarifies the meaning of `--context-limit` in agentic mode.
6. Unit tests cover prompt generation and mode validation.
7. CLI tests cover a positive agentic run and a negative invalid-mode run.

## Validation Plan
1. `gofmt -w cmd/canon/main.go cmd/canon/main_test.go internal/canon/init.go internal/canon/init_test.go`
2. `go test ./...`
3. `go vet ./...`
4. Positive manual CLI check:
   - `go run ./cmd/canon init --root <fixture-root> --crawl agentic --no-interactive`
5. Negative manual CLI check:
   - `go run ./cmd/canon init --root <fixture-root> --crawl invalid`

## AI Enhancements

# Canon Init Agentic Crawl Mode
`canon init` currently decomposes repositories from a bounded, preassembled prompt context. This works for small projects, but it does not scale well to larger repositories because the provider only sees a subset of file contents at once. Users need `canon init` to behave more like an interactive repository analysis session: start from a lightweight inventory, inspect repository evidence iteratively, and then generate specs from discovered behavior.
Add a new crawl mode for `canon init` so users can choose between the existing bundled snapshot workflow and an agentic workflow where the AI provider starts from a seed inventory and inspects repository files directly during decomposition.
```sh
canon init [options]
```
Add a new flag:
--crawl <mode>
Supported modes:
1. `snapshot`: the existing bounded prompt mode.
2. `agentic`: seed inventory plus direct repository inspection mode.
Default mode: `snapshot`.
Validation requirements:
1. Unsupported crawl modes must be rejected by the CLI with a clear error.
2. `--response-file` remains valid and bypasses live provider execution regardless of crawl mode.
## Snapshot Mode
`snapshot` preserves the current bundled prompt behavior.
In `snapshot` mode, `--context-limit` continues to define the size budget for the initial bundled project context sent to the provider.
## Agentic Mode
`agentic` mode must use a distinct prompt path designed for iterative repository inspection.
The initial context should be a lightweight seed inventory derived from local scan metadata, not a full inline dump of many file bodies. The seed inventory should include:
1. Directory tree.
2. High-signal file paths.
3. README excerpt when available.
4. Scan statistics.
In `agentic` mode, `--context-limit` defines the budget for the seed inventory rather than a full bundled prompt snapshot.
The prompt must explicitly instruct the provider to:
1. Inspect repository files directly as needed.
2. Avoid modifying files.
3. Describe current behavior only.
4. Cover key components, user-facing features, infrastructure, workflows, and runtime wiring.
5. Avoid producing only top-level summaries when evidence supports more specific subsystem and feature coverage.
The provider runtime is assumed to run in the target repository root and to have read access to repository files.
## User-Facing Output
When `canon init --crawl agentic` runs, progress and status text must clarify that the seed inventory is only a starting point for agentic inspection. Output should not imply that only the seed inventory files are considered.
2. Preserve current bundled prompt behavior as `snapshot`.
3. Add `agentic` mode using a lightweight seed inventory and direct repo inspection instructions.
5. Update user-visible init output for agentic runs.
7. Update README/help text for the new flag and the meaning of `--context-limit` in agentic mode.
2. `canon init --crawl agentic` uses a distinct prompt path designed for iterative repository inspection.
3. Agentic prompt includes seed inventory and explicit instructions to inspect the repository directly.
5. README/help text documents `--crawl` and clarifies `--context-limit` semantics in agentic mode.
7. CLI tests cover one positive agentic run and one negative invalid-mode run.
1. Run `gofmt -w cmd/canon/main.go cmd/canon/main_test.go internal/canon/init.go internal/canon/init_test.go`.
2. Run `go test ./...`.
3. Run `go vet ./...`.
go run ./cmd/canon init --root <fixture-root> --crawl agentic --no-interactive
go run ./cmd/canon init --root <fixture-root> --crawl invalid
