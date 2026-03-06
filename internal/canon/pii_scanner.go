package canon

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const piiScanMaxFileBytes int64 = 1 << 20

var (
	emailLiteralPattern   = regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)
	phoneLiteralPattern   = regexp.MustCompile(`(?i)(?:\+?1[\s\-\.]?)?(?:\(\d{3}\)\s*|\d{3}[\s\-\.])\d{3}[\s\-\.]\d{4}\b`)
	ssnLiteralPattern     = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)
	ccLiteralPattern      = regexp.MustCompile(`\b(?:\d[\s\-]?){13,19}\b`)
	ipv4LiteralPattern    = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	addressLiteralPattern = regexp.MustCompile(
		`(?i)\b\d{1,5}\s+[A-Za-z0-9\.\- ]+\s(?:Street|St|Avenue|Ave|Road|Rd|Boulevard|Blvd|Lane|Ln|Drive|Dr|Court|Ct|Way|Place|Pl)\b`,
	)
	dobLiteralPattern = regexp.MustCompile(
		`(?i)\b(?:dob|date[_ -]?of[_ -]?birth|birth[_ -]?date)\b[^\n]{0,40}\b(?:19|20)\d{2}[-/](?:0?[1-9]|1[0-2])[-/](?:0?[1-9]|[12]\d|3[01])\b`,
	)
	passportLicenseLiteralPattern = regexp.MustCompile(`(?i)(?:"(?:passport|driver[_ -]?license|license[_ -]?number|dl[_ -]?number)"\s*:\s*"[A-Z0-9\-]{6,14}"|(?:passport|driver[_ -]?license|license[_ -]?number|dl[_ -]?number)\s*=\s*[A-Z0-9\-]{6,14})`)
	structuredNamePattern         = regexp.MustCompile(`(?:"|')?(?:full_name|name|customer_name|user_name)(?:"|')?\s*[:=]\s*(?:"|')([A-Z][a-z]+\s+[A-Z][a-z]+)(?:"|')`)

	logCallPattern         = regexp.MustCompile(`(?i)\b(?:fmt\.(?:Sprintf|Printf|Errorf)|log\.(?:Printf|Println|Print|Fatalf|Panicf)|slog\.(?:Debug|Info|Warn|Error)|zerolog\.|zap\.|logrus\.|errors\.Wrap(?:f)?)\s*\(`)
	loggerMethodPattern    = regexp.MustCompile(`(?i)\.(?:Debugf|Infof|Warnf|Errorf|WithField|WithFields)\s*\(`)
	genericKeyValuePattern = regexp.MustCompile(`^\s*(?:export\s+)?["']?([A-Za-z0-9_.\-]+)["']?\s*[:=]\s*(.+?)\s*$`)

	sqlStoragePattern = regexp.MustCompile(`(?i)\b(password|passwd|ssn|social[_ ]?security|card[_ ]?number|credit[_ ]?card|cvv|dob|birth[_ ]?date|passport|driver[_ ]?license|email|phone|address)\b[^\n]{0,80}\b(varchar|text|char|string|json|blob)\b`)
	goStructPattern   = regexp.MustCompile(`\b(?:Password|Passwd|SSN|SocialSecurity|CardNumber|CreditCard|CVV|DOB|BirthDate|Passport|DriverLicense|Email|Phone|Address)\b\s+\*?string\b`)
	fileWritePattern  = regexp.MustCompile(`(?i)\b(?:WriteFile|WriteString|INSERT\s+INTO|Exec(?:Context)?\s*\().*(password|ssn|card|credit|dob|passport|license|email|phone|address)\b`)
)

var piiKeywords = []string{
	"email",
	"name",
	"phone",
	"mobile",
	"address",
	"street",
	"ssn",
	"social",
	"token",
	"password",
	"dob",
	"birth",
	"passport",
	"license",
	"card",
	"credit",
	"customer",
	"user",
}

var highRiskPIIKeywords = map[string]struct{}{
	"ssn":      {},
	"social":   {},
	"token":    {},
	"password": {},
	"dob":      {},
	"passport": {},
	"license":  {},
	"card":     {},
	"credit":   {},
}

var secretKeyIndicators = []string{
	"password",
	"passwd",
	"pwd",
	"secret",
	"token",
	"api_key",
	"apikey",
	"access_key",
	"private_key",
	"client_secret",
	"credential",
	"db_url",
	"database_url",
}

