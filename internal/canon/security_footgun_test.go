package canon

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseSecurityFootgunSeverity(t *testing.T) {
	severity, err := parseSecurityFootgunSeverity(" High ")
	if err != nil {
		t.Fatalf("expected high severity to parse, got error: %v", err)
	}
	if severity != SecurityFootgunSeverityHigh {
		t.Fatalf("expected high severity, got %s", severity)
	}

	if _, err := parseSecurityFootgunSeverity("urgent"); err == nil {
		t.Fatalf("expected invalid severity error")
	}
}

func TestSortSecurityFootgunFindingsDeterministically(t *testing.T) {
	findings := []SecurityFootgunFinding{
		{RuleID: securityFootgunRuleWeakHashImport, Category: securityFootgunCategoryCryptography, Severity: SecurityFootgunSeverityMedium, File: "b.go", Line: 2},
		{RuleID: securityFootgunRuleExecShellC, Category: securityFootgunCategoryCommandExecution, Severity: SecurityFootgunSeverityHigh, File: "z.go", Line: 8},
		{RuleID: securityFootgunRuleExecShellC, Category: securityFootgunCategoryCommandExecution, Severity: SecurityFootgunSeverityHigh, File: "a.go", Line: 4},
		{RuleID: securityFootgunRuleHardcodedCredentialLiteral, Category: securityFootgunCategorySecrets, Severity: SecurityFootgunSeverityCritical, File: "c.go", Line: 1},
	}

	sortSecurityFootgunFindings(findings)

	expectedOrder := []string{
		securityFootgunRuleHardcodedCredentialLiteral,
		securityFootgunRuleExecShellC,
		securityFootgunRuleExecShellC,
		securityFootgunRuleWeakHashImport,
	}
	for i, ruleID := range expectedOrder {
		if findings[i].RuleID != ruleID {
			t.Fatalf("unexpected finding at index %d: got %s want %s", i, findings[i].RuleID, ruleID)
		}
	}
	if findings[1].File != "a.go" || findings[2].File != "z.go" {
		t.Fatalf("expected lexical tie-break on file path, got %+v", findings[1:3])
	}
}

func TestSecurityFootgunDetectsRulesAndSummarizesDeterministically(t *testing.T) {
	root := t.TempDir()

	writeSecurityFootgunGoFile(t, root, "a_secrets.go", `package fixture

const apiToken = "prodTokenValue123"
`)

	writeSecurityFootgunGoFile(t, root, "b_transport.go", `package fixture

import (
	"crypto/tls"
	"os/exec"
)

func connect() {
	cfg := &tls.Config{InsecureSkipVerify: true}
	cfg.InsecureSkipVerify = true
	_, _ = exec.Command("/bin/sh", "-c", "echo hello").Output()
}
`)

	writeSecurityFootgunGoFile(t, root, "c_crypto.go", `package fixture

import (
	"crypto/md5"
	"crypto/sha1"
	rand "math/rand/v2"
)

func issueToken() int {
	_ = md5.New
	_ = sha1.New
	token := rand.Int()
	return token
}
`)

	result, err := SecurityFootgun(root, SecurityFootgunOptions{FailOn: SecurityFootgunSeverityHigh})
	if err != nil {
		t.Fatalf("SecurityFootgun failed: %v", err)
	}

	if result.FilesScanned != 3 {
		t.Fatalf("expected 3 files scanned, got %d", result.FilesScanned)
	}
	if len(result.Findings) != 7 {
		t.Fatalf("expected 7 findings, got %d", len(result.Findings))
	}

	expectedRuleOrder := []string{
		securityFootgunRuleHardcodedCredentialLiteral,
		securityFootgunRuleExecShellC,
		securityFootgunRuleInsecureRandSensitive,
		securityFootgunRuleTLSInsecureSkipVerify,
		securityFootgunRuleTLSInsecureSkipVerify,
		securityFootgunRuleWeakHashImport,
		securityFootgunRuleWeakHashImport,
	}
	for i, expected := range expectedRuleOrder {
		if result.Findings[i].RuleID != expected {
			t.Fatalf("unexpected rule order at index %d: got=%s want=%s", i, result.Findings[i].RuleID, expected)
		}
	}

	if result.Summary.TotalFindings != 7 {
		t.Fatalf("expected total findings 7, got %d", result.Summary.TotalFindings)
	}
	if result.Summary.HighestSeverity != SecurityFootgunSeverityCritical {
		t.Fatalf("expected highest severity critical, got %s", result.Summary.HighestSeverity)
	}
	if result.Summary.FindingsBySeverity.Low != 0 ||
		result.Summary.FindingsBySeverity.Medium != 2 ||
		result.Summary.FindingsBySeverity.High != 4 ||
		result.Summary.FindingsBySeverity.Critical != 1 {
		t.Fatalf("unexpected severity counts: %+v", result.Summary.FindingsBySeverity)
	}
	if !result.ThresholdExceeded {
		t.Fatalf("expected high fail-on threshold to be exceeded")
	}
	if result.FailOn != SecurityFootgunSeverityHigh {
		t.Fatalf("expected fail-on high, got %s", result.FailOn)
	}

	again, err := SecurityFootgun(root, SecurityFootgunOptions{FailOn: SecurityFootgunSeverityHigh})
	if err != nil {
		t.Fatalf("SecurityFootgun second run failed: %v", err)
	}
	if !reflect.DeepEqual(result.Findings, again.Findings) {
		t.Fatalf("expected deterministic findings across runs")
	}
	if !reflect.DeepEqual(result.Summary, again.Summary) {
		t.Fatalf("expected deterministic summary across runs")
	}
}

func TestSecurityFootgunThresholdEvaluation(t *testing.T) {
	result := SecurityFootgunResult{
		Summary: SecurityFootgunSummary{HighestSeverity: SecurityFootgunSeverityHigh},
	}

	if !securityFootgunExceedsThreshold(result, SecurityFootgunSeverityMedium) {
		t.Fatalf("expected high severity to exceed medium threshold")
	}
	if !securityFootgunExceedsThreshold(result, SecurityFootgunSeverityHigh) {
		t.Fatalf("expected high severity to exceed high threshold")
	}
	if securityFootgunExceedsThreshold(result, SecurityFootgunSeverityCritical) {
		t.Fatalf("did not expect high severity to exceed critical threshold")
	}
	if securityFootgunExceedsThreshold(result, SecurityFootgunSeverityNone) {
		t.Fatalf("did not expect none threshold to fail")
	}
}

func TestSecurityFootgunEmptyScan(t *testing.T) {
	root := t.TempDir()

	result, err := SecurityFootgun(root, SecurityFootgunOptions{})
	if err != nil {
		t.Fatalf("SecurityFootgun failed: %v", err)
	}

	if result.FilesScanned != 0 {
		t.Fatalf("expected 0 files scanned, got %d", result.FilesScanned)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings, got %d", len(result.Findings))
	}
	if result.Summary.HighestSeverity != SecurityFootgunSeverityNone {
		t.Fatalf("expected highest severity none, got %s", result.Summary.HighestSeverity)
	}
}

func writeSecurityFootgunGoFile(t *testing.T, root string, rel string, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed to create dir for %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed writing %s: %v", rel, err)
	}
}
