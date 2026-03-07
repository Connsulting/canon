package canon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivacyPolicyCheckFromResponseNormalizesSummarizesAndSorts(t *testing.T) {
	root := t.TempDir()
	policyPath := writePrivacyFixtureFile(t, root, "docs/privacy-policy.md", "# Privacy\n\nWe delete user data on request.")
	writePrivacyFixtureFile(t, root, "internal/store.go", "package internal\n\nfunc store() {}\n")

	responsePath := filepath.Join(root, "privacy-response.json")
	response := map[string]any{
		"model": "codex-headless",
		"findings": []map[string]any{
			{
				"claim_id":          "claim-delete",
				"claim":             "User data is deleted on request",
				"status":            "contradicted",
				"severity":          "critical",
				"reason":            "Retention job keeps deleted records forever.",
				"evidence_paths":    []string{"internal/store.go"},
				"evidence_snippets": []string{"records are retained indefinitely"},
			},
			{
				"claim_id":          "claim-delete",
				"claim":             "User data is deleted on request",
				"status":            "contradicted",
				"severity":          "critical",
				"reason":            "Retention job keeps deleted records forever.",
				"evidence_paths":    []string{"internal/store.go"},
				"evidence_snippets": []string{"records are retained indefinitely"},
			},
			{
				"claim_id":          "claim-minimal",
				"claim":             "Only minimal data is collected",
				"status":            "supported",
				"severity":          "low",
				"reason":            "Only account id is persisted.",
				"evidence_paths":    []string{"internal/store.go"},
				"evidence_snippets": []string{"account_id"},
			},
			{
				"claim_id":          "",
				"claim":             "Encryption at rest is documented",
				"status":            "unknown",
				"severity":          "",
				"reason":            "",
				"evidence_paths":    []string{"./internal/store.go", "internal/store.go"},
				"evidence_snippets": []string{"  encrypted  ", "encrypted"},
			},
			{
				"claim_id":          "empty-claim",
				"claim":             "",
				"status":            "supported",
				"severity":          "low",
				"reason":            "ignored",
				"evidence_paths":    []string{},
				"evidence_snippets": []string{},
			},
		},
	}
	writePrivacyResponse(t, responsePath, response)

	result, err := PrivacyPolicyCheck(root, PrivacyPolicyCheckOptions{
		PolicyFile:   policyPath,
		CodePaths:    []string{"internal"},
		AIMode:       "from-response",
		ResponseFile: responsePath,
	})
	if err != nil {
		t.Fatalf("PrivacyPolicyCheck failed: %v", err)
	}

	if result.Summary.TotalFindings != 3 {
		t.Fatalf("expected 3 findings after normalization, got %d", result.Summary.TotalFindings)
	}
	if result.Summary.HighestSeverity != PrivacyPolicySeverityCritical {
		t.Fatalf("expected highest severity critical, got %s", result.Summary.HighestSeverity)
	}
	if result.Summary.FindingsByStatus.Supported != 1 || result.Summary.FindingsByStatus.Contradicted != 1 || result.Summary.FindingsByStatus.Unverifiable != 1 {
		t.Fatalf("unexpected status counts: %+v", result.Summary.FindingsByStatus)
	}
	if result.Summary.FindingsBySeverity.Low != 1 || result.Summary.FindingsBySeverity.Medium != 1 || result.Summary.FindingsBySeverity.Critical != 1 {
		t.Fatalf("unexpected severity counts: %+v", result.Summary.FindingsBySeverity)
	}

	if len(result.Findings) != 3 {
		t.Fatalf("expected 3 findings, got %d", len(result.Findings))
	}
	if result.Findings[0].ClaimID != "claim-delete" {
		t.Fatalf("expected highest severity finding first, got %s", result.Findings[0].ClaimID)
	}
	var normalized PrivacyPolicyFinding
	foundNormalized := false
	for _, finding := range result.Findings {
		if finding.ClaimID == "claim-004" {
			normalized = finding
			foundNormalized = true
			break
		}
	}
	if !foundNormalized {
		t.Fatalf("expected generated claim id claim-004, findings=%+v", result.Findings)
	}
	if normalized.Status != PrivacyPolicyFindingStatusUnverifiable {
		t.Fatalf("expected unknown status to normalize to unverifiable, got %s", normalized.Status)
	}
	if normalized.Severity != PrivacyPolicySeverityMedium {
		t.Fatalf("expected default medium severity for unverifiable finding, got %s", normalized.Severity)
	}
	if normalized.Reason != "No adjudication reason provided" {
		t.Fatalf("unexpected default reason: %q", normalized.Reason)
	}
	if len(normalized.EvidencePaths) != 1 || normalized.EvidencePaths[0] != "internal/store.go" {
		t.Fatalf("expected normalized evidence paths, got %+v", normalized.EvidencePaths)
	}

	if result.ScannedFiles == 0 || result.ContextFiles == 0 || result.ContextBytes == 0 {
		t.Fatalf("expected non-zero scan metrics, got scanned=%d contextFiles=%d contextBytes=%d", result.ScannedFiles, result.ContextFiles, result.ContextBytes)
	}
}

