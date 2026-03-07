package canon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSchemaEvolutionDetectsHeuristicsAndSummarizesDeterministically(t *testing.T) {
	root := t.TempDir()
	writeSchemaEvolutionMigrationFile(t, root, "db/migrations/001_core_changes.sql", `ALTER TABLE widgets DROP COLUMN old_flag;
ALTER TABLE widgets ADD COLUMN status TEXT NOT NULL;
`)
	writeSchemaEvolutionMigrationFile(t, root, "db/migrations/002_breaking_changes.sql", `DROP TABLE legacy_widgets;
ALTER TABLE widgets ALTER COLUMN name TYPE VARCHAR(128);
ALTER TABLE widgets RENAME COLUMN code TO legacy_code;
ALTER TABLE widgets ALTER COLUMN owner_id SET NOT NULL;
`)

	result, err := SchemaEvolution(root, SchemaEvolutionOptions{})
	if err != nil {
		t.Fatalf("SchemaEvolution failed: %v", err)
	}

	if result.MigrationFileCount != 2 {
		t.Fatalf("expected 2 migration files, got %d", result.MigrationFileCount)
	}
	if result.StatementCount != 6 {
		t.Fatalf("expected 6 statements, got %d", result.StatementCount)
	}
	if len(result.Findings) != 6 {
		t.Fatalf("expected 6 findings, got %d", len(result.Findings))
	}

	expectedOrder := []struct {
		severity SchemaEvolutionSeverity
		ruleID   string
		file     string
		line     int
	}{
		{SchemaEvolutionSeverityHigh, schemaEvolutionRuleDropColumn, "db/migrations/001_core_changes.sql", 1},
		{SchemaEvolutionSeverityHigh, schemaEvolutionRuleDropTable, "db/migrations/002_breaking_changes.sql", 1},
		{SchemaEvolutionSeverityHigh, schemaEvolutionRuleAlterColumnType, "db/migrations/002_breaking_changes.sql", 2},
		{SchemaEvolutionSeverityHigh, schemaEvolutionRuleRenameColumn, "db/migrations/002_breaking_changes.sql", 3},
		{SchemaEvolutionSeverityMedium, schemaEvolutionRuleAddNotNullNoDefault, "db/migrations/001_core_changes.sql", 2},
		{SchemaEvolutionSeverityMedium, schemaEvolutionRuleSetNotNull, "db/migrations/002_breaking_changes.sql", 4},
	}

	for i, expected := range expectedOrder {
		finding := result.Findings[i]
		if finding.Severity != expected.severity || finding.RuleID != expected.ruleID || finding.File != expected.file || finding.Line != expected.line {
			t.Fatalf("unexpected finding at index %d: got=%+v expected=%+v", i, finding, expected)
		}
	}

	if result.Summary.TotalFindings != 6 {
		t.Fatalf("expected summary total findings 6, got %d", result.Summary.TotalFindings)
	}
	if result.Summary.HighestSeverity != SchemaEvolutionSeverityHigh {
		t.Fatalf("expected highest severity high, got %s", result.Summary.HighestSeverity)
	}
	if result.Summary.FindingsBySeverity.High != 4 || result.Summary.FindingsBySeverity.Medium != 2 || result.Summary.FindingsBySeverity.Low != 0 || result.Summary.FindingsBySeverity.Critical != 0 {
		t.Fatalf("unexpected severity counts: %+v", result.Summary.FindingsBySeverity)
	}
}

func TestSchemaEvolutionNoFindingsForSafeMigrations(t *testing.T) {
	root := t.TempDir()
	writeSchemaEvolutionMigrationFile(t, root, "db/migrations/001_safe.sql", `CREATE TABLE users (
  id BIGINT PRIMARY KEY
);
ALTER TABLE users ADD COLUMN created_at TIMESTAMP NOT NULL DEFAULT NOW();
`)

	result, err := SchemaEvolution(root, SchemaEvolutionOptions{})
	if err != nil {
		t.Fatalf("SchemaEvolution failed: %v", err)
	}

	if result.MigrationFileCount != 1 {
		t.Fatalf("expected 1 migration file, got %d", result.MigrationFileCount)
	}
	if result.StatementCount != 2 {
		t.Fatalf("expected 2 statements, got %d", result.StatementCount)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings, got %d", len(result.Findings))
	}
	if result.Summary.HighestSeverity != SchemaEvolutionSeverityNone {
		t.Fatalf("expected highest severity none, got %s", result.Summary.HighestSeverity)
	}
}

func TestSchemaEvolutionThresholdBehavior(t *testing.T) {
	root := t.TempDir()
	writeSchemaEvolutionMigrationFile(t, root, "db/migrations/001_threshold.sql", `ALTER TABLE users ADD COLUMN email TEXT NOT NULL;`)

	mediumResult, err := SchemaEvolution(root, SchemaEvolutionOptions{FailOn: SchemaEvolutionSeverityMedium})
	if err != nil {
		t.Fatalf("SchemaEvolution with medium fail-on failed: %v", err)
	}
	if mediumResult.Summary.HighestSeverity != SchemaEvolutionSeverityMedium {
		t.Fatalf("expected highest severity medium, got %s", mediumResult.Summary.HighestSeverity)
	}
	if !mediumResult.ThresholdExceeded {
		t.Fatalf("expected threshold to be exceeded for medium fail-on")
	}
	if mediumResult.FailOn != SchemaEvolutionSeverityMedium {
		t.Fatalf("expected fail-on medium in result, got %s", mediumResult.FailOn)
	}

	highResult, err := SchemaEvolution(root, SchemaEvolutionOptions{FailOn: SchemaEvolutionSeverityHigh})
	if err != nil {
		t.Fatalf("SchemaEvolution with high fail-on failed: %v", err)
	}
	if highResult.ThresholdExceeded {
		t.Fatalf("did not expect threshold to be be exceeded for high fail-on")
	}
}

func TestSchemaEvolutionErrorPaths(t *testing.T) {
	missingRoot := filepath.Join(t.TempDir(), "missing-root")
	if _, err := SchemaEvolution(missingRoot, SchemaEvolutionOptions{}); err == nil {
		t.Fatalf("expected error for missing root")
	}

	root := t.TempDir()
	if _, err := SchemaEvolution(root, SchemaEvolutionOptions{FailOn: SchemaEvolutionSeverity("urgent")}); err == nil {
		t.Fatalf("expected error for unsupported fail-on severity")
	} else if !strings.Contains(err.Error(), "unsupported severity") {
		t.Fatalf("unexpected unsupported severity error: %v", err)
	}
}

func writeSchemaEvolutionMigrationFile(t *testing.T, root string, relPath string, content string) {
	t.Helper()
	absPath := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatalf("failed creating migration directory: %v", err)
	}
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed writing migration file: %v", err)
	}
}
