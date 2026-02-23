---
id: b3f2d9c
type: feature
title: Canon Log Graph Readability Alignment
created: 2026-02-23T14:30:00Z
domain: canon-cli
depends_on: []
touched_domains: [canon-cli]
---

This spec aligns one line graph output from `canon log --graph` with the visual density of git styled history lines.

Goals
The graph should keep all existing data fields but adjust only formatting.

Requirements
The row for branch merge and split events should use tighter `|`, `/`, and `\` strokes without extra horizontal spacing noise.
The graph output should keep one line row shape and remain unbroken by explicit edge rows that show unrelated connectors.
The default one line summary fields should stay unchanged so existing scripts and output parsers remain stable.
