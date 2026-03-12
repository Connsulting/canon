package canon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type aiSemanticDiffResponse struct {
	Model        string                      `json:"model"`
	Summary      string                      `json:"summary"`
	Explanations []aiSemanticDiffExplanation `json:"explanations"`
}

type aiSemanticDiffExplanation struct {
	ID        string                   `json:"id"`
	Category  string                   `json:"category"`
	Impact    string                   `json:"impact"`
	Summary   string                   `json:"summary"`
	Rationale string                   `json:"rationale"`
	Evidence  []aiSemanticDiffEvidence `json:"evidence"`
}

type aiSemanticDiffEvidence struct {
	File     string `json:"file"`
	Kind     string `json:"kind"`
	OldStart int    `json:"old_start"`
	OldLines int    `json:"old_lines"`
	NewStart int    `json:"new_start"`
	NewLines int    `json:"new_lines"`
}

type semanticDiffFileAccumulator struct {
	Path    string
	Status  string
	Added   int
	Deleted int
	Hunks   int
}

func SemanticDiff(root string, opts SemanticDiffOptions) (SemanticDiffResult, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return SemanticDiffResult{}, err
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
		return SemanticDiffResult{}, fmt.Errorf("unsupported semantic-diff ai mode: %s", mode)
	}

	provider := strings.ToLower(strings.TrimSpace(opts.AIProvider))
	if provider == "" {
		provider = "codex"
	}

	input, err := collectSemanticDiffInput(absRoot, strings.TrimSpace(opts.DiffFile))
	if err != nil {
		return SemanticDiffResult{}, err
	}

	var response aiSemanticDiffResponse
	switch mode {
	case "from-response":
		response, err = parseAISemanticDiffResponse(absRoot, responseFile)
	case "auto":
		if !aiProviderRuntimeReady(provider) {
			return SemanticDiffResult{}, fmt.Errorf("ai provider %s is not runtime-ready", provider)
		}
		response, err = runHeadlessAISemanticDiff(provider, absRoot, input)
	default:
		return SemanticDiffResult{}, fmt.Errorf("unsupported semantic-diff ai mode: %s", mode)
	}
	if err != nil {
		return SemanticDiffResult{}, err
	}

	explanations := normalizeSemanticDiffExplanations(response.Explanations)
	added, deleted, hunks := semanticDiffChangeTotals(input.ChangedFiles)
	result := SemanticDiffResult{
		Root:              absRoot,
		DiffSource:        input.DiffSource,
		DiffBytes:         len(input.DiffText),
		ChangedFileCount:  len(input.ChangedFiles),
		TotalAddedLines:   added,
		TotalDeletedLines: deleted,
		TotalHunks:        hunks,
		ChangedFiles:      input.ChangedFiles,
		Explanations:      explanations,
		Summary:           summarizeSemanticDiff(explanations, response.Model, response.Summary),
	}
	return result, nil
}

func collectSemanticDiffInput(root string, diffFile string) (SemanticDiffInput, error) {
	diffSource := "git"
	diffText := ""
	var err error

	if strings.TrimSpace(diffFile) != "" {
		path := strings.TrimSpace(diffFile)
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return SemanticDiffInput{}, readErr
		}
		diffSource = filepath.ToSlash(path)
		diffText = string(data)
	} else {
		diffText, err = readSemanticGitDiff(root)
		if err != nil {
			return SemanticDiffInput{}, err
		}
	}

	if strings.TrimSpace(diffText) == "" {
		return SemanticDiffInput{}, fmt.Errorf("no diff content found for semantic-diff")
	}

	changed := parseSemanticDiffChangedFiles(diffText)
	return SemanticDiffInput{
		Root:         root,
		DiffSource:   diffSource,
		DiffText:     diffText,
		ChangedFiles: changed,
	}, nil
}

