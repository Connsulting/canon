# Security Foot-Gun Finder (Draft)

## Problem Statement
The repository lacks a deterministic static analyzer focused on common security anti-patterns in Go source code. Teams need an offline CLI check that catches high-signal foot-guns early and can be used in CI with severity threshold gating.

## Scope (v1)
In scope:
- New CLI command: `canon security-footgun`
- Deterministic, offline analysis only (no network, no AI adjudication)
- Go source file scanning only (`.go` files)
- JSON and text output formats
- Severity threshold gating with `--fail-on`

Out of scope:
- AI-assisted semantic adjudication
- Non-Go language ecosystems
- Dynamic/runtime security testing

## CLI Contract
Command:
- `canon security-footgun [--root <path>] [--json] [--fail-on <severity>]`

Flags:
- `--root <path>` repository root to scan (default `.`)
- `--json` emit machine-readable JSON
- `--fail-on <severity>` fail command when highest finding severity meets/exceeds threshold

Accepted severity values:
- `none`, `low`, `medium`, `high`, `critical`

Behavior:
- Reject positional arguments.
- Reject invalid `--fail-on` values with deterministic error text.
- Return non-zero when threshold is exceeded.

## Rule Set (Go only)
The analyzer emits findings for these anti-patterns:
1. `InsecureSkipVerify: true`
- Rule ID: `tls-insecure-skip-verify`
- Severity: `high`
- Category: `transport-security`

2. `os/exec` shell invocation with `-c`/`/c`
- Rule ID: `exec-shell-c`
- Severity: `high`
- Category: `command-execution`
- Example patterns: `exec.Command("sh", "-c", ...)`, `exec.Command("cmd", "/c", ...)`

3. Weak hash imports (`crypto/md5`, `crypto/sha1`)
- Rule ID: `weak-hash-import`
- Severity: `medium`
- Category: `cryptography`

4. Hardcoded credential literals assigned to sensitive identifiers
- Rule ID: `hardcoded-credential-literal`
- Severity: `critical`
- Category: `secrets-management`

5. `math/rand` use in security-sensitive contexts
- Rule ID: `insecure-rand-sensitive-context`
- Severity: `high`
- Category: `cryptography`

## Result Model
Result includes:
- Root and files scanned
- Findings list (stable fields for CI assertions)
- Summary counts and highest severity
- Optional fail-on threshold + threshold exceeded boolean

Each finding includes stable machine-readable fields:
- `rule_id`, `category`, `severity`, `file`, `line`, `column`, `snippet`, `message`

## Determinism Guarantees
- Deterministic file collection and sorting.
- Deterministic finding sort order and tie-breakers.
- Stable summary aggregation independent of filesystem traversal order.

## Acceptance Criteria
- `canon security-footgun` command available and documented.
- Supports `--root`, `--json`, and `--fail-on`.
- Emits deterministic findings for defined rule set.
- Supports CI-style threshold failure semantics.
- Unit tests cover rule detection, ordering, summary, thresholds, empty scans, and invalid severity parsing.
- CLI tests cover JSON output shape, threshold non-zero behavior, invalid `--fail-on`, and positional-argument rejection.

## Manual CLI Validation
Positive command:
```bash
go run ./cmd/canon security-footgun --root . --json
```

Negative command:
```bash
go run ./cmd/canon security-footgun --fail-on urgent
```
