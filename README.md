# Canon

Canon is a Go CLI for turning specs into a canonical expected application state.

## Current Scope
Phase 1 only:
1. Ingest specs into `.canon/` as source of truth.
2. Render effective expected state into `state/`.

`render` defaults to AI-assisted synthesis (`--ai auto`) and falls back to deterministic output if AI fails.

## Commands
- `go run ./cmd/canon init`
- `go run ./cmd/canon ingest <spec-file>`
- `go run ./cmd/canon import <spec-file>` (alias for ingest)
- `go run ./cmd/canon raw`
- `go run ./cmd/canon log`
- `go run ./cmd/canon show <spec-id>`
- `go run ./cmd/canon reset <spec-id>`
- `go run ./cmd/canon render`
- `go run ./cmd/canon status`
- `go run ./cmd/canon gc`
- `go run ./cmd/canon index`
- `go run ./cmd/canon check`
- `go run ./cmd/canon blame "<behavior description>"`
- `go run ./cmd/canon deps-risk`
- `go run ./cmd/canon schema-evolution`
- `go run ./cmd/canon semantic-diff`
- `go run ./cmd/canon version`
- `go run ./cmd/canon help`

## Command Reference

### init
`go run ./cmd/canon init [options]`

- `--root <path>` repository root (default: `.`)
- `--ai off|auto` AI bootstrap mode (default: `auto`)
- `--ai-provider codex|claude` override the configured provider
- `--response-file <path>` precomputed AI response JSON; when paired with the default `--ai auto`, Canon switches to replay mode
- `--no-interactive` accept generated specs without review
- `--accept-all` alias for `--no-interactive`
- `--max-specs <n>` maximum specs to generate (default: `10`)
- `--context-limit <kb>` max project context size in KB (default: `100`)
- `--include <glob>` additional include pattern (repeatable)
- `--exclude <glob>` additional exclude pattern (repeatable)

### ingest / import
`go run ./cmd/canon ingest <spec-file>`
`go run ./cmd/canon import <spec-file>`

- `--root <path>` repository root (default: `.`)
- `--file <path>` pass the source markdown file without using a positional argument
- `--title <title>` override the ingested spec title
- `--domain <name>` override the ingested spec domain
- `--type <name>` override the ingested spec type
- `--id <spec-id>` override the ingested spec id
- `--created <timestamp>` override the RFC3339 created timestamp
- `--depends-on <id1,id2,...>` override dependency ids
- `--touches <domain1,domain2,...>` override touched domains
- `--parents <id1,id2,...>` override parent spec ids
- `--ai-provider codex|claude` override the configured provider
- `--response-file <path>` use a precomputed conflict-check response

### raw
`go run ./cmd/canon raw`
`go run ./cmd/canon raw --text "<freeform text>"`

- `--root <path>` repository root (default: `.`)
- `--text <text>` provide the raw text directly; if omitted, Canon prompts for interactive input
- `--title <title>` override the ingested spec title
- `--domain <name>` override the ingested spec domain
- `--type <name>` override the ingested spec type
- `--id <spec-id>` override the ingested spec id
- `--created <timestamp>` override the RFC3339 created timestamp
- `--depends-on <id1,id2,...>` override dependency ids
- `--touches <domain1,domain2,...>` override touched domains
- `--parents <id1,id2,...>` override parent spec ids
- `--ai-provider codex|claude` override the configured provider
- `--response-file <path>` use a precomputed conflict-check response

### log
`go run ./cmd/canon log [options]`

- `--root <path>` repository root (default: `.`)
- `--graph` render the dependency graph view from `depends_on`
- `--oneline` render compact one-line rows
- `--all` include all disconnected heads (default: `true`; use `--all=false` to scope to the primary head)
- `-n <count>` max rows (default: `50`)
- `--grep <text>` case-insensitive title filter
- `--domain <name>` exact domain filter
- `--type <name>` exact type filter
- `--color auto|always|never` ANSI color output (default: `auto`)
- `--date absolute|relative` timestamp display mode (default: `relative`)
- `--show-tags` include qualified `[type/domain]` tags

### show
`go run ./cmd/canon show <spec-id>`

- `--root <path>` repository root (default: `.`)

### reset
`go run ./cmd/canon reset <spec-id>`

- `--root <path>` repository root (default: `.`)

### render
`go run ./cmd/canon render [options]`

- `--root <path>` repository root (default: `.`)
- `--write` write generated artifacts instead of a dry run
- `--ai off|auto|from-response` AI render mode (default: `auto`)
- `--ai-provider codex|claude` override the configured provider
- `--response-file <path>` precomputed AI render response; when paired with the default `--ai auto`, Canon switches to replay mode

### status
`go run ./cmd/canon status`

- `--root <path>` repository root (default: `.`)

### gc
`go run ./cmd/canon gc [options]`

- `--root <path>` repository root (default: `.`)
- `--domain <name>` consolidate all specs in one domain
- `--specs <id1,id2,...>` consolidate specific specs by id
- `--write` execute consolidation (default is dry run)
- `--min-specs <n>` minimum specs before consolidation runs (default: `5`)
- `--force` allow consolidation below the minimum count
- `--ai-provider codex|claude` override the configured provider
- `--response-file <path>` use precomputed AI response JSON

