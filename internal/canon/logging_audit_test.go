package canon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseLoggingAuditSeverity(t *testing.T) {
	severity, err := parseLoggingAuditSeverity(" Medium ")
	if err != nil {
		t.Fatalf("expected medium severity to parse, got error: %v", err)
	}
	if severity != LoggingAuditSeverityMedium {
		t.Fatalf("expected medium severity, got %s", severity)
	}

	if _, err := parseLoggingAuditSeverity("urgent"); err == nil {
		t.Fatalf("expected invalid severity error")
	}
}

func TestSortLoggingAuditFindingsDeterministically(t *testing.T) {
	findings := []LoggingAuditFinding{
		{RuleID: loggingAuditRuleWeakTitle, Category: loggingAuditCategoryQuality, Severity: LoggingAuditSeverityLow, SpecID: "z"},
		{RuleID: loggingAuditRuleMissingParent, Category: loggingAuditCategoryIntegrity, Severity: LoggingAuditSeverityHigh, SpecID: "b"},
		{RuleID: loggingAuditRuleMissingParent, Category: loggingAuditCategoryIntegrity, Severity: LoggingAuditSeverityHigh, SpecID: "a"},
		{RuleID: loggingAuditRuleMetadataMismatch, Category: loggingAuditCategoryQuality, Severity: LoggingAuditSeverityMedium, Field: "title"},
	}

	sortLoggingAuditFindings(findings)

	expectedRuleOrder := []string{
		loggingAuditRuleMissingParent,
		loggingAuditRuleMissingParent,
		loggingAuditRuleMetadataMismatch,
		loggingAuditRuleWeakTitle,
	}
	for i, ruleID := range expectedRuleOrder {
		if findings[i].RuleID != ruleID {
			t.Fatalf("unexpected finding at index %d: got %s want %s", i, findings[i].RuleID, ruleID)
		}
	}
	if findings[0].SpecID != "a" || findings[1].SpecID != "b" {
		t.Fatalf("expected lexical tie-break on spec id, got %+v", findings[:2])
	}
}

func TestSummarizeLoggingAuditFindings(t *testing.T) {
	summary := summarizeLoggingAuditFindings([]LoggingAuditFinding{
		{Severity: LoggingAuditSeverityLow},
		{Severity: LoggingAuditSeverityMedium},
		{Severity: LoggingAuditSeverityHigh},
		{Severity: LoggingAuditSeverityHigh},
	})

	if summary.TotalFindings != 4 {
		t.Fatalf("expected 4 total findings, got %d", summary.TotalFindings)
	}
	if summary.HighestSeverity != LoggingAuditSeverityHigh {
		t.Fatalf("expected highest severity high, got %s", summary.HighestSeverity)
	}
	if summary.FindingsBySeverity.Low != 1 || summary.FindingsBySeverity.Medium != 1 || summary.FindingsBySeverity.High != 2 || summary.FindingsBySeverity.Critical != 0 {
		t.Fatalf("unexpected severity counts: %+v", summary.FindingsBySeverity)
	}
}

func TestLoggingAuditThresholdEvaluation(t *testing.T) {
	result := LoggingAuditResult{
		Summary: LoggingAuditSummary{HighestSeverity: LoggingAuditSeverityHigh},
	}

	if !loggingAuditExceedsThreshold(result, LoggingAuditSeverityMedium) {
		t.Fatalf("expected high severity to exceed medium threshold")
	}
	if !loggingAuditExceedsThreshold(result, LoggingAuditSeverityHigh) {
		t.Fatalf("expected high severity to exceed high threshold")
	}
	if loggingAuditExceedsThreshold(result, LoggingAuditSeverityCritical) {
		t.Fatalf("did not expect high severity to exceed critical threshold")
	}
	if loggingAuditExceedsThreshold(result, LoggingAuditSeverityNone) {
		t.Fatalf("did not expect none threshold to fail")
	}
}

func TestLoggingAuditHealthyCanonArtifacts(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	if _, err := Ingest(root, IngestInput{
		ConflictMode: "off",
		Text: `---
id: abc1234
type: feature
title: "Healthy Spec"
domain: canon
created: 2026-03-01T12:00:00Z
depends_on: []
touched_domains: [canon]
---
Healthy logging audit fixture.
`,
	}); err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}

	result, err := LoggingAudit(root, LoggingAuditOptions{})
	if err != nil {
		t.Fatalf("LoggingAudit failed: %v", err)
	}

	if result.LedgerEntries != 1 || result.SpecFiles != 1 || result.SourceFiles != 1 {
		t.Fatalf("unexpected artifact counts: %+v", result)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings, got %d", len(result.Findings))
	}
	if result.Summary.HighestSeverity != LoggingAuditSeverityNone {
		t.Fatalf("expected highest severity none, got %s", result.Summary.HighestSeverity)
	}
}

