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
- `go run ./cmd/canon render --write`
- `go run ./cmd/canon status`

Spec ID convention:
- Use 7-char SHA-like hex IDs (for example `a1b2c3d`) for consistency with git-style history views.
- Canon-generated fallback IDs also use the same 7-char SHA-like format.

Render options:
- `--ai off|auto|from-response` (default: `auto`)
- `--ai-provider codex|claude` (default from config)
- `--response-file <path>` (required for `from-response`; implied when provided with `auto`)

Log options:
- `--graph` render dependency graph view from `depends_on`
- `--oneline` compact one-line rows
- `--all` include all disconnected heads (default scopes to primary head)
- `--grep <text>` case-insensitive title filter
- `--domain <name>` exact domain filter
- `--type <name>` exact type filter
- `--color auto|always|never` ANSI color output (default: `auto`)
- `--date absolute|relative` timestamp display mode (default: `absolute`)
- `-n <count>` max rows (defaults to 50)

Examples:

```bash
go run ./cmd/canon log --graph --oneline --all -n 100
go run ./cmd/canon log --oneline --domain api --type feature --grep rate
go run ./cmd/canon log --graph --oneline --all --date relative --color always -n 100
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
- `.canon/ledger/`
- `.canon/sources/`
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
