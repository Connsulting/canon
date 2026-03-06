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
	"time"
)

const (
	privacyCheckDefaultContextKB      = 120
	privacyCheckDefaultMaxFileBytes   = 64 * 1024
	privacyCheckDefaultMaxPolicyBytes = 80 * 1024
	privacyCheckTreeDepth             = 4
)

type aiPrivacyCheckResponse struct {
	Model    string                  `json:"model"`
	Findings []aiPrivacyCheckFinding `json:"findings"`
}

type aiPrivacyCheckFinding struct {
	ClaimID          string   `json:"claim_id"`
	Claim            string   `json:"claim"`
	Status           string   `json:"status"`
	Severity         string   `json:"severity"`
	Reason           string   `json:"reason"`
	EvidencePaths    []string `json:"evidence_paths"`
	EvidenceSnippets []string `json:"evidence_snippets"`
}

type privacyCheckCodeContext struct {
	CodePaths     []string
	FoundFiles    int
	IncludedFiles int
	ExcludedFiles int
	Context       string
	ContextBytes  int
	ContextLimit  int
	MaxFileBytes  int
	Truncated     bool
}

func PrivacyCheck(root string, opts PrivacyCheckOptions) (PrivacyCheckResult, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return PrivacyCheckResult{}, err
	}

	policyPath, policyText, err := loadPrivacyPolicyText(absRoot, opts.PolicyFile)
	if err != nil {
		return PrivacyCheckResult{}, err
	}

	codeContext, err := collectPrivacyCheckCodeContext(absRoot, opts.CodePaths, opts.ContextLimitBytes, opts.MaxFileBytes)
	if err != nil {
		return PrivacyCheckResult{}, err
	}
	if codeContext.IncludedFiles == 0 || strings.TrimSpace(codeContext.Context) == "" {
		return PrivacyCheckResult{}, fmt.Errorf("no eligible code files found in privacy-check scope")
	}

	mode := strings.ToLower(strings.TrimSpace(opts.AIMode))
	if mode == "" {
		mode = "auto"
	}
	if strings.TrimSpace(opts.ResponseFile) != "" && mode == "auto" {
		mode = "from-response"
	}

	provider := strings.ToLower(strings.TrimSpace(opts.AIProvider))
	if provider == "" {
		provider = "codex"
	}

	var response aiPrivacyCheckResponse
	switch mode {
	case "auto":
		if !aiProviderRuntimeReady(provider) {
			return PrivacyCheckResult{}, fmt.Errorf("ai provider %s is not runtime-ready", provider)
		}
		response, err = runHeadlessAIPrivacyCheck(provider, absRoot, policyPath, policyText, codeContext)
	case "from-response":
		response, err = parseAIPrivacyCheckResponse(absRoot, opts.ResponseFile)
	default:
		return PrivacyCheckResult{}, fmt.Errorf("unsupported privacy-check ai mode: %s", mode)
	}
	if err != nil {
		return PrivacyCheckResult{}, err
	}

	findings := normalizePrivacyCheckFindings(response.Findings, absRoot)
	summary := summarizePrivacyCheckFindings(findings)

	result := PrivacyCheckResult{
		Root:       absRoot,
		PolicyFile: policyPath,
		CodePaths:  codeContext.CodePaths,
		Context: PrivacyCheckContextSummary{
			FoundFiles:      codeContext.FoundFiles,
			IncludedFiles:   codeContext.IncludedFiles,
			ExcludedFiles:   codeContext.ExcludedFiles,
			ContextBytes:    codeContext.ContextBytes,
			ContextLimit:    codeContext.ContextLimit,
			MaxFileBytes:    codeContext.MaxFileBytes,
			TruncatedToFit:  codeContext.Truncated,
			PolicyBytesUsed: len(policyText),
		},
		Findings: findings,
		Summary:  summary,
	}

	if strings.TrimSpace(string(opts.FailOn)) != "" {
		failOn, err := parsePrivacyCheckSeverity(string(opts.FailOn))
		if err != nil {
			return PrivacyCheckResult{}, err
		}
		if failOn != PrivacyCheckSeverityNone {
			result.FailOn = failOn
			result.ThresholdExceeded = privacyCheckExceedsThreshold(result, failOn)
		}
	}

	return result, nil
}

