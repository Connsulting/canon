package canon

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	privacyPolicyDefaultContextLimit = 100 * 1024
	privacyPolicyDefaultMaxFileBytes = 8 * 1024
)

type aiPrivacyPolicyResponse struct {
	Model    string                   `json:"model"`
	Findings []aiPrivacyPolicyFinding `json:"findings"`
}

type aiPrivacyPolicyFinding struct {
	ClaimKey string   `json:"claim_key"`
	Claim    string   `json:"claim"`
	Status   string   `json:"status"`
	Severity string   `json:"severity"`
	Message  string   `json:"message"`
	Evidence []string `json:"evidence"`
}

type privacyPolicyContextFile struct {
	Path      string
	Content   string
	Truncated bool
}

func PrivacyPolicyCheck(root string, opts PrivacyPolicyOptions) (PrivacyPolicyResult, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return PrivacyPolicyResult{}, err
	}

	mode := strings.ToLower(strings.TrimSpace(opts.AIMode))
	if mode == "" {
		mode = "auto"
	}
	responseFile := strings.TrimSpace(opts.ResponseFile)
	if responseFile != "" && mode == "auto" {
		mode = "from-response"
	}
	if mode != "auto" && mode != "from-response" {
		return PrivacyPolicyResult{}, fmt.Errorf("unsupported privacy-check ai mode: %s", mode)
	}

	policyPath, policyText, err := loadPrivacyPolicyFile(absRoot, opts.PolicyFile)
	if err != nil {
		return PrivacyPolicyResult{}, err
	}

	contextLimit := opts.ContextLimit
	if contextLimit <= 0 {
		contextLimit = privacyPolicyDefaultContextLimit
	}
	maxFileBytes := opts.MaxFileBytes
	if maxFileBytes <= 0 {
		maxFileBytes = privacyPolicyDefaultMaxFileBytes
	}

	codeRoots, err := resolvePrivacyPolicyCodePaths(absRoot, opts.CodePaths)
	if err != nil {
		return PrivacyPolicyResult{}, err
	}
	contextFiles, contextText, err := collectPrivacyPolicyContext(absRoot, codeRoots, maxFileBytes, contextLimit)
	if err != nil {
		return PrivacyPolicyResult{}, err
	}

	provider := strings.ToLower(strings.TrimSpace(opts.AIProvider))
	if provider == "" {
		provider = "codex"
	}

	var response aiPrivacyPolicyResponse
	switch mode {
	case "auto":
		if !aiProviderRuntimeReady(provider) {
			return PrivacyPolicyResult{}, fmt.Errorf("ai provider %s is not runtime-ready", provider)
		}
		response, err = runHeadlessAIPrivacyPolicy(provider, absRoot, policyText, contextText)
	case "from-response":
		response, err = parseAIPrivacyPolicyResponse(absRoot, responseFile)
	}
	if err != nil {
		return PrivacyPolicyResult{}, err
	}

	if strings.TrimSpace(response.Model) == "" {
		response.Model = "headless-ai"
	}
	if response.Findings == nil {
		return PrivacyPolicyResult{}, fmt.Errorf("AI privacy-check response missing findings")
	}

	findings := normalizePrivacyPolicyFindings(response.Findings)
	if len(response.Findings) > 0 && len(findings) == 0 {
		return PrivacyPolicyResult{}, fmt.Errorf("AI privacy-check response contains no valid findings")
	}
	summary := summarizePrivacyPolicyFindings(findings)
	result := PrivacyPolicyResult{
		Root:             absRoot,
		PolicyFile:       policyPath,
		CodePathCount:    len(codeRoots),
		ContextFileCount: len(contextFiles),
		ContextBytes:     len(contextText),
		ContextLimit:     contextLimit,
		MaxFileBytes:     maxFileBytes,
		Findings:         findings,
		Summary:          summary,
	}

	if strings.TrimSpace(string(opts.FailOn)) != "" {
		failOn, err := parsePrivacyPolicySeverity(string(opts.FailOn))
		if err != nil {
			return PrivacyPolicyResult{}, err
		}
		if failOn != PrivacyPolicySeverityNone {
			result.FailOn = failOn
			result.ThresholdExceeded = privacyPolicyExceedsThreshold(result, failOn)
		}
	}

	return result, nil
}

