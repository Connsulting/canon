package canon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRoadmapEntropyDetectsHeuristicsAndSummarizesDeterministically(t *testing.T) {
	root := setupRoadmapEntropyLayout(t)

	specs := []Spec{
		{ID: "f100", Type: "feature", Title: "Auth baseline", Domain: "auth", Created: "2026-02-01T00:00:00Z", TouchedDomains: []string{"auth"}},
		{ID: "f200", Type: "feature", Title: "Billing baseline", Domain: "billing", Created: "2026-02-02T00:00:00Z", TouchedDomains: []string{"billing"}},
		{ID: "f300", Type: "feature", Title: "API baseline", Domain: "api", Created: "2026-02-03T00:00:00Z", TouchedDomains: []string{"api"}},
		{ID: "t400", Type: "technical", Title: "Infra expansion", Domain: "infra", Created: "2026-02-04T00:00:00Z", TouchedDomains: []string{"infra", "auth"}},
		{ID: "r500", Type: "resolution", Title: "Ops resolution", Domain: "ops", Created: "2026-02-05T00:00:00Z", TouchedDomains: []string{"ops", "billing"}},
		{ID: "t600", Type: "technical", Title: "Data expansion", Domain: "data", Created: "2026-02-06T00:00:00Z", TouchedDomains: []string{"data", "analytics"}},
	}
	for _, spec := range specs {
		writeRoadmapEntropySpec(t, root, spec)
	}
	for i, spec := range specs {
		writeRoadmapEntropyLedgerEntry(t, root, LedgerEntry{
			SpecID:     spec.ID,
			Title:      spec.Title,
			Type:       spec.Type,
			Domain:     spec.Domain,
			Parents:    []string{},
			Sequence:   int64(i + 1),
			IngestedAt: spec.Created,
		})
	}

	result, err := RoadmapEntropy(root, RoadmapEntropyOptions{Window: 3})
	if err != nil {
		t.Fatalf("RoadmapEntropy failed: %v", err)
	}

	if result.OrderedSpecCount != 6 {
		t.Fatalf("expected ordered spec count 6, got %d", result.OrderedSpecCount)
	}
	if result.InsufficientHistory {
		t.Fatalf("did not expect insufficient history")
	}
	if !reflect.DeepEqual(result.BaselineSpecIDs, []string{"f100", "f200", "f300"}) {
		t.Fatalf("unexpected baseline ids: %+v", result.BaselineSpecIDs)
	}
	if !reflect.DeepEqual(result.RecentSpecIDs, []string{"t400", "r500", "t600"}) {
		t.Fatalf("unexpected recent ids: %+v", result.RecentSpecIDs)
	}

	if len(result.Findings) != 4 {
		t.Fatalf("expected 4 findings, got %d", len(result.Findings))
	}

	expectedOrder := []struct {
		severity RoadmapEntropySeverity
		category string
		ruleID   string
	}{
		{RoadmapEntropySeverityCritical, roadmapEntropyCategoryDrift, roadmapEntropyRuleNonFeatureRatioRise},
		{RoadmapEntropySeverityCritical, roadmapEntropyCategoryDrift, roadmapEntropyRuleOrphanNonFeatureSpecs},
		{RoadmapEntropySeverityCritical, roadmapEntropyCategoryScopeCreep, roadmapEntropyRuleNewPrimaryDomains},
		{RoadmapEntropySeverityHigh, roadmapEntropyCategoryScopeCreep, roadmapEntropyRuleTouchedDomainExpansion},
	}
	for i, expected := range expectedOrder {
		finding := result.Findings[i]
		if finding.Severity != expected.severity || finding.Category != expected.category || finding.RuleID != expected.ruleID {
			t.Fatalf("unexpected finding order at index %d: got=%+v expected=%+v", i, finding, expected)
		}
	}

	if result.Summary.TotalFindings != 4 {
		t.Fatalf("expected summary total findings 4, got %d", result.Summary.TotalFindings)
	}
	if result.Summary.HighestSeverity != RoadmapEntropySeverityCritical {
		t.Fatalf("expected highest severity critical, got %s", result.Summary.HighestSeverity)
	}
	if result.Summary.FindingsByCategory.ScopeCreep != 2 || result.Summary.FindingsByCategory.Drift != 2 {
		t.Fatalf("unexpected category counts: %+v", result.Summary.FindingsByCategory)
	}
	if result.Summary.FindingsBySeverity.High != 1 || result.Summary.FindingsBySeverity.Critical != 3 || result.Summary.FindingsBySeverity.Medium != 0 || result.Summary.FindingsBySeverity.Low != 0 {
		t.Fatalf("unexpected severity counts: %+v", result.Summary.FindingsBySeverity)
	}

	second, err := RoadmapEntropy(root, RoadmapEntropyOptions{Window: 3})
	if err != nil {
		t.Fatalf("second RoadmapEntropy failed: %v", err)
	}
	if !reflect.DeepEqual(result.Findings, second.Findings) {
		t.Fatalf("findings changed between runs:\nfirst=%+v\nsecond=%+v", result.Findings, second.Findings)
	}
}