var redactionIndicators = []string{
	"redact",
	"mask",
	"hashed",
	"hash",
	"encrypt",
	"tokenize",
	"anonym",
	"obfuscat",
}

func PIIScan(root string, opts PIIScanOptions) (PIIScanResult, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return PIIScanResult{}, err
	}

	if stat, err := os.Stat(absRoot); err != nil {
		return PIIScanResult{}, err
	} else if !stat.IsDir() {
		return PIIScanResult{}, fmt.Errorf("pii scan root is not a directory: %s", filepath.ToSlash(absRoot))
	}

	failOn := PIISeverityNone
	if strings.TrimSpace(string(opts.FailOn)) != "" {
		parsed, err := parsePIISeverity(string(opts.FailOn))
		if err != nil {
			return PIIScanResult{}, err
		}
		failOn = parsed
	}

	ignorePatterns, err := loadGitignorePatterns(absRoot)
	if err != nil {
		return PIIScanResult{}, err
	}

	findings := make([]PIIFinding, 0)
	seen := make(map[string]struct{})
	scannedFiles := 0

	err = filepath.WalkDir(absRoot, func(pathAbs string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if pathAbs == absRoot {
			return nil
		}

		rel, relErr := filepath.Rel(absRoot, pathAbs)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)

		if entry.IsDir() {
			if shouldSkipPIIScanDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}

		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if isLikelyBinaryPath(rel) {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() > piiScanMaxFileBytes {
			return nil
		}

		b, err := os.ReadFile(pathAbs)
		if err != nil {
			return err
		}
		if isBinaryContent(b) {
			return nil
		}

		scannedFiles++
		content := strings.ReplaceAll(string(b), "\r\n", "\n")
		lines := strings.Split(content, "\n")

		detectHardcodedPIIFindings(rel, lines, seen, &findings)
		detectPIIInLogFindings(rel, lines, seen, &findings)
		detectEnvSecretFindings(rel, lines, seen, &findings)
		detectUnencryptedStorageFindings(rel, lines, seen, &findings)
		return nil
	})
	if err != nil {
		return PIIScanResult{}, err
	}

	if err := detectGitignoreGapFindings(absRoot, ignorePatterns, seen, &findings); err != nil {
		return PIIScanResult{}, err
	}

	sortPIIFindings(findings)
	summary := summarizePIIFindings(findings)

	result := PIIScanResult{
		Root:         absRoot,
		ScannedFiles: scannedFiles,
		Findings:     findings,
		Summary:      summary,
	}

	if failOn != PIISeverityNone {
		result.FailOn = failOn
		result.ThresholdExceeded = piiScanExceedsThreshold(result, failOn)
	}

	return result, nil
}

func shouldSkipPIIScanDir(rel string) bool {
	parts := strings.Split(strings.TrimSpace(rel), "/")
	for _, part := range parts {
		switch part {
		case ".git", "vendor", "third_party", "third-party", "node_modules":
			return true
		}
	}
	return false
}

