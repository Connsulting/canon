package canon

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeTestFlakinessOptionsDefaultsAndValidation(t *testing.T) {
	normalized, err := normalizeTestFlakinessOptions(TestFlakinessOptions{})
	if err != nil {
		t.Fatalf("normalizeTestFlakinessOptions failed: %v", err)
	}
	if normalized.Runs != testFlakinessDefaultRuns {
		t.Fatalf("expected default runs %d, got %d", testFlakinessDefaultRuns, normalized.Runs)
	}
	if !reflect.DeepEqual(normalized.Packages, []string{"./..."}) {
		t.Fatalf("unexpected default packages: %+v", normalized.Packages)
	}

	normalized, err = normalizeTestFlakinessOptions(TestFlakinessOptions{
		Runs:     5,
		Packages: []string{" ./pkg/... ", "./pkg/...", "./cmd/...", " ./cmd/... "},
	})
	if err != nil {
		t.Fatalf("normalizeTestFlakinessOptions with explicit values failed: %v", err)
	}
	expected := []string{"./cmd/...", "./pkg/..."}
	if !reflect.DeepEqual(normalized.Packages, expected) {
		t.Fatalf("unexpected normalized package list: got=%+v want=%+v", normalized.Packages, expected)
	}

	if _, err := normalizeTestFlakinessOptions(TestFlakinessOptions{Runs: 1}); err == nil || !strings.Contains(err.Error(), "--runs must be at least 2") {
		t.Fatalf("expected runs validation error, got: %v", err)
	}
}

func TestParseGoTestJSONTerminalActionsIgnoresNoiseAndCapturesTerminalActions(t *testing.T) {
	raw := strings.Join([]string{
		"this is not json",
		`{"Action":"run","Package":"example.com/repo/pkg","Test":"TestStable"}`,
		`{"Action":"pass","Package":"example.com/repo/pkg","Test":"TestStable"}`,
		`{"Action":"run","Package":"example.com/repo/pkg","Test":"TestFlaky"}`,
		`{"Action":"fail","Package":"example.com/repo/pkg","Test":"TestFlaky"}`,
		`{"Action":"pass","Package":"example.com/repo/pkg","Test":"TestFlaky"}`,
		`{"Action":"skip","Package":"example.com/repo/pkg","Test":"TestSkipped"}`,
		`{"Action":"output","Package":"example.com/repo/pkg","Test":"TestStable"}`,
		`{"Action":"pass","Package":"example.com/repo/pkg"}`,
	}, "\n")

	parsed, err := parseGoTestJSONTerminalActions([]byte(raw))
	if err != nil {
		t.Fatalf("parseGoTestJSONTerminalActions failed: %v", err)
	}

	expected := map[testFlakinessKey]testFlakinessOutcome{
		{Package: "example.com/repo/pkg", Test: "TestStable"}:  testFlakinessOutcomePass,
		{Package: "example.com/repo/pkg", Test: "TestFlaky"}:   testFlakinessOutcomePass,
		{Package: "example.com/repo/pkg", Test: "TestSkipped"}: testFlakinessOutcomeSkip,
	}
	if !reflect.DeepEqual(parsed.Outcomes, expected) {
		t.Fatalf("unexpected terminal actions: got=%+v want=%+v", parsed.Outcomes, expected)
	}
	if len(parsed.FailedPackagesWithoutOutcomes) != 0 {
		t.Fatalf("did not expect failed packages without outcomes: %+v", parsed.FailedPackagesWithoutOutcomes)
	}
}

func TestParseGoTestJSONTerminalActionsRequiresParsableJSON(t *testing.T) {
	if _, err := parseGoTestJSONTerminalActions([]byte("not-json\n")); err == nil || !strings.Contains(err.Error(), "no parsable JSON events") {
		t.Fatalf("expected parse error for non-json output, got: %v", err)
	}
}

func TestParseGoTestJSONTerminalActionsTracksFailedPackagesWithoutOutcomes(t *testing.T) {
	raw := strings.Join([]string{
		`{"Action":"pass","Package":"example.com/repo/pkg","Test":"TestStable"}`,
		`{"Action":"fail","Package":"example.com/repo/broken"}`,
	}, "\n")

	parsed, err := parseGoTestJSONTerminalActions([]byte(raw))
	if err != nil {
		t.Fatalf("parseGoTestJSONTerminalActions failed: %v", err)
	}

	if len(parsed.FailedPackagesWithoutOutcomes) != 1 {
		t.Fatalf("expected one failed package without outcomes, got %+v", parsed.FailedPackagesWithoutOutcomes)
	}
	if parsed.FailedPackagesWithoutOutcomes[0] != "example.com/repo/broken" {
		t.Fatalf("unexpected failed package list: %+v", parsed.FailedPackagesWithoutOutcomes)
	}
}

