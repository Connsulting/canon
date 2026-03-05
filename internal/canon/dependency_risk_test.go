package canon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDependencyRiskDetectsHeuristicsAndSummarizesDeterministically(t *testing.T) {
	root := t.TempDir()
	writeGoModFile(t, root, `module example.com/risk-test

go 1.22

require (
	github.com/acme/alpha v0.9.0
	github.com/acme/beta v1.2.3-rc1
	github.com/acme/gamma v0.0.0-20240101112233-abcdef123456
)

replace github.com/acme/alpha => ../alpha-local
replace github.com/acme/beta => example.com/forks/beta v1.2.3
`)

	result, err := DependencyRisk(root, DependencyRiskOptions{})
	if err != nil {
		t.Fatalf("DependencyRisk failed: %v", err)
	}

	if result.DependencyCount != 3 {
		t.Fatalf("expected 3 dependencies, got %d", result.DependencyCount)
	}
	if len(result.Findings) != 7 {
		t.Fatalf("expected 7 findings, got %d", len(result.Findings))
	}

	expectedOrder := []struct {
		severity DependencyRiskSeverity
		category string
		ruleID   string
		module   string
	}{
		{DependencyRiskSeverityHigh, dependencyRiskCategorySecurity, dependencyRiskRuleMissingGoSum, ""},
		{DependencyRiskSeverityHigh, dependencyRiskCategorySecurity, dependencyRiskRuleReplaceLocalPath, "github.com/acme/alpha"},
		{DependencyRiskSeverityMedium, dependencyRiskCategoryMaintenance, dependencyRiskRuleMajorZeroVersion, "github.com/acme/alpha"},
		{DependencyRiskSeverityMedium, dependencyRiskCategoryMaintenance, dependencyRiskRuleMajorZeroVersion, "github.com/acme/gamma"},
		{DependencyRiskSeverityMedium, dependencyRiskCategoryMaintenance, dependencyRiskRulePseudoVersion, "github.com/acme/gamma"},
		{DependencyRiskSeverityMedium, dependencyRiskCategorySecurity, dependencyRiskRuleReplaceModulePath, "github.com/acme/beta"},
		{DependencyRiskSeverityLow, dependencyRiskCategoryMaintenance, dependencyRiskRulePrereleaseVersion, "github.com/acme/beta"},
	}
	for i, expected := range expectedOrder {
		finding := result.Findings[i]
		if finding.Severity != expected.severity || finding.Category != expected.category || finding.RuleID != expected.ruleID || finding.Module != expected.module {
			t.Fatalf("unexpected finding order at index %d: got=%+v expected=%+v", i, finding, expected)
		}
	}

	if result.Summary.TotalFindings != 7 {
		t.Fatalf("expected summary total findings 7, got %d", result.Summary.TotalFindings)
	}
	if result.Summary.SecurityFindings != 3 {
		t.Fatalf("expected 3 security findings, got %d", result.Summary.SecurityFindings)
	}
	if result.Summary.MaintenanceFindings != 4 {
		t.Fatalf("expected 4 maintenance findings, got %d", result.Summary.MaintenanceFindings)
	}
	if result.Summary.HighestSeverity != DependencyRiskSeverityHigh {
		t.Fatalf("expected highest severity high, got %s", result.Summary.HighestSeverity)
	}
	if result.Summary.FindingsBySeverity.Low != 1 || result.Summary.FindingsBySeverity.Medium != 4 || result.Summary.FindingsBySeverity.High != 2 || result.Summary.FindingsBySeverity.Critical != 0 {
		t.Fatalf("unexpected severity counts: %+v", result.Summary.FindingsBySeverity)
	}
}

func TestDependencyRiskEmptyModuleRequirements(t *testing.T) {
	root := t.TempDir()
	writeGoModFile(t, root, `module example.com/empty

go 1.22
`)

	result, err := DependencyRisk(root, DependencyRiskOptions{})
	if err != nil {
		t.Fatalf("DependencyRisk failed: %v", err)
	}

	if result.DependencyCount != 0 {
		t.Fatalf("expected 0 dependencies, got %d", result.DependencyCount)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings, got %d", len(result.Findings))
	}
	if result.Summary.HighestSeverity != DependencyRiskSeverityNone {
		t.Fatalf("expected highest severity none, got %s", result.Summary.HighestSeverity)
	}
}

func TestDependencyRiskThresholdBehavior(t *testing.T) {
	root := t.TempDir()
	writeGoModFile(t, root, `module example.com/threshold

go 1.22

require github.com/acme/alpha v0.9.0
`)
	if err := os.WriteFile(filepath.Join(root, "go.sum"), []byte(""), 0o644); err != nil {
		t.Fatalf("failed to write go.sum: %v", err)
	}

	mediumResult, err := DependencyRisk(root, DependencyRiskOptions{FailOn: DependencyRiskSeverityMedium})
	if err != nil {
		t.Fatalf("DependencyRisk with medium fail-on failed: %v", err)
	}
	if mediumResult.Summary.HighestSeverity != DependencyRiskSeverityMedium {
		t.Fatalf("expected highest severity medium, got %s", mediumResult.Summary.HighestSeverity)
	}
	if !mediumResult.ThresholdExceeded {
		t.Fatalf("expected threshold to be exceeded for medium fail-on")
	}
	if mediumResult.FailOn != DependencyRiskSeverityMedium {
		t.Fatalf("expected fail-on medium in result, got %s", mediumResult.FailOn)
	}

	highResult, err := DependencyRisk(root, DependencyRiskOptions{FailOn: DependencyRiskSeverityHigh})
	if err != nil {
		t.Fatalf("DependencyRisk with high fail-on failed: %v", err)
	}
	if highResult.ThresholdExceeded {
		t.Fatalf("did not expect threshold to be exceeded for high fail-on")
	}
}

func writeGoModFile(t *testing.T, root string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(content), 0o644); err != nil {
		t.Fatalf("failed writing go.mod: %v", err)
	}
}