func loadPrivacyPolicyText(root string, policyFile string) (string, string, error) {
	path := strings.TrimSpace(policyFile)
	if path == "" {
		return "", "", fmt.Errorf("privacy-check requires --policy-file")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", fmt.Errorf("policy file not found: %s", filepath.ToSlash(path))
		}
		return "", "", err
	}

	text := strings.TrimSpace(string(b))
	if text == "" {
		return "", "", fmt.Errorf("policy file is empty: %s", filepath.ToSlash(path))
	}
	if len(text) > privacyCheckDefaultMaxPolicyBytes {
		text = strings.TrimSpace(truncateText(text, privacyCheckDefaultMaxPolicyBytes))
	}
	return path, text, nil
}

func collectPrivacyCheckCodeContext(root string, codePaths []string, contextLimitBytes int, maxFileBytes int64) (privacyCheckCodeContext, error) {
	limit := contextLimitBytes
	if limit <= 0 {
		limit = privacyCheckDefaultContextKB * 1024
	}
	maxFile := maxFileBytes
	if maxFile <= 0 {
		maxFile = privacyCheckDefaultMaxFileBytes
	}

	scopes, err := resolvePrivacyCheckScopePaths(root, codePaths)
	if err != nil {
		return privacyCheckCodeContext{}, err
	}

	ignorePatterns, err := loadGitignorePatterns(root)
	if err != nil {
		return privacyCheckCodeContext{}, err
	}

	seenFiles := make(map[string]struct{})
	allFiles := make([]string, 0)
	candidates := make([]initScanCandidate, 0)
	found := 0
	excluded := 0

	processFile := func(rel string, pathAbs string, ignoreBypass bool, mode fs.FileMode) error {
		rel = filepath.ToSlash(rel)
		rel = strings.TrimPrefix(rel, "./")
		if rel == "" || rel == "." {
			return nil
		}
		if _, ok := seenFiles[rel]; ok {
			return nil
		}
		seenFiles[rel] = struct{}{}

		found++

		if mode&fs.ModeSymlink != 0 {
			excluded++
			return nil
		}
		if !ignoreBypass && matchesIgnorePatterns(rel, false, ignorePatterns) {
			excluded++
			return nil
		}
		if isLikelyBinaryPath(rel) {
			excluded++
			return nil
		}

		info, err := os.Stat(pathAbs)
		if err != nil {
			return err
		}
		if info.Size() > maxFile {
			excluded++
			return nil
		}

		b, err := os.ReadFile(pathAbs)
		if err != nil {
			return err
		}
		if isBinaryContent(b) {
			excluded++
			return nil
		}

		content := string(b)
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		priority := initFilePriority(rel)
		maxPerFile := maxBytesForPriority(priority)
		truncated := false
		if len(content) > maxPerFile {
			content = truncateText(content, maxPerFile)
			truncated = true
		}

		candidates = append(candidates, initScanCandidate{
			Path:      rel,
			Priority:  priority,
			Size:      len(content),
			Content:   content,
			Truncated: truncated,
		})
		allFiles = append(allFiles, rel)
		return nil
	}

	for _, scope := range scopes {
		scopeAbs := filepath.Join(root, scope)
		info, err := os.Lstat(scopeAbs)
		if err != nil {
			return privacyCheckCodeContext{}, err
		}

		if !info.IsDir() {
			if err := processFile(scope, scopeAbs, true, info.Mode()); err != nil {
				return privacyCheckCodeContext{}, err
			}
			continue
		}

		err = filepath.WalkDir(scopeAbs, func(pathAbs string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if pathAbs == scopeAbs {
				return nil
			}
			rel, relErr := filepath.Rel(root, pathAbs)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)

			if entry.IsDir() {
				if matchesIgnorePatterns(rel, true, ignorePatterns) {
					if privacyCheckScopeRequiresDir(rel, scopes) {
						return nil
					}
					return filepath.SkipDir
				}
				if shouldSkipDir(rel) && !privacyCheckScopeRequiresDir(rel, scopes) {
					return filepath.SkipDir
				}
				return nil
			}

			if err := processFile(rel, pathAbs, false, entry.Type()); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			return privacyCheckCodeContext{}, err
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Priority == candidates[j].Priority {
			return candidates[i].Path < candidates[j].Path
		}
		return candidates[i].Priority < candidates[j].Priority
	})
	sort.Strings(allFiles)

	tree := buildDirectoryTree(allFiles, privacyCheckTreeDepth)
	context, included := buildPrivacyCheckContext(tree, candidates, limit)
	excluded += len(candidates) - included
	if excluded < 0 {
		excluded = 0
	}

	return privacyCheckCodeContext{
		CodePaths:     scopes,
		FoundFiles:    found,
		IncludedFiles: included,
		ExcludedFiles: excluded,
		Context:       context,
		ContextBytes:  len(context),
		ContextLimit:  limit,
		MaxFileBytes:  int(maxFile),
		Truncated:     included < len(candidates),
	}, nil
}

