package canon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGCSkipsWhenBelowThresholdWithoutForce(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	if _, err := Ingest(root, IngestInput{
		Text:   "Clients must authenticate.",
		Title:  "Auth Requirement",
		Domain: "api",
	}); err != nil {
		t.Fatalf("ingest failed: %v", err)
	}
	if _, err := Ingest(root, IngestInput{
		Text:   "Auth tokens must expire after one hour.",
		Title:  "Auth Expiry",
		Domain: "api",
	}); err != nil {
		t.Fatalf("ingest failed: %v", err)
	}

	result, err := GC(root, GCInput{
		Domain:   "api",
		MinSpecs: 5,
	})
	if err != nil {
		t.Fatalf("GC failed: %v", err)
	}
	if !result.Skip {
		t.Fatalf("expected gc to skip below threshold")
	}
	if result.SkipReason == "" {
		t.Fatalf("expected skip reason")
	}
}

func TestGCRefusesConflictsBeforeConsolidating(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	_, err := Ingest(root, IngestInput{
		Text:   "API keys must be validated before every request.",
		Title:  "API Strict Validation",
		Domain: "api",
	})
	if err != nil {
		t.Fatalf("ingest failed: %v", err)
	}
	_, err = Ingest(root, IngestInput{
		Text:   "API keys must not be required.",
		Title:  "API Optional Validation",
		Domain: "api",
	})
	if err != nil {
		t.Fatalf("ingest failed: %v", err)
	}

	if _, err := GC(root, GCInput{
		Domain:   "api",
		MinSpecs: 1,
		Force:    true,
		AIMode:   "from-response",
	}); err == nil {
		t.Fatalf("expected gc to reject conflicting specs")
	}
}

func TestGCTargetsSpecificSpecsAcrossDomains(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	_, err := Ingest(root, IngestInput{
		ID:      "api-a1",
		Text:    "API traffic should include trace ids.",
		Title:   "API Trace IDs",
		Domain:  "api",
		Created: "2026-02-21T08:00:00Z",
	})
	if err != nil {
		t.Fatalf("ingest api-a1 failed: %v", err)
	}
	_, err = Ingest(root, IngestInput{
		ID:      "platform-b1",
		Text:    "Platform health endpoint is always enabled.",
		Title:   "Platform Health",
		Domain:  "platform",
		Created: "2026-02-21T08:30:00Z",
	})
	if err != nil {
		t.Fatalf("ingest platform-b1 failed: %v", err)
	}

	responsePath := filepath.Join(root, "gc-response.json")
	writeGCResponse(t, responsePath, map[string]any{
		"model":   "test-model",
		"summary": "Merge targeted API spec only.",
		"consolidated_specs": []any{
			map[string]any{
				"id":              "api-consolidated",
				"type":            "feature",
				"title":           "API Consolidated",
				"domain":          "api",
				"created":         "2026-02-21T09:00:00Z",
				"depends_on":      []string{},
				"touched_domains": []string{"api"},
				"consolidates":    []string{"api-a1"},
				"body":            "API traffic should include trace ids.",
			},
		},
	})

	result, err := GC(root, GCInput{
		SpecIDs:      []string{"api-a1"},
		MinSpecs:     1,
		Force:        true,
		Write:        true,
		AIMode:       "from-response",
		ResponseFile: responsePath,
	})
	if err != nil {
		t.Fatalf("GC failed: %v", err)
	}
	if len(result.TargetSpecs) != 1 {
		t.Fatalf("expected one target spec, got %d", len(result.TargetSpecs))
	}
	if result.TargetSpecs[0].ID != "api-a1" {
		t.Fatalf("unexpected target spec id: %s", result.TargetSpecs[0].ID)
	}

	if _, err := os.Stat(filepath.Join(root, ".canon", "archive", "specs", "platform-b1.spec.md")); err == nil {
		t.Fatalf("expected non-target platform spec to stay active")
	}
}

