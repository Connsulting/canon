package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"canon/internal/canon"
)

type blameJSONContract struct {
	Query   string                  `json:"query"`
	Found   bool                    `json:"found"`
	Results []blameJSONContractItem `json:"results"`
}

type blameJSONContractItem struct {
	SpecID        string   `json:"spec_id"`
	Title         string   `json:"title"`
	Domain        string   `json:"domain"`
	Confidence    string   `json:"confidence"`
	Created       string   `json:"created"`
	RelevantLines []string `json:"relevant_lines"`
}

type checkJSONContract struct {
	Passed         bool                      `json:"passed"`
	TotalSpecs     int                       `json:"total_specs"`
	TotalConflicts int                       `json:"total_conflicts"`
	Conflicts      []checkJSONContractRecord `json:"conflicts"`
	ReportPaths    []string                  `json:"report_paths,omitempty"`
}

type checkJSONContractRecord struct {
	SpecA        string `json:"spec_a"`
	TitleA       string `json:"title_a"`
	SpecB        string `json:"spec_b"`
	TitleB       string `json:"title_b"`
	Domain       string `json:"domain"`
	StatementKey string `json:"statement_key"`
	LineA        string `json:"line_a"`
	LineB        string `json:"line_b"`
}

type dependencyRiskJSONContract struct {
	Root              string                             `json:"root"`
	GoModPath         string                             `json:"go_mod_path"`
	GoSumPath         string                             `json:"go_sum_path"`
	GoSumPresent      bool                               `json:"go_sum_present"`
	DependencyCount   int                                `json:"dependency_count"`
	Findings          []dependencyRiskJSONContractRecord `json:"findings"`
	Summary           dependencyRiskJSONContractSummary  `json:"summary"`
	FailOn            string                             `json:"fail_on,omitempty"`
	ThresholdExceeded bool                               `json:"threshold_exceeded"`
}

type dependencyRiskJSONContractRecord struct {
	RuleID   string `json:"rule_id"`
	Category string `json:"category"`
	Severity string `json:"severity"`
	Module   string `json:"module,omitempty"`
	Version  string `json:"version,omitempty"`
	Replace  string `json:"replace,omitempty"`
	Message  string `json:"message"`
}

type dependencyRiskJSONContractSummary struct {
	TotalFindings       int                                  `json:"total_findings"`
	SecurityFindings    int                                  `json:"security_findings"`
	MaintenanceFindings int                                  `json:"maintenance_findings"`
	HighestSeverity     string                               `json:"highest_severity"`
	FindingsBySeverity  dependencyRiskJSONContractSeverities `json:"findings_by_severity"`
}

type dependencyRiskJSONContractSeverities struct {
	Low      int `json:"low"`
	Medium   int `json:"medium"`
	High     int `json:"high"`
	Critical int `json:"critical"`
}

type schemaEvolutionJSONContract struct {
	Root               string                              `json:"root"`
	MigrationFileCount int                                 `json:"migration_file_count"`
	StatementCount     int                                 `json:"statement_count"`
	Findings           []schemaEvolutionJSONContractRecord `json:"findings"`
	Summary            schemaEvolutionJSONContractSummary  `json:"summary"`
	FailOn             string                              `json:"fail_on,omitempty"`
	ThresholdExceeded  bool                                `json:"threshold_exceeded"`
}

