package canon

import (
	"testing"
)

func TestRoadmapEntropyDetectsHeuristicsAndSummarizesDeterministically(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	ingestRoadmapEntropySpec(t, root, ingestRoadmapEntropyFixture{
		ID:      "base001",
		Type:    "feature",
		Title:   "Billing Baseline",
		Domain:  "billing",
		Created: "2026-03-01T00:00:00Z",
		Touches: []string{"billing"},
	})
	ingestRoadmapEntropySpec(t, root, ingestRoadmapEntropyFixture{
		ID:      "base002",
		Type:    "feature",
		Title:   "Auth Baseline",
		Domain:  "auth",
		Created: "2026-03-01T01:00:00Z",
		Touches: []string{"auth"},
	})
	ingestRoadmapEntropySpec(t, root, ingestRoadmapEntropyFixture{
		ID:      "base003",
		Type:    "feature",
		Title:   "Checkout Baseline",
		Domain:  "checkout",
		Created: "2026-03-01T02:00:00Z",
		Touches: []string{"checkout"},
	})

	ingestRoadmapEntropySpec(t, root, ingestRoadmapEntropyFixture{
		ID:      "recf001",
		Type:    "feature",
		Title:   "Search Launch",
		Domain:  "search",
		Created: "2026-03-02T00:00:00Z",
		Touches: []string{"search", "auth"},
	})
	ingestRoadmapEntropySpec(t, root, ingestRoadmapEntropyFixture{
		ID:      "rect001",
		Type:    "technical",
		Title:   "Platform Pipeline",
		Domain:  "platform",
		Created: "2026-03-02T01:00:00Z",
		Touches: []string{"platform"},
	})
	ingestRoadmapEntropySpec(t, root, ingestRoadmapEntropyFixture{
		ID:      "recr001",
		Type:    "resolution",
		Title:   "Compliance Follow-up",
		Domain:  "compliance",
		Created: "2026-03-02T02:00:00Z",
		Touches: []string{"compliance"},
	})

	result, err := RoadmapEntropy(root, RoadmapEntropyOptions{Window: 3})
	if err != nil {
		t.Fatalf("RoadmapEntropy failed: %v", err)
	}

	if result.RecentWindow.Specs != 3 || result.BaselineWindow.Specs != 3 {
		t.Fatalf("unexpected window sizes: recent=%d baseline=%d", result.RecentWindow.Specs, result.BaselineWindow.Specs)
	}
	if result.Summary.TotalFindings != 4 {
		t.Fatalf("expected 4 findings, got %d", result.Summary.TotalFindings)
	}
	if result.Summary.ScopeCreepFindings != 2 || result.Summary.DriftFindings != 2 {
		t.Fatalf("unexpected category counts: %+v", result.Summary)
	}
	if result.Summary.HighestSeverity != RoadmapEntropySeverityHigh {
		t.Fatalf("expected highest severity high, got %s", result.Summary.HighestSeverity)
	}
	if result.Summary.FindingsBySeverity.Low != 0 ||
		result.Summary.FindingsBySeverity.Medium != 2 ||
		result.Summary.FindingsBySeverity.High != 2 ||
		result.Summary.FindingsBySeverity.Critical != 0 {
		t.Fatalf("unexpected severity counts: %+v", result.Summary.FindingsBySeverity)
	}

	expectedRules := []string{
		roadmapEntropyRuleNonFeatureRatioRise,
		roadmapEntropyRuleNewDomains,
		roadmapEntropyRuleOrphanNonFeatureSpecs,
		roadmapEntropyRuleTouchedDomainExpansion,
	}
	for i, ruleID := range expectedRules {
		if result.Findings[i].RuleID != ruleID {
			t.Fatalf("unexpected finding order at index %d: got=%s want=%s", i, result.Findings[i].RuleID, ruleID)
		}
	}

	orphanFinding := result.Findings[2]
	if len(orphanFinding.SpecIDs) != 2 || orphanFinding.SpecIDs[0] != "recr001" || orphanFinding.SpecIDs[1] != "rect001" {
		t.Fatalf("unexpected orphan finding spec ids: %+v", orphanFinding.SpecIDs)
	}
}

