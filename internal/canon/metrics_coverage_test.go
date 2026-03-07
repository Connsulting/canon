package canon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMetricsCoverageDetectsInstrumentationAndOrdersDeterministically(t *testing.T) {
	root := t.TempDir()

	writeMetricsCoverageGoFile(t, root, "cmd/alpha/main.go", `package main

func cmdAlpha(args []string) error {
	telemetry.EmitMetric("cli.alpha")
	return nil
}

func cmdGamma(args []string) error {
	cliMetrics.WithLabelValues("ok").Inc()
	return nil
}
`)

	writeMetricsCoverageGoFile(t, root, "cmd/beta/main.go", `package main

func cmdBeta(args []string) error {
	return nil
}
`)

	result, err := MetricsCoverage(root, MetricsCoverageOptions{})
	if err != nil {
		t.Fatalf("MetricsCoverage failed: %v", err)
	}

	if result.Summary.TargetCount != 3 {
		t.Fatalf("expected 3 targets, got %d", result.Summary.TargetCount)
	}
	if result.Summary.InstrumentedCount != 2 {
		t.Fatalf("expected 2 instrumented handlers, got %d", result.Summary.InstrumentedCount)
	}
	if result.Summary.MissingCount != 1 {
		t.Fatalf("expected 1 missing handler, got %d", result.Summary.MissingCount)
	}
	if result.Summary.CoveragePercent != 66.67 {
		t.Fatalf("expected coverage 66.67, got %.2f", result.Summary.CoveragePercent)
	}

	if len(result.Findings) != 3 {
		t.Fatalf("expected 3 findings, got %d", len(result.Findings))
	}
	if result.Findings[0].Handler != "cmdAlpha" || !result.Findings[0].Instrumented {
		t.Fatalf("unexpected first finding: %+v", result.Findings[0])
	}
	if result.Findings[1].Handler != "cmdGamma" || !result.Findings[1].Instrumented {
		t.Fatalf("unexpected second finding: %+v", result.Findings[1])
	}
	if result.Findings[2].Handler != "cmdBeta" || result.Findings[2].Instrumented {
		t.Fatalf("unexpected third finding: %+v", result.Findings[2])
	}

	expectedMissing := "cmdBeta (cmd/beta/main.go:3)"
	if len(result.MissingTargets) != 1 || result.MissingTargets[0] != expectedMissing {
		t.Fatalf("unexpected missing targets: %+v", result.MissingTargets)
	}
}

func TestMetricsCoverageNoTargetsUsesFullCoverageBaseline(t *testing.T) {
	root := t.TempDir()
	writeMetricsCoverageGoFile(t, root, "internal/app/main.go", `package app

func Run() {}
`)
	writeMetricsCoverageGoFile(t, root, "cmd/tool/main.go", `package main

func cmdSkip() error {
	return nil
}
`)

	failUnder := 90.0
	result, err := MetricsCoverage(root, MetricsCoverageOptions{FailUnder: &failUnder})
	if err != nil {
		t.Fatalf("MetricsCoverage failed: %v", err)
	}

	if result.Summary.TargetCount != 0 {
		t.Fatalf("expected 0 targets, got %d", result.Summary.TargetCount)
	}
	if result.Summary.CoveragePercent != 100 {
		t.Fatalf("expected 100 coverage for empty target set, got %.2f", result.Summary.CoveragePercent)
	}
	if result.ThresholdExceeded {
		t.Fatalf("did not expect threshold exceed for empty target set")
	}
	if len(result.Findings) != 0 || len(result.MissingTargets) != 0 {
		t.Fatalf("expected no findings for empty target set, got findings=%d missing=%d", len(result.Findings), len(result.MissingTargets))
	}
}

func TestMetricsCoverageInvalidRootPath(t *testing.T) {
	_, err := MetricsCoverage(filepath.Join(t.TempDir(), "missing"), MetricsCoverageOptions{})
	if err == nil {
		t.Fatalf("expected error for missing root path")
	}
	if !strings.Contains(err.Error(), "root path not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMetricsCoverageThresholdLogic(t *testing.T) {
	root := t.TempDir()
	writeMetricsCoverageGoFile(t, root, "cmd/canon/main.go", `package main

func cmdOne(args []string) error {
	metrics.Record("cmd.one")
	return nil
}

func cmdTwo(args []string) error {
	return nil
}
`)

	failUnderHigh := 75.0
	highResult, err := MetricsCoverage(root, MetricsCoverageOptions{FailUnder: &failUnderHigh})
	if err != nil {
		t.Fatalf("MetricsCoverage with fail-under=75 failed: %v", err)
	}
	if !highResult.ThresholdExceeded {
		t.Fatalf("expected threshold exceed for fail-under=75 and coverage %.2f", highResult.Summary.CoveragePercent)
	}

	failUnderEqual := 50.0
	equalResult, err := MetricsCoverage(root, MetricsCoverageOptions{FailUnder: &failUnderEqual})
	if err != nil {
		t.Fatalf("MetricsCoverage with fail-under=50 failed: %v", err)
	}
	if equalResult.ThresholdExceeded {
		t.Fatalf("did not expect threshold exceed at equal coverage/fail-under")
	}
}

func TestParseMetricsCoverageFailUnderValidation(t *testing.T) {
	value, err := parseMetricsCoverageFailUnder("75.555")
	if err != nil {
		t.Fatalf("expected valid threshold parse, got error: %v", err)
	}
	if value != 75.56 {
		t.Fatalf("expected rounded value 75.56, got %.2f", value)
	}

	invalid := []string{"", "abc", "-1", "101"}
	for _, item := range invalid {
		if _, err := parseMetricsCoverageFailUnder(item); err == nil {
			t.Fatalf("expected parse error for %q", item)
		}
	}
}

func writeMetricsCoverageGoFile(t *testing.T, root string, relPath string, content string) {
	t.Helper()
	path := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed creating parent directory for %s: %v", relPath, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed writing file %s: %v", relPath, err)
	}
}
