---
id: repository-health-layout-repair
type: feature
title: Repository Health and Layout Repair
domain: canon-cli
status: draft
created: 2026-04-14T22:45:00Z
depends_on: []
touched_domains: [canon-cli, layout, status, init]
---
# Repository Health and Layout Repair

## Problem

`canon status` currently treats every missing required layout directory as the
same error. A repository with intact canonical artifacts but missing support
folders is repairable, but users only see a generic missing-directory failure.

## Desired Behavior

`canon status` classifies repository layout before loading specs and ledger:

- `healthy`: all required directories exist and are directories.
- `repairable`: core artifact directories exist, but one or more support
  directories are missing.
- `invalid`: a core artifact directory is missing, any required path is not a
  directory, a required path is inaccessible, or spec/ledger loading fails after
  layout classification.

`canon init --ai off` remains the idempotent repair path. It creates missing
support directories without rewriting, deleting, or moving existing canonical
artifacts.

## Success Criteria

- Layout path definitions remain centralized in `internal/canon/layout.go`.
- `internal/canon` exposes typed layout health and problem details.
- `canon status` reports the layout state and gives an actionable repair command
  for repairable repositories.
- Invalid layouts fail clearly.
- Unit and command tests cover healthy, repairable, invalid, and non-destructive
  repair behavior.
