package canon

import (
	"context"
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
	privacyPolicyDefaultContextKB   = 120
	privacyPolicyDefaultMaxFileSize = int64(65536)
	privacyPolicyModeAuto           = "auto"
	privacyPolicyModeFromResponse   = "from-response"
)

type aiPrivacyPolicyResponse struct {
	Model    string                   `json:"model"`
	Findings []aiPrivacyPolicyFinding `json:"findings"`
}

type aiPrivacyPolicyFinding struct {
	ClaimID          string   `json:"claim_id"`
	Claim            string   `json:"claim"`
	Status           string   `json:"status"`
	Severity         string   `json:"severity"`
	Reason           string   `json:"reason"`
	EvidencePaths    []string `json:"evidence_paths"`
	EvidenceSnippets []string `json:"evidence_snippets"`
}

type privacyCodeContext struct {
	CodePaths    []string
	ScannedFiles int
	ContextFiles int
	ContextBytes int
	Context      string
}

type privacyCodeCandidate struct {
	Path    string
	Content string
}

type privacyScopePath struct {
	AbsPath string
	RelPath string
	IsDir   bool
}

func PrivacyPolicyCheck(root string, opts PrivacyPolicyCheckOptions) (PrivacyPolicyCheckResult, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return PrivacyPolicyCheckResult{}, err
	}

	mode, err := normalizePrivacyPolicyMode(opts.AIMode, opts.ResponseFile)
	if err != nil {
		return PrivacyPolicyCheckResult{}, err
	}

	provider := strings.ToLower(strings.TrimSpace(opts.AIProvider))
	if provider == "" {
		provider = "codex"
	}

	policyPath, policyBody, err := loadPrivacyPolicyFile(absRoot, opts.PolicyFile)
	if err != nil {
		return PrivacyPolicyCheckResult{}, err
	}

	contextLimitBytes := opts.ContextLimit * 1024
	if contextLimitBytes <= 0 {
		contextLimitBytes = privacyPolicyDefaultContextKB * 1024
	}
	maxFileBytes := opts.MaxFileBytes
	if maxFileBytes <= 0 {
		maxFileBytes = privacyPolicyDefaultMaxFileSize
	}

	codeContext, err := collectPrivacyCodeContext(absRoot, opts.CodePaths, contextLimitBytes, maxFileBytes)
	if err != nil {
		return PrivacyPolicyCheckResult{}, err
	}

	response, err := resolveAIPrivacyPolicyResponse(absRoot, mode, provider, strings.TrimSpace(opts.ResponseFile), policyBody, codeContext.Context)
	if err != nil {
		return PrivacyPolicyCheckResult{}, err
	}

	findings := normalizePrivacyPolicyFindings(response.Findings)
	summary := summarizePrivacyPolicyFindings(findings)

	result := PrivacyPolicyCheckResult{
		Root:              absRoot,
		PolicyFile:        policyPath,
		CodePaths:         codeContext.CodePaths,
		ScannedFiles:      codeContext.ScannedFiles,
		ContextFiles:      codeContext.ContextFiles,
		ContextBytes:      codeContext.ContextBytes,
		ContextLimitBytes: contextLimitBytes,
		MaxFileBytes:      maxFileBytes,
		Findings:          findings,
		Summary:           summary,
	}

	if strings.TrimSpace(string(opts.FailOn)) != "" {
		failOn, err := parsePrivacyPolicySeverity(string(opts.FailOn))
		if err != nil {
			return PrivacyPolicyCheckResult{}, err
		}
		if failOn != PrivacyPolicySeverityNone {
			result.FailOn = failOn
			result.ThresholdExceeded = privacyPolicyExceedsThreshold(result, failOn)
		}
	}

	return result, nil
}

func normalizePrivacyPolicyMode(mode string, responseFile string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(mode))
	if normalized == "" {
		normalized = privacyPolicyModeAuto
	}
	if strings.TrimSpace(responseFile) != "" && normalized == privacyPolicyModeAuto {
		normalized = privacyPolicyModeFromResponse
	}
	switch normalized {
	case privacyPolicyModeAuto, privacyPolicyModeFromResponse:
		return normalized, nil
	default:
		return "", fmt.Errorf("unsupported privacy-check ai mode: %s", strings.TrimSpace(mode))
	}
}

