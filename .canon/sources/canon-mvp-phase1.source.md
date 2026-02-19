# Canon MVP

## Summary
Canon is a CLI for turning human specs into a canonical expected application state.

Current MVP focus is Phase 1 only.
1. Ingest specs into a canonical source of truth.
2. Render a deterministic expected state from those canonical specs.

Phase 2 (state to full app code generation) is intentionally out of scope for this MVP.

## Principles
- `.canon/` is the source of truth.
- `state/` is a deterministic build artifact from `.canon/specs/`.
- Ingest is AI driven by default for metadata extraction and conflict adjudication.
- File ingest preserves source content as the primary body.
- Ordering is explicit in an append only canonical ledger.

## CLI Commands (MVP)

### `canon init`
Creates required repository layout:
- `.canon/specs/`
- `.canon/ledger/`
- `.canon/sources/`
- `.canon/conflict-reports/`
- `state/interactions/`

### `canon ingest <file>`
Primary lock in operation for authored specs.

Behavior:
- Reads input file.
- Runs headless AI ingest end to end.
- AI infers metadata (title, domain, type, etc) when missing.
- AI performs semantic conflict detection against active canonical specs.
- Writes canonical spec to `.canon/specs/<spec-id>.spec.md`.
- Writes original source snapshot to `.canon/sources/<spec-id>.source.md`.
- Appends ledger entry to `.canon/ledger/<timestamp>-<spec-id>.json`.

Notes:
- `canon import <file>` is an alias for ingest.
- If AI reports conflicts, ingest fails and writes a conflict report.
- No prompt only workflow exists in MVP.

### `canon raw`
Interactive freeform intake for rough notes or voice style text.

Usage:
- Run `canon raw`
- Paste or type freeform text
- End with a line containing only `.done`

Alternative:
- `canon raw --text "<freeform note>"`

Behavior:
- AI synthesizes a canonical spec from freeform input.
- AI infers metadata and conflict status.
- Result is locked in the same way as `ingest`.

### `canon log`
Shows canonical ledger newest first.

Each entry includes:
- Spec ID
- Human readable title
- Type and domain
- Ingest date
- Parent spec references
- Content hash
- Canonical spec path
- Source snapshot path

### `canon show <spec-id>`
Displays canonical spec markdown from `.canon/specs/`.

### `canon index`
Builds deterministic index from canonical specs.

### `canon render --write`
Materializes expected state into `state/` from canonical specs.

### `canon status`
Shows repository summary metrics (spec counts, domains, edges, ledger state).

## Canonical Data Model

### Canonical Spec (`.canon/specs/*.spec.md`)
Frontmatter:
- `id`
- `type` (`feature | technical | resolution`)
- `title`
- `domain`
- `status` (`active | superseded | withdrawn`)
- `created` (RFC3339)
- `depends_on`
- `touched_domains`

Body:
- For file ingest: source body is preserved, with optional `AI Enhancements` section.
- For raw ingest: AI generated canonical body.

### Source Snapshot (`.canon/sources/*.source.md`)
Exact source input for auditability and post ingest editing workflows.

### Ledger Entry (`.canon/ledger/*.json`)
Single JSON object per file with:
- `spec_id`
- `title`
- `type`
- `domain`
- `parents`
- `sequence`
- `ingested_at`
- `content_hash`
- `spec_path`
- `source_path`

Ordering:
- Canon log reads all ledger entries and sorts newest first using sequence and ingest time.
- Ledger is append only, similar to commit history.

## Conflict Model
- Conflict detection is AI adjudicated during ingest.
- If conflict exists, write `.canon/conflict-reports/*.yaml` and fail ingest.
- State remains unchanged when conflicts are detected.

## Scalability Strategy

### Storage and Merge Strategy
- One spec per file in `.canon/specs/` reduces merge contention.
- One ledger entry per file in `.canon/ledger/` avoids giant shared manifests.
- State is always regenerable from canonical specs.

### Branching Model
- Multiple branches can ingest concurrently.
- Merge conflicts should mostly occur in generated artifacts, not in canonical sources.
- Canonical reconciliation path is: merge branch specs, rerun ingest if needed, rerun render.

### Deterministic Outputs
- Canonical formatting is normalized.
- Render output is deterministic from current canonical set.
- Re-running render without spec changes should produce no diff.

## Configuration
Canon supports git style layered config for AI provider selection.

Load order:
1. Built in defaults
2. Global: `~/.canonconfig`
3. Local repo: `./.canonconfig` (overrides global)

Config file format:

```ini
[ai]
provider = codex
```

Supported providers:
- `codex`
- `claude`

## End to End Workflow (Current MVP)
1. `canon init`
2. `canon ingest specs/canon-mvp.md`
3. `canon render --write`
4. `canon log`
5. `canon show <new-spec-id>`

This validates lock in plus deterministic render from canonical source of truth.
