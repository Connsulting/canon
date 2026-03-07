---
id: e83d4ab
type: technical
title: "Semantic Diff Explainer Iteration 3"
domain: canon-cli
created: 2026-03-07T12:00:00Z
depends_on: [c5e7d91]
touched_domains: [ai-runtime, canon-cli]
---
# Semantic Diff Explainer (Iteration 3)

## Problem Statement
The `semantic-diff` command existed, but two correctness issues blocked approval:
1. Valid git diffs with quoted file paths containing spaces were parsed incorrectly.
2. Explanation deduplication did not match the declared semantic signature contract.

## Desired Behavior
Preserve the v1 command contract while closing the approval blockers:
1. Parse changed-file metadata correctly for quoted and spaced file paths from git diff headers and patch lines.
2. Normalize and deduplicate explanations by the declared semantic signature: `category|impact|summary|evidence`.
3. Keep deterministic ordering and replay behavior unchanged.

## Scope
In scope:
1. Parser fixes for `diff --git`, `+++`, `---`, and rename metadata path extraction.
2. Signature normalization change so rationale text differences do not produce duplicate explanations.
3. Unit test coverage for these corrected behaviors.

Out of scope:
1. New impact categories or schema changes.
2. AST/runtime semantic proof.
3. Additional command flags.

## Acceptance Criteria
1. Diffs containing quoted paths with spaces are parsed into correct `changed_files` entries.
2. Explanations with identical `category`, `impact`, `summary`, and `evidence` are deduplicated even when rationale text differs.
3. `go test ./...` and `go vet ./...` pass.
4. Manual CLI checks pass:
   - Positive: `semantic-diff` with `--diff-file` and `--response-file` succeeds.
   - Negative: invalid `--ai` mode fails with non-zero status.
