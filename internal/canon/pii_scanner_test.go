package canon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePIIScanSeverity(t *testing.T) {
	severity, err := parsePIIScanSeverity(" High ")
	if err != nil {
		t.Fatalf("expected high severity to parse, got error: %v", err)
	}
	if severity != PIIScanSeverityHigh {
		t.Fatalf("expected high severity, got %s", severity)
	}

	if _, err := parsePIIScanSeverity("urgent"); err == nil {
		t.Fatalf("expected invalid severity error")
	}
}

func TestSortPIIScanFindingsDeterministically(t *testing.T) {
	findings := []PIIScanFinding{
		{File: "b.go", Line: 2, Category: PIIScanCategoryHardcodedPII, Severity: PIIScanSeverityMedium, Detail: "b"},
		{File: "a.go", Line: 3, Category: PIIScanCategoryHardcodedPII, Severity: PIIScanSeverityHigh, Detail: "a"},
		{File: "a.go", Line: 2, Category: PIIScanCategoryEnvSecret, Severity: PIIScanSeverityHigh, Detail: "a"},
		{File: "a.go", Line: 1, Category: PIIScanCategoryHardcodedPII, Severity: PIIScanSeverityHigh, Detail: "a"},
	}

	sortPIIScanFindings(findings)

	expected := []PIIScanFinding{
		{File: "a.go", Line: 2, Category: PIIScanCategoryEnvSecret, Severity: PIIScanSeverityHigh},
		{File: "a.go", Line: 1, Category: PIIScanCategoryHardcodedPII, Severity: PIIScanSeverityHigh},
		{File: "a.go", Line: 3, Category: PIIScanCategoryHardcodedPII, Severity: PIIScanSeverityHigh},
		{File: "b.go", Line: 2, Category: PIIScanCategoryHardcodedPII, Severity: PIIScanSeverityMedium},
	}

	for i := range expected {
		if findings[i].File != expected[i].File || findings[i].Line != expected[i].Line || findings[i].Category != expected[i].Category || findings[i].Severity != expected[i].Severity {
			t.Fatalf("unexpected ordering at index %d: got %+v want %+v", i, findings[i], expected[i])
		}
	}
}

func TestSummarizePIIScanFindings(t *testing.T) {
	summary := summarizePIIScanFindings([]PIIScanFinding{
		{Category: PIIScanCategoryHardcodedPII, Severity: PIIScanSeverityMedium},
		{Category: PIIScanCategoryEnvSecret, Severity: PIIScanSeverityHigh},
		{Category: PIIScanCategoryGitignoreGap, Severity: PIIScanSeverityLow},
		{Category: PIIScanCategoryGitignoreGap, Severity: PIIScanSeverityCritical},
	})

	if summary.TotalFindings != 4 {
		t.Fatalf("expected 4 findings, got %d", summary.TotalFindings)
	}
	if summary.HighestSeverity != PIIScanSeverityCritical {
		t.Fatalf("expected highest severity critical, got %s", summary.HighestSeverity)
	}
	if summary.FindingsBySeverity.Low != 1 || summary.FindingsBySeverity.Medium != 1 || summary.FindingsBySeverity.High != 1 || summary.FindingsBySeverity.Critical != 1 {
		t.Fatalf("unexpected severity counts: %+v", summary.FindingsBySeverity)
	}
	if summary.FindingsByCategory.HardcodedPII != 1 || summary.FindingsByCategory.EnvSecret != 1 || summary.FindingsByCategory.GitignoreCoverage != 2 {
		t.Fatalf("unexpected category counts: %+v", summary.FindingsByCategory)
	}
}

func TestPIIScanThresholdEvaluation(t *testing.T) {
	result := PIIScanResult{
		Summary: PIIScanSummary{
			HighestSeverity: PIIScanSeverityHigh,
		},
	}
	if !piiScanExceedsThreshold(result, PIIScanSeverityMedium) {
		t.Fatalf("expected high to exceed medium threshold")
	}
	if !piiScanExceedsThreshold(result, PIIScanSeverityHigh) {
		t.Fatalf("expected high to exceed high threshold")
	}
	if piiScanExceedsThreshold(result, PIIScanSeverityCritical) {
		t.Fatalf("did not expect high to exceed critical threshold")
	}
	if piiScanExceedsThreshold(result, PIIScanSeverityNone) {
		t.Fatalf("did not expect none threshold to fail")
	}
}