func loadPrivacyPolicyFile(root string, policyFile string) (string, string, error) {
	path := strings.TrimSpace(policyFile)
	if path == "" {
		return "", "", fmt.Errorf("privacy-check requires --policy-file")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("policy file must be inside root: %s", filepath.ToSlash(path))
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", fmt.Errorf("policy file not found: %s", filepath.ToSlash(path))
		}
		return "", "", err
	}
	if info.IsDir() {
		return "", "", fmt.Errorf("policy file must be a file: %s", filepath.ToSlash(path))
	}

	body, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	if isBinaryContent(body) {
		return "", "", fmt.Errorf("policy file appears to be binary: %s", filepath.ToSlash(path))
	}
	text := strings.TrimSpace(string(body))
	if text == "" {
		return "", "", fmt.Errorf("policy file is empty: %s", filepath.ToSlash(path))
	}
	return filepath.ToSlash(path), text, nil
}

func resolvePrivacyPolicyCodePaths(root string, codePaths []string) ([]string, error) {
	if len(codePaths) == 0 {
		return []string{root}, nil
	}

	resolved := make([]string, 0, len(codePaths))
	seen := make(map[string]struct{}, len(codePaths))
	for _, raw := range codePaths {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, p)
		}
		p = filepath.Clean(p)

		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil, err
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("code path must be inside root: %s", filepath.ToSlash(p))
		}
		if _, err := os.Stat(p); err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("code path not found: %s", filepath.ToSlash(p))
			}
			return nil, err
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		resolved = append(resolved, p)
	}

	if len(resolved) == 0 {
		return nil, fmt.Errorf("privacy-check requires at least one valid --code-path when provided")
	}
	sort.Strings(resolved)
	return resolved, nil
}

func collectPrivacyPolicyContext(root string, codeRoots []string, maxFileBytes int, contextLimit int) ([]privacyPolicyContextFile, string, error) {
	candidates := make([]privacyPolicyContextFile, 0)
	seen := make(map[string]struct{})

	appendFile := func(absPath string) error {
		rel, err := filepath.Rel(root, absPath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." || rel == "" {
			return nil
		}
		if _, ok := seen[rel]; ok {
			return nil
		}
		if isLikelyBinaryPath(rel) {
			return nil
		}

		body, err := os.ReadFile(absPath)
		if err != nil {
			return err
		}
		if isBinaryContent(body) {
			return nil
		}

		text := string(body)
		text = strings.TrimSpace(text)
		if text == "" {
			return nil
		}

		truncated := false
		if maxFileBytes > 0 && len(text) > maxFileBytes {
			text = strings.TrimSpace(truncateText(text, maxFileBytes))
			truncated = true
			if text == "" {
				return nil
			}
		}

		seen[rel] = struct{}{}
		candidates = append(candidates, privacyPolicyContextFile{
			Path:      rel,
			Content:   text,
			Truncated: truncated,
		})
		return nil
	}

	for _, codeRoot := range codeRoots {
		info, err := os.Lstat(codeRoot)
		if err != nil {
			return nil, "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if !info.IsDir() {
			if err := appendFile(codeRoot); err != nil {
				return nil, "", err
			}
			continue
		}

		err = filepath.WalkDir(codeRoot, func(absPath string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if absPath == codeRoot {
				return nil
			}

			rel, err := filepath.Rel(root, absPath)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)

			if entry.IsDir() {
				if shouldSkipDir(rel) {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Type()&fs.ModeSymlink != 0 {
				return nil
			}
			return appendFile(absPath)
		})
		if err != nil {
			return nil, "", err
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Path < candidates[j].Path
	})

	selected := make([]privacyPolicyContextFile, 0, len(candidates))
	var b strings.Builder
	for _, file := range candidates {
		block := renderPrivacyPolicyContextBlock(file)
		if contextLimit > 0 && b.Len()+len(block) > contextLimit {
			break
		}
		selected = append(selected, file)
		b.WriteString(block)
	}

	return selected, b.String(), nil
}

func renderPrivacyPolicyContextBlock(file privacyPolicyContextFile) string {
	title := file.Path
	if file.Truncated {
		title += " (truncated)"
	}
	return "### " + title + "\n```text\n" + file.Content + "\n```\n\n"
}

func runHeadlessAIPrivacyPolicy(provider string, root string, policyText string, contextText string) (aiPrivacyPolicyResponse, error) {
	promptText := buildAIPrivacyPolicyPrompt(provider, policyText, contextText)
	schemaText := aiPrivacyPolicyJSONSchema()

	switch provider {
	case "codex":
		schemaFile, err := os.CreateTemp("", "canon-privacy-schema-*.json")
		if err != nil {
			return aiPrivacyPolicyResponse{}, err
		}
		schemaPath := schemaFile.Name()
		defer func() {
			schemaFile.Close()
			_ = os.Remove(schemaPath)
		}()
		if _, err := schemaFile.WriteString(schemaText); err != nil {
			return aiPrivacyPolicyResponse{}, err
		}
		if err := schemaFile.Close(); err != nil {
			return aiPrivacyPolicyResponse{}, err
		}

		responseFile, err := os.CreateTemp("", "canon-privacy-response-*.json")
		if err != nil {
			return aiPrivacyPolicyResponse{}, err
		}
		responsePath := responseFile.Name()
		responseFile.Close()
		defer func() { _ = os.Remove(responsePath) }()

		cmd := exec.Command(
			"codex",
			"exec",
			"-C",
			root,
			"--skip-git-repo-check",
			"--output-schema",
			schemaPath,
			"-o",
			responsePath,
			"-",
		)
		cmd.Stdin = strings.NewReader(promptText)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return aiPrivacyPolicyResponse{}, fmt.Errorf("codex exec failed: %w\n%s", err, strings.TrimSpace(string(output)))
		}

		responseBytes, err := os.ReadFile(responsePath)
		if err != nil {
			return aiPrivacyPolicyResponse{}, err
		}
		return decodeAIPrivacyPolicyResponse(responseBytes)

	case "claude":
		cmd := exec.Command(
			"claude",
			"--print",
			"--output-format",
			"json",
			"--json-schema",
			schemaText,
			promptText,
		)
		cmd.Dir = root
		output, err := cmd.CombinedOutput()
		if err != nil {
			return aiPrivacyPolicyResponse{}, fmt.Errorf("claude --print failed: %w\n%s", err, strings.TrimSpace(string(output)))
		}
		return decodeAIPrivacyPolicyResponse(output)

	default:
		return aiPrivacyPolicyResponse{}, fmt.Errorf("unsupported ai provider: %s", provider)
	}
}