func loadPrivacyPolicyFile(root string, policyFile string) (string, string, error) {
	pathValue := strings.TrimSpace(policyFile)
	if pathValue == "" {
		return "", "", fmt.Errorf("privacy-check requires --policy-file")
	}
	pathAbs := pathValue
	if !filepath.IsAbs(pathAbs) {
		pathAbs = filepath.Join(root, pathAbs)
	}
	pathAbs = filepath.Clean(pathAbs)

	b, err := os.ReadFile(pathAbs)
	if err != nil {
		return "", "", err
	}
	policyText := strings.TrimSpace(string(b))
	if policyText == "" {
		return "", "", fmt.Errorf("privacy-check policy file is empty: %s", filepath.ToSlash(pathAbs))
	}
	return pathAbs, policyText, nil
}

func collectPrivacyCodeContext(root string, requestedPaths []string, contextLimitBytes int, maxFileBytes int64) (privacyCodeContext, error) {
	scopes, err := resolvePrivacyScopePaths(root, requestedPaths)
	if err != nil {
		return privacyCodeContext{}, err
	}

	ignorePatterns, err := loadGitignorePatterns(root)
	if err != nil {
		return privacyCodeContext{}, err
	}

	candidates := map[string]privacyCodeCandidate{}
	scanned := 0
	explicitPaths := len(requestedPaths) > 0

	for _, scope := range scopes {
		if scope.IsDir {
			err := filepath.WalkDir(scope.AbsPath, func(pathAbs string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				rel, relErr := filepath.Rel(root, pathAbs)
				if relErr != nil {
					return relErr
				}
				rel = filepath.ToSlash(rel)
				if rel == "." {
					return nil
				}

				if entry.IsDir() {
					if shouldSkipDir(rel) {
						return filepath.SkipDir
					}
					return nil
				}

				scanned++
				candidate, ok, readErr := buildPrivacyCandidate(root, rel, pathAbs, entry, ignorePatterns, explicitPaths, maxFileBytes)
				if readErr != nil {
					return readErr
				}
				if !ok {
					return nil
				}
				candidates[candidate.Path] = candidate
				return nil
			})
			if err != nil {
				return privacyCodeContext{}, err
			}
			continue
		}

		entryInfo, err := os.Lstat(scope.AbsPath)
		if err != nil {
			return privacyCodeContext{}, err
		}
		if entryInfo.Mode()&os.ModeSymlink != 0 {
			scanned++
			continue
		}
		if !entryInfo.Mode().IsRegular() {
			continue
		}

		scanned++
		entry := fs.FileInfoToDirEntry(entryInfo)
		candidate, ok, err := buildPrivacyCandidate(root, scope.RelPath, scope.AbsPath, entry, ignorePatterns, true, maxFileBytes)
		if err != nil {
			return privacyCodeContext{}, err
		}
		if ok {
			candidates[candidate.Path] = candidate
		}
	}

	if len(candidates) == 0 {
		return privacyCodeContext{}, fmt.Errorf("privacy-check found no eligible text files in selected code paths")
	}

	ordered := make([]privacyCodeCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		ordered = append(ordered, candidate)
	}
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].Path < ordered[j].Path
	})

	contextText, included := buildPrivacyCodeContextText(ordered, contextLimitBytes)
	if included == 0 {
		return privacyCodeContext{}, fmt.Errorf("privacy-check context-limit is too small to include any code context")
	}

	paths := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		paths = append(paths, scope.RelPath)
	}

	return privacyCodeContext{
		CodePaths:    paths,
		ScannedFiles: scanned,
		ContextFiles: included,
		ContextBytes: len(contextText),
		Context:      contextText,
	}, nil
}

func resolvePrivacyScopePaths(root string, requestedPaths []string) ([]privacyScopePath, error) {
	rawPaths := requestedPaths
	if len(rawPaths) == 0 {
		rawPaths = []string{"."}
	}

	scopes := make([]privacyScopePath, 0, len(rawPaths))
	seen := map[string]struct{}{}

	for _, raw := range rawPaths {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}

		absPath := trimmed
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(root, absPath)
		}
		absPath = filepath.Clean(absPath)
		relPath, err := filepath.Rel(root, absPath)
		if err != nil {
			return nil, err
		}
		relPath = filepath.ToSlash(relPath)
		if relPath == "" {
			relPath = "."
		}
		if relPath == ".." || strings.HasPrefix(relPath, "../") {
			return nil, fmt.Errorf("--code-path %q must be inside root %s", raw, filepath.ToSlash(root))
		}

		statInfo, err := os.Stat(absPath)
		if err != nil {
			return nil, fmt.Errorf("--code-path %q: %w", raw, err)
		}

		key := strings.ToLower(relPath)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		scopes = append(scopes, privacyScopePath{
			AbsPath: absPath,
			RelPath: relPath,
			IsDir:   statInfo.IsDir(),
		})
	}

	if len(scopes) == 0 {
		return nil, fmt.Errorf("privacy-check requires at least one non-empty --code-path")
	}

	sort.Slice(scopes, func(i, j int) bool {
		return scopes[i].RelPath < scopes[j].RelPath
	})
	return scopes, nil
}

