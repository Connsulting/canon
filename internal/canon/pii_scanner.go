package canon

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	piiEmailPattern        = regexp.MustCompile(`(?i)\b[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}\b`)
	piiPhonePattern        = regexp.MustCompile(`(?i)\b(?:\+?\d{1,2}[ .-]?)?(?:\(?\d{3}\)?[ .-]?\d{3}[ .-]?\d{4})\b`)
	piiSSNPattern          = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)
	piiDigitSpanPattern    = regexp.MustCompile(`\b(?:\d[ -]*){13,19}\b`)
	piiIPv4Pattern         = regexp.MustCompile(`\b(?:25[0-5]|2[0-4]\d|1?\d?\d)(?:\.(?:25[0-5]|2[0-4]\d|1?\d?\d)){3}\b`)
	piiAddressPattern      = regexp.MustCompile(`(?i)\b\d{1,6}\s+[A-Za-z0-9.'-]+\s+(?:Street|St|Avenue|Ave|Road|Rd|Boulevard|Blvd|Lane|Ln|Drive|Dr|Court|Ct|Way|Place|Pl)\b`)
	piiDatePattern         = regexp.MustCompile(`\b(?:19|20)\d{2}[-/](?:0[1-9]|1[0-2])[-/](?:0[1-9]|[12]\d|3[01])\b`)
	piiNamePattern         = regexp.MustCompile(`\b[A-Z][a-z]{1,30}\s+[A-Z][a-z]{1,30}\b`)
	piiPassportIDPattern   = regexp.MustCompile(`\b[A-Z0-9]{6,12}\b`)
	piiLogCallPattern      = regexp.MustCompile(`\b(?:fmt\.(?:Sprintf|Errorf)|log\.Printf|slog\.(?:Debug|Info|Warn|Error)|\w+\.Msgf|zap\.\w+|logrus\.\w+|errors\.(?:Wrap|Wrapf))\s*\(`)
	piiSecretAssignPattern = regexp.MustCompile(`^\s*["']?([A-Za-z0-9_.-]+)["']?\s*[:=]\s*(.+?)\s*$`)
)

var piiStructuredExtensions = map[string]struct{}{
	".json": {},
	".yaml": {},
	".yml":  {},
	".csv":  {},
	".sql":  {},
}

var piiSourceExtensions = map[string]struct{}{
	".go":   {},
	".js":   {},
	".jsx":  {},
	".ts":   {},
	".tsx":  {},
	".java": {},
	".kt":   {},
	".py":   {},
	".rb":   {},
	".php":  {},
	".cs":   {},
	".rs":   {},
}

var piiRedactionHints = []string{
	"redact",
	"masked",
	"hash",
	"hashed",
	"encrypt",
	"cipher",
	"tokenize",
	"anonym",
	"***",
}

var piiHighRiskLogTerms = []string{
	"ssn",
	"social_security",
	"credit_card",
	"card_number",
	"token",
	"password",
	"secret",
	"api_key",
	"private_key",
}

var piiGeneralTerms = []string{
	"email",
	"phone",
	"mobile",
	"name",
	"full_name",
	"address",
	"ssn",
	"social_security",
	"token",
	"secret",
	"password",
	"credit_card",
	"card_number",
	"dob",
	"birthdate",
}

var piiIdentityContextHints = []string{
	"identifier",
	"identity",
	"user_id",
	"customer_id",
	"client_id",
	"session_id",
	"account_id",
}

var piiSecretKeyTerms = []string{
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
	"database_url",
	"db_url",
}

var piiPlaceholderValues = map[string]struct{}{
	"":            {},
	"changeme":    {},
	"change-me":   {},
	"example":     {},
	"example123":  {},
	"dummy":       {},
	"test":        {},
	"testing":     {},
	"sample":      {},
	"placeholder": {},
	"localhost":   {},
	"redacted":    {},
	"masked":      {},
}

