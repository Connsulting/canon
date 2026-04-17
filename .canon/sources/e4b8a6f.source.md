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
