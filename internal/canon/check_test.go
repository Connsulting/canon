package canon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckDetectsConflictsFromAIResponse(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	writeCheckSpec(t, root, Spec{
		ID:             "spec-a1",
		Type:           "feature",
		Title:          "Auth Required",
		Domain:         "api",
		Created:        "2026-02-21T10:00:00Z",
		TouchedDomains: []string{"api"},
		Body:           "API endpoints must require authentication.",
	})
	writeCheckSpec(t, root, Spec{
		ID:             "spec-b2",
		Type:           "feature",
		Title:          "Auth Optional",
		Domain:         "api",
		Created:        "2026-02-21T10:01:00Z",
		TouchedDomains: []string{"api"},
		Body:           "API endpoints must not require authentication.",
	})

	responsePath := writeAICheckResponse(t, root, map[string]any{
		"model": "codex-headless",
		"conflict_check": map[string]any{
			"has_conflicts": true,
			"summary":       "Found contradictory API auth requirements.",
			"conflicts": []map[string]any{
				{
					"spec_a":        "spec-a1",
					"spec_b":        "spec-b2",
					"domain":        "api",
					"statement_key": "api auth required",
					"line_a":        "API endpoints must require authentication.",
					"line_b":        "API endpoints must not require authentication.",
					"reason":        "Direct contradiction on auth requirements.",
				},
				{
					"spec_a":        "spec-a1",
					"spec_b":        "spec-b2",
					"domain":        "api",
					"statement_key": "api auth required",
					"line_a":        "API endpoints must require authentication.",
					"line_b":        "API endpoints must not require authentication.",
					"reason":        "Direct contradiction on auth requirements.",
				},
			},
		},
	})

	result, err := Check(root, CheckOptions{
		AIMode:       "from-response",
		ResponseFile: responsePath,
	})
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if result.Passed {
		t.Fatalf("expected failed check result")
	}
	if result.TotalSpecs != 2 {
		t.Fatalf("expected total specs 2, got %d", result.TotalSpecs)
	}
	if result.TotalConflicts != 1 {
		t.Fatalf("expected deduped conflict count 1, got %d", result.TotalConflicts)
	}

	conflict := result.Conflicts[0]
	if conflict.SpecA != "spec-a1" || conflict.SpecB != "spec-b2" {
		t.Fatalf("unexpected conflict pair: %+v", conflict)
	}
	if conflict.Domain != "api" {
		t.Fatalf("expected api domain, got %s", conflict.Domain)
	}
	if !strings.Contains(conflict.LineA, "must require authentication") {
		t.Fatalf("unexpected line a: %s", conflict.LineA)
	}
	if !strings.Contains(conflict.LineB, "must not require authentication") {
		t.Fatalf("unexpected line b: %s", conflict.LineB)
	}
}

