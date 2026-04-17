package canon

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type aiIngestResponse struct {
	Model         string                `json:"model"`
	CanonicalSpec aiCanonicalSpec       `json:"canonical_spec"`
	ConflictCheck aiIngestConflictCheck `json:"conflict_check"`
}

type aiCanonicalSpec struct {
	ID              string   `json:"id"`
	Type            string   `json:"type"`
	Title           string   `json:"title"`
	Domain          string   `json:"domain"`
	Created         string   `json:"created"`
	RequirementKind string   `json:"requirement_kind"`
	SourceIssue     string   `json:"source_issue"`
	ApprovalState   string   `json:"approval_state"`
	DependsOn       []string `json:"depends_on"`
	TouchedDomains  []string `json:"touched_domains"`
	Body            string   `json:"body"`
}

type aiIngestConflictCheck struct {
	HasConflicts bool                   `json:"has_conflicts"`
	Summary      string                 `json:"summary"`
	Conflicts    []aiConflictDetailItem `json:"conflicts"`
}

func prepareCandidateSpecFromAI(root string, existing []Spec, rawText string, input IngestInput, mode string, now time.Time) (Spec, []specConflict, error) {
	provider := strings.ToLower(strings.TrimSpace(input.AIProvider))
	if provider == "" {
		provider = "codex"
	}

	provisional, err := provisionalSpecFromInput(rawText, input, now)
	if err != nil {
		return Spec{}, nil, err
	}

	var response aiIngestResponse
	switch mode {
	case "from-response":
		response, err = parseAIIngestResponse(root, input.ResponseFile)
		if err != nil {
			return Spec{}, nil, err
		}
	case "auto":
		response, err = runHeadlessAIIngest(provider, root, existing, rawText, input, provisional)
		if err != nil {
			return Spec{}, nil, err
		}
	default:
		return Spec{}, nil, fmt.Errorf("unsupported AI ingest mode: %s", mode)
	}

	response = ensureAIResponseDefaults(response, provisional)
	spec, err := canonicalSpecFromAIResponse(response, rawText, input, provisional, now)
	if err != nil {
		return Spec{}, nil, err
	}
	conflicts := conflictsFromAIResponse(spec.ID, spec.Title, response.ConflictCheck)
	return spec, conflicts, nil
}

func provisionalSpecFromInput(rawText string, input IngestInput, now time.Time) (Spec, error) {
	_, body, hasFrontmatter, parseErr := splitOptionalFrontmatter(rawText)
	if parseErr != nil {
		return Spec{}, parseErr
	}
	return buildSpecFromInput(input, rawText, body, hasFrontmatter, now)
}

func runHeadlessAIIngest(provider string, root string, existing []Spec, rawText string, input IngestInput, provisional Spec) (aiIngestResponse, error) {
	promptText := buildAIIngestPrompt(provider, existing, rawText, input)
	schemaText := aiIngestJSONSchema()

	switch provider {
	case "codex":
		schemaFile, err := os.CreateTemp("", "canon-schema-*.json")
		if err != nil {
			return aiIngestResponse{}, err
		}
		schemaPath := schemaFile.Name()
		defer func() {
			schemaFile.Close()
			_ = os.Remove(schemaPath)
		}()
		if _, err := schemaFile.WriteString(schemaText); err != nil {
			return aiIngestResponse{}, err
		}
		if err := schemaFile.Close(); err != nil {
			return aiIngestResponse{}, err
		}

		responseFile, err := os.CreateTemp("", "canon-response-*.json")
		if err != nil {
			return aiIngestResponse{}, err
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
			return aiIngestResponse{}, fmt.Errorf("codex exec failed: %w\n%s", err, strings.TrimSpace(string(output)))
		}

		responseBytes, err := os.ReadFile(responsePath)
		if err != nil {
			return aiIngestResponse{}, err
		}
		response, err := decodeAIIngestResponse(responseBytes)
		if err != nil {
			return aiIngestResponse{}, err
		}
		return ensureAIResponseDefaults(response, provisional), nil

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
			return aiIngestResponse{}, fmt.Errorf("claude --print failed: %w\n%s", err, strings.TrimSpace(string(output)))
		}
		response, err := decodeAIIngestResponse(output)
		if err != nil {
			return aiIngestResponse{}, err
		}
		return ensureAIResponseDefaults(response, provisional), nil

	default:
		return aiIngestResponse{}, fmt.Errorf("unsupported ai provider: %s", provider)
	}
}

