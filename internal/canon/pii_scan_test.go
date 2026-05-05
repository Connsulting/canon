package canon

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func initGitRepoForPIIScan(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "--quiet", "--initial-branch=main"},
		{"-c", "user.email=test@example.test", "-c", "user.name=test", "config", "user.email", "test@example.test"},
		{"-c", "user.email=test@example.test", "-c", "user.name=test", "config", "user.name", "test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
}

func gitTrackInPIIScan(t *testing.T, root string, paths ...string) {
	t.Helper()
	addArgs := append([]string{"add", "--"}, paths...)
	cmd := exec.Command("git", addArgs...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add %v failed: %v\n%s", paths, err, out)
	}
	cmd = exec.Command("git", "-c", "user.email=test@example.test", "-c", "user.name=test", "commit", "--quiet", "-m", "fixture")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %v\n%s", err, out)
	}
}

func writePIIFixtureFile(t *testing.T, root string, rel string, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir for %s failed: %v", rel, err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s failed: %v", rel, err)
	}
}

func TestPIIScanDetectsCriticalLiteralsAndSecretsAndOrders(t *testing.T) {
	root := t.TempDir()
	initGitRepoForPIIScan(t, root)

	writePIIFixtureFile(t, root, "src/leak.go", `package src

const supportEmail = "agent@example.com"
const ssn = "123-45-6789"
const card = "4111-1111-1111-1111"
const apiKey = "AKIAIOSFODNN7EXAMPLE12345"
`)
	writePIIFixtureFile(t, root, "config/config.yaml", `aws_secret_access_key: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
note: harmless line
`)
	gitTrackInPIIScan(t, root, "src/leak.go", "config/config.yaml")

	result, err := PIIScan(root, PIIScanOptions{})
	if err != nil {
		t.Fatalf("PIIScan failed: %v", err)
	}

	if result.FilesScanned != 2 {
		t.Fatalf("expected 2 files scanned, got %d", result.FilesScanned)
	}
	if len(result.Findings) == 0 {
		t.Fatalf("expected findings, got none")
	}
	if result.Summary.HighestSeverity != PIIScanSeverityCritical {
		t.Fatalf("expected highest severity critical, got %s", result.Summary.HighestSeverity)
	}

	rules := map[string]int{}
	for _, f := range result.Findings {
		rules[f.RuleID]++
	}
	for _, want := range []string{piiScanRuleSSNLiteral, piiScanRuleCreditCardLiteral, piiScanRuleSecretKeyAssign, piiScanRuleEmailLiteral} {
		if rules[want] == 0 {
			t.Fatalf("expected at least one finding with rule %s, got %v", want, rules)
		}
	}

	for i := 1; i < len(result.Findings); i++ {
		prev := piiScanSeverityRank(result.Findings[i-1].Severity)
		cur := piiScanSeverityRank(result.Findings[i].Severity)
		if cur > prev {
			t.Fatalf("findings not sorted by severity desc at index %d: %s before %s", i, result.Findings[i-1].Severity, result.Findings[i].Severity)
		}
	}
}

func TestPIIScanNoFindingsOnCleanRepo(t *testing.T) {
	root := t.TempDir()
	initGitRepoForPIIScan(t, root)

	writePIIFixtureFile(t, root, "src/clean.go", `package src

const greeting = "hello world"
`)
	gitTrackInPIIScan(t, root, "src/clean.go")

	result, err := PIIScan(root, PIIScanOptions{})
	if err != nil {
		t.Fatalf("PIIScan failed: %v", err)
	}
	if result.Summary.TotalFindings != 0 {
		t.Fatalf("expected zero findings, got %d (%+v)", result.Summary.TotalFindings, result.Findings)
	}
	if result.Summary.HighestSeverity != PIIScanSeverityNone {
		t.Fatalf("expected highest severity none, got %s", result.Summary.HighestSeverity)
	}
	if result.ThresholdExceeded {
		t.Fatalf("expected ThresholdExceeded=false on clean repo, got true")
	}
}