func TestCheckSupportsDomainAndSpecFilters(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	writeCheckSpec(t, root, Spec{
		ID:             "api-a",
		Type:           "feature",
		Title:          "API Auth Required",
		Domain:         "api",
		Created:        "2026-02-21T11:00:00Z",
		TouchedDomains: []string{"api"},
		Body:           "API endpoints must require authentication.",
	})
	writeCheckSpec(t, root, Spec{
		ID:             "api-b",
		Type:           "feature",
		Title:          "API Auth Optional",
		Domain:         "api",
		Created:        "2026-02-21T11:01:00Z",
		TouchedDomains: []string{"api"},
		Body:           "API endpoints must not require authentication.",
	})
	writeCheckSpec(t, root, Spec{
		ID:             "billing-a",
		Type:           "feature",
		Title:          "Billing Address Required",
		Domain:         "billing",
		Created:        "2026-02-21T11:02:00Z",
		TouchedDomains: []string{"billing"},
		Body:           "Invoices must include customer address.",
	})
	writeCheckSpec(t, root, Spec{
		ID:             "billing-b",
		Type:           "feature",
		Title:          "Billing Address Omitted",
		Domain:         "billing",
		Created:        "2026-02-21T11:03:00Z",
		TouchedDomains: []string{"billing"},
		Body:           "Invoices must not include customer address.",
	})

	apiResponsePath := writeAICheckResponse(t, root, map[string]any{
		"model": "codex-headless",
		"conflict_check": map[string]any{
			"has_conflicts": true,
			"summary":       "API conflict only.",
			"conflicts": []map[string]any{
				{
					"spec_a":        "api-a",
					"spec_b":        "api-b",
					"domain":        "api",
					"statement_key": "api auth required",
					"line_a":        "API endpoints must require authentication.",
					"line_b":        "API endpoints must not require authentication.",
					"reason":        "Direct contradiction.",
				},
			},
		},
	})

	apiResult, err := Check(root, CheckOptions{
		Domain:       "api",
		AIMode:       "from-response",
		ResponseFile: apiResponsePath,
	})
	if err != nil {
		t.Fatalf("Check api domain failed: %v", err)
	}
	if apiResult.TotalSpecs != 2 || apiResult.TotalConflicts != 1 {
		t.Fatalf("unexpected api result: %+v", apiResult)
	}

	specResponsePath := writeAICheckResponse(t, root, map[string]any{
		"model": "codex-headless",
		"conflict_check": map[string]any{
			"has_conflicts": true,
			"summary":       "Multiple conflicts detected.",
			"conflicts": []map[string]any{
				{
					"spec_a":        "billing-a",
					"spec_b":        "billing-b",
					"domain":        "billing",
					"statement_key": "billing address requirement",
					"line_a":        "Invoices must include customer address.",
					"line_b":        "Invoices must not include customer address.",
					"reason":        "Direct contradiction.",
				},
				{
					"spec_a":        "api-a",
					"spec_b":        "api-b",
					"domain":        "api",
					"statement_key": "api auth required",
					"line_a":        "API endpoints must require authentication.",
					"line_b":        "API endpoints must not require authentication.",
					"reason":        "Direct contradiction.",
				},
			},
		},
	})

	specResult, err := Check(root, CheckOptions{
		SpecID:       "billing-b",
		AIMode:       "from-response",
		ResponseFile: specResponsePath,
	})
	if err != nil {
		t.Fatalf("Check spec filter failed: %v", err)
	}
	if specResult.TotalSpecs != 4 || specResult.TotalConflicts != 1 {
		t.Fatalf("unexpected spec scoped result: %+v", specResult)
	}
	if specResult.Conflicts[0].SpecB != "billing-b" {
		t.Fatalf("expected billing-b as checked candidate")
	}

	_, err = Check(root, CheckOptions{
		SpecID:       "missing-spec",
		AIMode:       "from-response",
		ResponseFile: specResponsePath,
	})
	if err == nil {
		t.Fatalf("expected missing spec error")
	}
}

func TestCheckReportsProductRequirementReadinessGaps(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	writeCheckSpec(t, root, Spec{
		ID:              "req-gap",
		Type:            "feature",
		Title:           "Incomplete Product Requirement",
		Domain:          "product",
		Created:         "2026-04-14T10:00:00Z",
		RequirementKind: "product",
		ApprovalState:   "draft",
		TouchedDomains:  []string{"product"},
		Body: `## Problem statement
Known problem.`,
	})

	responsePath := writeAICheckResponse(t, root, map[string]any{
		"model": "codex-headless",
		"conflict_check": map[string]any{
			"has_conflicts": false,
			"summary":       "No conflicts.",
			"conflicts":     []map[string]any{},
		},
	})

	result, err := Check(root, CheckOptions{
		AIMode:       "from-response",
		ResponseFile: responsePath,
	})
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if result.Passed {
		t.Fatalf("expected readiness gaps to fail check")
	}
	if result.TotalConflicts != 0 {
		t.Fatalf("expected no conflicts, got %d", result.TotalConflicts)
	}
	if result.TotalReadinessGaps == 0 {
		t.Fatalf("expected readiness gaps")
	}
	got := formatReadinessGapMessages(result.ReadinessGaps)
	if !strings.Contains(got, "source_issue is required") || !strings.Contains(got, "approval_state must be approved") {
		t.Fatalf("missing expected readiness gaps:\n%s", got)
	}
}