func buildAIIngestPrompt(provider string, existing []Spec, rawText string, input IngestInput) string {
	lines := []string{
		"# Canon AI Ingest",
		"",
		"Provider: " + provider,
		"",
		"Task:",
		"1. Convert the input into one canonical spec.",
		"2. Infer metadata when not explicitly provided by CLI overrides.",
		"3. Evaluate semantic conflicts against existing specs.",
		"4. Return JSON only with the schema below.",
		"",
		"Required JSON schema:",
		"{",
		`  "model": "string",`,
		`  "canonical_spec": {`,
		`    "id": "spec-id",`,
		`    "type": "feature|technical|resolution",`,
		`    "title": "string",`,
		`    "domain": "string",`,
		`    "created": "RFC3339 timestamp",`,
		`    "requirement_kind": "product|technical_helper|",`,
		`    "source_issue": "string",`,
		`    "approval_state": "approved|draft|unknown|",`,
		`    "depends_on": ["spec-id"],`,
		`    "touched_domains": ["domain"],`,
		`    "body": "markdown"`,
		"  },",
		`  "conflict_check": {`,
		`    "has_conflicts": true|false,`,
		`    "summary": "string",`,
		`    "conflicts": [`,
		`      {"existing_spec_id":"spec-id","reason":"string"}`,
		"    ]",
		"  }",
		"}",
		"",
		"If has_conflicts is false, conflicts must be [].",
		"",
		"## Raw Input",
		"",
		rawText,
		"",
		"## CLI Overrides",
		"",
		"id: " + valueOrEmpty(input.ID),
		"type: " + valueOrEmpty(input.Type),
		"title: " + valueOrEmpty(input.Title),
		"domain: " + valueOrEmpty(input.Domain),
		"created: " + valueOrEmpty(input.Created),
		"depends_on: " + strings.Join(input.DependsOn, ","),
		"touched_domains: " + strings.Join(input.TouchedDomains, ","),
		"",
		"## Existing Specs",
		"",
	}

	ordered := make([]Spec, len(existing))
	copy(ordered, existing)
	sortSpecsStable(ordered)
	for _, spec := range ordered {
		lines = append(lines,
			"### "+spec.ID,
			"",
			canonicalSpecText(spec),
			"",
		)
	}

	return strings.Join(lines, "\n")
}

func ensureAIResponseDefaults(response aiIngestResponse, provisional Spec) aiIngestResponse {
	if strings.TrimSpace(response.Model) == "" {
		response.Model = "headless-ai"
	}
	if strings.TrimSpace(response.CanonicalSpec.ID) == "" {
		response.CanonicalSpec.ID = provisional.ID
	}
	if strings.TrimSpace(response.CanonicalSpec.Type) == "" {
		response.CanonicalSpec.Type = provisional.Type
	}
	if strings.TrimSpace(response.CanonicalSpec.Title) == "" {
		response.CanonicalSpec.Title = provisional.Title
	}
	if strings.TrimSpace(response.CanonicalSpec.Domain) == "" {
		response.CanonicalSpec.Domain = provisional.Domain
	}
	if strings.TrimSpace(response.CanonicalSpec.Created) == "" {
		response.CanonicalSpec.Created = provisional.Created
	}
	if strings.TrimSpace(response.CanonicalSpec.RequirementKind) == "" {
		response.CanonicalSpec.RequirementKind = provisional.RequirementKind
	}
	if strings.TrimSpace(response.CanonicalSpec.SourceIssue) == "" {
		response.CanonicalSpec.SourceIssue = provisional.SourceIssue
	}
	if strings.TrimSpace(response.CanonicalSpec.ApprovalState) == "" {
		response.CanonicalSpec.ApprovalState = provisional.ApprovalState
	}
	if len(response.CanonicalSpec.DependsOn) == 0 {
		response.CanonicalSpec.DependsOn = provisional.DependsOn
	}
	if len(response.CanonicalSpec.TouchedDomains) == 0 {
		response.CanonicalSpec.TouchedDomains = provisional.TouchedDomains
	}
	if strings.TrimSpace(response.CanonicalSpec.Body) == "" {
		response.CanonicalSpec.Body = provisional.Body
	}
	return response
}