func buildPrivacyCandidate(root string, relPath string, absPath string, entry fs.DirEntry, ignorePatterns []ignorePattern, forceInclude bool, maxFileBytes int64) (privacyCodeCandidate, bool, error) {
	if entry.Type()&fs.ModeSymlink != 0 {
		return privacyCodeCandidate{}, false, nil
	}
	if !forceInclude && matchesIgnorePatterns(relPath, false, ignorePatterns) {
		return privacyCodeCandidate{}, false, nil
	}
	if isLikelyBinaryPath(relPath) {
		return privacyCodeCandidate{}, false, nil
	}

	info, err := entry.Info()
	if err != nil {
		return privacyCodeCandidate{}, false, err
	}
	if !info.Mode().IsRegular() {
		return privacyCodeCandidate{}, false, nil
	}
	if info.Size() > maxFileBytes {
		return privacyCodeCandidate{}, false, nil
	}

	b, err := os.ReadFile(absPath)
	if err != nil {
		return privacyCodeCandidate{}, false, err
	}
	if isBinaryContent(b) {
		return privacyCodeCandidate{}, false, nil
	}
	if int64(len(b)) > maxFileBytes {
		return privacyCodeCandidate{}, false, nil
	}

	content := string(b)
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	rel := filepath.ToSlash(relPath)
	if rel == "." {
		rel = filepath.ToSlash(strings.TrimPrefix(strings.TrimPrefix(absPath, root), string(filepath.Separator)))
	}
	return privacyCodeCandidate{Path: rel, Content: content}, true, nil
}

func buildPrivacyCodeContextText(candidates []privacyCodeCandidate, contextLimitBytes int) (string, int) {
	limit := contextLimitBytes
	if limit <= 0 {
		limit = privacyPolicyDefaultContextKB * 1024
	}

	var builder strings.Builder
	builder.WriteString("# Privacy Check Code Context\n\n")
	included := 0

	for _, candidate := range candidates {
		var section strings.Builder
		section.WriteString("## File: ")
		section.WriteString(candidate.Path)
		section.WriteString("\n")
		section.WriteString("```\n")
		section.WriteString(candidate.Content)
		if !strings.HasSuffix(candidate.Content, "\n") {
			section.WriteString("\n")
		}
		section.WriteString("```\n\n")

		chunk := section.String()
		if builder.Len()+len(chunk) > limit {
			remaining := limit - builder.Len()
			if remaining > 256 {
				builder.WriteString(truncateText(chunk, remaining))
				included++
			}
			break
		}

		builder.WriteString(chunk)
		included++
	}

	return builder.String(), included
}

func resolveAIPrivacyPolicyResponse(root string, mode string, provider string, responseFile string, policyBody string, codeContext string) (aiPrivacyPolicyResponse, error) {
	switch mode {
	case privacyPolicyModeAuto:
		if !aiProviderRuntimeReady(provider) {
			return aiPrivacyPolicyResponse{}, fmt.Errorf("ai provider %s is not runtime-ready", provider)
		}
		return runHeadlessAIPrivacyPolicy(provider, root, policyBody, codeContext)
	case privacyPolicyModeFromResponse:
		return parseAIPrivacyPolicyResponse(root, responseFile)
	default:
		return aiPrivacyPolicyResponse{}, fmt.Errorf("unsupported privacy-check ai mode: %s", mode)
	}
}

