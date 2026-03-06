package canon

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPIIScanDetectsRequiredCategoriesAndSummarizes(t *testing.T) {
	root := t.TempDir()
	setupGitRepo(t, root)

	writePIITestFile(t, root, ".gitignore", "build/\n")
	writePIITestFile(t, root, ".env", "DB_PASSWORD=super-secret-password\n")
	writePIITestFile(t, root, "app/users.json", `{"full_name":"Jane Doe","email":"jane.doe@corp.co","dob":"1991-10-12"}`)
	writePIITestFile(t, root, "internal/logging.go", `package internal

import "log"

func audit(userEmail string) {
	log.Printf("failed login email=%s", userEmail)
}
`)
	writePIITestFile(t, root, "db/schema.sql", `CREATE TABLE users (
	id SERIAL PRIMARY KEY,
	ssn VARCHAR(11),
	password VARCHAR(255)
);
`)
	writePIITestFile(t, root, "vendor/acme/sample.json", `{"email":"vendor.person@corp.co","ssn":"321-54-6789"}`)
	gitAddAll(t, root)

	result, err := PIIScan(root, PIIScanOptions{})
	if err != nil {
		t.Fatalf("PIIScan failed: %v", err)
	}

	if result.Summary.TotalFindings != len(result.Findings) {
		t.Fatalf("expected summary total %d to equal finding count %d", result.Summary.TotalFindings, len(result.Findings))
	}
	if result.Summary.FindingsByCategory.HardcodedPII == 0 {
		t.Fatalf("expected hardcoded-pii findings")
	}
	if result.Summary.FindingsByCategory.PIIInLogs == 0 {
		t.Fatalf("expected pii-in-logs findings")
	}
	if result.Summary.FindingsByCategory.EnvSecret == 0 {
		t.Fatalf("expected env-secret findings")
	}
	if result.Summary.FindingsByCategory.UnencryptedStorage == 0 {
		t.Fatalf("expected unencrypted-storage findings")
	}
	if result.Summary.FindingsByCategory.GitignoreGap == 0 {
		t.Fatalf("expected gitignore-gap findings")
	}
	if result.Summary.HighestSeverity != PIISeverityCritical {
		t.Fatalf("expected highest severity critical, got %s", result.Summary.HighestSeverity)
	}

	for _, finding := range result.Findings {
		if strings.HasPrefix(finding.File, "vendor/") {
			t.Fatalf("expected vendor directory to be excluded, got finding: %+v", finding)
		}
		if finding.Line <= 0 {
			t.Fatalf("expected positive line number, got %+v", finding)
		}
	}
}

func TestPIIScanCapsFixturePIISeverityAtMedium(t *testing.T) {
	root := t.TempDir()
	setupGitRepo(t, root)
	writePIITestFile(t, root, ".gitignore", ".env*\n*.pem\n*.key\ncredentials.*\nsecrets.*\n*.sql\n*.csv\n*.xlsx\n")
	writePIITestFile(t, root, "testdata/users.json", `{"full_name":"Jane Doe","email":"jane.doe@corp.co","ssn":"321-54-6789"}`)
	gitAddAll(t, root)

	result, err := PIIScan(root, PIIScanOptions{})
	if err != nil {
		t.Fatalf("PIIScan failed: %v", err)
	}

	foundFixture := false
	for _, finding := range result.Findings {
		if finding.File != "testdata/users.json" || finding.Category != PIIFindingCategoryHardcodedPII {
			continue
		}
		foundFixture = true
		if piiSeverityRank(finding.Severity) > piiSeverityRank(PIISeverityMedium) {
			t.Fatalf("expected fixture finding to be medium or lower, got %+v", finding)
		}
	}
	if !foundFixture {
		t.Fatalf("expected at least one hardcoded fixture finding")
	}
}

func TestPIIScanDeterministicOrderingAndThreshold(t *testing.T) {
	root := t.TempDir()
	setupGitRepo(t, root)
	writePIITestFile(t, root, ".gitignore", ".env*\n*.pem\n*.key\ncredentials.*\nsecrets.*\n*.sql\n*.csv\n*.xlsx\n")
	writePIITestFile(t, root, ".env", "API_TOKEN=abc123456789\n")
	writePIITestFile(t, root, "app/log.go", `package app

import "fmt"

func f(userSSN string) string {
	return fmt.Errorf("lookup failed for ssn=%s", userSSN).Error()
}
`)
	gitAddAll(t, root)

	first, err := PIIScan(root, PIIScanOptions{FailOn: PIISeverityHigh})
	if err != nil {
		t.Fatalf("first PIIScan failed: %v", err)
	}
	second, err := PIIScan(root, PIIScanOptions{FailOn: PIISeverityHigh})
	if err != nil {
		t.Fatalf("second PIIScan failed: %v", err)
	}

	if !reflect.DeepEqual(first.Findings, second.Findings) {
		t.Fatalf("expected deterministic findings ordering")
	}
	if first.FailOn != PIISeverityHigh {
		t.Fatalf("expected fail-on high, got %s", first.FailOn)
	}
	if !first.ThresholdExceeded {
		t.Fatalf("expected threshold to be exceeded")
	}

	for i := 1; i < len(first.Findings); i++ {
		if piiSeverityRank(first.Findings[i-1].Severity) < piiSeverityRank(first.Findings[i].Severity) {
			t.Fatalf("expected descending severity order, got %s before %s", first.Findings[i-1].Severity, first.Findings[i].Severity)
		}
	}
}

func TestPIIScanExcludesVendorDirectory(t *testing.T) {
	root := t.TempDir()
	setupGitRepo(t, root)
	writePIITestFile(t, root, ".gitignore", ".env*\n*.pem\n*.key\ncredentials.*\nsecrets.*\n*.sql\n*.csv\n*.xlsx\n")
	writePIITestFile(t, root, "vendor/private/data.json", `{"email":"real.person@corp.co","ssn":"321-54-6789"}`)
	gitAddAll(t, root)

	result, err := PIIScan(root, PIIScanOptions{})
	if err != nil {
		t.Fatalf("PIIScan failed: %v", err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings when only vendor content exists, got %d", len(result.Findings))
	}
}

func TestParsePIISeverityRejectsUnsupported(t *testing.T) {
	if _, err := parsePIISeverity("urgent"); err == nil {
		t.Fatalf("expected parsePIISeverity to reject unsupported values")
	}
}

func setupGitRepo(t *testing.T, root string) {
	t.Helper()
	cmd := exec.Command("git", "init")
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, string(output))
	}
}

func gitAddAll(t *testing.T, root string) {
	t.Helper()
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v\n%s", err, string(output))
	}
}

func writePIITestFile(t *testing.T, root string, rel string, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed for %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write failed for %s: %v", rel, err)
	}
}
