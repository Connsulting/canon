package canon

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	piiScanCategoryHardcodedLiteral = "hardcoded-literal"
	piiScanCategorySecretCredential = "secret-credential"
	piiScanCategorySensitiveFile    = "sensitive-file"

	piiScanRuleEmailLiteral      = "email-literal"
	piiScanRuleSSNLiteral        = "ssn-literal"
	piiScanRuleCreditCardLiteral = "credit-card-literal"
	piiScanRuleSecretKeyAssign   = "secret-key-assignment"
	piiScanRuleEnvFile           = "env-file-present"
	piiScanRulePrivateKeyFile    = "private-key-file-present"
	piiScanRuleCredentialFile    = "credential-file-present"
	piiScanRuleDumpFile          = "dump-file-present"
)

var (
	piiEmailPattern      = regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`)
	piiSSNPattern        = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)
	piiCreditCardPattern = regexp.MustCompile(`\b(?:\d{4}[- ]){3}\d{4}\b`)
	piiSecretKeyPattern  = regexp.MustCompile(`(?i)\b(api[_\-]?key|secret|token|password|passwd|client[_\-]?secret|private[_\-]?key|aws_access_key_id|aws_secret_access_key)\b\s*[:=]\s*["']?[A-Za-z0-9_\-/+=.]{8,}["']?`)

	piiSensitiveFileSuffixes = []sensitiveFileRule{
		{Suffix: ".pem", RuleID: piiScanRulePrivateKeyFile, Severity: PIIScanSeverityHigh, Detail: "PEM-encoded credential file present", Recommendation: "Move out of working tree, .gitignore the path, and rotate the credential."},
		{Suffix: ".key", RuleID: piiScanRulePrivateKeyFile, Severity: PIIScanSeverityHigh, Detail: "Private key file present", Recommendation: "Move out of working tree, .gitignore the path, and rotate the credential."},
	}
	piiSensitiveFileExactPrefixes = []sensitiveFileRule{
		{Prefix: ".env", RuleID: piiScanRuleEnvFile, Severity: PIIScanSeverityHigh, Detail: "Environment file present in working tree", Recommendation: "Confirm the path is .gitignored, exclude from build artifacts, and store secrets in a secret manager."},
		{Prefix: "credentials.", RuleID: piiScanRuleCredentialFile, Severity: PIIScanSeverityHigh, Detail: "Credentials file present in working tree", Recommendation: "Move out of working tree, .gitignore the path, and rotate the credential."},
		{Prefix: "secrets.", RuleID: piiScanRuleCredentialFile, Severity: PIIScanSeverityHigh, Detail: "Secrets file present in working tree", Recommendation: "Move out of working tree, .gitignore the path, and rotate the credential."},
	}
	piiSensitiveDumpSuffixes = []sensitiveFileRule{
		{Suffix: ".sql", RuleID: piiScanRuleDumpFile, Severity: PIIScanSeverityMedium, Detail: "SQL dump-style file present", Recommendation: "Verify it does not contain real customer data; if it does, remove from history and use synthetic fixtures."},
		{Suffix: ".csv", RuleID: piiScanRuleDumpFile, Severity: PIIScanSeverityMedium, Detail: "CSV file present", Recommendation: "Verify it does not contain real customer data; if it does, remove from history and use synthetic fixtures."},
		{Suffix: ".xlsx", RuleID: piiScanRuleDumpFile, Severity: PIIScanSeverityMedium, Detail: "Spreadsheet file present", Recommendation: "Verify it does not contain real customer data; if it does, remove from history and use synthetic fixtures."},
	}

	piiExcludedRoots = []string{"vendor/", "third_party/", "node_modules/", ".git/", ".canon/"}
)

type sensitiveFileRule struct {
	Suffix         string
	Prefix         string
	RuleID         string
	Severity       PIIScanSeverity
	Detail         string
	Recommendation string
}

func PIIScan(root string, opts PIIScanOptions) (PIIScanResult, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return PIIScanResult{}, err
	}

	tracked, err := listTrackedFiles(absRoot)
	if err != nil {
		return PIIScanResult{}, err
	}

	scannableFiles := make([]string, 0, len(tracked))
	for _, rel := range tracked {
		if isExcludedPath(rel) {
			continue
		}
		scannableFiles = append(scannableFiles, rel)
	}
	sort.Strings(scannableFiles)

	findings := make([]PIIScanFinding, 0)
	for _, rel := range scannableFiles {
		fileFindings, err := scanFileForPII(absRoot, rel)
		if err != nil {
			return PIIScanResult{}, err
		}
		findings = append(findings, fileFindings...)
	}

	sensitive, err := scanWorkingTreeForSensitiveFiles(absRoot)
	if err != nil {
		return PIIScanResult{}, err
	}
	findings = append(findings, sensitive...)

	sortPIIScanFindings(findings)

	result := PIIScanResult{
		Root:         absRoot,
		FilesScanned: len(scannableFiles),
		Findings:     findings,
		Summary:      summarizePIIScanFindings(findings),
	}

	if strings.TrimSpace(string(opts.FailOn)) != "" {
		failOn, err := parsePIIScanSeverity(string(opts.FailOn))
		if err != nil {
			return PIIScanResult{}, err
		}
		if failOn != PIIScanSeverityNone {
			result.FailOn = failOn
			result.ThresholdExceeded = piiScanExceedsThreshold(result, failOn)
		}
	}

	return result, nil
}

