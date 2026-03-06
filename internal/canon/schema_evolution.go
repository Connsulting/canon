package canon

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	schemaEvolutionRuleDropTable           = "drop-table"
	schemaEvolutionRuleDropColumn          = "drop-column"
	schemaEvolutionRuleRenameColumn        = "rename-column"
	schemaEvolutionRuleAlterColumnType     = "alter-column-type"
	schemaEvolutionRuleAddNotNullNoDefault = "add-not-null-no-default"
	schemaEvolutionRuleSetNotNull          = "set-not-null"
)

var (
	schemaEvolutionMigrationNamePattern = regexp.MustCompile(`^(?:v?\d+__.*|\d{8,}.*|\d+[-_].*)\.sql$`)

	schemaEvolutionDropTablePattern       = regexp.MustCompile(`\bdrop table\b`)
	schemaEvolutionDropColumnPattern      = regexp.MustCompile(`\balter table\b.*\bdrop column\b`)
	schemaEvolutionRenameColumnPattern    = regexp.MustCompile(`\balter table\b.*\brename column\b`)
	schemaEvolutionAlterColumnTypePattern = regexp.MustCompile(`\balter table\b.*\balter column\b.*\btype\b`)
	schemaEvolutionAddNotNullPattern      = regexp.MustCompile(`\balter table\b.*\badd column\b.*\bnot null\b`)
	schemaEvolutionSetNotNullPattern      = regexp.MustCompile(`\balter table\b.*\balter column\b.*\bset not null\b`)
	schemaEvolutionDefaultWordPattern     = regexp.MustCompile(`\bdefault\b`)
)

type schemaEvolutionStatement struct {
	Text  string
	Start int
}

func SchemaEvolution(root string, opts SchemaEvolutionOptions) (SchemaEvolutionResult, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return SchemaEvolutionResult{}, err
	}

	migrationFiles, err := findSchemaEvolutionMigrationFiles(absRoot)
	if err != nil {
		return SchemaEvolutionResult{}, err
	}

	findings := make([]SchemaEvolutionFinding, 0)
	statementCount := 0

	for _, relPath := range migrationFiles {
		absPath := filepath.Join(absRoot, filepath.FromSlash(relPath))
		contentBytes, err := os.ReadFile(absPath)
		if err != nil {
			return SchemaEvolutionResult{}, fmt.Errorf("read migration file %s: %w", filepath.ToSlash(absPath), err)
		}

		sanitized := stripSchemaEvolutionSQLComments(string(contentBytes))
		statements := splitSchemaEvolutionSQLStatements(sanitized)
		statementCount += len(statements)

		for _, statement := range statements {
			normalized := normalizeSchemaEvolutionSQLForMatch(statement.Text)
			if normalized == "" {
				continue
			}

			line := schemaEvolutionLineAt(sanitized, statement.Start)
			snippet := compactSchemaEvolutionStatement(statement.Text)
			findings = appendSchemaEvolutionFindings(findings, relPath, line, snippet, normalized)
		}
	}

	sortSchemaEvolutionFindings(findings)

	result := SchemaEvolutionResult{
		Root:               absRoot,
		MigrationFileCount: len(migrationFiles),
		StatementCount:     statementCount,
		Findings:           findings,
		Summary:            summarizeSchemaEvolutionFindings(findings),
	}

	if strings.TrimSpace(string(opts.FailOn)) != "" {
		failOn, err := parseSchemaEvolutionSeverity(string(opts.FailOn))
		if err != nil {
			return SchemaEvolutionResult{}, err
		}
		if failOn != SchemaEvolutionSeverityNone {
			result.FailOn = failOn
			result.ThresholdExceeded = schemaEvolutionExceedsThreshold(result, failOn)
		}
	}

	return result, nil
}

