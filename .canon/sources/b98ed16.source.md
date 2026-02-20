# AI Render Effective State

## Problem
As specs accumulate over time, many requirements overlap or are superseded by later decisions.
The rendered state must represent current effective product intent, not a raw union of all historical prose.

## Requirements
- Render must support AI-assisted synthesis so cross-cutting and overlapping specs can be compressed.
- Render must regenerate from `.canon/` even if `state/` is missing.
- Render output does not need exact byte-for-byte determinism, but structure and coverage must remain stable.
- Defaults must be sane so running `canon render` works without heavy flag usage.
- Conflict resolution remains in ingest; render assumes canonical specs are already conflict-resolved.

## Compression Semantics
- Many historical specs may map to the same concept.
- Later intent and explicit resolution specs should override earlier overlapping content.
- Rendered state should be substantially smaller than the sum of all source specs when overlap is high.
- Superseded details should be retained in provenance, not emitted as active state prose.

## Validation
- Add end-to-end tests that ingest overlapping specs and confirm render produces compressed effective state.
- Add tests for rebuilding from scratch when `state/` is absent.
- Add tests for AI render integration paths.