func listTrackedFiles(root string) ([]string, error) {
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files failed in %s: %w", filepath.ToSlash(root), err)
	}
	if len(output) == 0 {
		return []string{}, nil
	}
	parts := strings.Split(strings.TrimRight(string(output), "\x00"), "\x00")
	files := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		files = append(files, filepath.ToSlash(p))
	}
	return files, nil
}

func isExcludedPath(rel string) bool {
	normalized := filepath.ToSlash(rel)
	for _, prefix := range piiExcludedRoots {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	return false
}

func scanFileForPII(root string, rel string) ([]PIIScanFinding, error) {
	absPath := filepath.Join(root, filepath.FromSlash(rel))
	st, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if st.IsDir() || st.Size() == 0 {
		return nil, nil
	}

	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	findings := make([]PIIScanFinding, 0)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		findings = append(findings, scanLineForPII(rel, lineNo, line)...)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", rel, err)
	}
	return findings, nil
}

func scanLineForPII(rel string, lineNo int, line string) []PIIScanFinding {
	findings := make([]PIIScanFinding, 0)

	if piiSSNPattern.MatchString(line) {
		findings = append(findings, PIIScanFinding{
			RuleID:         piiScanRuleSSNLiteral,
			Category:       piiScanCategoryHardcodedLiteral,
			Severity:       PIIScanSeverityCritical,
			File:           rel,
			Line:           lineNo,
			Detail:         "Hardcoded SSN-shaped literal",
			Recommendation: "Replace with synthetic test data; rotate any real value that may have been committed.",
		})
	}
	if piiCreditCardPattern.MatchString(line) {
		findings = append(findings, PIIScanFinding{
			RuleID:         piiScanRuleCreditCardLiteral,
			Category:       piiScanCategoryHardcodedLiteral,
			Severity:       PIIScanSeverityCritical,
			File:           rel,
			Line:           lineNo,
			Detail:         "Hardcoded credit-card-shaped literal",
			Recommendation: "Replace with synthetic test data; rotate any real value that may have been committed.",
		})
	}
	if piiSecretKeyPattern.MatchString(line) {
		findings = append(findings, PIIScanFinding{
			RuleID:         piiScanRuleSecretKeyAssign,
			Category:       piiScanCategorySecretCredential,
			Severity:       PIIScanSeverityCritical,
			File:           rel,
			Line:           lineNo,
			Detail:         "Hardcoded secret-credential assignment",
			Recommendation: "Move to environment variables or a secret manager and rotate the credential.",
		})
	}
	if piiEmailPattern.MatchString(line) {
		findings = append(findings, PIIScanFinding{
			RuleID:         piiScanRuleEmailLiteral,
			Category:       piiScanCategoryHardcodedLiteral,
			Severity:       PIIScanSeverityMedium,
			File:           rel,
			Line:           lineNo,
			Detail:         "Hardcoded email literal",
			Recommendation: "Replace with example.com fixtures or load from configuration.",
		})
	}
	return findings
}

