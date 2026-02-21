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
