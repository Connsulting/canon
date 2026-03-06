package canon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPrivacyPolicyTextValidationErrors(t *testing.T) {
	root := t.TempDir()

	if _, _, err := loadPrivacyPolicyText(root, ""); err == nil || !strings.Contains(err.Error(), "requires --policy-file") {
		t.Fatalf("expected missing policy-file error, got: %v", err)
	}

	if _, _, err := loadPrivacyPolicyText(root, "missing-policy.md"); err == nil || !strings.Contains(err.Error(), "policy file not found") {
		t.Fatalf("expected not found error, got: %v", err)
	}

	emptyPolicy := filepath.Join(root, "policy.md")
	if err := os.WriteFile(emptyPolicy, []byte("\n\t\n"), 0o644); err != nil {
		t.Fatalf("failed writing empty policy fixture: %v", err)
	}
	if _, _, err := loadPrivacyPolicyText(root, emptyPolicy); err == nil || !strings.Contains(err.Error(), "policy file is empty") {
		t.Fatalf("expected empty policy error, got: %v", err)
	}
}

func TestResolvePrivacyCheckScopePathsRejectsEscapesAndMissingPaths(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatalf("failed creating src directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "keep.go"), []byte("package src\n"), 0o644); err != nil {
		t.Fatalf("failed writing keep.go: %v", err)
	}

	if _, err := resolvePrivacyCheckScopePaths(root, []string{"../outside.go"}); err == nil || !strings.Contains(err.Error(), "escapes repository root") {
		t.Fatalf("expected escape-path error, got: %v", err)
	}

	if _, err := resolvePrivacyCheckScopePaths(root, []string{"src/missing.go"}); err == nil || !strings.Contains(err.Error(), "code path not found") {
		t.Fatalf("expected missing path error, got: %v", err)
	}
}

