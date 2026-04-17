package canon

import (
	"strings"
	"testing"
)

func TestProductRequirementMetadataParseAndRender(t *testing.T) {
	text := `---
id: req-100
type: feature
title: Checkout Readiness
domain: checkout
created: 2026-04-14T10:00:00Z
depends_on: []
touched_domains: [checkout]
requirement_kind: product
source_issue: CON-48
approval_state: approved
---
## Problem statement
Cart checkout needs a ready intake path.`

	spec, err := parseSpecText(text, "req.md")
	if err != nil {
		t.Fatalf("parseSpecText failed: %v", err)
	}
	if spec.RequirementKind != "product" || spec.SourceIssue != "CON-48" || spec.ApprovalState != "approved" {
		t.Fatalf("metadata not parsed: %+v", spec)
	}

	rendered := canonicalSpecText(spec)
	for _, want := range []string{
		"requirement_kind: product",
		"source_issue: CON-48",
		"approval_state: approved",
		"## Problem statement",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected rendered spec to contain %q:\n%s", want, rendered)
		}
	}
}

func TestProductRequirementReadinessGaps(t *testing.T) {
	spec := Spec{
		ID:              "req-gap",
		Type:            "feature",
		Title:           "Gap",
		Domain:          "product",
		Created:         "2026-04-14T10:00:00Z",
		RequirementKind: "product",
		ApprovalState:   "draft",
		Body: `## Problem statement
Known problem.

## Proposed solution
Known solution.`,
	}

	gaps := ProductRequirementReadinessGaps(spec)
	got := formatReadinessGapMessages(gaps)
	for _, want := range []string{
		"source_issue is required",
		"approval_state must be approved",
		"missing required section: Success criteria",
		"missing required section: Scope boundaries",
		"missing required section: Testability notes",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected gap %q in:\n%s", want, got)
		}
	}
}

func TestIngestRejectsIncompleteProductRequirement(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	_, err := Ingest(root, IngestInput{
		Text: `---
id: req-incomplete
type: feature
title: Incomplete Requirement
domain: product
created: 2026-04-14T10:00:00Z
depends_on: []
touched_domains: [product]
requirement_kind: product
approval_state: draft
---
## Problem statement
Known problem.`,
	})
	if err == nil {
		t.Fatalf("expected incomplete product requirement to fail ingest")
	}
	if !strings.Contains(err.Error(), "product requirement readiness gaps") {
		t.Fatalf("expected readiness error, got %v", err)
	}
	if !strings.Contains(err.Error(), "source_issue is required") {
		t.Fatalf("expected source_issue gap, got %v", err)
	}
}

func TestIngestAcceptsCompleteProductRequirement(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	result, err := Ingest(root, IngestInput{
		Text: completeProductRequirementText("req-complete"),
	})
	if err != nil {
		t.Fatalf("complete product requirement ingest failed: %v", err)
	}

	spec, err := loadSpecByID(root, result.SpecID)
	if err != nil {
		t.Fatalf("loadSpecByID failed: %v", err)
	}
	if spec.RequirementKind != "product" || spec.SourceIssue != "CON-48" || spec.ApprovalState != "approved" {
		t.Fatalf("metadata not preserved: %+v", spec)
	}
	if gaps := ProductRequirementReadinessGaps(spec); len(gaps) != 0 {
		t.Fatalf("expected no readiness gaps, got %v", gaps)
	}
}

func completeProductRequirementText(id string) string {
	return `---
id: ` + id + `
type: feature
title: Complete Product Requirement
domain: product
created: 2026-04-14T10:00:00Z
depends_on: []
touched_domains: [product]
requirement_kind: product
source_issue: CON-48
approval_state: approved
---
## Problem statement
Users need a complete requirement format.

## Proposed solution
Accept product requirement metadata and required sections.

## Success criteria
Complete requirements ingest successfully.

## Scope boundaries
Canon only validates local metadata and shape.

## Testability notes
The CLI can ingest, show, log, and check the spec.`
}