func TestSortRoadmapEntropyFindingsDeterministically(t *testing.T) {
	findings := []RoadmapEntropyFinding{
		{RuleID: roadmapEntropyRuleTouchedDomainExpansion, Category: roadmapEntropyCategoryScopeCreep, Severity: RoadmapEntropySeverityLow},
		{RuleID: roadmapEntropyRuleNewDomains, Category: roadmapEntropyCategoryScopeCreep, Severity: RoadmapEntropySeverityHigh},
		{RuleID: roadmapEntropyRuleOrphanNonFeatureSpecs, Category: roadmapEntropyCategoryDrift, Severity: RoadmapEntropySeverityMedium},
		{RuleID: roadmapEntropyRuleNonFeatureRatioRise, Category: roadmapEntropyCategoryDrift, Severity: RoadmapEntropySeverityHigh},
	}

	sortRoadmapEntropyFindings(findings)

	expected := []string{
		roadmapEntropyRuleNonFeatureRatioRise,
		roadmapEntropyRuleNewDomains,
		roadmapEntropyRuleOrphanNonFeatureSpecs,
		roadmapEntropyRuleTouchedDomainExpansion,
	}
	for i, ruleID := range expected {
		if findings[i].RuleID != ruleID {
			t.Fatalf("unexpected finding order at index %d: got=%s want=%s", i, findings[i].RuleID, ruleID)
		}
	}
}

func TestSummarizeRoadmapEntropyFindings(t *testing.T) {
	summary := summarizeRoadmapEntropyFindings([]RoadmapEntropyFinding{
		{Category: roadmapEntropyCategoryScopeCreep, Severity: RoadmapEntropySeverityLow},
		{Category: roadmapEntropyCategoryScopeCreep, Severity: RoadmapEntropySeverityMedium},
		{Category: roadmapEntropyCategoryDrift, Severity: RoadmapEntropySeverityHigh},
		{Category: roadmapEntropyCategoryDrift, Severity: RoadmapEntropySeverityHigh},
	})

	if summary.TotalFindings != 4 {
		t.Fatalf("expected 4 total findings, got %d", summary.TotalFindings)
	}
	if summary.ScopeCreepFindings != 2 || summary.DriftFindings != 2 {
		t.Fatalf("unexpected category counts: %+v", summary)
	}
	if summary.HighestSeverity != RoadmapEntropySeverityHigh {
		t.Fatalf("expected highest severity high, got %s", summary.HighestSeverity)
	}
	if summary.FindingsBySeverity.Low != 1 ||
		summary.FindingsBySeverity.Medium != 1 ||
		summary.FindingsBySeverity.High != 2 ||
		summary.FindingsBySeverity.Critical != 0 {
		t.Fatalf("unexpected severity counts: %+v", summary.FindingsBySeverity)
	}
}

func TestParseRoadmapEntropySeverity(t *testing.T) {
	severity, err := parseRoadmapEntropySeverity(" Medium ")
	if err != nil {
		t.Fatalf("expected medium severity to parse, got error: %v", err)
	}
	if severity != RoadmapEntropySeverityMedium {
		t.Fatalf("expected medium severity, got %s", severity)
	}

	if _, err := parseRoadmapEntropySeverity("urgent"); err == nil {
		t.Fatalf("expected invalid severity error")
	}
}

func TestRoadmapEntropyThresholdEvaluation(t *testing.T) {
	result := RoadmapEntropyResult{
		Summary: RoadmapEntropySummary{
			HighestSeverity: RoadmapEntropySeverityHigh,
		},
	}

	if !roadmapEntropyExceedsThreshold(result, RoadmapEntropySeverityMedium) {
		t.Fatalf("expected high severity to exceed medium threshold")
	}
	if !roadmapEntropyExceedsThreshold(result, RoadmapEntropySeverityHigh) {
		t.Fatalf("expected high severity to exceed high threshold")
	}
	if roadmapEntropyExceedsThreshold(result, RoadmapEntropySeverityCritical) {
		t.Fatalf("did not expect high severity to exceed critical threshold")
	}
	if roadmapEntropyExceedsThreshold(result, RoadmapEntropySeverityNone) {
		t.Fatalf("did not expect none threshold to fail")
	}
}

type ingestRoadmapEntropyFixture struct {
	ID        string
	Type      string
	Title     string
	Domain    string
	Created   string
	DependsOn []string
	Touches   []string
}

func ingestRoadmapEntropySpec(t *testing.T, root string, fixture ingestRoadmapEntropyFixture) {
	t.Helper()
	if _, err := Ingest(root, IngestInput{
		NoAutoParents:  true,
		ConflictMode:   "off",
		ID:             fixture.ID,
		Type:           fixture.Type,
		Title:          fixture.Title,
		Domain:         fixture.Domain,
		Created:        fixture.Created,
		DependsOn:      fixture.DependsOn,
		TouchedDomains: fixture.Touches,
		Text:           "Roadmap entropy fixture body.",
	}); err != nil {
		t.Fatalf("ingest fixture %s failed: %v", fixture.ID, err)
	}
}