func readSemanticGitDiff(root string) (string, error) {
	cmd := exec.Command("git", "diff", "--no-ext-diff", "--unified=0", "--", ".")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git diff failed: %w\n%s", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func parseSemanticDiffChangedFiles(diffText string) []SemanticDiffFileChange {
	lines := strings.Split(diffText, "\n")
	entries := make([]SemanticDiffFileChange, 0)
	var current *semanticDiffFileAccumulator

	flush := func() {
		if current == nil {
			return
		}
		path := normalizeSemanticDiffPath(current.Path)
		if path == "" {
			current = nil
			return
		}
		entries = append(entries, SemanticDiffFileChange{
			File:         path,
			Status:       normalizeSemanticDiffStatus(current.Status),
			AddedLines:   current.Added,
			DeletedLines: current.Deleted,
			HunkCount:    current.Hunks,
		})
		current = nil
	}

	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		if strings.HasPrefix(line, "diff --git ") {
			flush()
			current = parseSemanticDiffHeader(line)
			continue
		}
		if current == nil {
			continue
		}

		switch {
		case strings.HasPrefix(line, "new file mode "):
			current.Status = "added"
			continue
		case strings.HasPrefix(line, "deleted file mode "):
			current.Status = "deleted"
			continue
		case strings.HasPrefix(line, "rename to "):
			current.Status = "renamed"
			current.Path = strings.TrimSpace(strings.TrimPrefix(line, "rename to "))
			continue
		case strings.HasPrefix(line, "rename from "):
			if strings.TrimSpace(current.Path) == "" {
				current.Path = strings.TrimSpace(strings.TrimPrefix(line, "rename from "))
			}
			continue
		case strings.HasPrefix(line, "+++ "):
			path := parseSemanticDiffPatchPath(strings.TrimSpace(strings.TrimPrefix(line, "+++ ")))
			if path != "" {
				current.Path = path
			}
			continue
		case strings.HasPrefix(line, "--- "):
			if strings.TrimSpace(current.Path) == "" {
				path := parseSemanticDiffPatchPath(strings.TrimSpace(strings.TrimPrefix(line, "--- ")))
				if path != "" {
					current.Path = path
				}
			}
			continue
		case strings.HasPrefix(line, "@@ "):
			current.Hunks++
			continue
		}

		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			current.Added++
			continue
		}
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			current.Deleted++
		}
	}
	flush()

	merged := make(map[string]SemanticDiffFileChange, len(entries))
	for _, item := range entries {
		existing, ok := merged[item.File]
		if !ok {
			merged[item.File] = item
			continue
		}
		existing.Status = mergeSemanticDiffStatus(existing.Status, item.Status)
		existing.AddedLines += item.AddedLines
		existing.DeletedLines += item.DeletedLines
		existing.HunkCount += item.HunkCount
		merged[item.File] = existing
	}

	out := make([]SemanticDiffFileChange, 0, len(merged))
	for _, item := range merged {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File == out[j].File {
			return out[i].Status < out[j].Status
		}
		return out[i].File < out[j].File
	})
	return out
}

func parseSemanticDiffHeader(line string) *semanticDiffFileAccumulator {
	out := &semanticDiffFileAccumulator{Status: "modified"}
	parts := strings.Fields(line)
	if len(parts) < 4 {
		return out
	}
	oldPath := parseSemanticDiffGitPathToken(parts[2])
	newPath := parseSemanticDiffGitPathToken(parts[3])
	switch {
	case newPath != "":
		out.Path = newPath
	case oldPath != "":
		out.Path = oldPath
	}
	return out
}

func parseSemanticDiffGitPathToken(token string) string {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" || trimmed == "/dev/null" {
		return ""
	}
	trimmed = strings.TrimPrefix(trimmed, "a/")
	trimmed = strings.TrimPrefix(trimmed, "b/")
	return normalizeSemanticDiffPath(trimmed)
}

func parseSemanticDiffPatchPath(token string) string {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" || trimmed == "/dev/null" {
		return ""
	}
	if idx := strings.IndexAny(trimmed, "\t "); idx >= 0 {
		trimmed = trimmed[:idx]
	}
	trimmed = strings.TrimPrefix(trimmed, "a/")
	trimmed = strings.TrimPrefix(trimmed, "b/")
	return normalizeSemanticDiffPath(trimmed)
}

func normalizeSemanticDiffPath(path string) string {
	trimmed := strings.TrimSpace(path)
	trimmed = strings.Trim(trimmed, "\"")
	trimmed = strings.TrimPrefix(trimmed, "./")
	if trimmed == "" || trimmed == "/dev/null" {
		return ""
	}
	normalized := filepath.ToSlash(filepath.Clean(trimmed))
	if normalized == "." {
		return ""
	}
	return normalized
}

func normalizeSemanticDiffStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "added":
		return "added"
	case "deleted":
		return "deleted"
	case "renamed":
		return "renamed"
	default:
		return "modified"
	}
}

func mergeSemanticDiffStatus(left string, right string) string {
	left = normalizeSemanticDiffStatus(left)
	right = normalizeSemanticDiffStatus(right)
	if semanticDiffStatusRank(right) > semanticDiffStatusRank(left) {
		return right
	}
	if semanticDiffStatusRank(right) < semanticDiffStatusRank(left) {
		return left
	}
	if left <= right {
		return left
	}
	return right
}