func TestCheckSpecFilterIgnoresUnrelatedReadinessGaps(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	writeCheckSpec(t, root, Spec{
		ID:             "ok",
		Type:           "feature",
		Title:          "Ready Technical Spec",
		Domain:         "product",
		Created:        "2026-04-14T10:00:00Z",
		TouchedDomains: []string{"product"},
		Body:           "Technical helper specs do not need product readiness metadata.",
	})
	writeCheckSpec(t, root, Spec{
		ID:              "gap",
		Type:            "feature",
		Title:           "Incomplete Product Requirement",
		Domain:          "product",
		Created:         "2026-04-14T10:01:00Z",
		RequirementKind: "product",
		ApprovalState:   "draft",
		TouchedDomains:  []string{"product"},
		Body: `## Problem statement
Known problem.`,
	})

	responsePath := writeAICheckResponse(t, root, map[string]any{
		"model": "codex-headless",
		"conflict_check": map[string]any{
			"has_conflicts": false,
			"summary":       "No conflicts.",
			"conflicts":     []map[string]any{},
		},
	})

	targeted, err := Check(root, CheckOptions{
		SpecID:       "ok",
		AIMode:       "from-response",
		ResponseFile: responsePath,
	})
	if err != nil {
		t.Fatalf("Check spec filter failed: %v", err)
	}
	if !targeted.Passed || targeted.TotalReadinessGaps != 0 {
		t.Fatalf("expected targeted check to ignore unrelated readiness gaps, got %+v", targeted)
	}

	full, err := Check(root, CheckOptions{
		AIMode:       "from-response",
		ResponseFile: responsePath,
	})
	if err != nil {
		t.Fatalf("Check full scope failed: %v", err)
	}
	if full.Passed || full.TotalReadinessGaps == 0 {
		t.Fatalf("expected full check to report readiness gaps, got %+v", full)
	}
}

func TestCheckCandidateFileIgnoresUnrelatedExistingReadinessGaps(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	writeCheckSpec(t, root, Spec{
		ID:              "existing-gap",
		Type:            "feature",
		Title:           "Incomplete Existing Product Requirement",
		Domain:          "product",
		Created:         "2026-04-14T10:00:00Z",
		RequirementKind: "product",
		ApprovalState:   "draft",
		TouchedDomains:  []string{"product"},
		Body: `## Problem statement
Known problem.`,
	})
	candidatePath := writeCheckCandidateFile(t, root, Spec{
		ID:             "candidate-ready",
		Type:           "feature",
		Title:          "Ready Candidate",
		Domain:         "product",
		Created:        "2026-04-14T10:01:00Z",
		TouchedDomains: []string{"product"},
		Body:           "Technical candidate specs do not need product readiness metadata.",
	})
	responsePath := writeAICheckResponse(t, root, map[string]any{
		"model": "codex-headless",
		"conflict_check": map[string]any{
			"has_conflicts": false,
			"summary":       "No conflicts.",
			"conflicts":     []map[string]any{},
		},
	})

	result, err := Check(root, CheckOptions{
		CandidateFile: candidatePath,
		AIMode:        "from-response",
		ResponseFile:  responsePath,
	})
	if err != nil {
		t.Fatalf("Check candidate failed: %v", err)
	}
	if !result.Passed || result.TotalReadinessGaps != 0 {
		t.Fatalf("expected candidate check to ignore unrelated existing readiness gaps, got %+v", result)
	}
}