func TestGCWritesConsolidatedSpecsAndArchivesSources(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	baseSpec, err := Ingest(root, IngestInput{
		Text:   "Shared auth settings are defined in auth service.",
		Title:  "Shared Auth",
		Domain: "platform",
	})
	if err != nil {
		t.Fatalf("base ingest failed: %v", err)
	}
	apiSpecA, err := Ingest(root, IngestInput{
		Text:   "API tokens must be checked for revocation.",
		Title:  "API Token Revocation",
		Domain: "api",
	})
	if err != nil {
		t.Fatalf("ingest api-a failed: %v", err)
	}
	apiSpecB, err := Ingest(root, IngestInput{
		Text:   "API tokens must include user context.",
		Title:  "API Token Context",
		Domain: "api",
	})
	if err != nil {
		t.Fatalf("ingest api-b failed: %v", err)
	}

	entriesBefore, err := LoadLedger(root)
	if err != nil {
		t.Fatalf("LoadLedger before failed: %v", err)
	}

	responsePath := filepath.Join(root, "gc-response.json")
	writeGCResponse(t, responsePath, map[string]any{
		"model":   "test-model",
		"summary": "Merged API auth checks into one requirement.",
		"consolidated_specs": []any{
			map[string]any{
				"id":              "api-consolidated",
				"type":            "feature",
				"title":           "API Token Guard",
				"domain":          "api",
				"created":         "2026-02-21T12:00:00Z",
				"depends_on":      []string{baseSpec.SpecID},
				"touched_domains": []string{"api"},
				"consolidates":    []string{apiSpecA.SpecID, apiSpecB.SpecID},
				"body":            "API tokens must include user context and revocation checks.",
			},
		},
	})

	result, err := GC(root, GCInput{
		Domain:       "api",
		MinSpecs:     1,
		Force:        true,
		Write:        true,
		AIMode:       "from-response",
		ResponseFile: responsePath,
	})
	if err != nil {
		t.Fatalf("GC failed: %v", err)
	}
	if len(result.Consolidated) != 1 {
		t.Fatalf("expected one consolidated spec, got %d", len(result.Consolidated))
	}

	specPath := filepath.Join(root, ".canon", "specs", apiSpecA.SpecID+".spec.md")
	if _, err := os.Stat(specPath); !os.IsNotExist(err) {
		t.Fatalf("expected source api spec to be archived")
	}
	if _, err := os.Stat(filepath.Join(root, ".canon", "archive", "specs", apiSpecA.SpecID+".spec.md")); err != nil {
		t.Fatalf("expected archived api-a spec at %s", filepath.Join(root, ".canon", "archive", "specs", apiSpecA.SpecID+".spec.md"))
	}
	if _, err := os.Stat(filepath.Join(root, ".canon", "archive", "sources", apiSpecA.SpecID+".source.md")); err != nil {
		t.Fatalf("expected archived api-a source at %s", filepath.Join(root, ".canon", "archive", "sources", apiSpecA.SpecID+".source.md"))
	}
	if _, err := os.Stat(filepath.Join(root, ".canon", "archive", "specs", apiSpecB.SpecID+".spec.md")); err != nil {
		t.Fatalf("expected archived api-b spec at %s", filepath.Join(root, ".canon", "archive", "specs", apiSpecB.SpecID+".spec.md"))
	}
	if _, err := os.Stat(filepath.Join(root, ".canon", "archive", "sources", apiSpecB.SpecID+".source.md")); err != nil {
		t.Fatalf("expected archived api-b source at %s", filepath.Join(root, ".canon", "archive", "sources", apiSpecB.SpecID+".source.md"))
	}

	entriesAfter, err := LoadLedger(root)
	if err != nil {
		t.Fatalf("LoadLedger after failed: %v", err)
	}
	if len(entriesAfter) != len(entriesBefore)+1 {
		t.Fatalf("expected one new ledger entry")
	}

	consolidatedSpec := result.Consolidated[0]
	if consolidatedSpec.ID != "api-consolidated" {
		t.Fatalf("unexpected consolidated id: %s", consolidatedSpec.ID)
	}
	if !containsString(consolidatedSpec.Consolidates, apiSpecA.SpecID) || !containsString(consolidatedSpec.Consolidates, apiSpecB.SpecID) {
		t.Fatalf("consolidated spec missing source ids: %+v", consolidatedSpec.Consolidates)
	}
	if !containsString(consolidatedSpec.DependsOn, baseSpec.SpecID) {
		t.Fatalf("expected external dependency %s", baseSpec.SpecID)
	}

	created, err := loadSpecByID(root, consolidatedSpec.ID)
	if err != nil {
		t.Fatalf("load consolidated spec failed: %v", err)
	}
	if !containsString(created.DependsOn, baseSpec.SpecID) {
		t.Fatalf("consolidated spec file lacks external dependency")
	}
}

func TestCanonicalSpecTextRoundTripsConsolidates(t *testing.T) {
	spec := Spec{
		ID:             "cons-001",
		Type:           "feature",
		Title:          "Consolidated API Spec",
		Domain:         "api",
		Created:        "2026-02-21T12:00:00Z",
		DependsOn:      []string{"other-001"},
		TouchedDomains: []string{"api"},
		Consolidates:   []string{"a1", "a2", "a3"},
		Body:           "Consolidated behavior.",
	}
	text := canonicalSpecText(spec)
	parsed, err := parseSpecText(text, "inline")
	if err != nil {
		t.Fatalf("parseSpecText failed: %v", err)
	}
	if !containsString(parsed.Consolidates, "a1") || !containsString(parsed.Consolidates, "a2") || !containsString(parsed.Consolidates, "a3") {
		t.Fatalf("missing consolidates after parse, got %+v", parsed.Consolidates)
	}
}

func writeGCResponse(t *testing.T, path string, payload any) {
	t.Helper()
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("marshal gc response failed: %v", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write gc response failed: %v", err)
	}
}

func containsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}
