# PII Exposure Scanner (Draft)

## Problem
Repositories often contain inadvertent PII and secret exposures in source code, fixtures, config files, logs, and tracked data dumps. Canon currently lacks an offline deterministic scanner to surface these findings with stable output suitable for local audits and CI gates.

## Scope
Add a deterministic offline CLI command:

`canon pii-scan`

The scanner must walk repository text files and report potential exposure findings with line-aware metadata and summary counts.

### Success Criteria
- Supports categories:
  - `hardcoded-pii`
  - `pii-in-logs`
  - `env-secret`
  - `unencrypted-storage`
  - `gitignore-gap`
- Each finding includes:
  - `file`
  - `line`
  - `category`
  - `severity`
  - `detail`
  - `recommendation`
- Deterministic ordering and deterministic summary output.
- JSON and human-readable output modes.
- Supports `--fail-on low|medium|high|critical` severity gating.
- Excludes vendored and third-party code from content scanning.

## Non-Goals
- No auto-remediation.
- No network calls or external service lookups.
- No deep semantic NLP extraction; this is heuristic static scanning.
- No automatic file rewrites.

## Rule Matrix

### 1) Hardcoded PII patterns (`hardcoded-pii`)
Scan text and structured files for literals matching:
- Email addresses.
- Phone-number-like literals.
- SSN format (`NNN-NN-NNNN`).
- Credit-card-like digit spans (13-19 digits, allowing separators).
- IPv4 literals with identifier context.
- Address-like strings (street number + street suffix tokens).
- DOB-like values (key names suggest DOB/birthdate + date literal).
- Passport/driver-license identifier patterns in keyed context.
- Full person names in structured data (`json/yaml/yml/csv/sql`) when key/context suggests identity fields.

Severity guidance:
- `high`: SSN, credit-card-like values, DOB keyed values, passport/license IDs.
- `medium`: emails, phones, addresses, name fields in fixtures/seed data, IP identifiers.
- `low`: weak-confidence literal hints.
- Realistic-looking PII in test fixtures should be reported as `medium`.

### 2) PII in logs and error messages (`pii-in-logs`)
Detect logging/error calls that interpolate likely PII variables/fields without redaction, including:
- `fmt.Sprintf`, `fmt.Errorf`
- `log.Printf`
- `slog` calls
- `zerolog`, `zap`, `logrus`
- Similar printf/error wrapping patterns

Heuristics:
- Trigger when argument names/field names match PII terms (`email`, `phone`, `name`, `address`, `ssn`, `token`, etc.).
- Suppress if nearby format/message indicates redaction/masking (`redact`, `masked`, `hash`, `***`).

Severity guidance:
- `high`: SSN/credit card/token/password-related interpolation.
- `medium`: email/name/phone/address interpolation.
- `low`: ambiguous identity field logging.

### 3) Env and secret files (`env-secret`)
Detect plaintext credentials/secrets in:
- `.env` and `.env.*`
- `config.yaml`, `config.yml`, `config.json`
- `docker-compose*.yml|yaml` environment blocks
- CI workflows under `.github/workflows/`

Signal:
- Key names suggest credentials (`password`, `secret`, `token`, `api_key`, `private_key`, etc.) with non-placeholder values.

Severity guidance:
- `critical`: private keys, credentials with high likelihood of real secret.
- `high`: API keys/tokens/password values not obviously placeholders.
- `medium`: ambiguous secret-like entries.

### 4) Unencrypted storage (`unencrypted-storage`)
Detect schema/struct/write patterns that indicate storing sensitive fields in plaintext:
- Struct/column names for `password`, `ssn`, `credit_card`, `token`, etc.
- SQL DDL/seed fields storing sensitive values in raw columns.
- File writes (`WriteFile`, insert/update statements) with sensitive raw payload names.

Suppress when line suggests hashing/encryption/masking already present (`bcrypt`, `argon`, `hash`, `encrypt`, `cipher`, `kms`, `masked`).

Severity guidance:
- `critical`: plaintext passwords or direct credential persistence.
- `high`: SSN/credit card/token plaintext persistence.
- `medium`: other likely PII persistence without protection evidence.

### 5) Gitignore gaps (`gitignore-gap`)
Check `.gitignore` coverage for sensitive patterns:
- `.env*`
- `*.pem`
- `*.key`
- `credentials.*`
- `secrets.*`
- common dumps: `*.sql`, `*.csv`, `*.xlsx`

Also detect currently tracked files matching sensitive signatures.

Severity guidance:
- `high`: tracked sensitive files or missing `.env*`/key ignores.
- `medium`: missing dump-file ignore patterns.
- `low`: other recommended ignore hardening.

## Determinism and Exclusions
- Sort findings by: severity desc, category asc, file asc, line asc, detail asc.
- Normalize all paths to repo-relative slash paths.
- Exclude content scanning in: `vendor/`, `node_modules/`, `.git/`, `.canon/`, `state/`, known build output dirs.
- Skip binary files and symlinks.

## CLI Contract
Command: `canon pii-scan [--root .] [--json] [--fail-on <severity>]`

- `--root`: repository root.
- `--json`: machine JSON output.
- `--fail-on`: fail with non-zero exit when highest severity meets/exceeds threshold (`low|medium|high|critical`).

Output includes findings and summary totals by category and severity.

## Testing
- Unit tests must cover:
  - Category detection examples for all 5 categories.
  - False-positive controls and redaction suppression.
  - Deterministic ordering.
  - Summary aggregation by category and severity.
  - Vendor/third-party exclusion.
  - Threshold evaluation.
- CLI tests must cover:
  - JSON output shape.
  - Invalid `--fail-on` rejection.
  - Positional argument rejection.
  - Threshold failure behavior.

## E2E Manual Checks
- Positive: `go run ./cmd/canon pii-scan --root . --json`
- Negative: `go run ./cmd/canon pii-scan --fail-on urgent`

Both must pass expected behavior before sign-off.