var piiStorageSensitiveTerms = []string{
	"password",
	"passwd",
	"ssn",
	"social_security",
	"credit_card",
	"card_number",
	"cvv",
	"token",
	"secret",
	"private_key",
	"dob",
	"birthdate",
}

var piiStorageHighTerms = []string{
	"ssn",
	"social_security",
	"credit_card",
	"card_number",
	"token",
	"secret",
	"private_key",
}

var piiStorageProtectionTerms = []string{
	"bcrypt",
	"argon",
	"scrypt",
	"hash",
	"encrypt",
	"cipher",
	"kms",
	"masked",
	"redact",
	"sha256",
	"sha512",
}

var piiStorageContextTerms = []string{
	"create table",
	"alter table",
	"insert into",
	"update ",
	"writefile(",
	"persist",
	"store",
	"save",
	"varchar",
	"text",
	"json:\"",
	"bson:\"",
	"db:\"",
	"gorm:\"",
	"column",
}

type piiScanGitignoreRequirement struct {
	Pattern        string
	Severity       PIIScanSeverity
	Recommendation string
}

func PIIScan(root string, opts PIIScanOptions) (PIIScanResult, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return PIIScanResult{}, err
	}

	ignorePatterns, err := loadGitignorePatterns(absRoot)
	if err != nil {
		return PIIScanResult{}, err
	}
	gitignoreLines, err := readPIIScanGitignoreLines(absRoot)
	if err != nil {
		return PIIScanResult{}, err
	}

	findings := make([]PIIScanFinding, 0)
	dedup := make(map[string]struct{})
	scannedFiles := 0

	err = filepath.WalkDir(absRoot, func(pathAbs string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if pathAbs == absRoot {
			return nil
		}

		rel, err := filepath.Rel(absRoot, pathAbs)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		rel = strings.TrimPrefix(rel, "./")

		if entry.IsDir() {
			if shouldSkipPIIScanDir(rel) {
				return filepath.SkipDir
			}
			if matchesIgnorePatterns(rel, true, ignorePatterns) && !piiScanShouldBypassIgnore(rel) {
				return filepath.SkipDir
			}
			return nil
		}

		mode := entry.Type()
		if mode&fs.ModeSymlink != 0 {
			return nil
		}

		if shouldSkipPIIScanFile(rel) {
			return nil
		}
		if matchesIgnorePatterns(rel, false, ignorePatterns) && !piiScanShouldBypassIgnore(rel) {
			return nil
		}
		if isLikelyBinaryPath(rel) {
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
		lines := strings.Split(string(b), "\n")

		appendPIIScanFindings(&findings, dedup, detectHardcodedPIIFindings(rel, lines)...)
		appendPIIScanFindings(&findings, dedup, detectPIIInLogFindings(rel, lines)...)
		appendPIIScanFindings(&findings, dedup, detectEnvSecretFindings(rel, lines)...)
		appendPIIScanFindings(&findings, dedup, detectUnencryptedStorageFindings(rel, lines)...)
		return nil
	})
	if err != nil {
		return PIIScanResult{}, err
	}

	appendPIIScanFindings(&findings, dedup, detectGitignoreCoverageFindings(gitignoreLines)...)

	trackedFiles, trackedErr := listTrackedRepositoryFiles(absRoot)
	if trackedErr == nil {
		appendPIIScanFindings(&findings, dedup, detectTrackedSensitiveFilesFindings(trackedFiles)...)
	}

	sortPIIScanFindings(findings)
	summary := summarizePIIScanFindings(findings)

	result := PIIScanResult{
		Root:         absRoot,
		ScannedFiles: scannedFiles,
		Findings:     findings,
		Summary:      summary,
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

func detectHardcodedPIIFindings(rel string, lines []string) []PIIScanFinding {
	findings := make([]PIIScanFinding, 0)
	ext := strings.ToLower(filepath.Ext(rel))
	_, isStructured := piiStructuredExtensions[ext]

	for i, raw := range lines {
		lineNo := i + 1
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || isCommentLine(trimmed) {
			continue
		}
		lower := strings.ToLower(trimmed)

		if piiEmailPattern.MatchString(trimmed) {
			findings = append(findings, newPIIScanFinding(rel, lineNo, PIIScanCategoryHardcodedPII, adjustFixtureSeverity(rel, PIIScanSeverityMedium), "Possible hardcoded email address literal.", "Replace real addresses with synthetic placeholders or load from fixtures/env."))
		}
		if piiPhonePattern.MatchString(trimmed) {
			findings = append(findings, newPIIScanFinding(rel, lineNo, PIIScanCategoryHardcodedPII, adjustFixtureSeverity(rel, PIIScanSeverityMedium), "Possible hardcoded phone number literal.", "Use fake phone numbers reserved for examples or synthetic fixture values."))
		}
		if piiSSNPattern.MatchString(trimmed) {
			findings = append(findings, newPIIScanFinding(rel, lineNo, PIIScanCategoryHardcodedPII, adjustFixtureSeverity(rel, PIIScanSeverityHigh), "Possible SSN literal (NNN-NN-NNNN) found in plaintext.", "Remove SSN literals; use synthetic placeholders and encrypt/tokenize at rest."))
		}
		if hasCreditCardLikeDigits(trimmed) {
			findings = append(findings, newPIIScanFinding(rel, lineNo, PIIScanCategoryHardcodedPII, adjustFixtureSeverity(rel, PIIScanSeverityHigh), "Possible credit-card-like literal detected (13-19 digits).", "Do not commit card numbers; use test card tokens and masked values."))
		}
		if piiIPv4Pattern.MatchString(trimmed) && containsAny(lower, piiIdentityContextHints) {
			findings = append(findings, newPIIScanFinding(rel, lineNo, PIIScanCategoryHardcodedPII, adjustFixtureSeverity(rel, PIIScanSeverityMedium), "IP address appears to be used as an identity/identifier value.", "Avoid using raw IPs as identifiers; pseudonymize or hash if retention is required."))
		}
		if piiAddressPattern.MatchString(trimmed) {
			findings = append(findings, newPIIScanFinding(rel, lineNo, PIIScanCategoryHardcodedPII, adjustFixtureSeverity(rel, PIIScanSeverityMedium), "Address-like literal detected in plaintext.", "Replace with synthetic address data and avoid storing real addresses in repository fixtures."))
		}
		if containsAny(lower, []string{"dob", "date_of_birth", "birthdate", "born"}) && piiDatePattern.MatchString(trimmed) {
			findings = append(findings, newPIIScanFinding(rel, lineNo, PIIScanCategoryHardcodedPII, adjustFixtureSeverity(rel, PIIScanSeverityHigh), "Date-of-birth style literal found with DOB/birthdate context.", "Replace DOB literals with synthetic values and restrict exposure in logs/fixtures."))
		}
		if containsAny(lower, []string{"passport", "driver_license", "driver-license", "license_number", "dl_number"}) && piiPassportIDPattern.MatchString(trimmed) {
			findings = append(findings, newPIIScanFinding(rel, lineNo, PIIScanCategoryHardcodedPII, adjustFixtureSeverity(rel, PIIScanSeverityHigh), "Passport/driver-license identifier pattern found in plaintext.", "Avoid storing real license/passport IDs in source; tokenize or use synthetic IDs."))
		}
		if isStructured && containsAny(lower, []string{"full_name", "name", "first_name", "last_name", "\"name\"", "'name'"}) && piiNamePattern.MatchString(trimmed) {
			findings = append(findings, newPIIScanFinding(rel, lineNo, PIIScanCategoryHardcodedPII, PIIScanSeverityMedium, "Structured data appears to contain a realistic full person name.", "Use obviously fake names in fixtures/seeds and keep production identities out of repo data."))
		}
	}

	return findings
}

func detectPIIInLogFindings(rel string, lines []string) []PIIScanFinding {
	ext := strings.ToLower(filepath.Ext(rel))
	if _, ok := piiSourceExtensions[ext]; !ok {
		return nil
	}

	findings := make([]PIIScanFinding, 0)
	for i, raw := range lines {
		lineNo := i + 1
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || isCommentLine(trimmed) {
			continue
		}
		if !piiLogCallPattern.MatchString(trimmed) {
			continue
		}
		lower := strings.ToLower(trimmed)
		if containsAny(lower, piiRedactionHints) {
			continue
		}

		term := firstMatchingTerm(lower, piiGeneralTerms)
		if term == "" {
			continue
		}

		severity := PIIScanSeverityMedium
		if containsAny(lower, piiHighRiskLogTerms) {
			severity = PIIScanSeverityHigh
		}

		findings = append(findings, newPIIScanFinding(
			rel,
			lineNo,
			PIIScanCategoryPIIInLogs,
			severity,
			fmt.Sprintf("Log/error formatting appears to interpolate PII-related field (%s) without redaction.", term),
			"Redact, hash, or drop PII fields before logging; keep only minimal identifiers.",
		))
	}

	return findings
}

func detectEnvSecretFindings(rel string, lines []string) []PIIScanFinding {
	if !isEnvSecretCandidatePath(rel) {
		return nil
	}

	findings := make([]PIIScanFinding, 0)
	for i, raw := range lines {
		lineNo := i + 1
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || isCommentLine(trimmed) {
			continue
		}

		key, value, ok := parseKeyValueAssignment(trimmed)
		if !ok {
			continue
		}
		keyLower := strings.ToLower(strings.TrimSpace(key))
		if !isSecretKeyName(keyLower) {
			continue
		}
		if isLikelySecretPlaceholder(value) {
			continue
		}

		severity := PIIScanSeverityMedium
		switch {
		case strings.Contains(keyLower, "private_key") || strings.Contains(strings.ToUpper(value), "BEGIN PRIVATE KEY"):
			severity = PIIScanSeverityCritical
		case containsAny(keyLower, []string{"password", "secret", "token", "api_key", "apikey", "access_key", "client_secret"}):
			severity = PIIScanSeverityHigh
		}

		findings = append(findings, newPIIScanFinding(
			rel,
			lineNo,
			PIIScanCategoryEnvSecret,
			severity,
			fmt.Sprintf("Potential plaintext secret assignment detected for key %q.", key),
			"Move credentials to a secret manager or runtime env injection and remove plaintext from tracked files.",
		))
	}

	return findings
}

func detectUnencryptedStorageFindings(rel string, lines []string) []PIIScanFinding {
	findings := make([]PIIScanFinding, 0)
	for i, raw := range lines {
		lineNo := i + 1
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || isCommentLine(trimmed) {
			continue
		}

		lower := strings.ToLower(trimmed)
		if containsAny(lower, piiStorageProtectionTerms) {
			continue
		}
		if !containsAny(lower, piiStorageSensitiveTerms) {
			continue
		}
		if !containsAny(lower, piiStorageContextTerms) {
			continue
		}

		severity := PIIScanSeverityMedium
		switch {
		case containsAny(lower, []string{"password", "passwd", "pwd"}):
			severity = PIIScanSeverityCritical
		case containsAny(lower, piiStorageHighTerms):
			severity = PIIScanSeverityHigh
		}

		findings = append(findings, newPIIScanFinding(
			rel,
			lineNo,
			PIIScanCategoryUnencryptedData,
			severity,
			"Sensitive field appears to be stored/persisted without explicit hashing or encryption controls.",
			"Hash passwords (bcrypt/argon2) and encrypt/tokenize SSN/card/token values before persistence.",
		))
	}
	return findings
}

func detectGitignoreCoverageFindings(lines []string) []PIIScanFinding {
	findings := make([]PIIScanFinding, 0)
	required := []piiScanGitignoreRequirement{
		{Pattern: ".env*", Severity: PIIScanSeverityHigh, Recommendation: "Add `.env*` to .gitignore and keep secrets out of version control."},
		{Pattern: "*.pem", Severity: PIIScanSeverityHigh, Recommendation: "Add `*.pem` to .gitignore and rotate any committed private certificates/keys."},
		{Pattern: "*.key", Severity: PIIScanSeverityHigh, Recommendation: "Add `*.key` to .gitignore and rotate exposed key material."},
		{Pattern: "credentials.*", Severity: PIIScanSeverityHigh, Recommendation: "Add `credentials.*` to .gitignore and move credentials to secure storage."},
		{Pattern: "secrets.*", Severity: PIIScanSeverityHigh, Recommendation: "Add `secrets.*` to .gitignore and avoid committed secret bundles."},
		{Pattern: "*.sql", Severity: PIIScanSeverityMedium, Recommendation: "Add `*.sql` to .gitignore if dumps/seeds can contain user data."},
		{Pattern: "*.csv", Severity: PIIScanSeverityMedium, Recommendation: "Add `*.csv` to .gitignore for user-data exports and local analysis dumps."},
		{Pattern: "*.xlsx", Severity: PIIScanSeverityMedium, Recommendation: "Add `*.xlsx` to .gitignore for spreadsheet exports with user data."},
	}

	for _, requirement := range required {
		if gitignoreContainsPattern(lines, requirement.Pattern) {
			continue
		}
		findings = append(findings, newPIIScanFinding(
			".gitignore",
			1,
			PIIScanCategoryGitignoreGap,
			requirement.Severity,
			fmt.Sprintf(".gitignore is missing recommended sensitive pattern %q.", requirement.Pattern),
			requirement.Recommendation,
		))
	}
	return findings
}

func detectTrackedSensitiveFilesFindings(trackedFiles []string) []PIIScanFinding {
	findings := make([]PIIScanFinding, 0)
	for _, rel := range trackedFiles {
		normalized := filepath.ToSlash(strings.TrimSpace(rel))
		if normalized == "" || shouldSkipPIIScanFile(normalized) || shouldSkipPIIScanDir(normalized) {
			continue
		}

		base := strings.ToLower(pathBase(normalized))
		ext := strings.ToLower(filepath.Ext(base))
		severity := PIIScanSeverity("")
		switch {
		case strings.HasPrefix(base, ".env"):
			severity = PIIScanSeverityHigh
		case ext == ".pem" || ext == ".key":
			severity = PIIScanSeverityHigh
		case strings.HasPrefix(base, "credentials.") || strings.HasPrefix(base, "secrets."):
			severity = PIIScanSeverityHigh
		case ext == ".sql" || ext == ".csv" || ext == ".xlsx":
			severity = PIIScanSeverityMedium
		}
		if severity == "" {
			continue
		}

		findings = append(findings, newPIIScanFinding(
			normalized,
			1,
			PIIScanCategoryGitignoreGap,
			severity,
			"Tracked file matches a sensitive file signature and may expose user data or secrets.",
			"Add an appropriate .gitignore rule and remove the file from tracking with `git rm --cached`.",
		))
	}
	return findings
}

func appendPIIScanFindings(target *[]PIIScanFinding, dedup map[string]struct{}, findings ...PIIScanFinding) {
	for _, finding := range findings {
		key := fmt.Sprintf("%s|%d|%s|%s|%s", finding.File, finding.Line, finding.Category, finding.Severity, finding.Detail)
		if _, exists := dedup[key]; exists {
			continue
		}
		dedup[key] = struct{}{}
		*target = append(*target, finding)
	}
}

func newPIIScanFinding(file string, line int, category PIIScanCategory, severity PIIScanSeverity, detail string, recommendation string) PIIScanFinding {
	return PIIScanFinding{
		File:           filepath.ToSlash(strings.TrimSpace(file)),
		Line:           line,
		Category:       category,
		Severity:       severity,
		Detail:         detail,
		Recommendation: recommendation,
	}
}

func shouldSkipPIIScanDir(rel string) bool {
	if shouldSkipDir(rel) {
		return true
	}
	lower := strings.ToLower(filepath.ToSlash(strings.TrimSpace(rel)))
	if lower == "" || lower == "." {
		return false
	}
	parts := strings.Split(lower, "/")
	for _, part := range parts {
		switch part {
		case "third_party", "third-party", "external", "deps":
			return true
		}
	}
	return false
}

func shouldSkipPIIScanFile(rel string) bool {
	lower := strings.ToLower(filepath.ToSlash(strings.TrimSpace(rel)))
	if lower == "" || lower == "." {
		return true
	}
	if strings.HasPrefix(lower, ".git/") {
		return true
	}
	parts := strings.Split(lower, "/")
	for _, part := range parts[:len(parts)-1] {
		switch part {
		case "vendor", "node_modules", "third_party", "third-party", "external", "deps":
			return true
		}
	}
	return false
}

func piiScanShouldBypassIgnore(rel string) bool {
	lower := strings.ToLower(filepath.ToSlash(strings.TrimSpace(rel)))
	if lower == ".gitignore" {
		return true
	}
	base := strings.ToLower(pathBase(lower))
	return strings.HasPrefix(base, ".env")
}

func isCommentLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "--") || strings.HasPrefix(trimmed, "/*")
}