func runHeadlessAIPrivacyPolicy(provider string, root string, policyBody string, codeContext string) (aiPrivacyPolicyResponse, error) {
	promptText := buildAIPrivacyPolicyPrompt(provider, policyBody, codeContext)
	schemaText := aiPrivacyPolicyJSONSchema()
	timeout := aiRenderTimeout()

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

		ctx := context.Background()
		cancel := func() {}
		if timeout > 0 {
			ctx, cancel = context.WithTimeout(context.Background(), timeout)
		} else {
			ctx, cancel = context.WithCancel(context.Background())
		}
		defer cancel()

		cmd := exec.CommandContext(
			ctx,
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
			if ctx.Err() == context.DeadlineExceeded {
				return aiPrivacyPolicyResponse{}, fmt.Errorf("codex exec timed out after %s", timeout)
			}
			return aiPrivacyPolicyResponse{}, fmt.Errorf("codex exec failed: %w\n%s", err, strings.TrimSpace(string(output)))
		}

		responseBytes, err := os.ReadFile(responsePath)
		if err != nil {
			return aiPrivacyPolicyResponse{}, err
		}
		return decodeAIPrivacyPolicyResponse(responseBytes)

	case "claude":
		ctx := context.Background()
		cancel := func() {}
		if timeout > 0 {
			ctx, cancel = context.WithTimeout(context.Background(), timeout)
		} else {
			ctx, cancel = context.WithCancel(context.Background())
		}
		defer cancel()

		cmd := exec.CommandContext(
			ctx,
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
			if ctx.Err() == context.DeadlineExceeded {
				return aiPrivacyPolicyResponse{}, fmt.Errorf("claude --print timed out after %s", timeout)
			}
			return aiPrivacyPolicyResponse{}, fmt.Errorf("claude --print failed: %w\n%s", err, strings.TrimSpace(string(output)))
		}
		return decodeAIPrivacyPolicyResponse(output)

	default:
		return aiPrivacyPolicyResponse{}, fmt.Errorf("unsupported ai provider: %s", provider)
	}
}

func buildAIPrivacyPolicyPrompt(provider string, policyBody string, codeContext string) string {
	return strings.Join([]string{
		"# Canon Privacy Policy Consistency Check",
		"",
		"Provider: " + provider,
		"",
		"Task:",
		"1. Review the privacy policy claims.",
		"2. Compare each claim against the repository code context.",
		"3. Return findings only as JSON that matches the schema.",
		"",
		"Status values:",
		"- supported: code evidence supports the claim",
		"- contradicted: code evidence conflicts with the claim",
		"- unverifiable: not enough evidence in supplied context",
		"",
		"Severity values:",
		"- low, medium, high, critical",
		"",
		"Rules:",
		"- Use exact repository-relative paths for evidence_paths when possible.",
		"- Evidence snippets should be concise and directly relevant.",
		"- Skip speculative claims not grounded in policy text.",
		"",
		"## Privacy Policy",
		"```",
		policyBody,
		"```",
		"",
		"## Code Context",
		codeContext,
	}, "\n")
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
        "required": ["claim_id", "claim", "status", "severity", "reason", "evidence_paths", "evidence_snippets"],
        "additionalProperties": false,
        "properties": {
          "claim_id": {"type": "string"},
          "claim": {"type": "string"},
          "status": {"type": "string"},
          "severity": {"type": "string"},
          "reason": {"type": "string"},
          "evidence_paths": {
            "type": "array",
            "items": {"type": "string"}
          },
          "evidence_snippets": {
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
	pathValue := strings.TrimSpace(responseFile)
	if pathValue == "" {
		return aiPrivacyPolicyResponse{}, fmt.Errorf("from-response privacy-check mode requires --response-file")
	}
	if !filepath.IsAbs(pathValue) {
		pathValue = filepath.Join(root, pathValue)
	}
	b, err := os.ReadFile(pathValue)
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
		return aiPrivacyPolicyResponse{}, fmt.Errorf("invalid privacy-check AI response JSON")
	}
	fragment := text[first : last+1]
	if err := json.Unmarshal([]byte(fragment), &response); err != nil {
		return aiPrivacyPolicyResponse{}, fmt.Errorf("invalid privacy-check AI response JSON: %w", err)
	}
	return response, nil
}

func normalizePrivacyPolicyFindings(items []aiPrivacyPolicyFinding) []PrivacyPolicyFinding {
	findings := make([]PrivacyPolicyFinding, 0, len(items))
	for i, item := range items {
		claim := strings.TrimSpace(item.Claim)
		if claim == "" {
			continue
		}

		claimID := strings.TrimSpace(item.ClaimID)
		if claimID == "" {
			claimID = fmt.Sprintf("claim-%03d", i+1)
		}

		status := normalizePrivacyPolicyStatus(item.Status)
		severity := normalizePrivacyPolicyFindingSeverity(item.Severity, status)

		reason := strings.TrimSpace(item.Reason)
		if reason == "" {
			reason = "No adjudication reason provided"
		}

		finding := PrivacyPolicyFinding{
			ClaimID:          claimID,
			Claim:            claim,
			Status:           status,
			Severity:         severity,
			Reason:           reason,
			EvidencePaths:    normalizePrivacyPolicyEvidencePaths(item.EvidencePaths),
			EvidenceSnippets: normalizePrivacyPolicyEvidenceSnippets(item.EvidenceSnippets),
		}
		findings = append(findings, finding)
	}

	findings = dedupePrivacyPolicyFindings(findings)
	sort.Slice(findings, func(i, j int) bool {
		left := findings[i]
		right := findings[j]

		leftSeverityRank := privacyPolicySeverityRank(left.Severity)
		rightSeverityRank := privacyPolicySeverityRank(right.Severity)
		if leftSeverityRank != rightSeverityRank {
			return leftSeverityRank > rightSeverityRank
		}

		leftStatusRank := privacyPolicyStatusRank(left.Status)
		rightStatusRank := privacyPolicyStatusRank(right.Status)
		if leftStatusRank != rightStatusRank {
			return leftStatusRank < rightStatusRank
		}

		if left.ClaimID != right.ClaimID {
			return left.ClaimID < right.ClaimID
		}
		if left.Claim != right.Claim {
			return left.Claim < right.Claim
		}
		if left.Reason != right.Reason {
			return left.Reason < right.Reason
		}
		leftPaths := strings.Join(left.EvidencePaths, "|")
		rightPaths := strings.Join(right.EvidencePaths, "|")
		if leftPaths != rightPaths {
			return leftPaths < rightPaths
		}
		return strings.Join(left.EvidenceSnippets, "|") < strings.Join(right.EvidenceSnippets, "|")
	})

	return findings
}

func normalizePrivacyPolicyStatus(value string) PrivacyPolicyFindingStatus {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(PrivacyPolicyFindingStatusSupported):
		return PrivacyPolicyFindingStatusSupported
	case string(PrivacyPolicyFindingStatusContradicted):
		return PrivacyPolicyFindingStatusContradicted
	case string(PrivacyPolicyFindingStatusUnverifiable):
		return PrivacyPolicyFindingStatusUnverifiable
	default:
		return PrivacyPolicyFindingStatusUnverifiable
	}
}