func TestLoggingAuditDetectsBrokenArtifacts(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	specPath := filepath.Join(root, ".canon", "specs", "spec-1.spec.md")
	specText := `---
id: spec-1
type: feature
title: "Spec One"
domain: core
created: 2026-01-01T00:00:00Z
depends_on: []
touched_domains: [core]
---
Spec body.
`
	if err := os.WriteFile(specPath, []byte(specText), 0o644); err != nil {
		t.Fatalf("failed writing spec fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".canon", "sources", "spec-1.source.md"), []byte("source text\n"), 0o644); err != nil {
		t.Fatalf("failed writing source fixture: %v", err)
	}

	writeLoggingAuditLedgerFixture(t, filepath.Join(root, ".canon", "ledger", "20260101T000000Z-spec-1.json"), LedgerEntry{
		SpecID:      "spec-1",
		Title:       "x",
		Type:        "mystery",
		Domain:      "wrong",
		Parents:     []string{"missing-parent", "spec-1", "missing-parent", ""},
		Sequence:    200,
		IngestedAt:  "2026-01-01T00:00:00Z",
		ContentHash: "deadbeef",
		SpecPath:    ".canon/specs/spec-1.spec.md",
		SourcePath:  ".canon/sources/spec-1.source.md",
	})
	writeLoggingAuditLedgerFixture(t, filepath.Join(root, ".canon", "ledger", "20260101T010000Z-spec-2.json"), LedgerEntry{
		SpecID:      "spec-2",
		Title:       "Spec Two",
		Type:        "feature",
		Domain:      "core",
		Parents:     []string{},
		Sequence:    200,
		IngestedAt:  "2026-01-01T01:00:00Z",
		ContentHash: "deadbeef",
		SpecPath:    "/tmp/spec-2.spec.md",
		SourcePath:  ".canon/sources/spec-2.source.md",
	})

	result, err := LoggingAudit(root, LoggingAuditOptions{FailOn: LoggingAuditSeverityMedium})
	if err != nil {
		t.Fatalf("LoggingAudit failed: %v", err)
	}

	if result.Summary.TotalFindings == 0 {
		t.Fatalf("expected findings for broken artifacts")
	}
	if result.Summary.HighestSeverity != LoggingAuditSeverityHigh {
		t.Fatalf("expected highest severity high, got %s", result.Summary.HighestSeverity)
	}
	if !result.ThresholdExceeded {
		t.Fatalf("expected medium fail-on threshold to be exceeded")
	}
	if result.FailOn != LoggingAuditSeverityMedium {
		t.Fatalf("expected fail_on medium, got %s", result.FailOn)
	}

	hasRules := map[string]bool{}
	for _, finding := range result.Findings {
		hasRules[finding.RuleID] = true
	}

	requiredRules := []string{
		loggingAuditRuleMissingParent,
		loggingAuditRuleContentHashMismatch,
		loggingAuditRuleMetadataMismatch,
		loggingAuditRuleUnknownSpecType,
		loggingAuditRuleInvalidPath,
		loggingAuditRuleMissingSourceFile,
		loggingAuditRuleDuplicateSequence,
		loggingAuditRuleNonMonotonicSequence,
	}
	for _, ruleID := range requiredRules {
		if !hasRules[ruleID] {
			t.Fatalf("expected finding for rule %s", ruleID)
		}
	}

	for i := 1; i < len(result.Findings); i++ {
		left := loggingAuditSeverityRank(result.Findings[i-1].Severity)
		right := loggingAuditSeverityRank(result.Findings[i].Severity)
		if left < right {
			t.Fatalf("findings not sorted by severity at index %d: %+v then %+v", i, result.Findings[i-1], result.Findings[i])
		}
	}
}

func writeLoggingAuditLedgerFixture(t *testing.T, path string, entry LedgerEntry) {
	t.Helper()
	b, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal ledger entry: %v", err)
	}
	b = append(b, '\n')
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("failed writing ledger fixture: %v", err)
	}
}