func TestRoadmapEntropySeverityParsingAndThresholdBehavior(t *testing.T) {
	root := setupRoadmapEntropyLayout(t)
	writeRoadmapEntropySpec(t, root, Spec{
		ID:             "base-feature",
		Type:           "feature",
		Title:          "baseline feature",
		Domain:         "auth",
		Created:        "2026-02-01T00:00:00Z",
		TouchedDomains: []string{"auth"},
	})
	writeRoadmapEntropySpec(t, root, Spec{
		ID:             "only-tech",
		Type:           "technical",
		Title:          "only tech",
		Domain:         "platform",
		Created:        "2026-02-02T00:00:00Z",
		TouchedDomains: []string{"platform"},
	})
	writeRoadmapEntropyLedgerEntry(t, root, LedgerEntry{
		SpecID:     "base-feature",
		Title:      "baseline feature",
		Type:       "feature",
		Domain:     "auth",
		Parents:    []string{},
		Sequence:   1,
		IngestedAt: "2026-02-01T00:00:00Z",
	})
	writeRoadmapEntropyLedgerEntry(t, root, LedgerEntry{
		SpecID:     "only-tech",
		Title:      "only tech",
		Type:       "technical",
		Domain:     "platform",
		Parents:    []string{},
		Sequence:   2,
		IngestedAt: "2026-02-02T00:00:00Z",
	})

	result, err := RoadmapEntropy(root, RoadmapEntropyOptions{
		Window: 1,
		FailOn: RoadmapEntropySeverityLow,
	})
	if err != nil {
		t.Fatalf("RoadmapEntropy with low fail-on failed: %v", err)
	}
	if result.FailOn != RoadmapEntropySeverityLow {
		t.Fatalf("expected fail-on low, got %s", result.FailOn)
	}
	if !result.ThresholdExceeded {
		t.Fatalf("expected threshold exceeded for low fail-on")
	}
	if !roadmapEntropyExceedsThreshold(result, RoadmapEntropySeverityLow) {
		t.Fatalf("expected threshold helper to report exceeded")
	}
	if !roadmapEntropyExceedsThreshold(result, RoadmapEntropySeverityCritical) {
		t.Fatalf("expected critical threshold to be exceeded")
	}
	if roadmapEntropyExceedsThreshold(
		RoadmapEntropyResult{
			Summary: RoadmapEntropySummary{HighestSeverity: RoadmapEntropySeverityMedium},
		},
		RoadmapEntropySeverityHigh,
	) {
		t.Fatalf("did not expect high threshold to be exceeded for medium highest severity")
	}

	parsed, err := parseRoadmapEntropySeverity("  medium ")
	if err != nil {
		t.Fatalf("parse severity medium failed: %v", err)
	}
	if parsed != RoadmapEntropySeverityMedium {
		t.Fatalf("expected parsed medium, got %s", parsed)
	}

	if _, err := parseRoadmapEntropySeverity("urgent"); err == nil {
		t.Fatalf("expected invalid severity error")
	} else if !strings.Contains(err.Error(), "unsupported severity") {
		t.Fatalf("unexpected invalid severity error: %v", err)
	}
}