func TestCheckWriteCreatesConflictReports(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	writeCheckSpec(t, root, Spec{
		ID:             "write-a",
		Type:           "feature",
		Title:          "Audit Retention Required",
		Domain:         "auth",
		Created:        "2026-02-21T12:00:00Z",
		TouchedDomains: []string{"auth"},
		Body:           "Audit logs must retain user access history.",
	})
	writeCheckSpec(t, root, Spec{
		ID:             "write-b",
		Type:           "feature",
		Title:          "Audit Retention Forbidden",
		Domain:         "auth",
		Created:        "2026-02-21T12:01:00Z",
		TouchedDomains: []string{"auth"},
		Body:           "Audit logs must not retain user access history.",
	})

	responsePath := writeAICheckResponse(t, root, map[string]any{
		"model": "codex-headless",
		"conflict_check": map[string]any{
			"has_conflicts": true,
			"summary":       "Audit contradiction.",
			"conflicts": []map[string]any{
				{
					"spec_a":        "write-a",
					"spec_b":        "write-b",
					"domain":        "auth",
					"statement_key": "audit retention",
					"line_a":        "Audit logs must retain user access history.",
					"line_b":        "Audit logs must not retain user access history.",
					"reason":        "Direct contradiction.",
				},
			},
		},
	})

	result, err := Check(root, CheckOptions{
		Write:        true,
		AIMode:       "from-response",
		ResponseFile: responsePath,
	})
	if err != nil {
		t.Fatalf("Check write failed: %v", err)
	}
	if result.TotalConflicts != 1 {
		t.Fatalf("expected conflict, got %+v", result)
	}
	if len(result.ReportPaths) != 1 {
		t.Fatalf("expected one report path, got %d", len(result.ReportPaths))
	}
	if _, err := os.Stat(filepath.Join(root, result.ReportPaths[0])); err != nil {
		t.Fatalf("expected report file to exist: %v", err)
	}
}

func TestCheckCandidateFileDetectsAIConflictsWithoutWritingCandidate(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	writeCheckSpec(t, root, Spec{
		ID:             "api-auth-required",
		Type:           "feature",
		Title:          "API Auth Required",
		Domain:         "api",
		Created:        "2026-02-21T12:00:00Z",
		TouchedDomains: []string{"api"},
		Body:           "API endpoints must require authentication.",
	})
	candidatePath := writeCheckCandidateFile(t, root, Spec{
		ID:             "api-auth-public",
		Type:           "feature",
		Title:          "Public API Access",
		Domain:         "api",
		Created:        "2026-04-14T10:00:00Z",
		TouchedDomains: []string{"api"},
		Body:           "Public API endpoints must not require authentication.",
	})
	responsePath := writeAICheckResponse(t, root, map[string]any{
		"model": "codex-headless",
		"conflict_check": map[string]any{
			"has_conflicts": true,
			"summary":       "Public API auth conflict.",
			"conflicts": []map[string]any{
				{
					"spec_a":        "api-auth-required",
					"spec_b":        "api-auth-public",
					"domain":        "api",
					"statement_key": "api auth required",
					"line_a":        "API endpoints must require authentication.",
					"line_b":        "Public API endpoints must not require authentication.",
					"reason":        "Direct contradiction on API auth requirements.",
				},
			},
		},
	})

	result, err := Check(root, CheckOptions{
		CandidateFile: candidatePath,
		AIMode:        "from-response",
		ResponseFile:  responsePath,
	})
	if err != nil {
		t.Fatalf("Check candidate failed: %v", err)
	}
	if result.Passed {
		t.Fatalf("expected candidate conflict to fail")
	}
	if result.TotalSpecs != 2 || result.TotalConflicts != 1 {
		t.Fatalf("unexpected candidate result: %+v", result)
	}
	if result.Candidate == nil || result.Candidate.SpecID != "api-auth-public" || result.Candidate.Domain != "api" {
		t.Fatalf("unexpected candidate metadata: %+v", result.Candidate)
	}
	conflict := result.Conflicts[0]
	if conflict.SpecA != "api-auth-required" || conflict.SpecB != "api-auth-public" {
		t.Fatalf("unexpected conflict pair: %+v", conflict)
	}
	if conflict.DomainA != "api" || conflict.DomainB != "api" {
		t.Fatalf("expected per-spec domains in conflict: %+v", conflict)
	}
	if conflict.Reason == "" {
		t.Fatalf("expected conflict reason")
	}
	if _, err := os.Stat(filepath.Join(root, ".canon", "specs", "api-auth-public.spec.md")); !os.IsNotExist(err) {
		t.Fatalf("candidate spec should not be written, stat err=%v", err)
	}
	if reports := listCheckConflictReports(t, root); len(reports) != 0 {
		t.Fatalf("candidate check without write should not create reports: %v", reports)
	}
}