func detectHardcodedPIIFindings(rel string, lines []string, seen map[string]struct{}, findings *[]PIIFinding) {
	structured := isStructuredDataFile(rel)

	for i, line := range lines {
		lineNo := i + 1
		if line == "" || isPatternDefinitionLine(line) {
			continue
		}

		if emailLiteralPattern.MatchString(line) {
			severity := capFixtureSeverity(rel, PIISeverityHigh)
			if looksLikeExampleLiteral(line) {
				severity = capFixtureSeverity(rel, PIISeverityMedium)
			}
			addPIIFinding(seen, findings, PIIFinding{
				File:           rel,
				Line:           lineNo,
				Category:       PIIFindingCategoryHardcodedPII,
				Severity:       severity,
				Detail:         "Hardcoded email-like literal detected.",
				Recommendation: "Replace with synthetic placeholders in source and load real values from secure runtime inputs.",
			})
		}

		phone := phoneLiteralPattern.FindString(line)
		if phone != "" {
			severity := capFixtureSeverity(rel, PIISeverityHigh)
			if strings.Contains(phone, "555-") || strings.Contains(phone, "555 ") {
				severity = capFixtureSeverity(rel, PIISeverityMedium)
			}
			addPIIFinding(seen, findings, PIIFinding{
				File:           rel,
				Line:           lineNo,
				Category:       PIIFindingCategoryHardcodedPII,
				Severity:       severity,
				Detail:         "Hardcoded phone-number-like literal detected.",
				Recommendation: "Use clearly fake fixture values or generate test data at runtime.",
			})
		}

		ssn := ssnLiteralPattern.FindString(line)
		if ssn != "" && isValidSSN(ssn) {
			addPIIFinding(seen, findings, PIIFinding{
				File:           rel,
				Line:           lineNo,
				Category:       PIIFindingCategoryHardcodedPII,
				Severity:       capFixtureSeverity(rel, PIISeverityCritical),
				Detail:         "Hardcoded SSN-like literal detected.",
				Recommendation: "Remove SSN literals from repository history and use masked or synthetic identifiers.",
			})
		}

		for _, candidate := range ccLiteralPattern.FindAllString(line, -1) {
			if !looksLikeCreditCard(candidate) {
				continue
			}
			addPIIFinding(seen, findings, PIIFinding{
				File:           rel,
				Line:           lineNo,
				Category:       PIIFindingCategoryHardcodedPII,
				Severity:       capFixtureSeverity(rel, PIISeverityCritical),
				Detail:         "Hardcoded credit-card-like literal detected.",
				Recommendation: "Do not store card numbers in source; tokenize card data and keep only references.",
			})
			break
		}

		for _, ip := range ipv4LiteralPattern.FindAllString(line, -1) {
			if !isValidIPv4(ip) || !looksLikeIdentifierContext(line) {
				continue
			}
			addPIIFinding(seen, findings, PIIFinding{
				File:           rel,
				Line:           lineNo,
				Category:       PIIFindingCategoryHardcodedPII,
				Severity:       capFixtureSeverity(rel, PIISeverityMedium),
				Detail:         "IP address literal appears in identifier context.",
				Recommendation: "Avoid using raw client IP as user identifier; hash or truncate before persistence/logging.",
			})
			break
		}

		if addressLiteralPattern.MatchString(line) {
			addPIIFinding(seen, findings, PIIFinding{
				File:           rel,
				Line:           lineNo,
				Category:       PIIFindingCategoryHardcodedPII,
				Severity:       capFixtureSeverity(rel, PIISeverityHigh),
				Detail:         "Street-address-like literal detected.",
				Recommendation: "Replace with non-identifying placeholders or synthetic address fixtures.",
			})
		}

		if dobLiteralPattern.MatchString(line) {
			addPIIFinding(seen, findings, PIIFinding{
				File:           rel,
				Line:           lineNo,
				Category:       PIIFindingCategoryHardcodedPII,
				Severity:       capFixtureSeverity(rel, PIISeverityHigh),
				Detail:         "Date-of-birth-like literal detected.",
				Recommendation: "Use age buckets or synthetic dates rather than actual birth dates.",
			})
		}

		if passportLicenseLiteralPattern.MatchString(line) {
			addPIIFinding(seen, findings, PIIFinding{
				File:           rel,
				Line:           lineNo,
				Category:       PIIFindingCategoryHardcodedPII,
				Severity:       capFixtureSeverity(rel, PIISeverityHigh),
				Detail:         "Passport/driver-license-like literal detected.",
				Recommendation: "Replace with clearly fake IDs and avoid storing document numbers in plaintext.",
			})
		}

		if structured && structuredNamePattern.MatchString(line) {
			addPIIFinding(seen, findings, PIIFinding{
				File:           rel,
				Line:           lineNo,
				Category:       PIIFindingCategoryHardcodedPII,
				Severity:       capFixtureSeverity(rel, PIISeverityMedium),
				Detail:         "Structured data contains a full-person-name-like literal.",
				Recommendation: "Use obvious placeholder names (for example, Test User) in fixtures.",
			})
		}
	}
}