func TestRoadmapEntropyBaselineEmptyDoesNotProduceFindings(t *testing.T) {
	root := setupRoadmapEntropyLayout(t)
	writeRoadmapEntropySpec(t, root, Spec{
		ID:             "only-tech",
		Type:           "technical",
		Title:          "only tech",
		Domain:         "platform",
		Created:        "2026-02-01T00:00:00Z",
		TouchedDomains: []string{"platform"},
	})
	writeRoadmapEntropyLedgerEntry(t, root, LedgerEntry{
		SpecID:     "only-tech",
		Title:      "only tech",
		Type:       "technical",
		Domain:     "platform",
		Parents:    []string{},
		Sequence:   1,
		IngestedAt: "2026-02-01T00:00:00Z",
	})

	result, err := RoadmapEntropy(root, RoadmapEntropyOptions{
		Window: 8,
		FailOn: RoadmapEntropySeverityLow,
	})
	if err != nil {
		t.Fatalf("RoadmapEntropy failed: %v", err)
	}
	if !result.InsufficientHistory {
		t.Fatalf("expected insufficient history")
	}
	if len(result.BaselineSpecIDs) != 0 {
		t.Fatalf("expected empty baseline ids, got %+v", result.BaselineSpecIDs)
	}
	if len(result.RecentSpecIDs) != 1 || result.RecentSpecIDs[0] != "only-tech" {
		t.Fatalf("unexpected recent ids: %+v", result.RecentSpecIDs)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings with empty baseline, got %d", len(result.Findings))
	}
	if result.Summary.HighestSeverity != RoadmapEntropySeverityNone {
		t.Fatalf("expected highest severity none, got %s", result.Summary.HighestSeverity)
	}
	if result.ThresholdExceeded {
		t.Fatalf("did not expect threshold exceeded with empty baseline")
	}
}

func TestRoadmapEntropyInsufficientHistoryWithoutFindings(t *testing.T) {
	root := setupRoadmapEntropyLayout(t)

	result, err := RoadmapEntropy(root, RoadmapEntropyOptions{
		Window: 8,
		FailOn: RoadmapEntropySeverityLow,
	})
	if err != nil {
		t.Fatalf("RoadmapEntropy failed: %v", err)
	}
	if !result.InsufficientHistory {
		t.Fatalf("expected insufficient history")
	}
	if result.OrderedSpecCount != 0 {
		t.Fatalf("expected zero ordered specs, got %d", result.OrderedSpecCount)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings, got %d", len(result.Findings))
	}
	if result.Summary.HighestSeverity != RoadmapEntropySeverityNone {
		t.Fatalf("expected highest severity none, got %s", result.Summary.HighestSeverity)
	}
	if result.ThresholdExceeded {
		t.Fatalf("did not expect threshold exceeded with no findings")
	}
}

func TestRoadmapEntropyInvalidOptions(t *testing.T) {
	root := setupRoadmapEntropyLayout(t)
	if _, err := RoadmapEntropy(root, RoadmapEntropyOptions{Window: 0}); err == nil {
		t.Fatalf("expected error for non-positive window")
	} else if !strings.Contains(err.Error(), "window must be positive") {
		t.Fatalf("unexpected window error: %v", err)
	}

	if _, err := RoadmapEntropy(root, RoadmapEntropyOptions{
		Window: 1,
		FailOn: RoadmapEntropySeverity("urgent"),
	}); err == nil {
		t.Fatalf("expected error for unsupported fail-on severity")
	} else if !strings.Contains(err.Error(), "unsupported severity") {
		t.Fatalf("unexpected severity error: %v", err)
	}

	missingRoot := t.TempDir()
	if _, err := RoadmapEntropy(missingRoot, RoadmapEntropyOptions{Window: 1}); err == nil {
		t.Fatalf("expected error for missing .canon paths")
	} else if !strings.Contains(err.Error(), "required path not found") {
		t.Fatalf("unexpected missing path error: %v", err)
	}
}

func setupRoadmapEntropyLayout(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".canon", "specs"), 0o755); err != nil {
		t.Fatalf("failed to create specs dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".canon", "ledger"), 0o755); err != nil {
		t.Fatalf("failed to create ledger dir: %v", err)
	}
	return root
}

func writeRoadmapEntropySpec(t *testing.T, root string, spec Spec) {
	t.Helper()
	specPath := filepath.Join(root, ".canon", "specs", spec.ID+".spec.md")
	if err := os.WriteFile(specPath, []byte(canonicalSpecText(spec)), 0o644); err != nil {
		t.Fatalf("failed to write spec %s: %v", spec.ID, err)
	}
}

func writeRoadmapEntropyLedgerEntry(t *testing.T, root string, entry LedgerEntry) {
	t.Helper()
	if strings.TrimSpace(entry.IngestedAt) == "" {
		entry.IngestedAt = "2026-01-01T00:00:00Z"
	}
	path := filepath.Join(root, ".canon", "ledger", entry.SpecID+".json")
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal ledger entry: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("failed to write ledger entry %s: %v", entry.SpecID, err)
	}
}
