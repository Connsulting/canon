package canon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivacyPolicyCheckFromResponseNormalizesAndSummarizes(t *testing.T) {
	root := t.TempDir()
	writePrivacyPolicyFile(t, root, "policy/privacy.md", `# Privacy Policy
We retain account logs for 30 days.
Users must explicitly opt in before analytics tracking.
`)
	writePrivacyPolicyFile(t, root, "src/service.go", `package service

func Track() {}
`)

	responsePath := writePrivacyPolicyResponse(t, root, map[string]any{
		"model": "codex-headless",
		"findings": []map[string]any{
			{
				"claim_key": "retention-window",
				"claim":     "Account logs are retained for 30 days.",
				"status":    "contradicted",
				"severity":  "high",
				"message":   "log retention setting is unbounded in config",
				"evidence":  []string{"config/privacy.go:12", "config/privacy.go:12"},
			},
			{
				"claim_key": "retention-window",
				"claim":     "Account logs are retained for 30 days.",
				"status":    "contradicted",
				"severity":  "high",
				"message":   "log retention setting is unbounded in config",
				"evidence":  []string{"config/privacy.go:12", "config/privacy.go:12"},
			},
			{
				"claim_key": "tracking-consent",
				"claim":     "Analytics tracking requires explicit opt-in.",
				"status":    "supported",
				"severity":  "",
				"message":   "",
				"evidence":  []string{"src/consent.go:44"},
			},
			{
				"claim_key": "data-minimization",
				"claim":     "Collected telemetry is minimized to necessity.",
				"status":    "needs-review",
				"severity":  "",
				"message":   "",
				"evidence":  []string{"src/telemetry.go:28"},
			},
			{
				"claim_key": "",
				"claim":     "",
				"status":    "invalid-status",
				"severity":  "critical",
				"message":   "",
				"evidence":  []string{},
			},
		},
	})

	result, err := PrivacyPolicyCheck(root, PrivacyPolicyOptions{
		PolicyFile:   "policy/privacy.md",
		CodePaths:    []string{"src"},
		ContextLimit: 4096,
		MaxFileBytes: 4096,
		AIMode:       "from-response",
		ResponseFile: responsePath,
		FailOn:       PrivacyPolicySeverityMedium,
	})
	if err != nil {
		t.Fatalf("PrivacyPolicyCheck failed: %v", err)
	}

	if result.CodePathCount != 1 {
		t.Fatalf("expected code path count 1, got %d", result.CodePathCount)
	}
	if result.ContextFileCount != 1 {
		t.Fatalf("expected context file count 1, got %d", result.ContextFileCount)
	}
	if result.ContextBytes == 0 {
		t.Fatalf("expected non-zero context bytes")
	}

	if len(result.Findings) != 3 {
		t.Fatalf("expected 3 normalized findings, got %d", len(result.Findings))
	}

	if result.Findings[0].ClaimKey != "retention-window" || result.Findings[0].Severity != PrivacyPolicySeverityHigh {
		t.Fatalf("expected highest severity finding first, got %+v", result.Findings[0])
	}
	if result.Findings[1].ClaimKey != "data-minimization" || result.Findings[1].Status != PrivacyPolicyStatusUnknown {
		t.Fatalf("expected unknown finding second, got %+v", result.Findings[1])
	}
	if result.Findings[2].ClaimKey != "tracking-consent" || result.Findings[2].Status != PrivacyPolicyStatusSupported {
		t.Fatalf("expected supported finding third, got %+v", result.Findings[2])
	}

	if result.Summary.TotalFindings != 3 {
		t.Fatalf("expected total findings 3, got %d", result.Summary.TotalFindings)
	}
	if result.Summary.HighestSeverity != PrivacyPolicySeverityHigh {
		t.Fatalf("expected highest severity high, got %s", result.Summary.HighestSeverity)
	}
	if result.Summary.FindingsByStatus.Contradicted != 1 || result.Summary.FindingsByStatus.Unknown != 1 || result.Summary.FindingsByStatus.Supported != 1 {
		t.Fatalf("unexpected status counts: %+v", result.Summary.FindingsByStatus)
	}
	if result.Summary.FindingsBySeverity.High != 1 || result.Summary.FindingsBySeverity.Low != 1 {
		t.Fatalf("unexpected severity counts: %+v", result.Summary.FindingsBySeverity)
	}
	if result.FailOn != PrivacyPolicySeverityMedium {
		t.Fatalf("expected fail-on medium, got %s", result.FailOn)
	}
	if !result.ThresholdExceeded {
		t.Fatalf("expected threshold to be exceeded")
	}
}