func buildAIPrivacyPolicyPrompt(provider string, policyText string, contextText string) string {
	lines := []string{
		"# Canon Privacy Policy Consistency Check",
		"",
		"Provider: " + provider,
		"",
		"Task:",
		"1. Review the privacy policy and the supplied repository code context.",
		"2. Identify policy claims and adjudicate each as supported, contradicted, or unknown.",
		"3. Return JSON only that conforms to the schema.",
		"",
		"Adjudication rules:",
		"- status=supported when evidence clearly aligns with a policy claim.",
		"- status=contradicted when evidence conflicts with a policy claim.",
		"- status=unknown when evidence is insufficient or ambiguous.",
		"- use severity none for supported unless a stricter severity is justified.",
		"- include concrete evidence strings with file paths when possible.",
		"",
		"Schema:",
		"{",
		`  "model": "string",`,
		`  "findings": [`,
		`    {`,
		`      "claim_key": "string",`,
		`      "claim": "string",`,
		`      "status": "supported|contradicted|unknown",`,
		`      "severity": "none|low|medium|high|critical",`,
		`      "message": "string",`,
		`      "evidence": ["string"]`,
		`    }`,
		`  ]`,
		"}",
		"",
		"## Privacy Policy",
		"",
		policyText,
		"",
		"## Code Context",
		"",
	}

	if strings.TrimSpace(contextText) == "" {
		lines = append(lines, "(no code context files selected)")
	} else {
		lines = append(lines, contextText)
	}

	return strings.Join(lines, "\n")
}

func aiPrivacyPolicyJSONSchema() string {
	return `{
  "type": "object",
  "required": ["model", "findings"],
  "additionalProperties": false,
  "properties": {
    "model": {"type": "string"},
    "findings": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["claim_key", "claim", "status", "severity", "message", "evidence"],
        "additionalProperties": false,
        "properties": {
          "claim_key": {"type": "string"},
          "claim": {"type": "string"},
          "status": {"type": "string", "enum": ["supported", "contradicted", "unknown"]},
          "severity": {"type": "string", "enum": ["none", "low", "medium", "high", "critical"]},
          "message": {"type": "string"},
          "evidence": {
            "type": "array",
            "items": {"type": "string"}
          }
        }
      }
    }
  }
}`
}

func parseAIPrivacyPolicyResponse(root string, responseFile string) (aiPrivacyPolicyResponse, error) {
	path := strings.TrimSpace(responseFile)
	if path == "" {
		return aiPrivacyPolicyResponse{}, fmt.Errorf("from-response privacy-check mode requires --response-file")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return aiPrivacyPolicyResponse{}, err
	}
	return decodeAIPrivacyPolicyResponse(b)
}