func containsAny(value string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(value, strings.ToLower(term)) {
			return true
		}
	}
	return false
}

func firstMatchingTerm(value string, terms []string) string {
	for _, term := range terms {
		if strings.Contains(value, strings.ToLower(term)) {
			return term
		}
	}
	return ""
}

func hasCreditCardLikeDigits(line string) bool {
	matches := piiDigitSpanPattern.FindAllString(line, -1)
	for _, match := range matches {
		digits := extractDigits(match)
		if len(digits) < 13 || len(digits) > 19 {
			continue
		}
		if luhnValid(digits) {
			return true
		}
	}
	return false
}

func extractDigits(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c >= '0' && c <= '9' {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func luhnValid(number string) bool {
	if len(number) == 0 {
		return false
	}
	sum := 0
	doubleDigit := false
	for i := len(number) - 1; i >= 0; i-- {
		d := int(number[i] - '0')
		if doubleDigit {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		doubleDigit = !doubleDigit
	}
	return sum%10 == 0
}

func isEnvSecretCandidatePath(rel string) bool {
	lower := strings.ToLower(filepath.ToSlash(strings.TrimSpace(rel)))
	base := strings.ToLower(pathBase(lower))
	switch {
	case strings.HasPrefix(base, ".env"):
		return true
	case base == "config.yml" || base == "config.yaml" || base == "config.json":
		return true
	case strings.HasPrefix(base, "docker-compose") && (strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml")):
		return true
	case strings.HasPrefix(lower, ".github/workflows/") && (strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".yaml")):
		return true
	default:
		return false
	}
}

func parseKeyValueAssignment(line string) (string, string, bool) {
	candidate := strings.TrimSpace(line)
	candidate = strings.TrimPrefix(candidate, "export ")
	if strings.HasPrefix(candidate, "{") || strings.HasPrefix(candidate, "}") {
		return "", "", false
	}
	matches := piiSecretAssignPattern.FindStringSubmatch(candidate)
	if len(matches) != 3 {
		return "", "", false
	}
	key := strings.TrimSpace(matches[1])
	if key == "" {
		return "", "", false
	}

	value := strings.TrimSpace(matches[2])
	value = strings.TrimSuffix(value, ",")
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"'`)
	if key == "" {
		return "", "", false
	}
	return key, value, true
}

func isSecretKeyName(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	if lower == "" {
		return false
	}
	for _, term := range piiSecretKeyTerms {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

func isLikelySecretPlaceholder(value string) bool {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	if _, ok := piiPlaceholderValues[lower]; ok {
		return true
	}
	if strings.HasPrefix(lower, "${") || strings.HasPrefix(lower, "$(") || strings.HasPrefix(lower, "$") {
		return true
	}
	if strings.Contains(lower, "example") || strings.Contains(lower, "your_") || strings.Contains(lower, "<") || strings.Contains(lower, ">") {
		return true
	}
	return false
}

func gitignoreContainsPattern(lines []string, required string) bool {
	required = strings.ToLower(strings.TrimSpace(required))
	for _, line := range lines {
		candidate := strings.ToLower(strings.TrimSpace(line))
		if candidate == "" || strings.HasPrefix(candidate, "#") {
			continue
		}
		if strings.HasPrefix(candidate, "!") {
			candidate = strings.TrimSpace(strings.TrimPrefix(candidate, "!"))
		}
		switch required {
		case ".env*":
			if candidate == ".env*" || candidate == ".env" || candidate == ".env.*" || strings.HasSuffix(candidate, "/.env*") {
				return true
			}
		default:
			if candidate == required {
				return true
			}
			if strings.HasPrefix(candidate, "**/") && strings.TrimPrefix(candidate, "**/") == required {
				return true
			}
		}
	}
	return false
}

func readPIIScanGitignoreLines(root string) ([]string, error) {
	path := filepath.Join(root, ".gitignore")
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return strings.Split(string(b), "\n"), nil
}

func listTrackedRepositoryFiles(root string) ([]string, error) {
	cmd := exec.Command("git", "ls-files")
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		rel := strings.TrimSpace(line)
		if rel == "" {
			continue
		}
		out = append(out, filepath.ToSlash(rel))
	}
	sort.Strings(out)
	return out, nil
}

func pathBase(path string) string {
	if path == "" {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(path), "/")
	return parts[len(parts)-1]
}

func adjustFixtureSeverity(rel string, severity PIIScanSeverity) PIIScanSeverity {
	if piiScanSeverityRank(severity) <= piiScanSeverityRank(PIIScanSeverityMedium) {
		return severity
	}
	lower := strings.ToLower(filepath.ToSlash(rel))
	if containsAny(lower, []string{"testdata", "fixtures", "fixture", "_test", "/test/"}) {
		return PIIScanSeverityMedium
	}
	return severity
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
		if left.Detail != right.Detail {
			return left.Detail < right.Detail
		}
		return left.Recommendation < right.Recommendation
	})
}

func summarizePIIScanFindings(findings []PIIScanFinding) PIIScanSummary {
	summary := PIIScanSummary{
		TotalFindings:      len(findings),
		HighestSeverity:    PIIScanSeverityNone,
		FindingsBySeverity: PIIScanSeverityCounts{},
		FindingsByCategory: PIIScanCategoryCounts{},
	}

	for _, finding := range findings {
		switch finding.Severity {
		case PIIScanSeverityLow:
			summary.FindingsBySeverity.Low++
		case PIIScanSeverityMedium:
			summary.FindingsBySeverity.Medium++
		case PIIScanSeverityHigh:
			summary.FindingsBySeverity.High++
		case PIIScanSeverityCritical:
			summary.FindingsBySeverity.Critical++
		}

		switch finding.Category {
		case PIIScanCategoryHardcodedPII:
			summary.FindingsByCategory.HardcodedPII++
		case PIIScanCategoryPIIInLogs:
			summary.FindingsByCategory.PIIInLogs++
		case PIIScanCategoryEnvSecret:
			summary.FindingsByCategory.EnvSecret++
		case PIIScanCategoryUnencryptedData:
			summary.FindingsByCategory.UnencryptedData++
		case PIIScanCategoryGitignoreGap:
			summary.FindingsByCategory.GitignoreCoverage++
		}

		if piiScanSeverityRank(finding.Severity) > piiScanSeverityRank(summary.HighestSeverity) {
			summary.HighestSeverity = finding.Severity
		}
	}

	return summary
}

func parsePIIScanSeverity(value string) (PIIScanSeverity, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
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
