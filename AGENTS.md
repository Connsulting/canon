# Agent Workflow

Use this workflow for all new feature work in this repository.

## 1) Define the feature
- Clarify the problem, desired behavior, and success criteria.
- Confirm the scope before implementation starts.

## 2) Plan the feature
- Discuss and agree on an implementation plan.
- Break work into concrete steps and identify risks/tradeoffs.

## 3) Write a draft spec file
- Create a spec document for the feature (typically under `specs/`).
- Treat this as a working draft used for discussion and iteration.

## 4) Implement the feature
- Build the feature according to the agreed plan and spec.
- Iterate based on feedback until behavior matches expectations.

## 5) Include unit tests
- Add or update unit tests as part of implementation.
- Unit tests are required before feature sign-off.

## 6) Run end-to-end CLI tests
- Run end-to-end tests using real command-line flows.
- Verify behavior with actual CLI commands, not only unit tests.

## 7) Ingest and commit
- After final approval, ingest the spec into Canon artifacts (under `.canon/`) and update project state as needed.
- Do not commit the draft spec document itself (for example files in `specs/` used only for drafting).
- Commit the ingested Canon artifacts (the doc Canon output in `.canon/`) and related code/test changes.

## AI First Semantic Checks
- Treat semantic contradiction detection as an AI first capability.
- Prefer AI adjudication for `ingest`, `check`, and related semantic validation commands.
- Rule based semantic checks may exist only as legacy compatibility helpers, not as the primary decision path.