func scanWorkingTreeForSensitiveFiles(root string) ([]PIIScanFinding, error) {
	findings := make([]PIIScanFinding, 0)
	err := filepath.WalkDir(root, func(absPath string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if d.IsDir() {
			rel, _ := filepath.Rel(root, absPath)
			normalized := filepath.ToSlash(rel)
			if normalized == "." {
				return nil
			}
			base := filepath.Base(normalized)
			if base == ".git" || base == "vendor" || base == "third_party" || base == "node_modules" || base == ".canon" || base == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, absPath)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		base := filepath.Base(relSlash)
		lowerBase := strings.ToLower(base)
		lowerRel := strings.ToLower(relSlash)

		var rule sensitiveFileRule
		matched := false
		for _, r := range piiSensitiveFileExactPrefixes {
			if strings.HasPrefix(lowerBase, strings.ToLower(r.Prefix)) {
				rule = r
				matched = true
				break
			}
		}
		if !matched {
			for _, r := range piiSensitiveFileSuffixes {
				if strings.HasSuffix(lowerRel, strings.ToLower(r.Suffix)) {
					rule = r
					matched = true
					break
				}
			}
		}
		if !matched {
			for _, r := range piiSensitiveDumpSuffixes {
				if strings.HasSuffix(lowerRel, strings.ToLower(r.Suffix)) {
					rule = r
					matched = true
					break
				}
			}
		}
		if !matched {
			return nil
		}

		findings = append(findings, PIIScanFinding{
			RuleID:         rule.RuleID,
			Category:       piiScanCategorySensitiveFile,
			Severity:       rule.Severity,
			File:           relSlash,
			Detail:         rule.Detail,
			Recommendation: rule.Recommendation,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return findings, nil
}

func sortPIIScanFindings(findings []PIIScanFinding) {
	sort.Slice(findings, func(i, j int) bool {
		left := findings[i]
		right := findings[j]
		leftRank := piiScanSeverityRank(left.Severity)
		rightRank := piiScanSeverityRank(right.Severity)
		if leftRank != rightRank {
			return leftRank > rightRank
		}
		if left.Category != right.Category {
			return left.Category < right.Category
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
		return left.Detail < right.Detail
	})
}

func summarizePIIScanFindings(findings []PIIScanFinding) PIIScanSummary {
	summary := PIIScanSummary{
		TotalFindings:      len(findings),
		HighestSeverity:    PIIScanSeverityNone,
		FindingsBySeverity: PIIScanSeverityCounts{},
		FindingsByCategory: []PIIScanCategoryCount{},
	}

	categoryCounts := map[string]int{}
	for _, f := range findings {
		switch f.Severity {
		case PIIScanSeverityLow:
			summary.FindingsBySeverity.Low++
		case PIIScanSeverityMedium:
			summary.FindingsBySeverity.Medium++
		case PIIScanSeverityHigh:
			summary.FindingsBySeverity.High++
		case PIIScanSeverityCritical:
			summary.FindingsBySeverity.Critical++
		}
		if piiScanSeverityRank(f.Severity) > piiScanSeverityRank(summary.HighestSeverity) {
			summary.HighestSeverity = f.Severity
		}
		categoryCounts[f.Category]++
	}

	categories := make([]string, 0, len(categoryCounts))
	for c := range categoryCounts {
		categories = append(categories, c)
	}
	sort.Strings(categories)
	for _, c := range categories {
		summary.FindingsByCategory = append(summary.FindingsByCategory, PIIScanCategoryCount{Category: c, Count: categoryCounts[c]})
	}
	return summary
}

func parsePIIScanSeverity(value string) (PIIScanSeverity, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(PIIScanSeverityNone):
		return PIIScanSeverityNone, nil
	case string(PIIScanSeverityLow):
		return PIIScanSeverityLow, nil
	case string(PIIScanSeverityMedium):
		return PIIScanSeverityMedium, nil
	case string(PIIScanSeverityHigh):
		return PIIScanSeverityHigh, nil
	case string(PIIScanSeverityCritical):
		return PIIScanSeverityCritical, nil
	default:
		return "", fmt.Errorf("unsupported severity %q (expected one of: low, medium, high, critical)", strings.TrimSpace(value))
	}
}

func piiScanExceedsThreshold(result PIIScanResult, threshold PIIScanSeverity) bool {
	thresholdRank := piiScanSeverityRank(threshold)
	if thresholdRank <= piiScanSeverityRank(PIIScanSeverityNone) {
		return false
	}
	return piiScanSeverityRank(result.Summary.HighestSeverity) >= thresholdRank
}

func piiScanSeverityRank(severity PIIScanSeverity) int {
	switch severity {
	case PIIScanSeverityNone:
		return 0
	case PIIScanSeverityLow:
		return 1
	case PIIScanSeverityMedium:
		return 2
	case PIIScanSeverityHigh:
		return 3
	case PIIScanSeverityCritical:
		return 4
	default:
		return -1
	}
}