func TestPrivacyPolicyCheckValidationAndErrorPaths(t *testing.T) {
	root := t.TempDir()
	writePrivacyPolicyFile(t, root, "privacy.md", "policy body")

	if _, err := PrivacyPolicyCheck(root, PrivacyPolicyOptions{}); err == nil {
		t.Fatalf("expected error for missing policy file")
	} else if !strings.Contains(err.Error(), "requires --policy-file") {
		t.Fatalf("unexpected missing policy error: %v", err)
	}

	if _, err := PrivacyPolicyCheck(root, PrivacyPolicyOptions{
		PolicyFile: "privacy.md",
		AIMode:     "off",
	}); err == nil {
		t.Fatalf("expected error for unsupported mode")
	} else if !strings.Contains(err.Error(), "unsupported privacy-check ai mode") {
		t.Fatalf("unexpected mode error: %v", err)
	}

	if _, err := PrivacyPolicyCheck(root, PrivacyPolicyOptions{
		PolicyFile: "privacy.md",
		AIMode:     "from-response",
	}); err == nil {
		t.Fatalf("expected error for missing --response-file")
	} else if !strings.Contains(err.Error(), "requires --response-file") {
		t.Fatalf("unexpected response-file error: %v", err)
	}

	if _, err := PrivacyPolicyCheck(root, PrivacyPolicyOptions{
		PolicyFile: "privacy.md",
		AIMode:     "from-response",
		FailOn:     PrivacyPolicySeverity("urgent"),
		ResponseFile: writePrivacyPolicyResponse(t, root, map[string]any{
			"model":    "codex-headless",
			"findings": []map[string]any{},
		}),
	}); err == nil {
		t.Fatalf("expected error for invalid fail-on severity")
	} else if !strings.Contains(err.Error(), "unsupported severity") {
		t.Fatalf("unexpected fail-on error: %v", err)
	}

	outside := filepath.Join(root, "..")
	if _, err := PrivacyPolicyCheck(root, PrivacyPolicyOptions{
		PolicyFile: "privacy.md",
		CodePaths:  []string{outside},
		AIMode:     "from-response",
		ResponseFile: writePrivacyPolicyResponse(t, root, map[string]any{
			"model":    "codex-headless",
			"findings": []map[string]any{},
		}),
	}); err == nil {
		t.Fatalf("expected error for code path outside root")
	} else if !strings.Contains(err.Error(), "code path must be inside root") {
		t.Fatalf("unexpected outside-root error: %v", err)
	}

	if _, err := PrivacyPolicyCheck(root, PrivacyPolicyOptions{
		PolicyFile: "privacy.md",
		AIMode:     "from-response",
		ResponseFile: writePrivacyPolicyResponse(t, root, map[string]any{
			"model": "codex-headless",
		}),
	}); err == nil {
		t.Fatalf("expected error for missing findings field")
	} else if !strings.Contains(err.Error(), "missing findings") {
		t.Fatalf("unexpected missing findings error: %v", err)
	}

	if _, err := PrivacyPolicyCheck(root, PrivacyPolicyOptions{
		PolicyFile: "privacy.md",
		AIMode:     "from-response",
		ResponseFile: writePrivacyPolicyResponse(t, root, map[string]any{
			"model": "codex-headless",
			"findings": []map[string]any{
				{
					"claim_key": "x",
					"claim":     "x",
					"status":    "invalid",
					"severity":  "high",
					"message":   "x",
					"evidence":  []string{"x"},
				},
			},
		}),
	}); err == nil {
		t.Fatalf("expected error for unparseable findings")
	} else if !strings.Contains(err.Error(), "no valid findings") {
		t.Fatalf("unexpected unparseable findings error: %v", err)
	}
}

func TestDecodeAIPrivacyPolicyResponseErrors(t *testing.T) {
	if _, err := decodeAIPrivacyPolicyResponse([]byte("not-json")); err == nil {
		t.Fatalf("expected invalid JSON error")
	}
}

func TestCollectPrivacyPolicyContextHonorsLimits(t *testing.T) {
	root := t.TempDir()
	writePrivacyPolicyFile(t, root, "src/a.go", "package a\n\nconst A = 1\n")
	writePrivacyPolicyFile(t, root, "src/b.go", strings.Repeat("b", 4096))
	writePrivacyPolicyFile(t, root, "src/empty.txt", "   \n\n")

	files, context, err := collectPrivacyPolicyContext(root, []string{filepath.Join(root, "src")}, 256, 1024)
	if err != nil {
		t.Fatalf("collectPrivacyPolicyContext failed: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("expected at least one context file")
	}
	if len(context) == 0 {
		t.Fatalf("expected non-empty context")
	}

	foundTruncated := false
	for _, file := range files {
		if file.Path == "src/b.go" {
			foundTruncated = file.Truncated
		}
	}
	if !foundTruncated {
		t.Fatalf("expected src/b.go to be truncated")
	}
}

func TestPrivacyPolicyThresholdComparison(t *testing.T) {
	result := PrivacyPolicyResult{
		Summary: PrivacyPolicySummary{
			HighestSeverity: PrivacyPolicySeverityHigh,
		},
	}
	if !privacyPolicyExceedsThreshold(result, PrivacyPolicySeverityMedium) {
		t.Fatalf("expected threshold exceeded for medium")
	}
	if privacyPolicyExceedsThreshold(result, PrivacyPolicySeverityCritical) {
		t.Fatalf("did not expect threshold exceeded for critical")
	}
}

func writePrivacyPolicyFile(t *testing.T, root string, relPath string, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("failed creating directory for %s: %v", relPath, err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("failed writing %s: %v", relPath, err)
	}
}

func writePrivacyPolicyResponse(t *testing.T, root string, payload map[string]any) string {
	t.Helper()
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("failed marshaling response payload: %v", err)
	}
	path := filepath.Join(root, "privacy-response.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("failed writing response payload: %v", err)
	}
	return path
}