func parseAIIngestResponse(root string, responseFile string) (aiIngestResponse, error) {
	b, err := readAIResponseFile(root, responseFile)
	if err != nil {
		if errors.Is(err, errAIResponseFilePathRequired) {
			return aiIngestResponse{}, fmt.Errorf("from-response mode requires --response-file")
		}
		return aiIngestResponse{}, err
	}
	return decodeAIIngestResponse(b)
}

func decodeAIIngestResponse(b []byte) (aiIngestResponse, error) {
	return decodeAIResponseJSON[aiIngestResponse](b, "invalid AI response JSON")
}

func canonicalSpecFromAIResponse(response aiIngestResponse, rawText string, input IngestInput, provisional Spec, now time.Time) (Spec, error) {
	body := strings.TrimSpace(response.CanonicalSpec.Body)
	if shouldPreserveSourceBody(input) {
		body = mergeSourceAndAIBody(rawText, body)
	}
	spec := Spec{
		ID:              strings.TrimSpace(response.CanonicalSpec.ID),
		Type:            strings.TrimSpace(response.CanonicalSpec.Type),
		Title:           strings.TrimSpace(response.CanonicalSpec.Title),
		Domain:          strings.TrimSpace(response.CanonicalSpec.Domain),
		Created:         strings.TrimSpace(response.CanonicalSpec.Created),
		RequirementKind: strings.TrimSpace(response.CanonicalSpec.RequirementKind),
		SourceIssue:     strings.TrimSpace(response.CanonicalSpec.SourceIssue),
		ApprovalState:   strings.TrimSpace(response.CanonicalSpec.ApprovalState),
		DependsOn:       normalizeList(response.CanonicalSpec.DependsOn),
		TouchedDomains:  normalizeList(response.CanonicalSpec.TouchedDomains),
		Body:            body,
	}
	if strings.TrimSpace(input.Created) == "" {
		spec.Created = strings.TrimSpace(provisional.Created)
	}

	if strings.TrimSpace(input.ID) != "" {
		spec.ID = strings.TrimSpace(input.ID)
	}
	if strings.TrimSpace(input.Type) != "" {
		spec.Type = strings.TrimSpace(input.Type)
	}
	if strings.TrimSpace(input.Title) != "" {
		spec.Title = strings.TrimSpace(input.Title)
	}
	if strings.TrimSpace(input.Domain) != "" {
		spec.Domain = strings.TrimSpace(input.Domain)
	}
	if strings.TrimSpace(input.Created) != "" {
		spec.Created = strings.TrimSpace(input.Created)
	}
	if len(input.DependsOn) > 0 {
		spec.DependsOn = normalizeList(input.DependsOn)
	}
	if len(input.TouchedDomains) > 0 {
		spec.TouchedDomains = normalizeList(input.TouchedDomains)
	}

	if spec.ID == "" {
		title := spec.Title
		if title == "" {
			title = inferTitle(rawText)
		}
		spec.ID = generatedSpecID(now, title)
	}
	return normalizeSpecDefaults(spec)
}