func TestPIIScanFlagsSensitiveWorkingTreeFiles(t *testing.T) {
	root := t.TempDir()
	initGitRepoForPIIScan(t, root)
	writePIIFixtureFile(t, root, "README.md", "# project\n")
	gitTrackInPIIScan(t, root, "README.md")

	writePIIFixtureFile(t, root, ".env", "API_KEY=hunter2\n")
	writePIIFixtureFile(t, root, ".env.production", "API_KEY=prod-secret\n")
	writePIIFixtureFile(t, root, "deploy/server.pem", "-----BEGIN PRIVATE KEY-----\nfake\n-----END PRIVATE KEY-----\n")
	writePIIFixtureFile(t, root, "credentials.json", `{"key":"abc"}`+"\n")

	result, err := PIIScan(root, PIIScanOptions{})
	if err != nil {
		t.Fatalf("PIIScan failed: %v", err)
	}

	want := map[string]string{
		".env":             piiScanRuleEnvFile,
		".env.production":  piiScanRuleEnvFile,
		"deploy/server.pem": piiScanRulePrivateKeyFile,
		"credentials.json":  piiScanRuleCredentialFile,
	}
	got := map[string]string{}
	for _, f := range result.Findings {
		if f.Category == piiScanCategorySensitiveFile {
			got[f.File] = f.RuleID
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sensitive file findings mismatch\nwant: %v\ngot:  %v", want, got)
	}
	if result.Summary.HighestSeverity != PIIScanSeverityHigh {
		t.Fatalf("expected highest severity high, got %s", result.Summary.HighestSeverity)
	}
}

func TestPIIScanSkipsExcludedDirectories(t *testing.T) {
	root := t.TempDir()
	initGitRepoForPIIScan(t, root)

	writePIIFixtureFile(t, root, "vendor/github.com/example/lib.go", `const ssn = "111-22-3333"`+"\n")
	writePIIFixtureFile(t, root, "node_modules/leaky/index.js", `const card = "4111-1111-1111-1111"`+"\n")
	writePIIFixtureFile(t, root, "third_party/data.csv", "header\n")
	writePIIFixtureFile(t, root, "src/clean.go", "package src\n")
	gitTrackInPIIScan(t, root,
		"vendor/github.com/example/lib.go",
		"node_modules/leaky/index.js",
		"third_party/data.csv",
		"src/clean.go",
	)

	result, err := PIIScan(root, PIIScanOptions{})
	if err != nil {
		t.Fatalf("PIIScan failed: %v", err)
	}
	if result.FilesScanned != 1 {
		t.Fatalf("expected 1 scannable file (only src/clean.go), got %d", result.FilesScanned)
	}
	for _, f := range result.Findings {
		if strings.HasPrefix(f.File, "vendor/") || strings.HasPrefix(f.File, "node_modules/") || strings.HasPrefix(f.File, "third_party/") {
			t.Fatalf("excluded path leaked into findings: %+v", f)
		}
	}
}

func TestPIIScanFailOnThresholdExceeded(t *testing.T) {
	root := t.TempDir()
	initGitRepoForPIIScan(t, root)
	writePIIFixtureFile(t, root, "src/contact.go", `package src

const ContactEmail = "support@example.com"
`)
	gitTrackInPIIScan(t, root, "src/contact.go")

	medResult, err := PIIScan(root, PIIScanOptions{FailOn: PIIScanSeverityMedium})
	if err != nil {
		t.Fatalf("PIIScan medium failed: %v", err)
	}
	if !medResult.ThresholdExceeded {
		t.Fatalf("expected threshold exceeded for medium, got false (highest=%s)", medResult.Summary.HighestSeverity)
	}

	critResult, err := PIIScan(root, PIIScanOptions{FailOn: PIIScanSeverityCritical})
	if err != nil {
		t.Fatalf("PIIScan critical failed: %v", err)
	}
	if critResult.ThresholdExceeded {
		t.Fatalf("expected threshold NOT exceeded for critical (only medium finding), got true")
	}
	if critResult.FailOn != PIIScanSeverityCritical {
		t.Fatalf("expected FailOn critical, got %s", critResult.FailOn)
	}
}

func TestPIIScanRejectsUnknownSeverity(t *testing.T) {
	root := t.TempDir()
	initGitRepoForPIIScan(t, root)
	writePIIFixtureFile(t, root, "README.md", "# x\n")
	gitTrackInPIIScan(t, root, "README.md")

	if _, err := PIIScan(root, PIIScanOptions{FailOn: "loud"}); err == nil {
		t.Fatalf("expected error for unsupported severity")
	} else if !strings.Contains(err.Error(), "unsupported severity") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestPIIScanIsDeterministicAcrossInvocations(t *testing.T) {
	root := t.TempDir()
	initGitRepoForPIIScan(t, root)
	writePIIFixtureFile(t, root, "src/a.go", "package src\nconst e = \"alice@example.com\"\nconst s = \"123-45-6789\"\n")
	writePIIFixtureFile(t, root, "src/b.go", "package src\nconst c = \"4111-1111-1111-1111\"\n")
	gitTrackInPIIScan(t, root, "src/a.go", "src/b.go")
	writePIIFixtureFile(t, root, ".env", "KEY=v\n")

	first, err := PIIScan(root, PIIScanOptions{})
	if err != nil {
		t.Fatalf("PIIScan first run failed: %v", err)
	}
	second, err := PIIScan(root, PIIScanOptions{})
	if err != nil {
		t.Fatalf("PIIScan second run failed: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("PIIScan results are not deterministic\nfirst:  %+v\nsecond: %+v", first, second)
	}
}
