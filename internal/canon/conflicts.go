package canon

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type normalizedStatement struct {
	Key      string
	Negative bool
	Line     string
}

type statementOrigin struct {
	SpecID   string
	Negative bool
	Line     string
}

type specConflict struct {
	ExistingSpecID string
	CandidateSpec  string
	StatementKey   string
	ExistingLine   string
	CandidateLine  string
}

type aiConflictDetailItem struct {
	ExistingSpecID string `json:"existing_spec_id"`
	Reason         string `json:"reason"`
}

func detectSpecConflicts(existing []Spec, candidate Spec) []specConflict {
	candidateStatements := extractNormativeStatements(candidate)
	if len(candidateStatements) == 0 {
		return nil
	}

	byKey := make(map[string][]statementOrigin)
	for _, spec := range existing {
		if !domainsOverlap(spec, candidate) {
			continue
		}
		for _, statement := range extractNormativeStatements(spec) {
			byKey[statement.Key] = append(byKey[statement.Key], statementOrigin{
				SpecID:   spec.ID,
				Negative: statement.Negative,
				Line:     statement.Line,
			})
		}
	}

	conflicts := make([]specConflict, 0)
	seen := map[string]struct{}{}
	for _, statement := range candidateStatements {
		origins := byKey[statement.Key]
		for _, origin := range origins {
			if origin.Negative == statement.Negative {
				continue
			}
			sig := origin.SpecID + "|" + statement.Key + "|" + statement.Line
			if _, ok := seen[sig]; ok {
				continue
			}
			seen[sig] = struct{}{}
			conflicts = append(conflicts, specConflict{
				ExistingSpecID: origin.SpecID,
				CandidateSpec:  candidate.ID,
				StatementKey:   statement.Key,
				ExistingLine:   origin.Line,
				CandidateLine:  statement.Line,
			})
		}
	}

	sort.Slice(conflicts, func(i, j int) bool {
		if conflicts[i].ExistingSpecID == conflicts[j].ExistingSpecID {
			if conflicts[i].StatementKey == conflicts[j].StatementKey {
				return conflicts[i].CandidateLine < conflicts[j].CandidateLine
			}
			return conflicts[i].StatementKey < conflicts[j].StatementKey
		}
		return conflicts[i].ExistingSpecID < conflicts[j].ExistingSpecID
	})

	return conflicts
}

func domainsOverlap(left Spec, right Spec) bool {
	leftDomains := mustInclude(left.TouchedDomains, left.Domain)
	rightDomains := mustInclude(right.TouchedDomains, right.Domain)
	set := map[string]struct{}{}
	for _, domain := range leftDomains {
		set[domain] = struct{}{}
	}
	for _, domain := range rightDomains {
		if _, ok := set[domain]; ok {
			return true
		}
	}
	return false
}

func extractNormativeStatements(spec Spec) []normalizedStatement {
	lines := strings.Split(spec.Body, "\n")
	out := make([]normalizedStatement, 0)
	seen := map[string]struct{}{}
	for _, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "```") {
			continue
		}
		trimmed = strings.TrimPrefix(trimmed, "- ")
		trimmed = strings.TrimPrefix(trimmed, "* ")
		if len(trimmed) > 3 && trimmed[1] == '.' && trimmed[0] >= '0' && trimmed[0] <= '9' {
			trimmed = strings.TrimSpace(trimmed[2:])
		}
		lower := strings.ToLower(trimmed)
		if !isNormativeLine(lower) {
			continue
		}
		key := normalizeStatementKey(lower)
		if len(key) < 8 {
			continue
		}
		negative := isNegativeLine(lower)
		sig := key + "|" + fmt.Sprintf("%t", negative)
		if _, ok := seen[sig]; ok {
			continue
		}
		seen[sig] = struct{}{}
		out = append(out, normalizedStatement{Key: key, Negative: negative, Line: trimmed})
	}
	return out
}

func isNormativeLine(lower string) bool {
	keywords := []string{" must ", " should ", " shall ", " cannot ", " can't ", " never ", " always ", " required "}
	padded := " " + lower + " "
	for _, kw := range keywords {
		if strings.Contains(padded, kw) {
			return true
		}
	}
	return false
}

func isNegativeLine(lower string) bool {
	padded := " " + lower + " "
	negative := []string{" must not ", " should not ", " cannot ", " can't ", " never ", " shall not "}
	for _, kw := range negative {
		if strings.Contains(padded, kw) {
			return true
		}
	}
	return false
}

func normalizeStatementKey(lower string) string {
	replacer := strings.NewReplacer(
		".", " ",
		",", " ",
		":", " ",
		";", " ",
		"(", " ",
		")", " ",
		"[", " ",
		"]", " ",
		"{", " ",
		"}", " ",
		"\"", " ",
		"'", " ",
		"/", " ",
		"\\", " ",
		"-", " ",
	)
	clean := replacer.Replace(lower)
	clean = " " + clean + " "
	for _, token := range []string{" not ", " never ", " cannot ", " can't ", " no ", " must ", " should ", " shall ", " always ", " required ", " be ", " are ", " is ", " the ", " a ", " an "} {
		clean = strings.ReplaceAll(clean, token, " ")
	}
	fields := strings.Fields(clean)
	return strings.Join(fields, " ")
}

func writeConflictReport(root string, candidate Spec, conflicts []specConflict) (string, error) {
	timestamp := nowUTC().Format(timeRFC3339)
	reportID := "conflict-" + nowUTC().Format("20060102T150405Z") + "-" + slugify(candidate.ID)
	relPath := filepath.ToSlash(filepath.Join(".canon", "conflict-reports", reportID+".yaml"))
	absPath := filepath.Join(root, relPath)

	lines := []string{
		"id: " + reportID,
		"triggered_by: " + candidate.ID,
		"timestamp: " + timestamp,
		"conflicts:",
	}

	for _, conflict := range conflicts {
		lines = append(lines,
			"  - type: semantic_contradiction",
			"    existing_spec: "+yamlScalar(conflict.ExistingSpecID),
			"    candidate_spec: "+yamlScalar(conflict.CandidateSpec),
			"    statement_key: "+yamlScalar(conflict.StatementKey),
			"    existing_line: "+yamlScalar(conflict.ExistingLine),
			"    candidate_line: "+yamlScalar(conflict.CandidateLine),
		)
	}

	content := strings.Join(lines, "\n") + "\n"
	if _, err := writeTextIfChanged(absPath, content); err != nil {
		return "", err
	}
	return relPath, nil
}