func conflictsFromAIResponse(candidateSpecID string, candidateTitle string, check aiIngestConflictCheck) []specConflict {
	if !check.HasConflicts {
		return nil
	}
	conflicts := make([]specConflict, 0)
	for _, item := range check.Conflicts {
		existingID := strings.TrimSpace(item.ExistingSpecID)
		if existingID == "" {
			existingID = "unknown"
		}
		reason := strings.TrimSpace(item.Reason)
		if reason == "" {
			reason = strings.TrimSpace(check.Summary)
		}
		if reason == "" {
			reason = "AI reported semantic conflict"
		}
		conflicts = append(conflicts, specConflict{
			ExistingSpecID: existingID,
			CandidateSpec:  candidateSpecID,
			StatementKey:   "ai_adjudicated_conflict",
			ExistingLine:   reason,
			CandidateLine:  candidateTitle,
		})
	}
	if len(conflicts) == 0 {
		conflicts = append(conflicts, specConflict{
			ExistingSpecID: "unknown",
			CandidateSpec:  candidateSpecID,
			StatementKey:   "ai_adjudicated_conflict",
			ExistingLine:   strings.TrimSpace(check.Summary),
			CandidateLine:  candidateTitle,
		})
	}
	return conflicts
}

func aiIngestJSONSchema() string {
	return `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "required": ["model", "canonical_spec", "conflict_check"],
  "properties": {
    "model": {"type": "string"},
    "canonical_spec": {
      "type": "object",
      "additionalProperties": false,
      "required": ["id", "type", "title", "domain", "created", "requirement_kind", "source_issue", "approval_state", "depends_on", "touched_domains", "body"],
      "properties": {
        "id": {"type": "string"},
        "type": {"type": "string", "enum": ["feature", "technical", "resolution"]},
        "title": {"type": "string"},
        "domain": {"type": "string"},
        "created": {"type": "string"},
        "requirement_kind": {"type": "string"},
        "source_issue": {"type": "string"},
        "approval_state": {"type": "string"},
        "depends_on": {"type": "array", "items": {"type": "string"}},
        "touched_domains": {"type": "array", "items": {"type": "string"}},
        "body": {"type": "string"}
      }
    },
    "conflict_check": {
      "type": "object",
      "additionalProperties": false,
      "required": ["has_conflicts", "summary", "conflicts"],
      "properties": {
        "has_conflicts": {"type": "boolean"},
        "summary": {"type": "string"},
        "conflicts": {
          "type": "array",
          "items": {
            "type": "object",
            "additionalProperties": false,
            "required": ["existing_spec_id", "reason"],
            "properties": {
              "existing_spec_id": {"type": "string"},
              "reason": {"type": "string"}
            }
          }
        }
      }
    }
  }
}
`
}

func valueOrEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return strings.TrimSpace(value)
}

func sourceBodyFromRaw(rawText string) string {
	_, body, hasFrontmatter, err := splitOptionalFrontmatter(rawText)
	if err == nil && hasFrontmatter {
		return strings.TrimSpace(body)
	}
	return strings.TrimSpace(rawText)
}

func mergeSourceAndAIBody(rawText string, aiBody string) string {
	source := sourceBodyFromRaw(rawText)
	ai := strings.TrimSpace(aiBody)
	if source == "" {
		return ai
	}
	if ai == "" {
		return source
	}
	if normalizeTextForCompare(source) == normalizeTextForCompare(ai) {
		return source
	}

	extra := uniqueEnhancementLines(source, ai)
	if len(extra) == 0 {
		return source
	}
	return strings.TrimRight(source, "\n") + "\n\n## AI Enhancements\n\n" + strings.Join(extra, "\n") + "\n"
}

func uniqueEnhancementLines(source string, ai string) []string {
	sourceSet := make(map[string]struct{})
	for _, line := range strings.Split(source, "\n") {
		key := normalizeTextForCompare(line)
		if key != "" {
			sourceSet[key] = struct{}{}
		}
	}
	out := make([]string, 0)
	seen := make(map[string]struct{})
	for _, line := range strings.Split(ai, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		key := normalizeTextForCompare(trimmed)
		if key == "" {
			continue
		}
		if _, exists := sourceSet[key]; exists {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func normalizeTextForCompare(value string) string {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(value)))
	return strings.Join(fields, " ")
}

func shouldPreserveSourceBody(input IngestInput) bool {
	kind := strings.ToLower(strings.TrimSpace(input.IngestKind))
	if kind == "raw" {
		return false
	}
	if kind == "file" {
		return true
	}
	return strings.TrimSpace(input.File) != ""
}