func detectPIIInLogFindings(rel string, lines []string, seen map[string]struct{}, findings *[]PIIFinding) {
	if isTestCodeFile(rel) {
		return
	}

	for i, line := range lines {
		lineNo := i + 1
		if line == "" || isPatternDefinitionLine(line) {
			continue
		}
		if !logCallPattern.MatchString(line) && !loggerMethodPattern.MatchString(line) {
			continue
		}
		if hasRedactionIndicator(line) {
			continue
		}
		keyword, ok := detectPIIKeyword(line)
		if !ok {
			continue
		}

		severity := PIISeverityMedium
		if _, highRisk := highRiskPIIKeywords[keyword]; highRisk {
			severity = PIISeverityHigh
		}

		addPIIFinding(seen, findings, PIIFinding{
			File:           rel,
			Line:           lineNo,
			Category:       PIIFindingCategoryPIIInLogs,
			Severity:       severity,
			Detail:         fmt.Sprintf("Log/error statement may expose %q data without redaction.", keyword),
			Recommendation: "Redact or hash sensitive fields before logging and prefer stable non-PII identifiers.",
		})
	}
}

func detectEnvSecretFindings(rel string, lines []string, seen map[string]struct{}, findings *[]PIIFinding) {
	if !isEnvSecretRelevantFile(rel) {
		return
	}

	for i, raw := range lines {
		lineNo := i + 1
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := parseKeyValueLine(line)
		if !ok {
			continue
		}
		if !isLikelySecretKey(key) {
			continue
		}
		if !isPlaintextSecretValue(value) {
			continue
		}

		severity := severityForSecretKey(key, rel)
		addPIIFinding(seen, findings, PIIFinding{
			File:           rel,
			Line:           lineNo,
			Category:       PIIFindingCategoryEnvSecret,
			Severity:       severity,
			Detail:         fmt.Sprintf("Potential plaintext secret %q found in configuration.", key),
			Recommendation: "Store secrets in a secret manager or environment reference and keep committed configs non-sensitive.",
		})
	}
}

func detectUnencryptedStorageFindings(rel string, lines []string, seen map[string]struct{}, findings *[]PIIFinding) {
	if isTestCodeFile(rel) {
		return
	}

	for i, line := range lines {
		lineNo := i + 1
		if line == "" || isPatternDefinitionLine(line) {
			continue
		}
		if hasRedactionIndicator(line) {
			continue
		}

		if sqlStoragePattern.MatchString(line) {
			keyword, _ := detectPIIKeyword(line)
			if keyword == "" {
				keyword = "sensitive"
			}
			addPIIFinding(seen, findings, PIIFinding{
				File:           rel,
				Line:           lineNo,
				Category:       PIIFindingCategoryUnencryptedStorage,
				Severity:       storageSeverityForKeyword(keyword),
				Detail:         fmt.Sprintf("Potential plaintext DB storage for %q detected.", keyword),
				Recommendation: "Encrypt/tokenize sensitive columns and hash credentials before persistence.",
			})
		}

		if goStructPattern.MatchString(line) {
			keyword, _ := detectPIIKeyword(line)
			if keyword == "" {
				keyword = "sensitive"
			}
			addPIIFinding(seen, findings, PIIFinding{
				File:           rel,
				Line:           lineNo,
				Category:       PIIFindingCategoryUnencryptedStorage,
				Severity:       storageSeverityForKeyword(keyword),
				Detail:         fmt.Sprintf("String field for %q may be persisted unencrypted.", keyword),
				Recommendation: "Document and enforce encryption/hashing at write boundaries for sensitive fields.",
			})
		}

		if fileWritePattern.MatchString(line) {
			keyword, _ := detectPIIKeyword(line)
			if keyword == "" {
				keyword = "sensitive"
			}
			addPIIFinding(seen, findings, PIIFinding{
				File:           rel,
				Line:           lineNo,
				Category:       PIIFindingCategoryUnencryptedStorage,
				Severity:       storageSeverityForKeyword(keyword),
				Detail:         fmt.Sprintf("Potential plaintext write path for %q detected.", keyword),
				Recommendation: "Avoid writing raw PII to files/log stores; encrypt at rest or store irreversible hashes where possible.",
			})
		}
	}
}

