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
- For each implemented behavior, run at least one positive end-to-end command and one negative end-to-end command in this repo.
- Positive command: demonstrates the behavior you just implemented.
- Negative command: demonstrates a related behavior that should fail or be rejected.
- Example: for `git blame`, one run confirms expected output for a supported command and one run confirms expected failure for unsupported behavior.
- If any of these manual checks fail, fix the implementation and rerun until they pass.
- Report completion only after unit tests, linting, and both positive and negative manual end-to-end checks pass.

## 7) Ingest and commit
- After final approval, ingest the spec into Canon artifacts (under `.canon/`) and update project state as needed.
- Do not commit the draft spec document itself (for example files in `specs/` used only for drafting).
- Commit the ingested Canon artifacts (the doc Canon output in `.canon/`) and related code/test changes.