func TestCollectPrivacyCheckCodeContextFiltersIgnoredBinarySymlinkAndLargeFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatalf("failed creating src directory: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("src/ignored.go\n"), 0o644); err != nil {
		t.Fatalf("failed writing .gitignore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "keep.go"), []byte("package src\n\nfunc keep() {}\n"), 0o644); err != nil {
		t.Fatalf("failed writing keep.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "ignored.go"), []byte("package src\n\nfunc ignored() {}\n"), 0o644); err != nil {
		t.Fatalf("failed writing ignored.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "image.png"), []byte("not-image"), 0o644); err != nil {
		t.Fatalf("failed writing image.png: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "binary.dat"), []byte{0, 1, 2, 3}, 0o644); err != nil {
		t.Fatalf("failed writing binary.dat: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "large.go"), []byte(strings.Repeat("A", 200)), 0o644); err != nil {
		t.Fatalf("failed writing large.go: %v", err)
	}

	symlinkPath := filepath.Join(root, "src", "link.go")
	symlinkCreated := true
	if err := os.Symlink(filepath.Join(root, "src", "keep.go"), symlinkPath); err != nil {
		symlinkCreated = false
	}

	context, err := collectPrivacyCheckCodeContext(root, []string{"src"}, 16*1024, 128)
	if err != nil {
		t.Fatalf("collectPrivacyCheckCodeContext failed: %v", err)
	}

	if context.IncludedFiles != 1 {
		t.Fatalf("expected exactly one included file, got %d", context.IncludedFiles)
	}
	if !strings.Contains(context.Context, "## File: src/keep.go") {
		t.Fatalf("expected keep.go in context, got:\n%s", context.Context)
	}

	excludedPaths := []string{"src/ignored.go", "src/image.png", "src/binary.dat", "src/large.go"}
	if symlinkCreated {
		excludedPaths = append(excludedPaths, "src/link.go")
	}
	for _, path := range excludedPaths {
		if strings.Contains(context.Context, path) {
			t.Fatalf("did not expect excluded path %s in context", path)
		}
	}

	if context.ExcludedFiles < len(excludedPaths) {
		t.Fatalf("expected at least %d excluded files, got %d", len(excludedPaths), context.ExcludedFiles)
	}
}

func TestCollectPrivacyCheckCodeContextExplicitFileScopeBypassesIgnore(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatalf("failed creating src directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("src/ignored.go\n"), 0o644); err != nil {
		t.Fatalf("failed writing .gitignore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "ignored.go"), []byte("package src\n\nfunc ignored() {}\n"), 0o644); err != nil {
		t.Fatalf("failed writing ignored.go: %v", err)
	}

	context, err := collectPrivacyCheckCodeContext(root, []string{"src/ignored.go"}, 4*1024, 4*1024)
	if err != nil {
		t.Fatalf("collectPrivacyCheckCodeContext failed: %v", err)
	}
	if context.IncludedFiles != 1 {
		t.Fatalf("expected 1 included file, got %d", context.IncludedFiles)
	}
	if !strings.Contains(context.Context, "## File: src/ignored.go") {
		t.Fatalf("expected explicitly scoped ignored file in context")
	}
}

func TestParseAIPrivacyCheckResponseHandlesWrappedJSONAndRejectsInvalid(t *testing.T) {
	root := t.TempDir()
	responsePath := filepath.Join(root, "privacy-response.txt")
	wrapped := strings.TrimSpace(`
model output
{
  "model": "fixture-model",
  "findings": [
    {
      "claim": "We encrypt personal data.",
      "status": "supported",
      "severity": "low",
      "reason": "Encryption helper exists.",
      "evidence_paths": ["internal/crypto/encrypt.go"],
      "evidence_snippets": ["Encrypt(data)"]
    }
  ]
}
trailing text
`)
	if err := os.WriteFile(responsePath, []byte(wrapped), 0o644); err != nil {
		t.Fatalf("failed writing wrapped response: %v", err)
	}

	parsed, err := parseAIPrivacyCheckResponse(root, responsePath)
	if err != nil {
		t.Fatalf("parseAIPrivacyCheckResponse failed: %v", err)
	}
	if parsed.Model != "fixture-model" || len(parsed.Findings) != 1 {
		t.Fatalf("unexpected parsed response: %+v", parsed)
	}

	invalidPath := filepath.Join(root, "invalid-response.txt")
	if err := os.WriteFile(invalidPath, []byte("not json"), 0o644); err != nil {
		t.Fatalf("failed writing invalid response fixture: %v", err)
	}
	if _, err := parseAIPrivacyCheckResponse(root, invalidPath); err == nil || !strings.Contains(err.Error(), "invalid AI privacy-check response JSON") {
		t.Fatalf("expected invalid JSON parse error, got: %v", err)
	}
}

func TestNormalizePrivacyCheckFindingsDedupAndSortsDeterministically(t *testing.T) {
	root := t.TempDir()
	absEvidence := filepath.Join(root, "src", "handler.go")

	in := []aiPrivacyCheckFinding{
		{
			Claim:            " Users can delete account data ",
			Status:           "contradicted",
			Severity:         "high",
			Reason:           " Retention logic keeps deleted user rows. ",
			EvidencePaths:    []string{"./src/handler.go", absEvidence, "./src/handler.go"},
			EvidenceSnippets: []string{"deleted_at is ignored", "deleted_at is ignored"},
		},
		{
			Claim:            "Users can delete account data",
			Status:           "contradicted",
			Severity:         "high",
			Reason:           "Retention logic keeps deleted user rows.",
			EvidencePaths:    []string{"src/handler.go"},
			EvidenceSnippets: []string{"deleted_at is ignored"},
		},
		{
			Claim:    "Data is encrypted at rest",
			Status:   "supported",
			Severity: "none",
			Reason:   "",
		},
		{
			Claim:    "Third-party processors are listed",
			Status:   "unknown",
			Severity: "none",
			Reason:   "",
		},
		{
			Claim:    "   ",
			Status:   "supported",
			Severity: "low",
			Reason:   "ignored empty claim",
		},
	}

	out := normalizePrivacyCheckFindings(in, root)
	if len(out) != 3 {
		payload, _ := json.MarshalIndent(out, "", "  ")
		t.Fatalf("expected 3 normalized findings, got %d:\n%s", len(out), string(payload))
	}

	if out[0].Status != PrivacyCheckStatusContradicted || out[0].Severity != PrivacyCheckSeverityHigh {
		t.Fatalf("expected highest severity contradicted finding first, got %+v", out[0])
	}
	if len(out[0].EvidencePaths) != 1 || out[0].EvidencePaths[0] != "src/handler.go" {
		t.Fatalf("unexpected normalized evidence paths: %+v", out[0].EvidencePaths)
	}
	if out[0].ClaimID == "" {
		t.Fatalf("expected generated claim id on contradicted finding")
	}

	if out[1].Status != PrivacyCheckStatusUnverifiable || out[1].Severity != PrivacyCheckSeverityMedium {
		t.Fatalf("expected defaulted unverifiable medium finding second, got %+v", out[1])
	}
	if !strings.Contains(strings.ToLower(out[1].Reason), "no direct") {
		t.Fatalf("expected default unverifiable reason, got %q", out[1].Reason)
	}

	if out[2].Status != PrivacyCheckStatusSupported || out[2].Severity != PrivacyCheckSeverityNone {
		t.Fatalf("expected supported none finding last, got %+v", out[2])
	}
	if !strings.Contains(strings.ToLower(out[2].Reason), "consistent") {
		t.Fatalf("expected default supported reason, got %q", out[2].Reason)
	}
}

func TestParsePrivacyCheckSeverity(t *testing.T) {
	severity, err := parsePrivacyCheckSeverity(" Medium ")
	if err != nil {
		t.Fatalf("expected medium severity to parse, got error: %v", err)
	}
	if severity != PrivacyCheckSeverityMedium {
		t.Fatalf("expected medium severity, got %s", severity)
	}

	if _, err := parsePrivacyCheckSeverity("urgent"); err == nil {
		t.Fatalf("expected invalid severity error")
	}
}

func TestPrivacyCheckThresholdEvaluation(t *testing.T) {
	result := PrivacyCheckResult{
		Summary: PrivacyCheckSummary{HighestSeverity: PrivacyCheckSeverityHigh},
	}

	if !privacyCheckExceedsThreshold(result, PrivacyCheckSeverityLow) {
		t.Fatalf("expected high severity to exceed low threshold")
	}
	if !privacyCheckExceedsThreshold(result, PrivacyCheckSeverityHigh) {
		t.Fatalf("expected high severity to exceed high threshold")
	}
	if privacyCheckExceedsThreshold(result, PrivacyCheckSeverityCritical) {
		t.Fatalf("did not expect high severity to exceed critical threshold")
	}
	if privacyCheckExceedsThreshold(result, PrivacyCheckSeverityNone) {
		t.Fatalf("did not expect none threshold to fail")
	}
}