func detectGitignoreGapFindings(root string, ignorePatterns []ignorePattern, seen map[string]struct{}, findings *[]PIIFinding) error {
	required := []string{".env*", "*.pem", "*.key", "credentials.*", "secrets.*", "*.sql", "*.csv", "*.xlsx"}
	for _, pattern := range required {
		if hasGitignoreCoverage(ignorePatterns, pattern) {
			continue
		}
		addPIIFinding(seen, findings, PIIFinding{
			File:           ".gitignore",
			Line:           1,
			Category:       PIIFindingCategoryGitignoreGap,
			Severity:       PIISeverityLow,
			Detail:         fmt.Sprintf("Missing recommended ignore pattern %q for sensitive material.", pattern),
			Recommendation: fmt.Sprintf("Add %q to .gitignore or document why committed instances are safe.", pattern),
		})
	}

	trackedFiles, err := listTrackedFiles(root)
	if err != nil {
		return err
	}
	for _, rel := range trackedFiles {
		rel = filepath.ToSlash(strings.TrimSpace(rel))
		if rel == "" || shouldSkipPIIScanDir(path.Dir(rel)) {
			continue
		}
		severity, detail, ok := classifyTrackedSensitiveFile(rel)
		if !ok {
			continue
		}
		addPIIFinding(seen, findings, PIIFinding{
			File:           rel,
			Line:           1,
			Category:       PIIFindingCategoryGitignoreGap,
			Severity:       severity,
			Detail:         detail,
			Recommendation: "Untrack sensitive files, rotate impacted credentials, and keep deny patterns in .gitignore.",
		})
	}

	return nil
}

func addPIIFinding(seen map[string]struct{}, findings *[]PIIFinding, finding PIIFinding) {
	if strings.TrimSpace(finding.File) == "" {
		return
	}
	if finding.Line <= 0 {
		finding.Line = 1
	}
	if finding.Severity == "" {
		finding.Severity = PIISeverityLow
	}
	key := finding.File + "|" + strconv.Itoa(finding.Line) + "|" + string(finding.Category) + "|" + finding.Detail
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	*findings = append(*findings, finding)
}

func listTrackedFiles(root string) ([]string, error) {
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git ls-files failed: %w\n%s", err, strings.TrimSpace(string(output)))
	}

	parts := bytes.Split(output, []byte{0})
	files := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		files = append(files, string(part))
	}
	return files, nil
}

func hasGitignoreCoverage(patterns []ignorePattern, required string) bool {
	required = strings.ToLower(strings.TrimSpace(required))
	for _, pattern := range patterns {
		if pattern.Negated {
			continue
		}
		candidate := strings.ToLower(strings.TrimSpace(pattern.Pattern))
		if candidate == "" {
			continue
		}
		if candidate == required {
			return true
		}

		switch required {
		case ".env*":
			if candidate == ".env" || candidate == ".env.*" || candidate == "**/.env*" {
				return true
			}
		case "credentials.*", "secrets.*":
			prefix := strings.TrimSuffix(required, "*")
			if strings.HasPrefix(candidate, prefix) {
				return true
			}
		default:
			if strings.HasPrefix(required, "*.") {
				ext := strings.TrimPrefix(required, "*")
				if strings.HasSuffix(candidate, ext) && strings.Contains(candidate, "*") {
					return true
				}
			}
		}
	}
	return false
}

func classifyTrackedSensitiveFile(rel string) (PIISeverity, string, bool) {
	lowerRel := strings.ToLower(rel)
	base := path.Base(lowerRel)
	ext := path.Ext(base)

	if strings.HasPrefix(base, ".env") {
		return PIISeverityCritical, "Tracked .env-style file may expose secrets or user data.", true
	}
	if strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key") {
		return PIISeverityCritical, "Tracked key material file detected (.pem/.key).", true
	}
	if strings.HasPrefix(base, "credentials.") || strings.HasPrefix(base, "secrets.") {
		return PIISeverityCritical, "Tracked credentials/secrets file detected.", true
	}
	if ext == ".sql" || ext == ".csv" || ext == ".xlsx" {
		if strings.Contains(lowerRel, "user") || strings.Contains(lowerRel, "customer") || strings.Contains(lowerRel, "person") || strings.Contains(lowerRel, "pii") || strings.Contains(lowerRel, "dump") || strings.Contains(lowerRel, "export") || strings.Contains(lowerRel, "backup") {
			return PIISeverityMedium, "Tracked data-dump-style file with user-data naming detected.", true
		}
	}

	return "", "", false
}

func sortPIIFindings(findings []PIIFinding) {
	sort.Slice(findings, func(i, j int) bool {
		left := findings[i]
		right := findings[j]

		leftRank := piiSeverityRank(left.Severity)
		rightRank := piiSeverityRank(right.Severity)
		if leftRank != rightRank {
			return leftRank > rightRank
		}
		if left.File != right.File {
			return left.File < right.File
		}
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		if left.Category != right.Category {
			return left.Category < right.Category
		}
		if left.Detail != right.Detail {
			return left.Detail < right.Detail
		}
		return left.Recommendation < right.Recommendation
	})
}

