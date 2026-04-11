---
id: f2a9c8d
type: feature
title: "Canon Init Managed Multi-Pass Crawl"
domain: init
created: 2026-04-11T10:53:19Z
depends_on: [e4b8a6f]
touched_domains: [ai-runtime, canon-cli, init]
---
# Canon Init Managed Multi-Pass Crawl RFE

## Problem Statement
`canon init --crawl agentic` avoids the one-big-prompt failure mode by giving the provider a seed inventory and asking it to inspect the repository directly. That scales better, but Canon does not own or measure the crawl. The provider decides what to inspect, Canon does not persist or synthesize a coverage view, and users cannot tell whether major project areas were analyzed or skipped.

Large repositories need a Canon-managed crawl mode that decomposes repository discovery into bounded passes, collects evidence by area, and synthesizes specs from explicit intermediate analysis instead of depending on a single opaque provider session.

## Desired Behavior
Add a managed multi-pass crawl mode for `canon init` that:
1. Builds a deterministic crawl plan from the gitignore-filtered repository inventory.
2. Groups files into bounded analysis areas such as root config files, top-level directories, docs, tests, application code, and infrastructure.
3. Sends focused evidence packs to the AI provider for per-area analysis.
4. Synthesizes final Canon specs from the collected area summaries and coverage data.
5. Reports crawl progress and coverage-oriented status to the user.

## Scope
In scope:
1. Add `--crawl multipass` as a third init crawl mode alongside `snapshot` and `agentic`.
2. Reuse the existing gitignore-aware scan so ignored files remain excluded unless explicitly included.
3. Create a deterministic area plan from filtered file paths.
4. Build bounded per-area evidence packs containing selected file paths and text excerpts.
5. Add a provider call for per-area analysis using a small structured JSON schema.
6. Add a final synthesis provider call that produces the existing init spec JSON shape.
7. Preserve `--response-file` behavior as a bypass for live provider work.
8. Add progress output that shows multipass planning, area analysis, and synthesis.
9. Add tests covering plan creation, prompt shape, CLI acceptance, and invalid mode rejection.

Out of scope for this iteration:
1. Persisting full crawl transcripts under `.canon/`.
2. Parallel provider calls.
3. Resumable crawl sessions.
4. A separate verifier pass that rejects unsupported claims.
5. Perfect semantic completeness guarantees for every repository size.

## Assumptions
1. The existing local scan provides a reliable gitignore-filtered file inventory.
2. Bounded per-area evidence packs are more scalable than one bundled repository prompt.
3. Per-area summaries give the final synthesis pass better coverage context than a single seed inventory.
4. This iteration should remain provider-compatible with the existing `codex` and `claude` execution paths.

## CLI Contract
Command:
- `canon init [options]`

Updated flag:
1. `--crawl <mode>`
   - `snapshot`: existing bounded prompt mode
   - `agentic`: seed inventory plus provider-directed repository inspection
   - `multipass`: Canon-managed area planning, per-area analysis, and final synthesis
   - default: `snapshot`

Validation:
1. Unsupported crawl modes must be rejected before init work begins.
2. `--response-file` remains valid and bypasses live provider work regardless of crawl mode.

Context budget:
1. In `snapshot`, `--context-limit` remains the bundled project context budget.
2. In `agentic`, `--context-limit` remains the seed inventory budget.
3. In `multipass`, `--context-limit` is the per-area evidence-pack budget.

## Managed Crawl Requirements
1. Area planning must be deterministic for a given file inventory.
2. Root-level files must be grouped into a root/config area instead of being dropped.
3. Top-level directories must become candidate areas.
4. Areas must record file counts and selected evidence paths.
5. Evidence packs must prefer high-signal files such as README/docs, manifests, entrypoints, schemas, routes, tests, and infrastructure definitions.
6. Evidence packs must not include ignored files, binary files, symlinks, or oversized file contents that the existing scan excludes.
7. Per-area analysis must request current behavior, key components, user-facing features, runtime wiring, risks/gaps, and evidence file paths.
8. Final synthesis must use all area summaries and include coverage instructions so major areas are represented or explicitly omitted as support-only.
9. User-facing output must clearly indicate that Canon is managing a multi-pass crawl.

## Acceptance Criteria
1. `canon init --crawl multipass` accepts the new mode and prints multipass-specific progress.
2. The multipass path performs at least one per-area provider request before final synthesis when live AI is used.
3. The final synthesis prompt includes area summaries and coverage data rather than a raw whole-repo dump.
4. `--response-file` still ingests a precomputed final init response without per-area provider calls.
5. Invalid crawl modes remain rejected by both the library and CLI.
6. Unit tests cover deterministic area planning and multipass prompt construction.
7. CLI tests cover a positive multipass run and a negative invalid-mode run.

## Validation Plan
1. `gofmt -w cmd/canon/main.go cmd/canon/main_test.go internal/canon/init.go internal/canon/init_test.go`
2. `go test ./internal/canon -run 'TestInit|TestScanProjectForInit|TestBuildInit'`
3. `go test ./cmd/canon -run 'TestInitCommand'`
4. `go test ./...`
5. `go vet ./...`
6. Positive manual CLI check:
   - `go run ./cmd/canon init --root <fixture-root> --crawl multipass --accept-all`
7. Negative manual CLI check:
   - `go run ./cmd/canon init --root <fixture-root> --crawl invalid`

## AI Enhancements

# Canon Init Managed Multi-Pass Crawl
`canon init --crawl agentic` avoids the one-big-prompt failure mode by giving the provider a seed inventory and asking it to inspect the repository directly. That scales better than a bounded snapshot prompt, but Canon still does not own or measure the crawl. The provider decides what to inspect, Canon does not persist or synthesize a coverage view, and users cannot tell whether major project areas were analyzed or skipped.
4. This iteration remains provider-compatible with the existing `codex` and `claude` execution paths.
```sh
canon init [options]
```
1. `--crawl <mode>` supports:
- `snapshot`: existing bounded prompt mode.
- `agentic`: seed inventory plus provider-directed repository inspection.
- `multipass`: Canon-managed area planning, per-area analysis, and final synthesis.
- default: `snapshot`.
6. Positive manual CLI check: `go run ./cmd/canon init --root <fixture-root> --crawl multipass --accept-all`
7. Negative manual CLI check: `go run ./cmd/canon init --root <fixture-root> --crawl invalid`