func decodeAIPrivacyPolicyResponse(b []byte) (aiPrivacyPolicyResponse, error) {
	var response aiPrivacyPolicyResponse
	if err := json.Unmarshal(b, &response); err == nil {
		return response, nil
	}
	text := strings.TrimSpace(string(b))
	first := strings.Index(text, "{")
	last := strings.LastIndex(text, "}")
	if first == -1 || last == -1 || last <= first {
		return aiPrivacyPolicyResponse{}, fmt.Errorf("invalid AI privacy-check response JSON")
	}
	fragment := text[first : last+1]
	if err := json.Unmarshal([]byte(fragment), &response); err != nil {
		return aiPrivacyPolicyResponse{}, fmt.Errorf("invalid AI privacy-check response JSON: %w", err)
	}
	return response, nil
}

func normalizePrivacyPolicyFindings(raw []aiPrivacyPolicyFinding) []PrivacyPolicyFinding {
	normalized := make([]PrivacyPolicyFinding, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))

	for i, item := range raw {
		status, ok := parsePrivacyPolicyStatus(item.Status)
		if !ok {
			continue
		}

		claimKey := normalizePrivacyPolicyClaimKey(item.ClaimKey)
		claim := strings.TrimSpace(item.Claim)
		if claimKey == "" {
			claimKey = normalizePrivacyPolicyClaimKey(claim)
		}
		if claimKey == "" {
			claimKey = fmt.Sprintf("claim-%03d", i+1)
		}
		if claim == "" {
			claim = claimKey
		}

		severity, ok := parsePrivacyPolicySeverityLoose(item.Severity)
		if !ok {
			severity = defaultPrivacyPolicySeverityForStatus(status)
		}

		message := strings.TrimSpace(item.Message)
		if message == "" {
			switch status {
			case PrivacyPolicyStatusSupported:
				message = "implementation appears consistent with policy claim"
			case PrivacyPolicyStatusContradicted:
				message = "implementation appears to contradict policy claim"
			default:
				message = "insufficient evidence to validate policy claim"
			}
		}

		evidence := normalizeList(item.Evidence)
		finding := PrivacyPolicyFinding{
			ClaimKey: claimKey,
			Claim:    claim,
			Status:   status,
			Severity: severity,
			Message:  message,
			Evidence: evidence,
		}

		sig := finding.ClaimKey + "|" + finding.Claim + "|" + string(finding.Status) + "|" + string(finding.Severity) + "|" + finding.Message + "|" + strings.Join(finding.Evidence, "\x1f")
		if _, exists := seen[sig]; exists {
			continue
		}
		seen[sig] = struct{}{}
		normalized = append(normalized, finding)
	}

	sortPrivacyPolicyFindings(normalized)
	return normalized
}

func sortPrivacyPolicyFindings(findings []PrivacyPolicyFinding) {
	sort.Slice(findings, func(i, j int) bool {
		left := findings[i]
		right := findings[j]

		leftSeverity := privacyPolicySeverityRank(left.Severity)
		rightSeverity := privacyPolicySeverityRank(right.Severity)
		if leftSeverity != rightSeverity {
			return leftSeverity > rightSeverity
		}

		leftStatus := privacyPolicyStatusRank(left.Status)
		rightStatus := privacyPolicyStatusRank(right.Status)
		if leftStatus != rightStatus {
			return leftStatus > rightStatus
		}

		if left.ClaimKey != right.ClaimKey {
			return left.ClaimKey < right.ClaimKey
		}
		if left.Claim != right.Claim {
			return left.Claim < right.Claim
		}
		if left.Message != right.Message {
			return left.Message < right.Message
		}

		return strings.Join(left.Evidence, "\x1f") < strings.Join(right.Evidence, "\x1f")
	})
}

func summarizePrivacyPolicyFindings(findings []PrivacyPolicyFinding) PrivacyPolicySummary {
	summary := PrivacyPolicySummary{
		TotalFindings:      len(findings),
		HighestSeverity:    PrivacyPolicySeverityNone,
		FindingsBySeverity: PrivacyPolicySeverityCounts{},
		FindingsByStatus:   PrivacyPolicyStatusCounts{},
	}

	for _, finding := range findings {
		switch finding.Status {
		case PrivacyPolicyStatusSupported:
			summary.FindingsByStatus.Supported++
		case PrivacyPolicyStatusContradicted:
			summary.FindingsByStatus.Contradicted++
		case PrivacyPolicyStatusUnknown:
			summary.FindingsByStatus.Unknown++
		}

		switch finding.Severity {
		case PrivacyPolicySeverityLow:
			summary.FindingsBySeverity.Low++
		case PrivacyPolicySeverityMedium:
			summary.FindingsBySeverity.Medium++
		case PrivacyPolicySeverityHigh:
			summary.FindingsBySeverity.High++
		case PrivacyPolicySeverityCritical:
			summary.FindingsBySeverity.Critical++
		}

		if privacyPolicySeverityRank(finding.Severity) > privacyPolicySeverityRank(summary.HighestSeverity) {
			summary.HighestSeverity = finding.Severity
		}
	}

	return summary
}