func summarizePIIFindings(findings []PIIFinding) PIIScanSummary {
	summary := PIIScanSummary{
		TotalFindings:   len(findings),
		HighestSeverity: PIISeverityNone,
	}

	for _, finding := range findings {
		switch finding.Category {
		case PIIFindingCategoryHardcodedPII:
			summary.FindingsByCategory.HardcodedPII++
		case PIIFindingCategoryPIIInLogs:
			summary.FindingsByCategory.PIIInLogs++
		case PIIFindingCategoryEnvSecret:
			summary.FindingsByCategory.EnvSecret++
		case PIIFindingCategoryUnencryptedStorage:
			summary.FindingsByCategory.UnencryptedStorage++
		case PIIFindingCategoryGitignoreGap:
			summary.FindingsByCategory.GitignoreGap++
		}

		switch finding.Severity {
		case PIISeverityLow:
			summary.FindingsBySeverity.Low++
		case PIISeverityMedium:
			summary.FindingsBySeverity.Medium++
		case PIISeverityHigh:
			summary.FindingsBySeverity.High++
		case PIISeverityCritical:
			summary.FindingsBySeverity.Critical++
		}

		if piiSeverityRank(finding.Severity) > piiSeverityRank(summary.HighestSeverity) {
			summary.HighestSeverity = finding.Severity
		}
	}

	return summary
}

func parsePIISeverity(value string) (PIISeverity, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "", string(PIISeverityNone):
		return PIISeverityNone, nil
	case string(PIISeverityLow):
		return PIISeverityLow, nil
	case string(PIISeverityMedium):
		return PIISeverityMedium, nil
	case string(PIISeverityHigh):
		return PIISeverityHigh, nil
	case string(PIISeverityCritical):
		return PIISeverityCritical, nil
	default:
		return "", fmt.Errorf("unsupported severity %q (expected one of: low, medium, high, critical)", strings.TrimSpace(value))
	}
}

func piiScanExceedsThreshold(result PIIScanResult, threshold PIISeverity) bool {
	thresholdRank := piiSeverityRank(threshold)
	if thresholdRank <= piiSeverityRank(PIISeverityNone) {
		return false
	}
	return piiSeverityRank(result.Summary.HighestSeverity) >= thresholdRank
}

func piiSeverityRank(severity PIISeverity) int {
	switch severity {
	case PIISeverityNone:
		return 0
	case PIISeverityLow:
		return 1
	case PIISeverityMedium:
		return 2
	case PIISeverityHigh:
		return 3
	case PIISeverityCritical:
		return 4
	default:
		return -1
	}
}

func isStructuredDataFile(rel string) bool {
	ext := strings.ToLower(path.Ext(rel))
	switch ext {
	case ".json", ".yaml", ".yml", ".csv", ".sql", ".tsv":
		return true
	default:
		return false
	}
}

func isTestFixturePath(rel string) bool {
	lower := strings.ToLower(filepath.ToSlash(rel))
	return strings.Contains(lower, "/fixtures/") || strings.Contains(lower, "/testdata/") || strings.Contains(lower, "/seeds/") || strings.HasPrefix(lower, "fixtures/") || strings.HasPrefix(lower, "testdata/") || strings.HasPrefix(lower, "seeds/") || strings.HasSuffix(lower, "_test.go")
}

func capFixtureSeverity(rel string, severity PIISeverity) PIISeverity {
	if !isTestFixturePath(rel) {
		return severity
	}
	if piiSeverityRank(severity) > piiSeverityRank(PIISeverityMedium) {
		return PIISeverityMedium
	}
	return severity
}

func looksLikeExampleLiteral(line string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, "example.com") || strings.Contains(lower, "test.com") || strings.Contains(lower, "@test") || strings.Contains(lower, "placeholder")
}

func looksLikeCreditCard(candidate string) bool {
	digits := digitsOnly(candidate)
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}
	if allSameDigit(digits) {
		return false
	}
	return luhnValid(digits)
}