func normalizePrivacyPolicyFindingSeverity(value string, status PrivacyPolicyFindingStatus) PrivacyPolicySeverity {
	severity, err := parsePrivacyPolicySeverity(value)
	if err == nil && severity != PrivacyPolicySeverityNone {
		return severity
	}

	switch status {
	case PrivacyPolicyFindingStatusSupported:
		return PrivacyPolicySeverityLow
	case PrivacyPolicyFindingStatusContradicted:
		return PrivacyPolicySeverityHigh
	default:
		return PrivacyPolicySeverityMedium
	}
}

func normalizePrivacyPolicyEvidencePaths(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(filepath.ToSlash(value))
		trimmed = strings.TrimPrefix(trimmed, "./")
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizePrivacyPolicyEvidenceSnippets(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func dedupePrivacyPolicyFindings(findings []PrivacyPolicyFinding) []PrivacyPolicyFinding {
	if len(findings) <= 1 {
		return findings
	}
	out := make([]PrivacyPolicyFinding, 0, len(findings))
	seen := map[string]struct{}{}
	for _, finding := range findings {
		sig := strings.Join([]string{
			finding.ClaimID,
			finding.Claim,
			string(finding.Status),
			string(finding.Severity),
			finding.Reason,
			strings.Join(finding.EvidencePaths, "\x1f"),
			strings.Join(finding.EvidenceSnippets, "\x1f"),
		}, "\x1e")
		if _, ok := seen[sig]; ok {
			continue
		}
		seen[sig] = struct{}{}
		out = append(out, finding)
	}
	return out
}

func summarizePrivacyPolicyFindings(findings []PrivacyPolicyFinding) PrivacyPolicySummary {
	summary := PrivacyPolicySummary{
		TotalFindings:      len(findings),
		HighestSeverity:    PrivacyPolicySeverityNone,
		FindingsByStatus:   PrivacyPolicyStatusCounts{},
		FindingsBySeverity: PrivacyPolicySeverityCounts{},
	}

	for _, finding := range findings {
		switch finding.Status {
		case PrivacyPolicyFindingStatusSupported:
			summary.FindingsByStatus.Supported++
		case PrivacyPolicyFindingStatusContradicted:
			summary.FindingsByStatus.Contradicted++
		case PrivacyPolicyFindingStatusUnverifiable:
			summary.FindingsByStatus.Unverifiable++
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

func privacyPolicyExceedsThreshold(result PrivacyPolicyCheckResult, threshold PrivacyPolicySeverity) bool {
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

func privacyPolicyStatusRank(status PrivacyPolicyFindingStatus) int {
	switch status {
	case PrivacyPolicyFindingStatusContradicted:
		return 0
	case PrivacyPolicyFindingStatusUnverifiable:
		return 1
	case PrivacyPolicyFindingStatusSupported:
		return 2
	default:
		return 3
	}
}