func resolvePrivacyCheckScopePaths(root string, codePaths []string) ([]string, error) {
	if len(codePaths) == 0 {
		codePaths = []string{"."}
	}

	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}

	resolved := make([]string, 0, len(codePaths))
	seen := map[string]struct{}{}
	for _, raw := range codePaths {
		scope := strings.TrimSpace(raw)
		if scope == "" {
			continue
		}

		scopeAbs := scope
		if !filepath.IsAbs(scopeAbs) {
			scopeAbs = filepath.Join(root, scopeAbs)
		}
		scopeAbs = filepath.Clean(scopeAbs)

		rel, err := filepath.Rel(root, scopeAbs)
		if err != nil {
			return nil, err
		}
		rel = filepath.ToSlash(rel)
		if rel == ".." || strings.HasPrefix(rel, "../") {
			return nil, fmt.Errorf("code path escapes repository root: %s", scope)
		}
		if rel == "" {
			rel = "."
		}
		rel = filepath.ToSlash(filepath.Clean(rel))
		if rel == "" {
			rel = "."
		}

		if _, err := os.Lstat(scopeAbs); err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("code path not found: %s", filepath.ToSlash(scope))
			}
			return nil, err
		}
		scopeResolved, err := filepath.EvalSymlinks(scopeAbs)
		if err != nil {
			return nil, err
		}
		if !privacyCheckPathWithinRoot(resolvedRoot, scopeResolved) {
			return nil, fmt.Errorf("code path escapes repository root via symlink: %s", filepath.ToSlash(scope))
		}

		if _, ok := seen[rel]; ok {
			continue
		}
		seen[rel] = struct{}{}
		resolved = append(resolved, rel)
	}

	if len(resolved) == 0 {
		resolved = append(resolved, ".")
	}
	sort.Strings(resolved)
	return resolved, nil
}

func privacyCheckScopeRequiresDir(rel string, scopes []string) bool {
	dir := strings.TrimSpace(filepath.ToSlash(rel))
	if dir == "" || dir == "." {
		return false
	}
	for _, scope := range scopes {
		if scope == "." {
			continue
		}
		if scope == dir || strings.HasPrefix(scope, dir+"/") || strings.HasPrefix(dir, scope+"/") {
			return true
		}
	}
	return false
}

func buildPrivacyCheckContext(tree string, candidates []initScanCandidate, limit int) (string, int) {
	if limit <= 0 {
		limit = privacyCheckDefaultContextKB * 1024
	}

	head := strings.Builder{}
	head.WriteString("# Canon Privacy Policy Check Scan\n\n")
	head.WriteString("## Directory Tree (depth-limited)\n")
	head.WriteString(tree)
	head.WriteString("\n")

	context := head.String()
	if len(context) >= limit {
		return context[:limit], 0
	}

	included := 0
	for _, candidate := range candidates {
		section := strings.Builder{}
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
		if len(context)+len(chunk) > limit {
			remaining := limit - len(context)
			if remaining > 256 {
				chunk = truncateText(chunk, remaining)
				context += chunk
				included++
			}
			break
		}
		context += chunk
		included++
	}

	return context, included
}

