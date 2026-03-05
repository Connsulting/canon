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
- `go run ./cmd/canon check`
- `go run ./cmd/canon show <spec-id>`
- `go run ./cmd/canon reset <spec-id>`
- `go run ./cmd/canon index`
- `go run ./cmd/canon render --write`
- `go run ./cmd/canon blame "<behavior description>"`
- `go run ./cmd/canon deps-risk`
- `go run ./cmd/canon status`
- `go run ./cmd/canon gc`
- `go run ./cmd/canon version` (aliases: `-v`, `--version`)

Spec ID convention:
- Use 7-char SHA-like hex IDs (for example `a1b2c3d`) for consistency with git-style history views.
- Canon-generated fallback IDs also use the same 7-char SHA-like format.

Init options:
- `--root <path>` repository root (default: `.`)
- `--ai off|auto` generation mode (default: `auto`)
- `--ai-provider codex|claude` provider override (default from config)
- `--response-file <path>` precomputed AI response JSON (implies `from-response` mode)
- `--no-interactive` accept all generated specs without prompting
- `--accept-all` alias for `--no-interactive`
- `--max-specs <n>` max generated specs (default: `10`)
- `--context-limit <kb>` max context size in KB (default: `100`)
- `--include <glob>` additional include glob (repeatable)
- `--exclude <glob>` additional exclude glob (repeatable)

Ingest / import options:
- positional `<spec-file>` or `--file <path>` (exactly one source path is required)
- `--root <path>` repository root (default: `.`)
- `--title <text>` override generated title
- `--domain <name>` override generated domain
- `--type <name>` override generated type
- `--id <spec-id>` override generated id
- `--created <rfc3339>` override created timestamp
- `--depends-on <id1,id2,...>` override dependency ids
- `--touches <domain1,domain2,...>` override touched domains
- `--parents <id1,id2,...>` force parent ids
- `--ai-provider codex|claude` provider override
- `--response-file <path>` JSON response file from headless AI run

Raw options:
- positional text or `--text <content>` (interactive prompt starts when omitted)
- `--root <path>` repository root (default: `.`)
- `--title <text>` override generated title
- `--domain <name>` override generated domain
- `--type <name>` override generated type
- `--id <spec-id>` override generated id
- `--created <rfc3339>` override created timestamp
- `--depends-on <id1,id2,...>` override dependency ids
- `--touches <domain1,domain2,...>` override touched domains
- `--parents <id1,id2,...>` force parent ids
- `--ai-provider codex|claude` provider override
- `--response-file <path>` JSON response file from headless AI run

Log options:
- `--root <path>` repository root (default: `.`)
- `--graph` render dependency graph view from `depends_on`
- `--oneline` compact one-line rows
- `--all` include all disconnected heads (default: `true`)
- `--show-tags` include `[type/domain]` tags in one-line/graph views
- `--grep <text>` case-insensitive title filter
- `--domain <name>` exact domain filter
- `--type <name>` exact type filter
- `--color auto|always|never` ANSI color output (default: `auto`)
- `--date absolute|relative` timestamp display mode (default: `relative`)
- `-n <count>` max rows (default: `50`)

Show options:
- `--root <path>` repository root (default: `.`)
- requires positional `<spec-id>`

Reset options:
- `--root <path>` repository root (default: `.`)
- requires positional `<spec-id>`

Index options:
- `--root <path>` repository root (default: `.`)
- `--write` write output to `.canon/index.yaml` (default prints YAML to stdout)

Render options:
- `--root <path>` repository root (default: `.`)
- `--write` write rendered artifacts (default is dry run)
- `--ai off|auto|from-response` (default: `auto`)
- `--ai-provider codex|claude` (default from config)
- `--response-file <path>` (required for `from-response`; implied when provided with `auto`)

Blame options:
- `--root <path>` repository root (default: `.`)
- `--domain <name>` restrict blame to one domain
- `--json` machine readable output
- `--ai-provider codex|claude` override configured provider
- `--response-file <path>` use precomputed AI response JSON

Blame defaults:
- `canon blame "<text>"` uses current directory as root
- output defaults to human readable terminal text
- AI provider defaults from config (`./.canonconfig`, then `~/.canonconfig`, then built in `codex`)

Check options:
- `--root <path>` repository root (default: `.`)
- `--domain <name>` restrict conflict scan to matching spec domains
- `--spec <id>` check one spec against the remaining in-scope specs
- `--ai auto|from-response` AI check mode (default: `auto`)
- `--ai-provider codex|claude` AI provider override
- `--response-file <path>` JSON response file for `from-response` mode
- `--json` emit machine-readable JSON
- `--write` persist conflict reports under `.canon/conflict-reports/`

Dependency risk options:
- `--root <path>` repository root containing `go.mod` (default: `.`)
- `--json` emit machine-readable JSON findings and summary
- `--fail-on <severity>` fail command when highest severity meets/exceeds threshold (`low`, `medium`, `high`, `critical`)

GC options:
- `--root <path>` repository root (default: `.`)
- `--domain <name>` consolidate all specs in one domain
- `--specs <id1,id2,...>` consolidate specific specs by id
- `--write` execute consolidation (default is dry run)
- `--min-specs <n>` minimum specs before consolidation runs (default: `5`)
- `--force` allow consolidation below the minimum count
- `--ai-provider codex|claude` override configured provider
- `--response-file <path>` use precomputed AI JSON response

Status options:
- `--root <path>` repository root (default: `.`)

Version options:
- `--short` print version only (for example `v0.1.0`)
- aliases: `canon -v`, `canon --version`

Examples:

```bash
go run ./cmd/canon log --graph --oneline --all -n 100
go run ./cmd/canon log --oneline --domain api --type feature --grep rate
go run ./cmd/canon log --graph --oneline --all --date relative --color always -n 100
go run ./cmd/canon index --write
go run ./cmd/canon blame "graph mode must use semantic dependencies from canonical specs"
go run ./cmd/canon deps-risk --root .
go run ./cmd/canon deps-risk --root . --fail-on medium
go run ./cmd/canon version --short
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