func semanticDiffStatusRank(status string) int {
	switch normalizeSemanticDiffStatus(status) {
	case "modified":
		return 1
	case "renamed":
		return 2
	case "added", "deleted":
		return 3
	default:
		return 0
	}
}

func runHeadlessAISemanticDiff(provider string, root string, input SemanticDiffInput) (aiSemanticDiffResponse, error) {
	promptText := buildAISemanticDiffPrompt(provider, input)
	schemaText := aiSemanticDiffJSONSchema()
	timeout := aiRenderTimeout()

	switch provider {
	case "codex":
		schemaFile, err := os.CreateTemp("", "canon-semantic-diff-schema-*.json")
		if err != nil {
			return aiSemanticDiffResponse{}, err
		}
		schemaPath := schemaFile.Name()
		defer func() {
			schemaFile.Close()
			_ = os.Remove(schemaPath)
		}()
		if _, err := schemaFile.WriteString(schemaText); err != nil {
			return aiSemanticDiffResponse{}, err
		}
		if err := schemaFile.Close(); err != nil {
			return aiSemanticDiffResponse{}, err
		}

		responseFile, err := os.CreateTemp("", "canon-semantic-diff-response-*.json")
		if err != nil {
			return aiSemanticDiffResponse{}, err
		}
		responsePath := responseFile.Name()
		responseFile.Close()
		defer func() { _ = os.Remove(responsePath) }()

		var (
			ctx    context.Context
			cancel context.CancelFunc
		)
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
				return aiSemanticDiffResponse{}, fmt.Errorf("codex exec timed out after %s", timeout)
			}
			return aiSemanticDiffResponse{}, fmt.Errorf("codex exec failed: %w\n%s", err, strings.TrimSpace(string(output)))
		}

		responseBytes, err := os.ReadFile(responsePath)
		if err != nil {
			return aiSemanticDiffResponse{}, err
		}
		return decodeAISemanticDiffResponse(responseBytes)

	case "claude":
		var (
			ctx    context.Context
			cancel context.CancelFunc
		)
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
				return aiSemanticDiffResponse{}, fmt.Errorf("claude --print timed out after %s", timeout)
			}
			return aiSemanticDiffResponse{}, fmt.Errorf("claude --print failed: %w\n%s", err, strings.TrimSpace(string(output)))
		}
		return decodeAISemanticDiffResponse(output)

	default:
		return aiSemanticDiffResponse{}, fmt.Errorf("unsupported ai provider: %s", provider)
	}
}

func buildAISemanticDiffPrompt(provider string, input SemanticDiffInput) string {
	lines := []string{
		"# Canon Semantic Diff",
		"",
		"Provider: " + provider,
		"",
		"Task:",
		"1. Explain the semantic and behavioral intent of these code changes.",
		"2. Focus on externally observable behavior, compatibility, and operational impact.",
		"3. Use only evidence from changed files in the provided diff.",
		"4. Return strict JSON matching the schema.",
		"",
		"Schema:",
		"{",
		`  "model": "string",`,
		`  "summary": "string",`,
		`  "explanations": [`,
		`    {`,
		`      "id": "string",`,
		`      "category": "string",`,
		`      "impact": "low|medium|high|critical",`,
		`      "summary": "string",`,
		`      "rationale": "string",`,
		`      "evidence": [`,
		`        {`,
		`          "file": "path",`,
		`          "kind": "file|hunk",`,
		`          "old_start": 0,`,
		`          "old_lines": 0,`,
		`          "new_start": 0,`,
		`          "new_lines": 0`,
		`        }`,
		`      ]`,
		`    }`,
		`  ]`,
		"}",
		"",
		"Rules:",
		"- Do not restate raw diff lines as output-only content; explain behavior/intent.",
		"- Keep each summary concise and specific.",
		"- Prefer file-level evidence if hunk ranges are unknown.",
		"- Use stable ids when possible.",
		"",
		"## Diff Source",
		input.DiffSource,
		"",
		"## Changed Files",
	}

	for _, file := range input.ChangedFiles {
		lines = append(lines,
			fmt.Sprintf("- %s (status=%s, +%d, -%d, hunks=%d)", file.File, file.Status, file.AddedLines, file.DeletedLines, file.HunkCount),
		)
	}
	if len(input.ChangedFiles) == 0 {
		lines = append(lines, "- (none parsed)")
	}

	lines = append(lines,
		"",
		"## Unified Diff",
		"```diff",
		strings.TrimSpace(input.DiffText),
		"```",
	)
	return strings.Join(lines, "\n")
}