func TestApplyRunOutcomesAndBuildResultSummaryDeterministic(t *testing.T) {
	aggregated := make(map[testFlakinessKey]*testFlakinessCounts)

	applyTestFlakinessRunOutcomes(aggregated, map[testFlakinessKey]testFlakinessOutcome{
		{Package: "pkg/b", Test: "TestFlakyZ"}:     testFlakinessOutcomePass,
		{Package: "pkg/a", Test: "TestStablePass"}: testFlakinessOutcomePass,
		{Package: "pkg/a", Test: "TestStableFail"}: testFlakinessOutcomeFail,
		{Package: "pkg/c", Test: "TestSkipOnly"}:   testFlakinessOutcomeSkip,
	})
	applyTestFlakinessRunOutcomes(aggregated, map[testFlakinessKey]testFlakinessOutcome{
		{Package: "pkg/b", Test: "TestFlakyZ"}:     testFlakinessOutcomeFail,
		{Package: "pkg/a", Test: "TestStablePass"}: testFlakinessOutcomePass,
		{Package: "pkg/a", Test: "TestStableFail"}: testFlakinessOutcomeFail,
		{Package: "pkg/c", Test: "TestSkipOnly"}:   testFlakinessOutcomeSkip,
	})

	result := buildTestFlakinessResult("/tmp/repo", normalizedTestFlakinessOptions{
		Runs:     2,
		Packages: []string{"./..."},
	}, aggregated)

	if result.Summary.TotalTests != 4 {
		t.Fatalf("expected total tests 4, got %d", result.Summary.TotalTests)
	}
	if result.Summary.FlakyTests != 1 {
		t.Fatalf("expected flaky tests 1, got %d", result.Summary.FlakyTests)
	}
	if result.Summary.StablePassingTests != 1 {
		t.Fatalf("expected stable passing tests 1, got %d", result.Summary.StablePassingTests)
	}
	if result.Summary.StableFailingTests != 1 {
		t.Fatalf("expected stable failing tests 1, got %d", result.Summary.StableFailingTests)
	}
	if result.Summary.SkipOnlyTests != 1 {
		t.Fatalf("expected skip-only tests 1, got %d", result.Summary.SkipOnlyTests)
	}

	if len(result.Findings) != 1 {
		t.Fatalf("expected one flaky finding, got %d", len(result.Findings))
	}
	finding := result.Findings[0]
	if finding.Package != "pkg/b" || finding.Test != "TestFlakyZ" {
		t.Fatalf("unexpected flaky finding ordering/content: %+v", finding)
	}
	if finding.Outcomes.Pass != 1 || finding.Outcomes.Fail != 1 || finding.Outcomes.Skip != 0 {
		t.Fatalf("unexpected flaky finding outcomes: %+v", finding.Outcomes)
	}
	if finding.RunsObserved != 2 {
		t.Fatalf("expected runs observed 2, got %d", finding.RunsObserved)
	}
}

func TestTestFlakinessReturnsErrorOnPartialPackageFailure(t *testing.T) {
	root := t.TempDir()

	goMod := `module example.com/partial

go 1.22
`
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("failed writing go.mod: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(root, "healthy"), 0o755); err != nil {
		t.Fatalf("failed creating healthy package: %v", err)
	}
	healthyTest := `package healthy

import "testing"

func TestHealthy(t *testing.T) {}
`
	if err := os.WriteFile(filepath.Join(root, "healthy", "healthy_test.go"), []byte(healthyTest), 0o644); err != nil {
		t.Fatalf("failed writing healthy test: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(root, "broken"), 0o755); err != nil {
		t.Fatalf("failed creating broken package: %v", err)
	}
	brokenTest := `package broken

import "testing"

func TestBroken(t *testing.T) {
	_ = doesNotCompile
}
`
	if err := os.WriteFile(filepath.Join(root, "broken", "broken_test.go"), []byte(brokenTest), 0o644); err != nil {
		t.Fatalf("failed writing broken test: %v", err)
	}

	_, err := TestFlakiness(root, TestFlakinessOptions{Runs: 2})
	if err == nil {
		t.Fatalf("expected partial package failure to return an error")
	}
	if !strings.Contains(err.Error(), "without test-level outcomes") {
		t.Fatalf("expected partial package failure guard error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "example.com/partial/broken") {
		t.Fatalf("expected broken package name in error, got: %v", err)
	}
}