type schemaEvolutionJSONContractRecord struct {
	RuleID    string `json:"rule_id"`
	Severity  string `json:"severity"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Statement string `json:"statement"`
	Message   string `json:"message"`
}

type schemaEvolutionJSONContractSummary struct {
	TotalFindings      int                                   `json:"total_findings"`
	HighestSeverity    string                                `json:"highest_severity"`
	FindingsBySeverity schemaEvolutionJSONContractSeverities `json:"findings_by_severity"`
}

type schemaEvolutionJSONContractSeverities struct {
	Low      int `json:"low"`
	Medium   int `json:"medium"`
	High     int `json:"high"`
	Critical int `json:"critical"`
}

func TestAPIContractBlameJSONStrict(t *testing.T) {
	root := t.TempDir()
	if err := canon.EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	ingestSpecForBlame(t, root, "api-a1", "API Pagination", "api", "2026-01-15T10:30:00Z", []string{"api"},
		"All list endpoints must return paginated responses.\nDefault page size must be 25 items.",
	)

	responsePath := filepath.Join(root, "blame-response.json")
	response := `{
  "model": "codex-headless",
  "found": true,
  "results": [
    {
      "spec_id": "api-a1",
      "confidence": "high",
      "relevant_lines": [
        "All list endpoints must return paginated responses.",
        "Default page size must be 25 items."
      ]
    }
  ]
}`
	if err := os.WriteFile(responsePath, []byte(response), 0o644); err != nil {
		t.Fatalf("failed writing response file: %v", err)
	}

	out := captureStdout(t, func() {
		if err := run([]string{
			"blame",
			"--root", root,
			"--json",
			"--response-file", responsePath,
			"API list endpoints are paginated",
		}); err != nil {
			t.Fatalf("blame command failed: %v", err)
		}
	})

	payload := decodeStrictJSON[blameJSONContract](t, out)
	if strings.TrimSpace(payload.Query) == "" || !payload.Found {
		t.Fatalf("unexpected blame payload: %+v", payload)
	}
	if len(payload.Results) != 1 {
		t.Fatalf("expected one blame result, got %d", len(payload.Results))
	}
	result := payload.Results[0]
	if strings.TrimSpace(result.SpecID) == "" ||
		strings.TrimSpace(result.Title) == "" ||
		strings.TrimSpace(result.Domain) == "" ||
		strings.TrimSpace(result.Confidence) == "" ||
		strings.TrimSpace(result.Created) == "" {
		t.Fatalf("missing required blame result fields: %+v", result)
	}
	if len(result.RelevantLines) == 0 {
		t.Fatalf("expected relevant lines in blame result")
	}
}

func TestAPIContractBlameRequiresQuery(t *testing.T) {
	err := run([]string{"blame", "--json"})
	if err == nil {
		t.Fatalf("expected blame command to fail without query")
	}
	if !strings.Contains(err.Error(), "blame requires a behavior description") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAPIContractBlameRejectsInvalidFlag(t *testing.T) {
	err := run([]string{"blame", "--invalid-flag", "query"})
	if err == nil {
		t.Fatalf("expected blame command to reject invalid flag")
	}
	if !strings.Contains(err.Error(), "flag provided but not defined") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAPIContractCheckJSONStrict(t *testing.T) {
	root := t.TempDir()
	if err := canon.EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	writeCLICheckSpec(t, root, "safe-a", "API Auth", "api", "API endpoints must require authentication.")
	writeCLICheckSpec(t, root, "safe-b", "Billing Retry", "billing", "Billing retries must happen daily.")
	responsePath := writeCLICheckResponse(t, root, map[string]any{
		"model": "codex-headless",
		"conflict_check": map[string]any{
			"has_conflicts": false,
			"summary":       "No conflicts.",
			"conflicts":     []map[string]any{},
		},
	})

	out := captureStdout(t, func() {
		if err := run([]string{"check", "--root", root, "--json", "--response-file", responsePath}); err != nil {
			t.Fatalf("check command failed: %v", err)
		}
	})

	payload := decodeStrictJSON[checkJSONContract](t, out)
	if !payload.Passed || payload.TotalSpecs != 2 || payload.TotalConflicts != 0 {
		t.Fatalf("unexpected check payload: %+v", payload)
	}
}

func TestAPIContractCheckConflictJSONAndNonZeroExit(t *testing.T) {
	root := t.TempDir()
	if err := canon.EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	writeCLICheckSpec(t, root, "conf-a", "Auth Required", "api", "API endpoints must require authentication.")
	writeCLICheckSpec(t, root, "conf-b", "Auth Optional", "api", "API endpoints must not require authentication.")
	responsePath := writeCLICheckResponse(t, root, map[string]any{
		"model": "codex-headless",
		"conflict_check": map[string]any{
			"has_conflicts": true,
			"summary":       "Found conflict.",
			"conflicts": []map[string]any{
				{
					"spec_a":        "conf-a",
					"spec_b":        "conf-b",
					"domain":        "api",
					"statement_key": "api auth required",
					"line_a":        "API endpoints must require authentication.",
					"line_b":        "API endpoints must not require authentication.",
					"reason":        "Direct contradiction.",
				},
			},
		},
	})

	var commandErr error
	out := captureStdout(t, func() {
		commandErr = run([]string{"check", "--root", root, "--json", "--response-file", responsePath})
	})
	if commandErr == nil {
		t.Fatalf("expected check command to fail when conflicts are present")
	}
	if !strings.Contains(commandErr.Error(), "check failed:") {
		t.Fatalf("unexpected check error: %v", commandErr)
	}

	payload := decodeStrictJSON[checkJSONContract](t, out)
	if payload.Passed || payload.TotalConflicts != 1 {
		t.Fatalf("unexpected failing check payload: %+v", payload)
	}
}

func TestAPIContractCheckRejectsPositionalArgs(t *testing.T) {
	err := run([]string{"check", "extra"})
	if err == nil {
		t.Fatalf("expected error for check positional arguments")
	}
	if !strings.Contains(err.Error(), "check does not accept positional arguments") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAPIContractCheckRejectsInvalidFlag(t *testing.T) {
	err := run([]string{"check", "--invalid-flag"})
	if err == nil {
		t.Fatalf("expected check command to reject invalid flag")
	}
	if !strings.Contains(err.Error(), "flag provided but not defined") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAPIContractDepsRiskJSONStrict(t *testing.T) {
	root := t.TempDir()
	writeCLIGoMod(t, root, `module example.com/contract-pass

go 1.22

require github.com/acme/stable v1.2.3
`)
	if err := os.WriteFile(filepath.Join(root, "go.sum"), []byte(""), 0o644); err != nil {
		t.Fatalf("failed writing go.sum: %v", err)
	}

	out := captureStdout(t, func() {
		if err := run([]string{"deps-risk", "--root", root, "--json"}); err != nil {
			t.Fatalf("deps-risk command failed: %v", err)
		}
	})

	payload := decodeStrictJSON[dependencyRiskJSONContract](t, out)
	if payload.DependencyCount != 1 {
		t.Fatalf("expected dependency count 1, got %d", payload.DependencyCount)
	}
	if strings.TrimSpace(payload.Root) == "" ||
		strings.TrimSpace(payload.GoModPath) == "" ||
		strings.TrimSpace(payload.GoSumPath) == "" {
		t.Fatalf("missing required deps-risk paths: %+v", payload)
	}
	if payload.Summary.TotalFindings != 0 || payload.Summary.HighestSeverity != "none" {
		t.Fatalf("unexpected deps-risk summary: %+v", payload.Summary)
	}
	if payload.ThresholdExceeded {
		t.Fatalf("did not expect threshold_exceeded without fail-on")
	}
}

func TestAPIContractDepsRiskThresholdJSONAndNonZeroExit(t *testing.T) {
	root := t.TempDir()
	writeCLIGoMod(t, root, `module example.com/contract-threshold

go 1.22

require github.com/acme/risky v0.9.0
`)

	var commandErr error
	out := captureStdout(t, func() {
		commandErr = run([]string{"deps-risk", "--root", root, "--json", "--fail-on", "medium"})
	})
	if commandErr == nil {
		t.Fatalf("expected deps-risk to fail when threshold is exceeded")
	}
	if !strings.Contains(commandErr.Error(), "dependency risk threshold failed") {
		t.Fatalf("unexpected deps-risk error: %v", commandErr)
	}

	payload := decodeStrictJSON[dependencyRiskJSONContract](t, out)
	if payload.FailOn != "medium" {
		t.Fatalf("expected fail_on=medium, got %q", payload.FailOn)
	}
	if !payload.ThresholdExceeded {
		t.Fatalf("expected threshold_exceeded=true")
	}
}

func TestAPIContractDepsRiskRejectsPositionalArgs(t *testing.T) {
	err := run([]string{"deps-risk", "extra"})
	if err == nil {
		t.Fatalf("expected error for deps-risk positional arguments")
	}
	if !strings.Contains(err.Error(), "deps-risk does not accept positional arguments") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAPIContractDepsRiskRejectsInvalidFlag(t *testing.T) {
	err := run([]string{"deps-risk", "--invalid-flag"})
	if err == nil {
		t.Fatalf("expected deps-risk command to reject invalid flag")
	}
	if !strings.Contains(err.Error(), "flag provided but not defined") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAPIContractSchemaEvolutionJSONStrict(t *testing.T) {
	root := t.TempDir()
	writeCLISchemaMigrationFile(t, root, "db/migrations/001_safe.sql", `CREATE TABLE users (id BIGINT PRIMARY KEY);`)

	out := captureStdout(t, func() {
		if err := run([]string{"schema-evolution", "--root", root, "--json"}); err != nil {
			t.Fatalf("schema-evolution command failed: %v", err)
		}
	})

	payload := decodeStrictJSON[schemaEvolutionJSONContract](t, out)
	if payload.MigrationFileCount != 1 || payload.StatementCount != 1 {
		t.Fatalf("unexpected schema-evolution counts: %+v", payload)
	}
	if strings.TrimSpace(payload.Root) == "" {
		t.Fatalf("missing root in schema-evolution payload")
	}
	if payload.Summary.TotalFindings != 0 || payload.Summary.HighestSeverity != "none" {
		t.Fatalf("unexpected schema-evolution summary: %+v", payload.Summary)
	}
	if payload.ThresholdExceeded {
		t.Fatalf("did not expect threshold_exceeded without fail-on")
	}
}

func TestAPIContractSchemaEvolutionThresholdJSONAndNonZeroExit(t *testing.T) {
	root := t.TempDir()
	writeCLISchemaMigrationFile(t, root, "db/migrations/001_breaking.sql", `DROP TABLE users;`)

	var commandErr error
	out := captureStdout(t, func() {
		commandErr = run([]string{"schema-evolution", "--root", root, "--json", "--fail-on", "medium"})
	})
	if commandErr == nil {
		t.Fatalf("expected schema-evolution to fail when threshold is exceeded")
	}
	if !strings.Contains(commandErr.Error(), "schema evolution threshold failed") {
		t.Fatalf("unexpected schema-evolution error: %v", commandErr)
	}

	payload := decodeStrictJSON[schemaEvolutionJSONContract](t, out)
	if payload.FailOn != "medium" {
		t.Fatalf("expected fail_on=medium, got %q", payload.FailOn)
	}
	if !payload.ThresholdExceeded {
		t.Fatalf("expected threshold_exceeded=true")
	}
}

func TestAPIContractSchemaEvolutionRejectsPositionalArgs(t *testing.T) {
	err := run([]string{"schema-evolution", "extra"})
	if err == nil {
		t.Fatalf("expected error for schema-evolution positional arguments")
	}
	if !strings.Contains(err.Error(), "schema-evolution does not accept positional arguments") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAPIContractSchemaEvolutionRejectsInvalidFlag(t *testing.T) {
	err := run([]string{"schema-evolution", "--invalid-flag"})
	if err == nil {
		t.Fatalf("expected schema-evolution command to reject invalid flag")
	}
	if !strings.Contains(err.Error(), "flag provided but not defined") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAPIContractRoadmapEntropyIsUnsupported(t *testing.T) {
	err := run([]string{"roadmap-entropy", "--json"})
	if err == nil {
		t.Fatalf("expected roadmap-entropy command to be unsupported in this branch")
	}
	if !strings.Contains(err.Error(), "unknown command: roadmap-entropy") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func decodeStrictJSON[T any](t *testing.T, raw string) T {
	t.Helper()

	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()

	var payload T
	if err := decoder.Decode(&payload); err != nil {
		t.Fatalf("failed strict JSON decode: %v\noutput:\n%s", err, raw)
	}

	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		t.Fatalf("unexpected trailing JSON content: %v\noutput:\n%s", err, raw)
	}

	return payload
}