func runHeadlessAIPrivacyCheck(provider string, root string, policyPath string, policyText string, codeContext privacyCheckCodeContext) (aiPrivacyCheckResponse, error) {
	promptText := buildAIPrivacyCheckPrompt(provider, policyPath, policyText, codeContext)
	schemaText := aiPrivacyCheckJSONSchema()
	timeout := aiRenderTimeout()

	switch provider {
	case "codex":
		schemaFile, err := os.CreateTemp("", "canon-privacy-check-schema-*.json")
		if err != nil {
			return aiPrivacyCheckResponse{}, err
		}
		schemaPath := schemaFile.Name()
		defer func() {
			schemaFile.Close()
			_ = os.Remove(schemaPath)
		}()
		if _, err := schemaFile.WriteString(schemaText); err != nil {
			return aiPrivacyCheckResponse{}, err
		}
		if err := schemaFile.Close(); err != nil {
			return aiPrivacyCheckResponse{}, err
		}

		responseFile, err := os.CreateTemp("", "canon-privacy-check-response-*.json")
		if err != nil {
			return aiPrivacyCheckResponse{}, err
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
		cmd.WaitDelay = 2 * time.Second
		cmd.Stdin = strings.NewReader(promptText)
		output, err := cmd.CombinedOutput()
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return aiPrivacyCheckResponse{}, fmt.Errorf("codex exec timed out after %s", timeout)
			}
			return aiPrivacyCheckResponse{}, fmt.Errorf("codex exec failed: %w\n%s", err, strings.TrimSpace(string(output)))
		}

		responseBytes, err := os.ReadFile(responsePath)
		if err != nil {
			return aiPrivacyCheckResponse{}, err
		}
		return decodeAIPrivacyCheckResponse(responseBytes)

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
		cmd.WaitDelay = 2 * time.Second
		cmd.Dir = root
		output, err := cmd.CombinedOutput()
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return aiPrivacyCheckResponse{}, fmt.Errorf("claude --print timed out after %s", timeout)
			}
			return aiPrivacyCheckResponse{}, fmt.Errorf("claude --print failed: %w\n%s", err, strings.TrimSpace(string(output)))
		}
		return decodeAIPrivacyCheckResponse(output)

	default:
		return aiPrivacyCheckResponse{}, fmt.Errorf("unsupported ai provider: %s", provider)
	}
}

func buildAIPrivacyCheckPrompt(provider string, policyPath string, policyText string, codeContext privacyCheckCodeContext) string {
	lines := []string{
		"# Canon Privacy Policy Consistency Check",
		"",
		"Provider: " + provider,
		"",
		"Task:",
		"1. Evaluate privacy policy claims against provided repository code context.",
		"2. Return one finding for each material claim.",
		"3. Classify each finding as supported, contradicted, or unverifiable.",
		"4. Assign severity: none, low, medium, high, or critical.",
		"5. Include concise evidence paths/snippets whenever possible.",
		"6. Return JSON only using the schema below.",
		"",
		"Rules:",
		"- Use only supplied policy text and code context.",
		"- Prefer contradicted when direct conflict evidence exists.",
		"- Use unverifiable when no direct evidence supports or contradicts the claim.",
		"- Include at least one concrete evidence path for supported/contradicted findings when available.",
		"- Do not emit duplicate findings for the same claim/status pair.",
		"",
		"Schema:",
		"{",
		`  "model": "string",`,
		`  "findings": [`,
		`    {`,
		`      "claim_id": "string (optional)",`,
		`      "claim": "string",`,
		`      "status": "supported|contradicted|unverifiable",`,
		`      "severity": "none|low|medium|high|critical",`,
		`      "reason": "string",`,
		`      "evidence_paths": ["path"],`,
		`      "evidence_snippets": ["snippet"]`,
		`    }`,
		"  ]",
		"}",
		"",
		"## Policy File",
		filepath.ToSlash(policyPath),
		"",
		"## Privacy Policy Text",
		"",
		policyText,
		"",
		"## Code Scope",
		fmt.Sprintf("paths=%s", strings.Join(codeContext.CodePaths, ",")),
		fmt.Sprintf("files=%d included=%d excluded=%d context_bytes=%d", codeContext.FoundFiles, codeContext.IncludedFiles, codeContext.ExcludedFiles, codeContext.ContextBytes),
		"",
		"## Code Context",
		"",
		codeContext.Context,
	}

	return strings.Join(lines, "\n")
}