func aiSemanticDiffJSONSchema() string {
	return `{
  "type": "object",
  "required": ["model", "summary", "explanations"],
  "additionalProperties": false,
  "properties": {
    "model": {"type": "string"},
    "summary": {"type": "string"},
    "explanations": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["id", "category", "impact", "summary", "rationale", "evidence"],
        "additionalProperties": false,
        "properties": {
          "id": {"type": "string"},
          "category": {"type": "string"},
          "impact": {"type": "string", "enum": ["low", "medium", "high", "critical"]},
          "summary": {"type": "string"},
          "rationale": {"type": "string"},
          "evidence": {
            "type": "array",
            "items": {
              "type": "object",
              "required": ["file", "kind"],
              "additionalProperties": false,
              "properties": {
                "file": {"type": "string"},
                "kind": {"type": "string", "enum": ["file", "hunk"]},
                "old_start": {"type": "integer"},
                "old_lines": {"type": "integer"},
                "new_start": {"type": "integer"},
                "new_lines": {"type": "integer"}
              }
            }
          }
        }
      }
    }
  }
}`
}

func parseAISemanticDiffResponse(root string, responseFile string) (aiSemanticDiffResponse, error) {
	b, err := readAIResponseFile(root, responseFile)
	if err != nil {
		if errors.Is(err, errAIResponseFilePathRequired) {
			return aiSemanticDiffResponse{}, fmt.Errorf("from-response semantic-diff mode requires --response-file")
		}
		return aiSemanticDiffResponse{}, err
	}
	return decodeAISemanticDiffResponse(b)
}

func decodeAISemanticDiffResponse(b []byte) (aiSemanticDiffResponse, error) {
	return decodeAIResponseJSON[aiSemanticDiffResponse](b, "invalid AI semantic-diff response JSON")
}