func findSchemaEvolutionMigrationFiles(root string) ([]string, error) {
	migrationFiles := make([]string, 0)

	err := filepath.WalkDir(root, func(absPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath, err := filepath.Rel(root, absPath)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)

		if relPath == "." {
			return nil
		}
		if entry.IsDir() {
			if shouldSkipDir(relPath) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if strings.ToLower(filepath.Ext(entry.Name())) != ".sql" {
			return nil
		}
		if !isSchemaEvolutionMigrationPath(relPath) {
			return nil
		}

		migrationFiles = append(migrationFiles, relPath)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(migrationFiles)
	return migrationFiles, nil
}

func isSchemaEvolutionMigrationPath(relPath string) bool {
	lowerRel := strings.ToLower(filepath.ToSlash(strings.TrimSpace(relPath)))
	if lowerRel == "" {
		return false
	}
	base := path.Base(lowerRel)

	if schemaEvolutionMigrationNamePattern.MatchString(base) {
		return true
	}

	if strings.Contains(base, "migration") || strings.Contains(base, "migrate") || strings.Contains(base, "schema") || strings.Contains(base, "ddl") {
		return true
	}

	pathTokens := []string{"migration", "migrations", "migrate", "schema", "schemas", "ddl"}
	for _, token := range pathTokens {
		sentinel := "/" + token + "/"
		pathWithDelimiters := "/" + strings.Trim(lowerRel, "/") + "/"
		if strings.Contains(pathWithDelimiters, sentinel) {
			return true
		}
	}

	return false
}

func stripSchemaEvolutionSQLComments(sql string) string {
	src := []byte(sql)
	dst := make([]byte, len(src))
	copy(dst, src)

	inSingleQuote := false
	inDoubleQuote := false
	inBacktickQuote := false

	for i := 0; i < len(dst); i++ {
		c := dst[i]

		if inSingleQuote {
			if c == '\'' {
				if i+1 < len(dst) && dst[i+1] == '\'' {
					i++
				} else {
					inSingleQuote = false
				}
			}
			continue
		}

		if inDoubleQuote {
			if c == '"' {
				if i+1 < len(dst) && dst[i+1] == '"' {
					i++
				} else {
					inDoubleQuote = false
				}
			}
			continue
		}

		if inBacktickQuote {
			if c == '`' {
				if i+1 < len(dst) && dst[i+1] == '`' {
					i++
				} else {
					inBacktickQuote = false
				}
			}
			continue
		}

		switch c {
		case '\'':
			inSingleQuote = true
			continue
		case '"':
			inDoubleQuote = true
			continue
		case '`':
			inBacktickQuote = true
			continue
		}

		if c == '-' && i+1 < len(dst) && dst[i+1] == '-' {
			dst[i] = ' '
			i++
			dst[i] = ' '
			for i+1 < len(dst) && dst[i+1] != '\n' {
				i++
				dst[i] = ' '
			}
			continue
		}

		if c == '/' && i+1 < len(dst) && dst[i+1] == '*' {
			dst[i] = ' '
			i++
			dst[i] = ' '
			for i+1 < len(dst) {
				if dst[i+1] == '*' && i+2 < len(dst) && dst[i+2] == '/' {
					i++
					dst[i] = ' '
					i++
					dst[i] = ' '
					break
				}
				i++
				if dst[i] != '\n' {
					dst[i] = ' '
				}
			}
		}
	}

	return string(dst)
}

func splitSchemaEvolutionSQLStatements(sql string) []schemaEvolutionStatement {
	statements := make([]schemaEvolutionStatement, 0)

	start := 0
	inSingleQuote := false
	inDoubleQuote := false
	inBacktickQuote := false

	for i := 0; i < len(sql); i++ {
		c := sql[i]

		if inSingleQuote {
			if c == '\'' {
				if i+1 < len(sql) && sql[i+1] == '\'' {
					i++
				} else {
					inSingleQuote = false
				}
			}
			continue
		}

		if inDoubleQuote {
			if c == '"' {
				if i+1 < len(sql) && sql[i+1] == '"' {
					i++
				} else {
					inDoubleQuote = false
				}
			}
			continue
		}

		if inBacktickQuote {
			if c == '`' {
				if i+1 < len(sql) && sql[i+1] == '`' {
					i++
				} else {
					inBacktickQuote = false
				}
			}
			continue
		}

		switch c {
		case '\'':
			inSingleQuote = true
			continue
		case '"':
			inDoubleQuote = true
			continue
		case '`':
			inBacktickQuote = true
			continue
		case ';':
			appendSchemaEvolutionStatement(sql, start, i, &statements)
			start = i + 1
		}
	}

	appendSchemaEvolutionStatement(sql, start, len(sql), &statements)
	return statements
}

func appendSchemaEvolutionStatement(sql string, start int, end int, out *[]schemaEvolutionStatement) {
	if end < start {
		return
	}

	statementStart := start
	for statementStart < end && isSchemaEvolutionWhitespace(sql[statementStart]) {
		statementStart++
	}
	if statementStart >= end {
		return
	}

	statementEnd := end
	for statementEnd > statementStart && isSchemaEvolutionWhitespace(sql[statementEnd-1]) {
		statementEnd--
	}
	if statementStart >= statementEnd {
		return
	}

	*out = append(*out, schemaEvolutionStatement{
		Text:  sql[statementStart:statementEnd],
		Start: statementStart,
	})
}

func appendSchemaEvolutionFindings(findings []SchemaEvolutionFinding, file string, line int, statement string, normalizedStatement string) []SchemaEvolutionFinding {
	if schemaEvolutionDropTablePattern.MatchString(normalizedStatement) {
		findings = append(findings, SchemaEvolutionFinding{
			RuleID:    schemaEvolutionRuleDropTable,
			Severity:  SchemaEvolutionSeverityHigh,
			File:      file,
			Line:      line,
			Statement: statement,
			Message:   "DROP TABLE can remove existing data and break dependent code paths",
		})
	}

	if schemaEvolutionDropColumnPattern.MatchString(normalizedStatement) {
		findings = append(findings, SchemaEvolutionFinding{
			RuleID:    schemaEvolutionRuleDropColumn,
			Severity:  SchemaEvolutionSeverityHigh,
			File:      file,
			Line:      line,
			Statement: statement,
			Message:   "ALTER TABLE ... DROP COLUMN can break reads/writes expecting the removed column",
		})
	}

	if schemaEvolutionRenameColumnPattern.MatchString(normalizedStatement) {
		findings = append(findings, SchemaEvolutionFinding{
			RuleID:    schemaEvolutionRuleRenameColumn,
			Severity:  SchemaEvolutionSeverityHigh,
			File:      file,
			Line:      line,
			Statement: statement,
			Message:   "ALTER TABLE ... RENAME COLUMN can break callers still using the old column name",
		})
	}

	if schemaEvolutionAlterColumnTypePattern.MatchString(normalizedStatement) {
		findings = append(findings, SchemaEvolutionFinding{
			RuleID:    schemaEvolutionRuleAlterColumnType,
			Severity:  SchemaEvolutionSeverityHigh,
			File:      file,
			Line:      line,
			Statement: statement,
			Message:   "ALTER TABLE ... ALTER COLUMN ... TYPE can cause cast failures or behavioral regressions",
		})
	}

	if schemaEvolutionAddNotNullPattern.MatchString(normalizedStatement) && !schemaEvolutionDefaultWordPattern.MatchString(normalizedStatement) {
		findings = append(findings, SchemaEvolutionFinding{
			RuleID:    schemaEvolutionRuleAddNotNullNoDefault,
			Severity:  SchemaEvolutionSeverityMedium,
			File:      file,
			Line:      line,
			Statement: statement,
			Message:   "adding a NOT NULL column without DEFAULT can fail on existing rows",
		})
	}

	if schemaEvolutionSetNotNullPattern.MatchString(normalizedStatement) {
		findings = append(findings, SchemaEvolutionFinding{
			RuleID:    schemaEvolutionRuleSetNotNull,
			Severity:  SchemaEvolutionSeverityMedium,
			File:      file,
			Line:      line,
			Statement: statement,
			Message:   "setting NOT NULL can fail if existing rows contain null values",
		})
	}

	return findings
}

func sortSchemaEvolutionFindings(findings []SchemaEvolutionFinding) {
	sort.Slice(findings, func(i, j int) bool {
		left := findings[i]
		right := findings[j]

		leftRank := schemaEvolutionSeverityRank(left.Severity)
		rightRank := schemaEvolutionSeverityRank(right.Severity)
		if leftRank != rightRank {
			return leftRank > rightRank
		}
		if left.File != right.File {
			return left.File < right.File
		}
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		if left.RuleID != right.RuleID {
			return left.RuleID < right.RuleID
		}
		return left.Statement < right.Statement
	})
}

func summarizeSchemaEvolutionFindings(findings []SchemaEvolutionFinding) SchemaEvolutionSummary {
	summary := SchemaEvolutionSummary{
		TotalFindings:      len(findings),
		HighestSeverity:    SchemaEvolutionSeverityNone,
		FindingsBySeverity: SchemaEvolutionSeverityCounts{},
	}

	for _, finding := range findings {
		switch finding.Severity {
		case SchemaEvolutionSeverityLow:
			summary.FindingsBySeverity.Low++
		case SchemaEvolutionSeverityMedium:
			summary.FindingsBySeverity.Medium++
		case SchemaEvolutionSeverityHigh:
			summary.FindingsBySeverity.High++
		case SchemaEvolutionSeverityCritical:
			summary.FindingsBySeverity.Critical++
		}

		if schemaEvolutionSeverityRank(finding.Severity) > schemaEvolutionSeverityRank(summary.HighestSeverity) {
			summary.HighestSeverity = finding.Severity
		}
	}

	return summary
}

func parseSchemaEvolutionSeverity(value string) (SchemaEvolutionSeverity, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "", string(SchemaEvolutionSeverityNone):
		return SchemaEvolutionSeverityNone, nil
	case string(SchemaEvolutionSeverityLow):
		return SchemaEvolutionSeverityLow, nil
	case string(SchemaEvolutionSeverityMedium):
		return SchemaEvolutionSeverityMedium, nil
	case string(SchemaEvolutionSeverityHigh):
		return SchemaEvolutionSeverityHigh, nil
	case string(SchemaEvolutionSeverityCritical):
		return SchemaEvolutionSeverityCritical, nil
	default:
		return "", fmt.Errorf("unsupported severity %q (expected one of: low, medium, high, critical)", strings.TrimSpace(value))
	}
}

func schemaEvolutionExceedsThreshold(result SchemaEvolutionResult, threshold SchemaEvolutionSeverity) bool {
	thresholdRank := schemaEvolutionSeverityRank(threshold)
	if thresholdRank <= schemaEvolutionSeverityRank(SchemaEvolutionSeverityNone) {
		return false
	}
	return schemaEvolutionSeverityRank(result.Summary.HighestSeverity) >= thresholdRank
}

func schemaEvolutionSeverityRank(severity SchemaEvolutionSeverity) int {
	switch severity {
	case SchemaEvolutionSeverityNone:
		return 0
	case SchemaEvolutionSeverityLow:
		return 1
	case SchemaEvolutionSeverityMedium:
		return 2
	case SchemaEvolutionSeverityHigh:
		return 3
	case SchemaEvolutionSeverityCritical:
		return 4
	default:
		return -1
	}
}

func normalizeSchemaEvolutionSQLForMatch(statement string) string {
	if strings.TrimSpace(statement) == "" {
		return ""
	}
	return strings.ToLower(strings.Join(strings.Fields(statement), " "))
}

func compactSchemaEvolutionStatement(statement string) string {
	if strings.TrimSpace(statement) == "" {
		return ""
	}
	compact := strings.Join(strings.Fields(statement), " ")
	if len(compact) > 220 {
		return compact[:217] + "..."
	}
	return compact
}

func schemaEvolutionLineAt(content string, index int) int {
	if index <= 0 {
		return 1
	}
	if index > len(content) {
		index = len(content)
	}
	return strings.Count(content[:index], "\n") + 1
}

func isSchemaEvolutionWhitespace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '\f':
		return true
	default:
		return false
	}
}
