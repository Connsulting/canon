---
id: 6d2c4fa
type: technical
title: "README CLI Documentation Parity Backfill"
domain: canon-cli
created: 2026-03-13T07:09:12Z
depends_on: [8cfdb59, a1e9c10, c0d3f0f, e1bcd34, e2cc388]
touched_domains: [canon-cli, release, tools]
---
# Documentation Backfiller Draft Spec

## Problem Statement
README command documentation has drifted from implemented CLI behavior. This causes operators to miss supported commands/flags and rely on incorrect defaults.

## Desired Behavior
Backfill README so command and flag docs match implemented `canon` CLI behavior, and add a unit test that guards critical parity to prevent future drift.

## Scope (Minimal High-Value)
In scope:
1. Update `README.md` command list and options where behavior is already implemented in `cmd/canon/main.go`.
2. Correct known drift for `log --all` default semantics.
3. Add docs for currently implemented but missing items (`index`, `version`, `log --show-tags`, `gc --root`, and related concrete flags needed for parity).
4. Add a unit test that enforces critical README/CLI parity checks.

Out of scope:
1. New CLI features.
2. Full docs site generation or broad prose rewrite.
3. Behavioral changes to CLI command handling.

## Assumptions
1. `cmd/canon/main.go` and `printUsage()` are the behavior source of truth.
2. README should focus on practical command usage and key options, not internal implementation detail.
3. Critical parity assertions are preferable to brittle full-text snapshot tests.

## Acceptance Criteria
1. README command list includes all user-facing commands currently routed by `run()`.
2. README includes docs for `index` and `version` commands.
3. README includes `log --show-tags` and corrected `log --all` default behavior.
4. README includes `gc --root` and other implemented core GC flags.
5. Unit tests include a dedicated README parity test for critical command/flag/default coverage.
6. `go test ./...` passes.
7. `go vet ./...` passes.
8. Manual CLI checks include one positive and one negative run for this feature.

## Validation Plan
1. `go test ./...`
2. `go vet ./...`
3. Positive CLI check: `go run ./cmd/canon version --short`
4. Negative CLI check: `go run ./cmd/canon does-not-exist`

## Risks / Tradeoffs
1. README parity tests can become too rigid; keep assertions focused on high-risk drift points.
2. Expanding docs too broadly increases maintenance burden; keep edits minimal and source-aligned.

## AI Enhancements

# README CLI Documentation Parity Backfill
## Problem
`README.md` command documentation has drifted from implemented `canon` CLI behavior. That drift hides supported commands and flags, and can misstate defaults used by operators.
## Source of Truth
Behavior for this work is defined by the implemented CLI in `cmd/canon/main.go` and `printUsage()`.
## Requirements
- Update `README.md` so the documented command list matches all user-facing commands currently routed by `run()`.
- Add missing command coverage for `index` and `version`.
- Correct README drift for `log --all` semantics and document `log --show-tags`.
- Document `gc --root` and the other implemented core GC flags needed for practical parity.
- Keep README guidance focused on practical command usage and key options rather than internal implementation details.
- Add a dedicated unit test that enforces critical README/CLI parity points without relying on a brittle full-text snapshot.
## Non-Goals
- Adding new CLI features.
- Broad documentation rewrites or docs-site generation.
- Behavioral changes to CLI command handling.
## Validation
- `go test ./...`
- `go vet ./...`
- Positive CLI check: `go run ./cmd/canon version --short`
- Negative CLI check: `go run ./cmd/canon does-not-exist`
## Risks
- README parity tests can become too rigid if they assert full text instead of high-risk parity points.
- Expanding docs beyond implemented behavior increases maintenance burden and future drift risk.
