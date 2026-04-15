---
id: b6e4c2a
type: feature
title: "Canon Blame Provenance and Gap JSON Contract"
domain: canon-cli
created: 2026-04-14T23:05:00Z
depends_on: [d72f3b8]
touched_domains: [canon-cli, ingest]
---
# Canon Blame Provenance and Gap JSON Contract

## Problem

Agents need `canon blame --json` to distinguish between specified behavior and specification gaps without guessing from prose output. The current blame result can omit gap details and does not attach citation line numbers resolved from canonical spec files.

## Desired Behavior

- Positive blame results must include spec ID, title, domain, created timestamp, explicit `high`, `medium`, or `low` confidence, and citation metadata.
- Citation metadata must include the nearest markdown section, start line, end line, and excerpt text derived from the canonical spec file.
- AI-returned spec IDs must be validated against the scoped corpus before they are emitted.
- If citation text cannot be resolved against the canonical spec file, the result must be emitted with an empty citation list rather than invented line numbers.
- Uncovered behavior must return stable JSON with `found: false`, `status: "unspecified"`, populated author-a-spec guidance, and `results: []`.
- Human output must show section and line citation details when available and keep author-a-spec guidance for gaps.

## Testability

- Unit tests cover markdown section and line citation extraction.
- Command tests cover positive `blame --json` provenance fields and unspecified gap JSON shape using `--response-file`.
- End-to-end CLI checks demonstrate both covered behavior and unspecified behavior routing.
