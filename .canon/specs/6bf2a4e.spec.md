---
id: 6bf2a4e
type: feature
title: "PII Exposure Scanner"
domain: security
created: 2026-03-06T06:36:07Z
depends_on: [9ad4f11, e1bcd34]
touched_domains: [canon-cli, security]
---
# PII Exposure Scanner (Draft)

## Problem
Repositories can accidentally expose personally identifiable information (PII) and secrets across code, config, data fixtures, logs, and tracked sensitive files. We need deterministic offline checks that can run in CI and local CLI workflows.

## Goal
Add a deterministic offline command:

`canon pii-scan --root <path> [--json] [--fail-on <low|medium|high|critical>]`

The command scans repository text files and reports findings with this schema:
- `file`
- `line`
- `category`
- `severity`
- `detail`
- `recommendation`

## Categories
- `hardcoded-pii`
- `pii-in-logs`
- `env-secret`
- `unencrypted-storage`
- `gitignore-gap`

## Detection Scope
1. Hardcoded PII literals: email, phone, SSN, credit-card-like numbers, IP literals used as identifiers, address-like literals, DOB literals, passport/license-like IDs, and realistic full names in structured data files.
2. PII in logs/errors: string formatting or logging calls that include likely PII fields without redaction/masking hints.
3. Env/secret files: plaintext credentials/tokens in `.env*`, config files, docker compose env blocks, and CI workflow files.
4. Unencrypted storage: field/column/file write patterns that suggest plaintext persistence of PII/sensitive values.
5. Gitignore gaps: required sensitive patterns coverage and tracked sensitive files that should be ignored.

## Severity Rubric
- `critical`: plaintext secrets/keys with strong confidence; raw credential persistence patterns.
- `high`: clear PII exposure in runtime logs or hardcoded sensitive IDs in non-test files.
- `medium`: realistic-looking PII in fixtures/test data; likely-but-not-certain storage/log exposure.
- `low`: policy/config hygiene gaps such as missing ignore patterns.

Fixture rule: realistic-looking PII in `fixtures/` or `testdata/` is capped at `medium`.

## Determinism and Exclusions
- Offline only; no network access.
- Exclude vendored/third-party directories (`vendor`, `third_party`, `third-party`, `node_modules`, `.git`).
- Skip binary files and large files beyond scanner max size.
- Deterministic output ordering across runs.

## Output
- Human-readable list of findings + totals by category and severity.
- `--json` emits machine-readable result including findings, summary, and threshold metadata.
- `--fail-on` returns non-zero when highest finding severity meets/exceeds threshold.

## Non-goals
- No automatic remediation.
- No deep language AST or taint tracking.
- No external secret-management API checks.

## AI Enhancements

# PII Exposure Scanner
Repositories can accidentally expose personally identifiable information (PII) and secrets across code, config, data fixtures, logs, and tracked sensitive files. Canon needs deterministic offline checks that run in CI and local CLI workflows.
## Output and Exit Behavior
- Human-readable list of findings plus totals by category and severity.
- `--json` emits machine-readable output including findings, summary, and threshold metadata.
- `--fail-on` returns non-zero when highest finding severity meets or exceeds the threshold.
## Validation
- Add unit tests for detector heuristics, severity mapping, fixture severity cap, exclusions, and deterministic ordering.
- Add CLI JSON contract tests for summary/findings/threshold metadata.
- Run one positive end-to-end CLI command that produces expected findings.
- Run one negative end-to-end CLI command that demonstrates expected failure or rejection behavior (for example threshold-triggered non-zero exit or invalid flag input).
