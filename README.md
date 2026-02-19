# Canon

Canon is a Go CLI for turning specs into a canonical expected application state.

## Current Scope
Phase 1 only:
1. Ingest specs into `.canon/` as source of truth.
2. Render deterministic expected state into `state/`.

`render` is deterministic and does not call AI.

## Commands
- `go run ./cmd/canon init`
- `go run ./cmd/canon ingest <spec-file>`
- `go run ./cmd/canon import <spec-file>` (alias for ingest)
- `go run ./cmd/canon raw`
- `go run ./cmd/canon log`
- `go run ./cmd/canon show <spec-id>`
- `go run ./cmd/canon render --write`
- `go run ./cmd/canon status`

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