func digitsOnly(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func allSameDigit(value string) bool {
	if value == "" {
		return false
	}
	first := value[0]
	for i := 1; i < len(value); i++ {
		if value[i] != first {
			return false
		}
	}
	return true
}

func luhnValid(digits string) bool {
	sum := 0
	double := false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}

func isValidIPv4(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 4 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || n > 255 {
			return false
		}
	}
	return true
}

func looksLikeIdentifierContext(line string) bool {
	lower := strings.ToLower(line)
	if strings.Contains(lower, "127.0.0.1") || strings.Contains(lower, "0.0.0.0") {
		return false
	}
	return strings.Contains(lower, "ip") || strings.Contains(lower, "user") || strings.Contains(lower, "customer") || strings.Contains(lower, "identifier") || strings.Contains(lower, "client")
}

func isValidSSN(value string) bool {
	parts := strings.Split(value, "-")
	if len(parts) != 3 {
		return false
	}
	if parts[0] == "000" || parts[0] == "666" || strings.HasPrefix(parts[0], "9") {
		return false
	}
	if parts[1] == "00" || parts[2] == "0000" {
		return false
	}
	return true
}

func hasRedactionIndicator(line string) bool {
	lower := strings.ToLower(line)
	for _, indicator := range redactionIndicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}
	return false
}

func detectPIIKeyword(line string) (string, bool) {
	lower := strings.ToLower(line)
	for _, keyword := range piiKeywords {
		if strings.Contains(lower, keyword) {
			return keyword, true
		}
	}
	return "", false
}

func isPatternDefinitionLine(line string) bool {
	if strings.Contains(line, "regexp.MustCompile(") {
		return true
	}
	return strings.Contains(line, "`(?") || strings.Contains(line, "(?i)")
}

func isTestCodeFile(rel string) bool {
	lower := strings.ToLower(filepath.ToSlash(rel))
	return strings.HasSuffix(lower, "_test.go")
}

func isEnvSecretRelevantFile(rel string) bool {
	lower := strings.ToLower(filepath.ToSlash(rel))
	base := path.Base(lower)
	ext := path.Ext(base)

	if strings.HasPrefix(base, ".env") {
		return true
	}
	if base == "config.yaml" || base == "config.yml" || base == "config.json" || base == "config.toml" {
		return true
	}
	if base == "docker-compose.yml" || base == "docker-compose.yaml" {
		return true
	}
	if strings.HasPrefix(lower, ".github/workflows/") && (ext == ".yml" || ext == ".yaml") {
		return true
	}
	return false
}

func parseKeyValueLine(line string) (string, string, bool) {
	matches := genericKeyValuePattern.FindStringSubmatch(line)
	if len(matches) != 3 {
		return "", "", false
	}
	key := strings.TrimSpace(matches[1])
	value := strings.TrimSpace(matches[2])
	if key == "" || value == "" {
		return "", "", false
	}
	value = strings.TrimSuffix(value, ",")
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"'`)
	if key == "" || value == "" {
		return "", "", false
	}
	return key, value, true
}

func isLikelySecretKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	for _, indicator := range secretKeyIndicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}
	return false
}

func isPlaintextSecretValue(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, "${") || strings.HasPrefix(trimmed, "$({") || strings.HasPrefix(trimmed, "ENC[") {
		return false
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "<") && strings.Contains(lower, ">") {
		return false
	}
	placeholders := []string{"changeme", "change_me", "example", "sample", "placeholder", "redacted", "your_token", "your-key", "dummy"}
	for _, placeholder := range placeholders {
		if lower == placeholder || strings.Contains(lower, placeholder) {
			return false
		}
	}
	return true
}

func severityForSecretKey(key string, rel string) PIISeverity {
	lower := strings.ToLower(key)
	if isExampleTemplateFile(rel) {
		return PIISeverityMedium
	}
	if strings.Contains(lower, "private_key") || strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "credential") {
		return PIISeverityCritical
	}
	return PIISeverityHigh
}

func isExampleTemplateFile(rel string) bool {
	lower := strings.ToLower(filepath.ToSlash(rel))
	base := path.Base(lower)
	return strings.Contains(base, "example") || strings.Contains(base, "sample") || strings.HasSuffix(base, ".dist")
}

func storageSeverityForKeyword(keyword string) PIISeverity {
	switch strings.ToLower(strings.TrimSpace(keyword)) {
	case "password", "ssn", "social", "card", "credit", "cvv", "token":
		return PIISeverityCritical
	default:
		return PIISeverityHigh
	}
}
