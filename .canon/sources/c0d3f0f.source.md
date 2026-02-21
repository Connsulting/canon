# Canon GC

## Problem

Canon's ledger is append-only. Specs are immutable once ingested. This is correct by design — it preserves full provenance and audit history. But over time, a domain accumulates dozens of specs that overlap, partially supersede each other, or refine the same concepts through iteration.

Consider a real scenario: a team iterates on their API design over six months. They now have 23 specs touching the `api` domain. Some are original features, some are refinements, some are resolutions to conflicts between earlier specs. The rendered state handles this through AI compression, but the spec corpus itself becomes unwieldy. Reading the specs directly means piecing together intent across 23 files. Onboarding a new team member means they have to understand the full history just to know what the current requirements are.

Git has `gc` for object compaction and `squash` for commit consolidation. Canon needs the equivalent: a way to consolidate a set of specs into fewer, clean, authoritative specs that represent the current effective intent — while preserving the originals as archived history.

## Proposed Solution

Add a `canon gc` command that consolidates specs within a domain (or across specified specs) into a smaller set of clean, authoritative canonical specs. The originals are archived, not deleted — maintaining full provenance.

## Requirements

### Core Behavior

- `canon gc --domain <name>` must load all specs in the specified domain and consolidate them into one or more clean specs that capture the current effective intent.
- Consolidation must be AI-driven. The AI receives all specs in the domain and produces a minimal set of canonical specs that preserve all active requirements while eliminating redundancy, resolving superseded content, and removing stale intent.
- The original specs must be archived, not deleted. Archival means:
  - Original spec files are moved from `.canon/specs/` to `.canon/archive/specs/`.
  - Original source files are moved from `.canon/sources/` to `.canon/archive/sources/`.
  - Original ledger entries remain untouched in `.canon/ledger/` (the ledger is always append-only).
- New consolidated specs are written to `.canon/specs/` with fresh IDs and ledger entries.
- Each consolidated spec must include a `consolidates` field in its frontmatter listing the IDs of all original specs it replaces.
- The `depends_on` graph of consolidated specs must preserve any external dependencies (dependencies on specs outside the consolidation set).

### Dry Run

- Without `--write`, `canon gc` must print the proposed consolidation plan: which specs would be archived, what the consolidated specs would look like, and how many specs the domain would go from/to.
- With `--write`, execute the consolidation.

### Scope Control

- `--domain <name>` consolidates all specs in a domain. This is the primary use case.
- `--specs <id1,id2,...>` consolidates a specific set of specs (regardless of domain). Useful for targeted cleanup.
- At least one of `--domain` or `--specs` is required. `canon gc` without a scope target is an error.
- `--min-specs <n>` sets the minimum number of specs in a domain before gc considers it worth consolidating (default: 5). If the domain has fewer specs than this threshold, gc prints a message and exits cleanly. Overridable with `--force`.

### AI Integration

- Consolidation must use the configured AI provider (from `.canonconfig`).
- The AI prompt must instruct the model to:
  - Preserve all active, non-superseded requirements.
  - Resolve contradictions by honoring the most recent spec's intent (using `created` timestamps).
  - Eliminate redundant or restated requirements.
  - Produce well-structured specs with clear sections.
  - Retain domain and type metadata.
- Support `--ai-provider <name>` override, consistent with `ingest` and `render`.
- Support `--response-file <path>` for offline/pre-computed AI responses, consistent with existing AI workflows.

### Archive Structure

```
.canon/
├── archive/
│   ├── specs/       # Archived original spec files
│   └── sources/     # Archived original source files
├── specs/           # Active canonical specs (post-gc, consolidated)
├── ledger/          # Untouched — append-only history preserved
├── sources/         # Active source files
└── conflict-reports/
```

- `canon init` must create `.canon/archive/specs/` and `.canon/archive/sources/`.
- Archived files keep their original filenames.

### Ledger Entries

- Each consolidated spec gets a new ledger entry.
- The ledger entry's `parents` field must reference the most recent head(s) at the time of consolidation, maintaining DAG continuity.
- A new ledger entry type or annotation should indicate this was a gc operation (e.g., a `gc: true` field or a `type: consolidation` marker on the ledger entry).

### Safety

- `canon gc` must run `canon check` logic before consolidation. If there are unresolved semantic conflicts in the target set, gc must refuse and report them. Consolidation of conflicting specs without explicit resolution is not allowed.
- The `--force` flag overrides the min-specs threshold but does NOT override conflict safety.

## CLI Interface

```
canon gc [options]

Options:
  --root <path>           repository root (default: .)
  --domain <name>         consolidate all specs in this domain
  --specs <id1,id2,...>   consolidate specific specs by ID
  --write                 execute consolidation (default: dry run)
  --min-specs <n>         minimum specs to trigger gc (default: 5)
  --force                 override min-specs threshold
  --ai-provider <name>    AI provider override
  --response-file <path>  pre-computed AI response
```

## Example Output

### Dry run
```
gc plan for domain: api
  specs to consolidate: 14
  estimated result: 3 consolidated specs
  external dependencies preserved: [e1bcd34, b98ed16]

  consolidated spec 1/3:
    title: "API Authentication and Authorization"
    consolidates: [a1b2c3d, f4e5d6c, 7890abc]
    ...preview body...

  consolidated spec 2/3:
    title: "API Rate Limiting and Quotas"
    consolidates: [def1234, 5678ghi]
    ...preview body...

  consolidated spec 3/3:
    title: "API Endpoint Design and Versioning"
    consolidates: [jkl9012, mno3456, pqr7890, stu1234, vwx5678, yza9012]
    ...preview body...

dry run complete; use --write to execute
```

### Execution
```
gc complete for domain: api
  archived: 14 specs
  created: 3 consolidated specs
  reduction: 14 -> 3 specs (78%)
  run 'canon render --write' to update state
```

## Non-Goals

- Automatic scheduled gc. This is always user-initiated.
- Cross-domain consolidation in a single gc pass. Each `--domain` call handles one domain. Use `--specs` for targeted cross-domain work.
- Deleting ledger history. The ledger is always append-only. Archived specs are traceable through ledger entries forever.
- Consolidating specs that have unresolved conflicts. Fix conflicts first (`canon check`), then gc.

## Validation

- Unit tests for archive file movement (specs and sources move to `.canon/archive/`).
- Unit tests confirming ledger entries are never deleted or modified during gc.
- Unit tests for the `consolidates` field in consolidated spec frontmatter.
- Unit tests for external dependency preservation in consolidated specs.
- Unit tests for conflict-safety gate (gc refuses when conflicts exist in target set).
- Unit tests for `--min-specs` threshold behavior.
- E2E CLI test: dry run prints plan without side effects.
- E2E CLI test: gc with `--write` archives originals and creates consolidated specs.
- E2E CLI test: `canon log` still shows full history after gc (ledger intact).
- E2E CLI test: `canon render --write` after gc produces equivalent state output.