func TestCheckCandidateFileWriteOnlyCreatesConflictReport(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	writeCheckSpec(t, root, Spec{
		ID:             "audit-required",
		Type:           "feature",
		Title:          "Audit Required",
		Domain:         "auth",
		Created:        "2026-02-21T12:00:00Z",
		TouchedDomains: []string{"auth"},
		Body:           "Audit logs must retain user access history.",
	})
	candidatePath := writeCheckCandidateFile(t, root, Spec{
		ID:             "audit-forbidden",
		Type:           "feature",
		Title:          "Audit Forbidden",
		Domain:         "auth",
		Created:        "2026-04-14T10:00:00Z",
		TouchedDomains: []string{"auth"},
		Body:           "Audit logs must not retain user access history.",
	})
	responsePath := writeAICheckResponse(t, root, map[string]any{
		"model": "codex-headless",
		"conflict_check": map[string]any{
			"has_conflicts": true,
			"summary":       "Audit conflict.",
			"conflicts": []map[string]any{
				{
					"spec_a":        "audit-required",
					"spec_b":        "audit-forbidden",
					"domain":        "auth",
					"statement_key": "audit retention",
					"line_a":        "Audit logs must retain user access history.",
					"line_b":        "Audit logs must not retain user access history.",
					"reason":        "Direct contradiction.",
				},
			},
		},
	})

	result, err := Check(root, CheckOptions{
		CandidateFile: candidatePath,
		Write:         true,
		AIMode:        "from-response",
		ResponseFile:  responsePath,
	})
	if err != nil {
		t.Fatalf("Check candidate write failed: %v", err)
	}
	if len(result.ReportPaths) != 1 {
		t.Fatalf("expected one report path, got %d", len(result.ReportPaths))
	}
	if _, err := os.Stat(filepath.Join(root, result.ReportPaths[0])); err != nil {
		t.Fatalf("expected candidate report file to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".canon", "specs", "audit-forbidden.spec.md")); !os.IsNotExist(err) {
		t.Fatalf("candidate spec should not be written, stat err=%v", err)
	}
}

func TestCheckCandidateFileRejectsSpecFlag(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}
	candidatePath := writeCheckCandidateFile(t, root, Spec{
		ID:             "candidate",
		Type:           "feature",
		Title:          "Candidate",
		Domain:         "api",
		Created:        "2026-04-14T10:00:00Z",
		TouchedDomains: []string{"api"},
		Body:           "Candidate behavior must be reviewed.",
	})

	_, err := Check(root, CheckOptions{
		SpecID:        "candidate",
		CandidateFile: candidatePath,
		AIMode:        "from-response",
		ResponseFile:  "unused.json",
	})
	if err == nil {
		t.Fatalf("expected --spec and --file conflict error")
	}
	if !strings.Contains(err.Error(), "use either --spec or --file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeCheckSpec(t *testing.T, root string, spec Spec) {
	t.Helper()
	if spec.Type == "" {
		spec.Type = "feature"
	}
	if spec.Created == "" {
		spec.Created = "2026-02-21T00:00:00Z"
	}
	if len(spec.TouchedDomains) == 0 {
		spec.TouchedDomains = []string{spec.Domain}
	}
	content := canonicalSpecText(spec)
	path := filepath.Join(root, ".canon", "specs", specFileName(spec.ID))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed writing check spec %s: %v", spec.ID, err)
	}
}

func writeCheckCandidateFile(t *testing.T, root string, spec Spec) string {
	t.Helper()
	path := filepath.Join(root, "candidate-"+spec.ID+".md")
	if err := os.WriteFile(path, []byte(canonicalSpecText(spec)), 0o644); err != nil {
		t.Fatalf("failed writing candidate file %s: %v", spec.ID, err)
	}
	return path
}

func listCheckConflictReports(t *testing.T, root string) []string {
	t.Helper()
	reports, err := filepath.Glob(filepath.Join(root, ".canon", "conflict-reports", "*.yaml"))
	if err != nil {
		t.Fatalf("failed listing conflict reports: %v", err)
	}
	return reports
}

func writeAICheckResponse(t *testing.T, root string, payload map[string]any) string {
	t.Helper()
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("failed marshaling AI check response: %v", err)
	}
	path := filepath.Join(root, "ai-check-response.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("failed writing AI check response file: %v", err)
	}
	return path
}