### index
`go run ./cmd/canon index [options]`

- `--root <path>` repository root (default: `.`)
- `--write` write `.canon/index.yaml`; without it, Canon prints YAML to stdout

### check
`go run ./cmd/canon check [options]`

- `--root <path>` repository root (default: `.`)
- `--domain <name>` restrict conflict scans to one domain
- `--spec <id>` check one spec against the remaining in-scope specs
- `--ai auto|from-response` AI check mode (default: `auto`)
- `--ai-provider codex|claude` AI provider override
- `--response-file <path>` JSON response file for replay mode
- `--json` emit machine-readable JSON
- `--write` persist conflict reports under `.canon/conflict-reports/`

### blame
`go run ./cmd/canon blame "<behavior description>"`

- `--root <path>` repository root (default: `.`)
- `--domain <name>` restrict blame to one domain
- `--json` machine-readable output
- `--ai-provider codex|claude` override the configured provider
- `--response-file <path>` use a precomputed AI response JSON

### deps-risk
`go run ./cmd/canon deps-risk [options]`

- `--root <path>` repository root containing `go.mod` (default: `.`)
- `--json` emit machine-readable JSON findings and summary
- `--fail-on <severity>` fail when the highest severity meets or exceeds `low`, `medium`, `high`, or `critical`

### schema-evolution
`go run ./cmd/canon schema-evolution [options]`

- `--root <path>` repository root containing SQL migration files (default: `.`)
- `--json` emit machine-readable JSON findings and summary
- `--fail-on <severity>` fail when the highest severity meets or exceeds `low`, `medium`, `high`, or `critical`

### semantic-diff
`go run ./cmd/canon semantic-diff [options]`

- `--root <path>` repository root used for `git diff` and config (default: `.`)
- `--diff-file <path>` read unified diff from file instead of `git diff`
- `--json` emit machine-readable JSON explanations and summary
- `--ai auto|from-response` AI mode (default: `auto`)
- `--ai-provider codex|claude` override the configured provider
- `--response-file <path>` deterministic replay input for `from-response` mode; when paired with the default `--ai auto`, Canon switches to replay mode

### version
`go run ./cmd/canon version`

- `--short` print the version string only
- `go run ./cmd/canon --version` and `go run ./cmd/canon -v` are aliases

### help
`go run ./cmd/canon help`

- `go run ./cmd/canon --help` and `go run ./cmd/canon -h` are aliases

Spec ID convention:
- Use 7-char SHA-like hex IDs (for example `a1b2c3d`) for consistency with git-style history views.
- Canon-generated fallback IDs also use the same 7-char SHA-like format.

Blame defaults:
- `canon blame "<text>"` uses the current directory as root
- Output defaults to human-readable terminal text
- The AI provider defaults from config (`./.canonconfig`, then `~/.canonconfig`, then built-in `codex`)

Examples:

```bash
go run ./cmd/canon log --graph --oneline --all -n 100
go run ./cmd/canon log --oneline --domain api --type feature --grep rate
go run ./cmd/canon log --graph --oneline --all --date relative --color always -n 100
go run ./cmd/canon blame "graph mode must use semantic dependencies from canonical specs"
go run ./cmd/canon deps-risk --root .
go run ./cmd/canon deps-risk --root . --fail-on medium
go run ./cmd/canon schema-evolution --root .
go run ./cmd/canon schema-evolution --root . --fail-on medium
go run ./cmd/canon semantic-diff --root .
go run ./cmd/canon semantic-diff --root . --diff-file fixtures/semantic.diff --response-file fixtures/semantic-response.json --json
```

## Interactive Raw Flow
Run:

```bash
go run ./cmd/canon raw
```

Then paste freeform text, and finish with:

```text
.done
```

Canon will AI synthesize a canonical spec and ingest it.

You can also pass text directly:

```bash
go run ./cmd/canon raw --text "voice note style requirement text"
```

## Config
Canon uses layered config like git.

Precedence:
1. built in defaults
2. global `~/.canonconfig`
3. local `./.canonconfig` (overrides global)

Example:

```ini
[ai]
provider = codex
```

Supported providers: `codex`, `claude`.

## AI Render Runtime
- Default render timeout: `10m`.
- Override timeout with `CANON_AI_RENDER_TIMEOUT_SECONDS=<seconds>`.
- Disable timeout entirely with `CANON_AI_RENDER_TIMEOUT_SECONDS=0`.
- When fallback is used, CLI prints `ai render fallback reason: ...`.

## Canon Layout
`canon init` creates:
- `.canon/specs/`
- `.canon/archive/specs/`
- `.canon/ledger/`
- `.canon/sources/`
- `.canon/archive/sources/`
- `.canon/conflict-reports/`
- `state/interactions/`

## Typical Local Run
```bash
go run ./cmd/canon init
go run ./cmd/canon ingest specs/canon-mvp.md
go run ./cmd/canon render --write
go run ./cmd/canon log
go run ./cmd/canon status
```
