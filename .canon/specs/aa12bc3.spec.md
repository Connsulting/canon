---
id: aa12bc3
type: technical
title: "Roadmap Entropy Detector"
domain: canon
created: 2026-03-06T00:00:00Z
depends_on: []
touched_domains: [canon, cli]
---
# Roadmap Entropy Detector Draft Spec

## Problem Statement
Canon currently lacks a deterministic signal for roadmap scope creep and intent drift across evolving specs. Teams need a CLI check that highlights suspicious expansion and drift patterns from Canon history artifacts before they become planning debt.

## Desired Behavior
Add a `roadmap-entropy` CLI command that analyzes Canon artifacts (`.canon/specs` and `.canon/ledger`) using deterministic offline heuristics and reports potential scope creep/drift in text or JSON, with optional CI fail gating.

## v1 Scope
In scope:
1. Deterministic local analysis only (no AI adjudication).
2. Windowed comparison between recent and baseline Canon history segments.
3. Scope-creep heuristics:
   - new primary domains in recent window vs baseline window
   - touched-domain expansion in recent window vs baseline window
4. Drift heuristics:
   - rising non-feature ratio (technical/resolution vs total specs)
   - orphan technical/resolution specs (no lineage/dependency relationships)
5. Stable text output and machine-readable JSON output.
6. Optional fail threshold via `--fail-on`.

Out of scope:
1. Policy decisions or automated remediation.
2. AI-first semantic contradiction adjudication for this command.
3. Non-Canon artifact analysis.

## Assumptions
1. Roadmap intent is sufficiently represented by active Canon artifacts in `.canon/specs` and `.canon/ledger`.
2. Ledger ordering approximates chronology for recent-vs-baseline comparison.
3. Deterministic heuristics are preferred for CI reproducibility in v1.

## CLI Contract
Command:
- `canon roadmap-entropy`

Flags:
1. `--root <path>` repository root containing `.canon/` artifacts (default `.`).
2. `--window <n>` specs per comparison window; recent and baseline windows are both size `n` (default `8`).
3. `--json` output machine-readable JSON.
4. `--fail-on <severity>` fail command when highest finding severity meets/exceeds threshold (`low`, `medium`, `high`, `critical`).

Validation:
1. Reject positional arguments.
2. Reject non-positive `--window`.
3. Reject invalid `--fail-on` values.

Exit behavior:
1. Return non-zero for validation/execution errors.
2. Return non-zero when threshold is exceeded with `--fail-on`.

## Severity Model
Severity ordering:
- `none < low < medium < high < critical`

Severity reflects v1 heuristic confidence/impact:
1. `scope-creep/new-domains`: medium-high-critical based on count of newly introduced primary domains.
2. `scope-creep/touched-domain-expansion`: low-medium-high based on expansion breadth.
3. `drift/non-feature-ratio-rise`: low-medium-high-critical based on ratio delta and resulting recent-window ratio.
4. `drift/orphan-non-feature-specs`: low-medium-high-critical based on orphan count growth and orphan resolution presence.

## Determinism Requirements
1. Window selection is deterministic from Canon history order.
2. Findings sorted deterministically by severity, category, and stable lexical tie-breakers.
3. Summary fields are deterministic:
   - total findings
   - findings by category
   - findings by severity
   - highest severity
4. JSON schema and key fields remain stable for CI assertions.

## Tradeoffs (v1)
1. Rule-based heuristics may over/under-report in edge cases but provide transparent deterministic behavior.
2. Recent-vs-baseline windows can miss long-horizon patterns by design; this keeps output simple and stable.
3. Orphan detection favors structural linkage signals over semantic intent.

## Acceptance Criteria
1. New command `canon roadmap-entropy` exists and is listed in CLI usage/docs.
2. Command supports text output and `--json` output.
3. Command enforces `--fail-on` threshold semantics.
4. Heuristics detect:
   - new domains
   - touched-domain expansion
   - non-feature ratio rise
   - orphan technical/resolution specs
5. Unit tests cover heuristic detection, deterministic ordering, summary aggregation, severity parsing, and threshold checks.
6. CLI tests cover JSON shape, threshold failure, invalid `--fail-on`, and positional-argument rejection.

## Validation Checklist
- [ ] `gofmt -w` on changed Go files.
- [ ] `go test ./...`.
- [ ] `go vet ./...`.
- [ ] Manual positive E2E: `go run ./cmd/canon roadmap-entropy --root . --json`.
- [ ] Manual negative E2E: `go run ./cmd/canon roadmap-entropy --fail-on urgent`.
