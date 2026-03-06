---
id: d9f4a2b
type: technical
title: "Privacy Policy Consistency Checker"
domain: privacy
created: 2026-03-06T05:13:08Z
depends_on: [9ad4f11]
touched_domains: [cli, privacy]
---
# Privacy Policy Consistency Checker Draft Spec

## Problem Statement
Canon has no built-in way to validate repository code against privacy policy claims. Teams need a repeatable CLI check that can identify where code appears to support, contradict, or fail to provide evidence for policy statements.

## Desired Behavior
Add an AI-first `privacy-check` command that compares a local privacy policy document against repository code context and returns structured findings with CI-friendly threshold gating.

## v1 Scope
In scope:
1. Static analysis only using repository files and policy text.
2. Local policy input (`--policy-file`) in Markdown/text format.
3. AI adjudication via `auto` and deterministic `from-response` mode.
4. Structured findings with status (`supported`, `contradicted`, `unverifiable`) and severity.
5. Human-readable and JSON output with fail threshold support.

Minimal v1 boundaries:
1. No network lookups or runtime probes; decisions are based only on local files gathered under the configured scan scope.
2. Deterministic CI operation is supported by `--response-file` and should be used in tests.
3. Claim evaluation quality is review-oriented, not proof-grade enforcement.

Out of scope:
1. Runtime tracing or live traffic validation.
2. External service/privacy vendor behavior verification.
3. Automatic policy claim extraction persistence or remediation.

## CLI Contract
Command: `canon privacy-check`

Flags:
1. `--root <path>` repository root (default `.`)
2. `--policy-file <path>` required local privacy policy file
3. `--code-path <path>` scope code analysis to one or more repo paths (repeatable)
4. `--context-limit <kb>` maximum code context size sent to AI (default `120`)
5. `--max-file-bytes <n>` max bytes per scanned file (default `65536`)
6. `--ai <mode>` `auto|from-response` (default `auto`)
7. `--ai-provider <name>` `codex|claude` override
8. `--response-file <path>` deterministic AI response input
9. `--json` machine-readable output
10. `--fail-on <severity>` fail when highest finding severity meets/exceeds threshold (`low|medium|high|critical`)

Validation:
1. Reject positional arguments.
2. Reject missing `--policy-file`.
3. Reject invalid `--fail-on` severity.
4. Reject unknown/unsupported AI mode.

Exit behavior:
1. Command errors return non-zero.
2. If threshold is exceeded, return non-zero with clear summary.

## AI Contract
Prompt includes:
1. Policy document body.
2. Scoped repository code context constrained by size limits.
3. Required output schema for findings.

Response schema (v1):
1. `model` string.
2. `findings` array.
3. Finding fields: `claim_id`, `claim`, `status`, `severity`, `reason`, `evidence_paths`, `evidence_snippets`.

## Determinism and Normalization
1. Deterministic tests rely on `--response-file`.
2. Findings are normalized (trimmed, deduplicated, defaulted severity/status).
3. Findings are sorted by severity then stable textual keys.
4. Summary includes status counts, severity counts, and highest severity.

## File Scope and Context Limits
1. Only text files from scoped paths are considered.
2. Binary/symlink/oversized/ignored files are excluded.
3. Aggregate context is truncated to configured limit while preserving stable ordering.

## Failure Modes and Handling
1. Missing/empty policy file returns clear validation error.
2. Empty eligible code context returns clear scan error.
3. Invalid AI response JSON returns parse error.
4. Unknown AI provider/runtime unavailability in `auto` mode returns runtime-ready error.

## Implementation Assumptions
1. Existing WIP changes in `internal/canon/privacy_policy.go`, `internal/canon/types.go`, `internal/canon/api.go`, and `cmd/canon/main.go` are the intended baseline.
2. `privacy-check` findings are static assertions over scanned file content and do not imply runtime behavior.
3. Severity threshold failures (`--fail-on`) are treated as command failure conditions for CI/CD usage.

## Risks and Tradeoffs
1. AI adjudication can produce false positives/negatives; evidence snippets and path references are required for reviewability.
2. Context limits reduce cost and improve speed but may miss contradictory evidence in excluded files.
3. Static-only analysis cannot prove runtime behavior.

## Acceptance Checks
1. Add core checker implementation and CLI wiring.
2. Add unit tests for policy loading, context selection, response parsing, normalization/dedupe, summary, and threshold logic.
3. Add CLI tests for success path, JSON shape, validation errors, and threshold failure exit semantics.
4. Run `gofmt`, `go test ./...`, and `go vet ./...` successfully.
5. Run one manual positive and one manual negative CLI end-to-end flow.

## AI Enhancements

Add a static privacy policy consistency checker command with deterministic response-file support and fail-on severity gating.
