# Security Foot-Gun Finder

## Problem
Developers need a fast, deterministic way to catch high-signal security anti-patterns in Go code before merge. Today, Canon has no built-in command for source-level security foot-guns, so obvious issues can slip through local checks and CI.

## Scope (v1)
In scope:
- Add `canon security-footgun` command.
- Scan Go source files (`.go`) under a root path.
- Produce deterministic findings with severity, rule id, category, file, line, and message.
- Support text and JSON output.
- Support CI gating with `--fail-on`.

Out of scope:
- AI adjudication.
- Network lookups.
- Non-Go ecosystems.
- Deep taint/dataflow analysis.

## CLI Contract
Command:
- `canon security-footgun [--root <path>] [--json] [--fail-on <severity>]`

Flags:
- `--root <path>`: repository root to scan (default `.`).
- `--json`: emit machine-readable JSON.
- `--fail-on <severity>`: fail when highest finding severity meets/exceeds one of `low|medium|high|critical`.

Behavior:
- Reject positional arguments.
- Reject unsupported `--fail-on` values.
- Exit non-zero when threshold is exceeded.

## Rule Set (Deterministic Heuristics)
1. `tls-insecure-skip-verify` (high)
- Detect `InsecureSkipVerify: true` in struct literals.
- Detect assignments like `cfg.InsecureSkipVerify = true`.

2. `exec-shell-c` (high)
- Detect `os/exec` calls to `Command` or `CommandContext` that invoke shell execution modes (for example `sh -c`, `bash -c`, `cmd /c`, `powershell -Command`).

3. `weak-hash-import` (medium)
- Detect imports of `crypto/md5` and `crypto/sha1`.

4. `hardcoded-credential-literal` (critical)
- Detect string literals assigned to sensitive identifiers (for example names containing password, token, secret, api key).
- Ignore obvious placeholders where possible.

5. `insecure-rand-sensitive-context` (high)
- Detect `math/rand` usage in sensitive naming contexts (for example variables/functions/arguments that imply token, key, secret, password).

## Severity Model
Supported severities:
- `none`, `low`, `medium`, `high`, `critical`

Threshold semantics:
- A scan fails when `highest_severity >= fail_on`.

## Determinism Requirements
- Deterministic file traversal and finding ordering.
- Stable JSON field names.
- Identical findings/summary across repeated runs over unchanged input.

## Acceptance Criteria
- Command is discoverable in CLI usage and README docs.
- Unit tests cover rule detection, deterministic ordering, summary counts/highest severity, threshold semantics, empty scan behavior, and invalid severity parsing.
- CLI tests cover JSON shape, threshold failure behavior, invalid `--fail-on`, and positional argument rejection.
- Positive and negative manual CLI checks pass:
  - Positive: `go run ./cmd/canon security-footgun --root . --json`
  - Negative: `go run ./cmd/canon security-footgun --fail-on urgent`