func TestPrivacyPolicyCheckValidationErrors(t *testing.T) {
	root := t.TempDir()
	writePrivacyFixtureFile(t, root, "internal/main.go", "package internal\n")

	_, err := PrivacyPolicyCheck(root, PrivacyPolicyCheckOptions{})
	if err == nil || !strings.Contains(err.Error(), "requires --policy-file") {
		t.Fatalf("expected missing policy file error, got %v", err)
	}

	policyPath := writePrivacyFixtureFile(t, root, "docs/privacy-policy.md", "policy")

	_, err = PrivacyPolicyCheck(root, PrivacyPolicyCheckOptions{
		PolicyFile: policyPath,
		AIMode:     "off",
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported privacy-check ai mode") {
		t.Fatalf("expected invalid ai mode error, got %v", err)
	}

	_, err = PrivacyPolicyCheck(root, PrivacyPolicyCheckOptions{
		PolicyFile: policyPath,
		AIMode:     "from-response",
	})
	if err == nil || !strings.Contains(err.Error(), "requires --response-file") {
		t.Fatalf("expected missing response file error, got %v", err)
	}

	_, err = PrivacyPolicyCheck(root, PrivacyPolicyCheckOptions{
		PolicyFile:   policyPath,
		CodePaths:    []string{"../outside"},
		AIMode:       "from-response",
		ResponseFile: filepath.Join(root, "response.json"),
	})
	if err == nil || !strings.Contains(err.Error(), "must be inside root") {
		t.Fatalf("expected code-path validation error, got %v", err)
	}
}

func TestPrivacyPolicyCheckThresholdBehavior(t *testing.T) {
	root := t.TempDir()
	policyPath := writePrivacyFixtureFile(t, root, "privacy.md", "policy")
	writePrivacyFixtureFile(t, root, "internal/main.go", "package internal\n")

	responsePath := filepath.Join(root, "privacy-response.json")
	writePrivacyResponse(t, responsePath, map[string]any{
		"model": "test",
		"findings": []map[string]any{
			{
				"claim_id":          "claim-1",
				"claim":             "Delete data on request",
				"status":            "contradicted",
				"severity":          "high",
				"reason":            "Records are retained",
				"evidence_paths":    []string{"internal/main.go"},
				"evidence_snippets": []string{"retain forever"},
			},
		},
	})

	medium, err := PrivacyPolicyCheck(root, PrivacyPolicyCheckOptions{
		PolicyFile:   policyPath,
		AIMode:       "from-response",
		ResponseFile: responsePath,
		FailOn:       PrivacyPolicySeverityMedium,
	})
	if err != nil {
		t.Fatalf("PrivacyPolicyCheck with fail-on medium failed: %v", err)
	}
	if !medium.ThresholdExceeded {
		t.Fatalf("expected threshold exceeded for fail-on medium")
	}
	if medium.FailOn != PrivacyPolicySeverityMedium {
		t.Fatalf("expected fail-on medium in result, got %s", medium.FailOn)
	}

	critical, err := PrivacyPolicyCheck(root, PrivacyPolicyCheckOptions{
		PolicyFile:   policyPath,
		AIMode:       "from-response",
		ResponseFile: responsePath,
		FailOn:       PrivacyPolicySeverityCritical,
	})
	if err != nil {
		t.Fatalf("PrivacyPolicyCheck with fail-on critical failed: %v", err)
	}
	if critical.ThresholdExceeded {
		t.Fatalf("did not expect threshold exceeded for fail-on critical")
	}

	_, err = PrivacyPolicyCheck(root, PrivacyPolicyCheckOptions{
		PolicyFile:   policyPath,
		AIMode:       "from-response",
		ResponseFile: responsePath,
		FailOn:       PrivacyPolicySeverity("urgent"),
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported severity") {
		t.Fatalf("expected invalid fail-on severity error, got %v", err)
	}
}

func TestPrivacyPolicyCheckMaxFileBytesValidation(t *testing.T) {
	root := t.TempDir()
	policyPath := writePrivacyFixtureFile(t, root, "privacy.md", "policy")
	writePrivacyFixtureFile(t, root, "internal/main.go", "package internal\n\nfunc main() {}\n")

	responsePath := filepath.Join(root, "privacy-response.json")
	writePrivacyResponse(t, responsePath, map[string]any{
		"model":    "test",
		"findings": []map[string]any{},
	})

	_, err := PrivacyPolicyCheck(root, PrivacyPolicyCheckOptions{
		PolicyFile:   policyPath,
		CodePaths:    []string{"internal"},
		ContextLimit: 0,
		MaxFileBytes: 64,
		AIMode:       "from-response",
		ResponseFile: responsePath,
	})
	if err != nil {
		t.Fatalf("unexpected error with defaults: %v", err)
	}

	_, err = PrivacyPolicyCheck(root, PrivacyPolicyCheckOptions{
		PolicyFile:   policyPath,
		CodePaths:    []string{"internal"},
		MaxFileBytes: 8,
		AIMode:       "from-response",
		ResponseFile: responsePath,
	})
	if err == nil || !strings.Contains(err.Error(), "no eligible text files") {
		t.Fatalf("expected max-file-bytes exclusion error, got %v", err)
	}
}

func TestDecodeAIPrivacyPolicyResponse(t *testing.T) {
	payload := `prefix text
{
  "model": "test",
  "findings": []
}
suffix text`

	response, err := decodeAIPrivacyPolicyResponse([]byte(payload))
	if err != nil {
		t.Fatalf("decodeAIPrivacyPolicyResponse failed: %v", err)
	}
	if response.Model != "test" {
		t.Fatalf("expected model test, got %s", response.Model)
	}

	if _, err := decodeAIPrivacyPolicyResponse([]byte("not-json")); err == nil {
		t.Fatalf("expected invalid json error")
	}
}

func TestParsePrivacyPolicySeverity(t *testing.T) {
	severity, err := parsePrivacyPolicySeverity(" HIGH ")
	if err != nil {
		t.Fatalf("parsePrivacyPolicySeverity failed: %v", err)
	}
	if severity != PrivacyPolicySeverityHigh {
		t.Fatalf("expected high severity, got %s", severity)
	}

	if _, err := parsePrivacyPolicySeverity("urgent"); err == nil {
		t.Fatalf("expected invalid severity error")
	}
}

func writePrivacyFixtureFile(t *testing.T, root string, rel string, content string) string {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed creating directory for %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed writing fixture %s: %v", rel, err)
	}
	return path
}

func writePrivacyResponse(t *testing.T, path string, payload map[string]any) {
	t.Helper()
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal response payload: %v", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("failed to write response payload: %v", err)
	}
}
