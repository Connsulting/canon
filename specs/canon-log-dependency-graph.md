---
id: e2cc388
type: feature
title: Canon Log Dependency Graph and Git-Style Filters
domain: canon-cli
created: 2026-02-20T00:00:00Z
depends_on: [b98ed16]
touched_domains: [canon-cli]
---
## Problem
`canon log` currently prints a linear ledger view with minimal filtering and no dependency visualization.
Users want a Git-like history experience with graph-style traversal and compact scanning.

## Requested Behavior
- Log output should feel closer to `git hlog` for day-to-day use.
- Users should be able to view relative timestamps (`6 weeks ago`) instead of only absolute RFC3339 values.
- Auto-generated spec IDs should be SHA-like so IDs look and behave more like commit identifiers.

## Requirements
- Keep current `canon log` default output backward-compatible.
- Add optional Git-inspired flags: `--graph`, `--oneline`, `--all`, `--grep`, `--domain`, `--type`, `--color`, and existing `-n`.
- Add optional date formatting flag `--date absolute|relative` so log views can mirror git-style relative time.
- Graph mode must use semantic dependencies (`depends_on`) from canonical specs.
- Graph rendering should emulate `git log --graph` branch-column style on single commit lines rather than separate dependency edge lines.
- Graph rendering should include connector transition rows for branch split/merge topology (for example `|\`, `|/`) when shape changes.
- Default scope should be primary head closure; `--all` should include all disconnected heads.
- Ordering in graph mode must be newest-first while respecting dependency topology.
- Missing dependencies must remain visible as placeholder nodes, not silently dropped.
- Ingest must merge ledger parent lineage into `depends_on` so commit-style reachability is preserved even when explicit dependencies are omitted.
- Log output should support ANSI color with `--color auto|always|never`.
- Auto-generated spec IDs should use short SHA-like hexadecimal IDs for consistency with commit-style identity.

## Non-Goals
- Full parity with every `git log` flag.
- Rendering branch/ref semantics beyond Canon head concept.

## Validation
- CLI tests for flag behavior and backward compatibility.
- Unit and integration tests for graph ordering, filtering, `--all`, missing dependencies, and cycle safety.
- Add synthetic branch/merge test coverage to validate fork and merge graph topology presentation.