func aiPrivacyCheckJSONSchema() string {
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
        "required": ["claim", "status", "severity", "reason", "evidence_paths", "evidence_snippets"],
        "additionalProperties": false,
        "properties": {
          "claim_id": {"type": "string"},
          "claim": {"type": "string"},
          "status": {"type": "string", "enum": ["supported", "contradicted", "unverifiable"]},
          "severity": {"type": "string", "enum": ["none", "low", "medium", "high", "critical"]},
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

func parseAIPrivacyCheckResponse(root string, responseFile string) (aiPrivacyCheckResponse, error) {
	path := strings.TrimSpace(responseFile)
	if path == "" {
		return aiPrivacyCheckResponse{}, fmt.Errorf("from-response privacy-check mode requires --response-file")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return aiPrivacyCheckResponse{}, err
	}
	return decodeAIPrivacyCheckResponse(b)
}

func decodeAIPrivacyCheckResponse(b []byte) (aiPrivacyCheckResponse, error) {
	var response aiPrivacyCheckResponse
	if err := json.Unmarshal(b, &response); err == nil {
		if err := validateAIPrivacyCheckResponse(response); err != nil {
			return aiPrivacyCheckResponse{}, err
		}
		return response, nil
	}
	text := strings.TrimSpace(string(b))
	first := strings.Index(text, "{")
	last := strings.LastIndex(text, "}")
	if first == -1 || last == -1 || last <= first {
		return aiPrivacyCheckResponse{}, fmt.Errorf("invalid AI privacy-check response JSON")
	}
	fragment := text[first : last+1]
	if err := json.Unmarshal([]byte(fragment), &response); err != nil {
		return aiPrivacyCheckResponse{}, fmt.Errorf("invalid AI privacy-check response JSON: %w", err)
	}
	if err := validateAIPrivacyCheckResponse(response); err != nil {
		return aiPrivacyCheckResponse{}, err
	}
	return response, nil
}

func validateAIPrivacyCheckResponse(response aiPrivacyCheckResponse) error {
	if strings.TrimSpace(response.Model) == "" {
		return fmt.Errorf("invalid AI privacy-check response JSON: missing model")
	}
	if response.Findings == nil {
		return fmt.Errorf("invalid AI privacy-check response JSON: missing findings")
	}
	return nil
}

func normalizePrivacyCheckFindings(in []aiPrivacyCheckFinding, root string) []PrivacyCheckFinding {
	out := make([]PrivacyCheckFinding, 0, len(in))
	seen := map[string]struct{}{}
	for _, item := range in {
		claim := strings.TrimSpace(item.Claim)
		if claim == "" {
			continue
		}

		status := normalizePrivacyCheckStatus(item.Status)
		severity, err := parsePrivacyCheckSeverity(item.Severity)
		if err != nil {
			severity = defaultPrivacyCheckSeverity(status)
		}
		if severity == PrivacyCheckSeverityNone && status != PrivacyCheckStatusSupported {
			severity = defaultPrivacyCheckSeverity(status)
		}

		claimID := strings.TrimSpace(item.ClaimID)
		if claimID == "" {
			claimID = "claim-" + checksum(strings.ToLower(claim))[:8]
		}

		reason := strings.TrimSpace(item.Reason)
		if reason == "" {
			reason = defaultPrivacyCheckReason(status)
		}

		evidencePaths := normalizePrivacyEvidencePaths(item.EvidencePaths, root)
		evidenceSnippets := normalizePrivacyEvidenceSnippets(item.EvidenceSnippets)

		signature := strings.Join([]string{
			claimID,
			strings.ToLower(claim),
			string(status),
			string(severity),
			reason,
			strings.Join(evidencePaths, "|"),
			strings.Join(evidenceSnippets, "|"),
		}, "::")
		if _, ok := seen[signature]; ok {
			continue
		}
		seen[signature] = struct{}{}

		out = append(out, PrivacyCheckFinding{
			ClaimID:          claimID,
			Claim:            claim,
			Status:           status,
			Severity:         severity,
			Reason:           reason,
			EvidencePaths:    evidencePaths,
			EvidenceSnippets: evidenceSnippets,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		left := out[i]
		right := out[j]

		leftRank := privacyCheckSeverityRank(left.Severity)
		rightRank := privacyCheckSeverityRank(right.Severity)
		if leftRank != rightRank {
			return leftRank > rightRank
		}

		leftStatusRank := privacyCheckStatusRank(left.Status)
		rightStatusRank := privacyCheckStatusRank(right.Status)
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
		return strings.Join(left.EvidencePaths, "|") < strings.Join(right.EvidencePaths, "|")
	})

	return out
}

func normalizePrivacyEvidencePaths(paths []string, root string) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, raw := range paths {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if isWindowsAbsolutePath(value) && !filepath.IsAbs(value) {
			continue
		}

		if filepath.IsAbs(value) {
			rel, err := filepath.Rel(root, value)
			if err != nil {
				continue
			}
			rel = filepath.ToSlash(rel)
			if rel == ".." || strings.HasPrefix(rel, "../") {
				continue
			}
			value = rel
		}
		value = filepath.ToSlash(filepath.Clean(value))
		value = strings.TrimPrefix(value, "./")
		if value == "" || value == "." || value == ".." || strings.HasPrefix(value, "../") {
			continue
		}
		if filepath.IsAbs(value) || isWindowsAbsolutePath(value) {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func normalizePrivacyEvidenceSnippets(snippets []string) []string {
	if len(snippets) == 0 {
		return nil
	}
	out := make([]string, 0, len(snippets))
	seen := map[string]struct{}{}
	for _, raw := range snippets {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func normalizePrivacyCheckStatus(raw string) PrivacyCheckStatus {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(PrivacyCheckStatusSupported):
		return PrivacyCheckStatusSupported
	case string(PrivacyCheckStatusContradicted):
		return PrivacyCheckStatusContradicted
	case string(PrivacyCheckStatusUnverifiable):
		fallthrough
	default:
		return PrivacyCheckStatusUnverifiable
	}
}

func defaultPrivacyCheckSeverity(status PrivacyCheckStatus) PrivacyCheckSeverity {
	switch status {
	case PrivacyCheckStatusContradicted:
		return PrivacyCheckSeverityHigh
	case PrivacyCheckStatusUnverifiable:
		return PrivacyCheckSeverityMedium
	default:
		return PrivacyCheckSeverityNone
	}
}

func defaultPrivacyCheckReason(status PrivacyCheckStatus) string {
	switch status {
	case PrivacyCheckStatusContradicted:
		return "Code evidence appears to contradict this policy claim."
	case PrivacyCheckStatusSupported:
		return "Code evidence appears consistent with this policy claim."
	default:
		return "No direct supporting or contradictory evidence was found in analyzed code context."
	}
}

func summarizePrivacyCheckFindings(findings []PrivacyCheckFinding) PrivacyCheckSummary {
	summary := PrivacyCheckSummary{
		TotalFindings:   len(findings),
		HighestSeverity: PrivacyCheckSeverityNone,
		FindingsByStatus: PrivacyCheckStatusCounts{
			Supported:    0,
			Contradicted: 0,
			Unverifiable: 0,
		},
		FindingsBySeverity: PrivacyCheckSeverityCounts{},
	}

	for _, finding := range findings {
		switch finding.Status {
		case PrivacyCheckStatusSupported:
			summary.FindingsByStatus.Supported++
		case PrivacyCheckStatusContradicted:
			summary.FindingsByStatus.Contradicted++
		case PrivacyCheckStatusUnverifiable:
			summary.FindingsByStatus.Unverifiable++
		}

		switch finding.Severity {
		case PrivacyCheckSeverityLow:
			summary.FindingsBySeverity.Low++
		case PrivacyCheckSeverityMedium:
			summary.FindingsBySeverity.Medium++
		case PrivacyCheckSeverityHigh:
			summary.FindingsBySeverity.High++
		case PrivacyCheckSeverityCritical:
			summary.FindingsBySeverity.Critical++
		}

		if privacyCheckSeverityRank(finding.Severity) > privacyCheckSeverityRank(summary.HighestSeverity) {
			summary.HighestSeverity = finding.Severity
		}
	}

	summary.SupportedClaims = summary.FindingsByStatus.Supported
	summary.ContradictedClaims = summary.FindingsByStatus.Contradicted
	summary.UnverifiableClaims = summary.FindingsByStatus.Unverifiable
	return summary
}

func parsePrivacyCheckSeverity(value string) (PrivacyCheckSeverity, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "", string(PrivacyCheckSeverityNone):
		return PrivacyCheckSeverityNone, nil
	case string(PrivacyCheckSeverityLow):
		return PrivacyCheckSeverityLow, nil
	case string(PrivacyCheckSeverityMedium):
		return PrivacyCheckSeverityMedium, nil
	case string(PrivacyCheckSeverityHigh):
		return PrivacyCheckSeverityHigh, nil
	case string(PrivacyCheckSeverityCritical):
		return PrivacyCheckSeverityCritical, nil
	default:
		return "", fmt.Errorf("unsupported severity %q (expected one of: low, medium, high, critical)", strings.TrimSpace(value))
	}
}

func privacyCheckExceedsThreshold(result PrivacyCheckResult, threshold PrivacyCheckSeverity) bool {
	thresholdRank := privacyCheckSeverityRank(threshold)
	if thresholdRank <= privacyCheckSeverityRank(PrivacyCheckSeverityNone) {
		return false
	}
	return privacyCheckSeverityRank(result.Summary.HighestSeverity) >= thresholdRank
}

func privacyCheckSeverityRank(severity PrivacyCheckSeverity) int {
	switch severity {
	case PrivacyCheckSeverityNone:
		return 0
	case PrivacyCheckSeverityLow:
		return 1
	case PrivacyCheckSeverityMedium:
		return 2
	case PrivacyCheckSeverityHigh:
		return 3
	case PrivacyCheckSeverityCritical:
		return 4
	default:
		return -1
	}
}

func privacyCheckStatusRank(status PrivacyCheckStatus) int {
	switch status {
	case PrivacyCheckStatusContradicted:
		return 0
	case PrivacyCheckStatusUnverifiable:
		return 1
	case PrivacyCheckStatusSupported:
		return 2
	default:
		return 3
	}
}

func privacyCheckPathWithinRoot(root string, candidate string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	rel = filepath.Clean(rel)
	if rel == ".." {
		return false
	}
	prefix := ".." + string(filepath.Separator)
	return !strings.HasPrefix(rel, prefix)
}

func isWindowsAbsolutePath(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return false
	}
	if len(trimmed) >= 2 && isASCIILetter(trimmed[0]) && trimmed[1] == ':' {
		return true
	}
	if strings.HasPrefix(trimmed, `\\`) || strings.HasPrefix(trimmed, `//`) || strings.HasPrefix(trimmed, `\`) {
		return true
	}
	return false
}

func isASCIILetter(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}
