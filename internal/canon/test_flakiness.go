package canon

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	testFlakinessDefaultRuns = 4
)

type testFlakinessOutcome string

const (
	testFlakinessOutcomePass testFlakinessOutcome = "pass"
	testFlakinessOutcomeFail testFlakinessOutcome = "fail"
	testFlakinessOutcomeSkip testFlakinessOutcome = "skip"
)

type testFlakinessKey struct {
	Package string
	Test    string
}

type testFlakinessCounts struct {
	Passes       int
	Fails        int
	Skips        int
	RunsObserved int
}

type normalizedTestFlakinessOptions struct {
	Runs        int
	Packages    []string
	FailOnFlaky bool
}

type goTestJSONEvent struct {
	Action  string `json:"Action"`
	Package string `json:"Package"`
	Test    string `json:"Test"`
}

func TestFlakiness(root string, opts TestFlakinessOptions) (TestFlakinessResult, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return TestFlakinessResult{}, err
	}

	normalized, err := normalizeTestFlakinessOptions(opts)
	if err != nil {
		return TestFlakinessResult{}, err
	}

	aggregated := make(map[testFlakinessKey]*testFlakinessCounts)
	for run := 0; run < normalized.Runs; run++ {
		runOutcomes, err := runGoTestJSONTerminalActions(absRoot, normalized.Packages)
		if err != nil {
			return TestFlakinessResult{}, err
		}
		applyTestFlakinessRunOutcomes(aggregated, runOutcomes)
	}

	result := buildTestFlakinessResult(absRoot, normalized, aggregated)
	if normalized.FailOnFlaky {
		result.FailGate = &TestFlakinessFailGate{
			Enabled:  true,
			Exceeded: testFlakinessExceedsThreshold(result),
		}
	}

	return result, nil
}

func normalizeTestFlakinessOptions(opts TestFlakinessOptions) (normalizedTestFlakinessOptions, error) {
	runs := opts.Runs
	if runs == 0 {
		runs = testFlakinessDefaultRuns
	}
	if runs < 2 {
		return normalizedTestFlakinessOptions{}, errors.New("--runs must be at least 2")
	}

	packages := normalizeTestFlakinessPackages(opts.Packages)
	if len(packages) == 0 {
		packages = []string{"./..."}
	}

	return normalizedTestFlakinessOptions{
		Runs:        runs,
		Packages:    packages,
		FailOnFlaky: opts.FailOnFlaky,
	}, nil
}

func normalizeTestFlakinessPackages(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	packages := make([]string, 0, len(values))
	for _, raw := range values {
		parts := strings.Split(raw, ",")
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				continue
			}
			if _, ok := seen[trimmed]; ok {
				continue
			}
			seen[trimmed] = struct{}{}
			packages = append(packages, trimmed)
		}
	}

	sort.Strings(packages)
	return packages
}

func runGoTestJSONTerminalActions(root string, packages []string) (map[testFlakinessKey]testFlakinessOutcome, error) {
	args := []string{"test", "-json", "-count=1"}
	args = append(args, packages...)

	cmd := exec.Command("go", args...)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()

	terminalActions, parseErr := parseGoTestJSONTerminalActions(output)
	if parseErr != nil {
		if err != nil {
			return nil, fmt.Errorf("go test -json failed: %w\n%s", err, strings.TrimSpace(string(output)))
		}
		return nil, parseErr
	}
	if len(terminalActions) == 0 {
		if err != nil {
			return nil, fmt.Errorf("go test -json produced no terminal test events: %w\n%s", err, strings.TrimSpace(string(output)))
		}
		return nil, errors.New("go test -json produced no terminal test events")
	}

	return terminalActions, nil
}

func parseGoTestJSONTerminalActions(raw []byte) (map[testFlakinessKey]testFlakinessOutcome, error) {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	terminalActions := make(map[testFlakinessKey]testFlakinessOutcome)
	sawJSONEvent := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event goTestJSONEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		sawJSONEvent = true

		pkg := strings.TrimSpace(event.Package)
		testName := strings.TrimSpace(event.Test)
		if pkg == "" || testName == "" {
			continue
		}

		action := testFlakinessOutcome(strings.ToLower(strings.TrimSpace(event.Action)))
		switch action {
		case testFlakinessOutcomePass, testFlakinessOutcomeFail, testFlakinessOutcomeSkip:
			terminalActions[testFlakinessKey{Package: pkg, Test: testName}] = action
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if !sawJSONEvent {
		return nil, errors.New("go test -json returned no parsable JSON events")
	}

	return terminalActions, nil
}

func applyTestFlakinessRunOutcomes(
	aggregated map[testFlakinessKey]*testFlakinessCounts,
	runOutcomes map[testFlakinessKey]testFlakinessOutcome,
) {
	for key, outcome := range runOutcomes {
		counts, ok := aggregated[key]
		if !ok {
			counts = &testFlakinessCounts{}
			aggregated[key] = counts
		}

		counts.RunsObserved++
		switch outcome {
		case testFlakinessOutcomePass:
			counts.Passes++
		case testFlakinessOutcomeFail:
			counts.Fails++
		case testFlakinessOutcomeSkip:
			counts.Skips++
		}
	}
}

func buildTestFlakinessResult(
	root string,
	opts normalizedTestFlakinessOptions,
	aggregated map[testFlakinessKey]*testFlakinessCounts,
) TestFlakinessResult {
	keys := make([]testFlakinessKey, 0, len(aggregated))
	for key := range aggregated {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Package == keys[j].Package {
			return keys[i].Test < keys[j].Test
		}
		return keys[i].Package < keys[j].Package
	})

	findings := make([]TestFlakinessFinding, 0)
	summary := TestFlakinessSummary{TotalTests: len(keys)}

	for _, key := range keys {
		counts := aggregated[key]
		switch {
		case counts.Passes > 0 && counts.Fails > 0:
			summary.FlakyTests++
			findings = append(findings, TestFlakinessFinding{
				Package: key.Package,
				Test:    key.Test,
				Outcomes: TestFlakinessOutcomeCounts{
					Pass: counts.Passes,
					Fail: counts.Fails,
					Skip: counts.Skips,
				},
				RunsObserved: counts.RunsObserved,
			})
		case counts.Passes > 0:
			summary.StablePassingTests++
		case counts.Fails > 0:
			summary.StableFailingTests++
		case counts.Skips > 0:
			summary.SkipOnlyTests++
		}
	}

	return TestFlakinessResult{
		Root:     root,
		Runs:     opts.Runs,
		Packages: opts.Packages,
		Findings: findings,
		Summary:  summary,
	}
}

func testFlakinessExceedsThreshold(result TestFlakinessResult) bool {
	return result.Summary.FlakyTests > 0
}
