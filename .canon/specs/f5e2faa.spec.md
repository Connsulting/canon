---
id: f5e2faa
type: technical
title: "Build Time Optimization"
domain: build-system
created: 2026-03-13T00:00:00Z
depends_on: [a1e9c10]
touched_domains: [build-system, ci, release]
---
# Build Time Optimization Draft Spec

## Problem Statement
The repository's only explicit build configuration is [`.github/workflows/release.yml`](/home/theconnman/git/connsulting/canon/.github/workflows/release.yml). It currently runs tag/version validation, `go test ./...`, and `go vet ./...` in a single serial verification job and relies on default `actions/setup-go` caching behavior, which is brittle for this repository because it has a `go.mod` file but no `go.sum`.

This makes release wall-clock time longer than necessary and risks losing Go build cache reuse in CI even though the release artifact matrix and CLI behavior are already stable.

## Scope
- In scope: optimize release/build wall-clock time in [`.github/workflows/release.yml`](/home/theconnman/git/connsulting/canon/.github/workflows/release.yml).
- In scope: add targeted regression coverage that protects the workflow optimization.
- In scope: validate the optimized workflow with unit tests, workflow linting, and positive/negative end-to-end build-flow checks.
- Out of scope: changing CLI behavior, changing emitted binaries, changing release asset naming, changing the existing OS/arch matrix, or refactoring Go packages for compile-time performance.

## Assumptions
- This task targets GitHub Actions workflow configuration rather than Go package internals because [`.github/workflows/release.yml`](/home/theconnman/git/connsulting/canon/.github/workflows/release.yml) is the repo's only explicit build configuration.
- The desired optimization is lower release wall-clock time without weakening release gates.
- The current repository intentionally has `go.mod` without `go.sum`, so workflow caching must key off `go.mod`.

## Current Bottlenecks
1. Tag/version validation, `go test ./...`, and `go vet ./...` execute serially inside one `verify` job.
2. The `build` matrix cannot start until the entire serial verification job completes.
3. `actions/setup-go` does not declare an explicit cache dependency path, so cache restoration is less reliable for a module that has no `go.sum`.

## Timing Evidence
Baseline on the pre-change tree:
- Cold `go test ./...`: 4.182s
- Cold `go vet ./...`: 0.611s
- Cold release builds: Linux 3.565s, Windows 3.336s, Darwin 6.927s
- Warm `go test ./...`: 0.295s
- Warm `go vet ./...`: 0.320s
- Warm release builds: Linux 0.110s, Windows 0.116s, Darwin 0.135s

Post-change rerun on the updated tree:
- Cold `go test ./...`: 3.910s
- Cold `go vet ./...`: 0.596s
- Cold release builds: Linux 2.945s, Windows 3.002s, Darwin 2.961s
- Warm `go test ./...`: 0.125s
- Warm `go vet ./...`: 0.120s
- Warm release builds: Linux 0.040s, Windows 0.056s, Darwin 0.039s

Expected release-gate impact:
- Pre-change cold verification gate was effectively `test + vet` = about 4.793s before the build matrix could start.
- Post-change cold verification gate is `max(test, vet)` = about 4.182s, because `test` and `vet` can run in parallel.
- That reduces the cold verification critical path by about 0.611s, roughly 13%, before any build-matrix work begins.

## Proposed Changes
1. Split serial verification work into separate `test` and `vet` jobs so GitHub Actions can run them in parallel after the shared checkout/setup phase.
2. Keep the release gate intact by making `build` depend on both verification jobs.
3. Preserve the existing tag/VERSION validation as its own gating job and keep the existing `go build` flags, asset names, and OS/arch matrix unchanged.
4. Configure `actions/setup-go` with `cache: true` and `cache-dependency-path: go.mod` everywhere the workflow sets up Go.
5. Add a lightweight Go regression test that reads the workflow file and asserts the expected cache configuration and job wiring remain present.

## Success Criteria
1. Release verification wall-clock time drops by allowing `go test ./...` and `go vet ./...` to run in parallel.
2. Go cache restoration is explicitly keyed from `go.mod`, making cache reuse reliable despite the absence of `go.sum`.
3. The build matrix still waits for all required release gates before artifacts are published.
4. Release artifact names, build flags, and target matrix remain unchanged.

## Validation Plan
1. Capture baseline timings for `go test ./...`, `go vet ./...`, and the three release `go build` commands with cold and warm Go caches.
2. Update the workflow and add regression coverage.
3. Run `gofmt -w` on changed Go files.
4. Run `go test ./...`.
5. Run `go vet ./...`.
6. Run `actionlint .github/workflows/release.yml`.
7. Compare post-change timings against the baseline and estimate the release verification wall-clock reduction from parallelizing test and vet.
8. Positive end-to-end check: execute the release-related build/test flow locally and confirm the expected release artifacts are produced.
9. Negative end-to-end check: run workflow validation against an intentionally invalid workflow variant and confirm it is rejected.

## Validation Results
- `go test ./...`: passed.
- `go vet ./...`: passed.
- `actionlint .github/workflows/release.yml`: passed.
- Positive end-to-end build-flow check: passed and produced `canon_v0.1.0_linux_amd64`, `canon_v0.1.0_windows_amd64.exe`, and `canon_v0.1.0_darwin_amd64`.
- Negative end-to-end workflow check: passed by confirming `actionlint` rejects a workflow variant where `build` needs a nonexistent `missing-job`.

## Risks And Tradeoffs
- Splitting verification into multiple jobs duplicates checkout/setup overhead, but the expected wall-clock gain from parallel test/vet execution should outweigh that cost.
- Workflow regression tests are string-structure based rather than full YAML semantic evaluation to avoid adding new dependencies.
- Local timing measurements are an approximation of CI behavior; the main runtime gain comes from parallel job scheduling rather than faster individual Go commands.

## Rollback Plan
If the workflow change causes CI instability, revert the workflow split and cache configuration changes, remove the regression test, and restore the previous single-job verification flow while investigating the failing CI behavior.