func TestPIIScanDetectsRequiredCategories(t *testing.T) {
	root := t.TempDir()

	mustWriteFile(t, filepath.Join(root, ".gitignore"), "node_modules/\n")
	mustWriteFile(t, filepath.Join(root, "data", "seed.json"), `{"email":"alice@example.com","phone":"212-555-0123","dob":"1990-01-02","address":"123 Main St","name":"Alice Johnson","ip_identifier":"192.168.1.20","ssn":"123-45-6789"}`+"\n")
	mustWriteFile(t, filepath.Join(root, "internal", "app.go"), `package app
import "log"
func logUser(userEmail string) {
	log.Printf("processing email=%s", userEmail)
}
`)
	mustWriteFile(t, filepath.Join(root, ".env"), "API_KEY=live_123456\n")
	mustWriteFile(t, filepath.Join(root, "db", "schema.sql"), "CREATE TABLE users (id INT, password TEXT, ssn TEXT);\n")
	mustWriteFile(t, filepath.Join(root, "testdata", "fixtures", "users.json"), `{"ssn":"123-45-6789","name":"Bob Carter"}`+"\n")

	result, err := PIIScan(root, PIIScanOptions{})
	if err != nil {
		t.Fatalf("PIIScan failed: %v", err)
	}

	if result.Summary.TotalFindings == 0 {
		t.Fatalf("expected findings")
	}

	hasCategory := map[PIIScanCategory]bool{}
	for _, finding := range result.Findings {
		hasCategory[finding.Category] = true
	}

	requiredCategories := []PIIScanCategory{
		PIIScanCategoryHardcodedPII,
		PIIScanCategoryPIIInLogs,
		PIIScanCategoryEnvSecret,
		PIIScanCategoryUnencryptedData,
		PIIScanCategoryGitignoreGap,
	}
	for _, category := range requiredCategories {
		if !hasCategory[category] {
			t.Fatalf("expected category %s in findings", category)
		}
	}

	var fixtureFinding *PIIScanFinding
	for i := range result.Findings {
		finding := &result.Findings[i]
		if strings.Contains(finding.File, "testdata/fixtures/users.json") && strings.Contains(strings.ToLower(finding.Detail), "ssn") {
			fixtureFinding = finding
			break
		}
	}
	if fixtureFinding == nil {
		t.Fatalf("expected fixture SSN finding")
	}
	if fixtureFinding.Severity != PIIScanSeverityMedium {
		t.Fatalf("expected fixture SSN severity to downgrade to medium, got %s", fixtureFinding.Severity)
	}
}

func TestPIIScanExcludesVendorAndThirdPartyContent(t *testing.T) {
	root := t.TempDir()

	mustWriteFile(t, filepath.Join(root, ".gitignore"), ".env*\n*.pem\n*.key\ncredentials.*\nsecrets.*\n*.sql\n*.csv\n*.xlsx\n")
	mustWriteFile(t, filepath.Join(root, "vendor", "pkg", "fixture.json"), `{"email":"vendor@example.com","ssn":"123-45-6789"}`+"\n")
	mustWriteFile(t, filepath.Join(root, "third_party", "sample.csv"), "name,email\nJohn Doe,john@example.com\n")
	mustWriteFile(t, filepath.Join(root, "app", "main.go"), "package main\n")

	result, err := PIIScan(root, PIIScanOptions{})
	if err != nil {
		t.Fatalf("PIIScan failed: %v", err)
	}

	for _, finding := range result.Findings {
		if strings.HasPrefix(finding.File, "vendor/") || strings.HasPrefix(finding.File, "third_party/") {
			t.Fatalf("unexpected finding from excluded third-party path: %+v", finding)
		}
	}
}

func mustWriteFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed creating directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("failed writing %s: %v", path, err)
	}
}