func parsePrivacyPolicySeverity(value string) (PrivacyPolicySeverity, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "", string(PrivacyPolicySeverityNone):
		return PrivacyPolicySeverityNone, nil
	case string(PrivacyPolicySeverityLow):
		return PrivacyPolicySeverityLow, nil
	case string(PrivacyPolicySeverityMedium):
		return PrivacyPolicySeverityMedium, nil
	case string(PrivacyPolicySeverityHigh):
		return PrivacyPolicySeverityHigh, nil
	case string(PrivacyPolicySeverityCritical):
		return PrivacyPolicySeverityCritical, nil
	default:
		return "", fmt.Errorf("unsupported severity %q (expected one of: low, medium, high, critical)", strings.TrimSpace(value))
	}
}

func parsePrivacyPolicySeverityLoose(value string) (PrivacyPolicySeverity, bool) {
	if strings.TrimSpace(value) == "" {
		return PrivacyPolicySeverityNone, false
	}
	severity, err := parsePrivacyPolicySeverity(value)
	if err != nil {
		return PrivacyPolicySeverityNone, false
	}
	return severity, true
}

func parsePrivacyPolicyStatus(value string) (PrivacyPolicyStatus, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case string(PrivacyPolicyStatusSupported), "consistent", "compliant":
		return PrivacyPolicyStatusSupported, true
	case string(PrivacyPolicyStatusContradicted), "inconsistent", "violation", "violated", "non-compliant", "noncompliant":
		return PrivacyPolicyStatusContradicted, true
	case string(PrivacyPolicyStatusUnknown), "unclear", "needs-review", "needs_review", "insufficient-evidence", "insufficient_evidence":
		return PrivacyPolicyStatusUnknown, true
	default:
		return "", false
	}
}

func privacyPolicyExceedsThreshold(result PrivacyPolicyResult, threshold PrivacyPolicySeverity) bool {
	thresholdRank := privacyPolicySeverityRank(threshold)
	if thresholdRank <= privacyPolicySeverityRank(PrivacyPolicySeverityNone) {
		return false
	}
	return privacyPolicySeverityRank(result.Summary.HighestSeverity) >= thresholdRank
}

func privacyPolicySeverityRank(severity PrivacyPolicySeverity) int {
	switch severity {
	case PrivacyPolicySeverityNone:
		return 0
	case PrivacyPolicySeverityLow:
		return 1
	case PrivacyPolicySeverityMedium:
		return 2
	case PrivacyPolicySeverityHigh:
		return 3
	case PrivacyPolicySeverityCritical:
		return 4
	default:
		return -1
	}
}

func privacyPolicyStatusRank(status PrivacyPolicyStatus) int {
	switch status {
	case PrivacyPolicyStatusSupported:
		return 1
	case PrivacyPolicyStatusUnknown:
		return 2
	case PrivacyPolicyStatusContradicted:
		return 3
	default:
		return -1
	}
}

func defaultPrivacyPolicySeverityForStatus(status PrivacyPolicyStatus) PrivacyPolicySeverity {
	switch status {
	case PrivacyPolicyStatusSupported:
		return PrivacyPolicySeverityNone
	case PrivacyPolicyStatusUnknown:
		return PrivacyPolicySeverityLow
	case PrivacyPolicyStatusContradicted:
		return PrivacyPolicySeverityMedium
	default:
		return PrivacyPolicySeverityNone
	}
}

func normalizePrivacyPolicyClaimKey(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return ""
	}
	trimmed = strings.ReplaceAll(trimmed, " ", "-")
	trimmed = strings.ReplaceAll(trimmed, "_", "-")
	trimmed = strings.ReplaceAll(trimmed, "/", "-")
	trimmed = strings.ReplaceAll(trimmed, "--", "-")
	trimmed = strings.Trim(trimmed, "-")
	if trimmed == "" {
		return ""
	}
	return trimmed
}