func normalizeSemanticDiffExplanations(items []aiSemanticDiffExplanation) []SemanticDiffExplanation {
	out := make([]SemanticDiffExplanation, 0, len(items))
	seen := make(map[string]struct{}, len(items))

	for i, item := range items {
		summary := strings.TrimSpace(item.Summary)
		rationale := strings.TrimSpace(item.Rationale)
		if summary == "" && rationale == "" {
			continue
		}
		if summary == "" {
			summary = rationale
		}
		if rationale == "" {
			rationale = summary
		}

		category := strings.ToLower(strings.TrimSpace(item.Category))
		if category == "" {
			category = "general"
		}

		impact := normalizeSemanticDiffImpact(item.Impact)
		if impact == SemanticDiffImpactNone {
			impact = SemanticDiffImpactMedium
		}

		explanationID := strings.TrimSpace(item.ID)
		if explanationID == "" {
			explanationID = fmt.Sprintf("exp-%03d", i+1)
		}

		evidence := normalizeSemanticDiffEvidence(item.Evidence)
		signature := semanticDiffExplanationSignature(category, impact, summary, rationale, evidence)
		if _, ok := seen[signature]; ok {
			continue
		}
		seen[signature] = struct{}{}

		out = append(out, SemanticDiffExplanation{
			ID:        explanationID,
			Category:  category,
			Impact:    impact,
			Summary:   summary,
			Rationale: rationale,
			Evidence:  evidence,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		left := out[i]
		right := out[j]
		leftRank := semanticDiffImpactRank(left.Impact)
		rightRank := semanticDiffImpactRank(right.Impact)
		if leftRank != rightRank {
			return leftRank > rightRank
		}
		if left.Category != right.Category {
			return left.Category < right.Category
		}
		if left.Summary != right.Summary {
			return left.Summary < right.Summary
		}
		return left.ID < right.ID
	})

	return out
}

func normalizeSemanticDiffEvidence(items []aiSemanticDiffEvidence) []SemanticDiffEvidence {
	out := make([]SemanticDiffEvidence, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		path := normalizeSemanticDiffPath(item.File)
		if path == "" {
			continue
		}

		kind := SemanticDiffEvidenceKind(strings.ToLower(strings.TrimSpace(item.Kind)))
		if kind != SemanticDiffEvidenceKindFile && kind != SemanticDiffEvidenceKindHunk {
			if item.OldStart > 0 || item.OldLines > 0 || item.NewStart > 0 || item.NewLines > 0 {
				kind = SemanticDiffEvidenceKindHunk
			} else {
				kind = SemanticDiffEvidenceKindFile
			}
		}

		entry := SemanticDiffEvidence{
			File:     path,
			Kind:     kind,
			OldStart: nonNegative(item.OldStart),
			OldLines: nonNegative(item.OldLines),
			NewStart: nonNegative(item.NewStart),
			NewLines: nonNegative(item.NewLines),
		}
		sig := fmt.Sprintf("%s|%s|%d|%d|%d|%d", entry.File, entry.Kind, entry.OldStart, entry.OldLines, entry.NewStart, entry.NewLines)
		if _, ok := seen[sig]; ok {
			continue
		}
		seen[sig] = struct{}{}
		out = append(out, entry)
	}

	sort.Slice(out, func(i, j int) bool {
		left := out[i]
		right := out[j]
		if left.File != right.File {
			return left.File < right.File
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.OldStart != right.OldStart {
			return left.OldStart < right.OldStart
		}
		if left.OldLines != right.OldLines {
			return left.OldLines < right.OldLines
		}
		if left.NewStart != right.NewStart {
			return left.NewStart < right.NewStart
		}
		return left.NewLines < right.NewLines
	})

	return out
}

func semanticDiffExplanationSignature(category string, impact SemanticDiffImpact, summary string, rationale string, evidence []SemanticDiffEvidence) string {
	evidenceParts := make([]string, 0, len(evidence))
	for _, item := range evidence {
		evidenceParts = append(evidenceParts, fmt.Sprintf("%s:%s:%d:%d:%d:%d", item.File, item.Kind, item.OldStart, item.OldLines, item.NewStart, item.NewLines))
	}
	return fmt.Sprintf("%s|%s|%s|%s|%s", category, impact, summary, rationale, strings.Join(evidenceParts, ","))
}

func semanticDiffChangeTotals(changes []SemanticDiffFileChange) (int, int, int) {
	added := 0
	deleted := 0
	hunks := 0
	for _, item := range changes {
		added += item.AddedLines
		deleted += item.DeletedLines
		hunks += item.HunkCount
	}
	return added, deleted, hunks
}

func summarizeSemanticDiff(explanations []SemanticDiffExplanation, model string, aiSummary string) SemanticDiffSummary {
	summary := SemanticDiffSummary{
		AIModel:           strings.TrimSpace(model),
		AISummary:         strings.TrimSpace(aiSummary),
		TotalExplanations: len(explanations),
		HighestImpact:     SemanticDiffImpactNone,
		ImpactCounts:      SemanticDiffImpactCounts{},
		CategoryCounts:    []SemanticDiffCategoryCount{},
	}

	categoryCounts := make(map[string]int)
	for _, explanation := range explanations {
		switch explanation.Impact {
		case SemanticDiffImpactLow:
			summary.ImpactCounts.Low++
		case SemanticDiffImpactMedium:
			summary.ImpactCounts.Medium++
		case SemanticDiffImpactHigh:
			summary.ImpactCounts.High++
		case SemanticDiffImpactCritical:
			summary.ImpactCounts.Critical++
		}
		if semanticDiffImpactRank(explanation.Impact) > semanticDiffImpactRank(summary.HighestImpact) {
			summary.HighestImpact = explanation.Impact
		}
		categoryCounts[explanation.Category]++
	}

	categories := make([]string, 0, len(categoryCounts))
	for category := range categoryCounts {
		categories = append(categories, category)
	}
	sort.Strings(categories)
	for _, category := range categories {
		summary.CategoryCounts = append(summary.CategoryCounts, SemanticDiffCategoryCount{
			Category: category,
			Count:    categoryCounts[category],
		})
	}

	return summary
}

func normalizeSemanticDiffImpact(value string) SemanticDiffImpact {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(SemanticDiffImpactLow):
		return SemanticDiffImpactLow
	case string(SemanticDiffImpactMedium):
		return SemanticDiffImpactMedium
	case string(SemanticDiffImpactHigh):
		return SemanticDiffImpactHigh
	case string(SemanticDiffImpactCritical):
		return SemanticDiffImpactCritical
	case string(SemanticDiffImpactNone):
		return SemanticDiffImpactNone
	default:
		return SemanticDiffImpactMedium
	}
}

func semanticDiffImpactRank(impact SemanticDiffImpact) int {
	switch impact {
	case SemanticDiffImpactNone:
		return 0
	case SemanticDiffImpactLow:
		return 1
	case SemanticDiffImpactMedium:
		return 2
	case SemanticDiffImpactHigh:
		return 3
	case SemanticDiffImpactCritical:
		return 4
	default:
		return -1
	}
}

func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}
