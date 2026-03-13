package canon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const (
	defaultBlameNarrowLimit = 24
	blameTieBuffer          = 8
)

type aiBlameResponse struct {
	Model   string          `json:"model"`
	Found   bool            `json:"found"`
	Results []aiBlameResult `json:"results"`
}

type aiBlameResult struct {
	SpecID       string   `json:"spec_id"`
	Confidence   string   `json:"confidence"`
	RelevantLine []string `json:"relevant_lines"`
}

type scoredBlameSpec struct {
	Spec  Spec
	Score int
}

func Blame(root string, input BlameInput) (BlameResult, error) {
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return BlameResult{}, fmt.Errorf("blame requires a behavior description")
	}

	specs, err := loadSpecs(root)
	if err != nil {
		return BlameResult{}, err
	}
	scoped := filterSpecsForBlameDomain(specs, input.Domain)
	if len(scoped) == 0 {
		return BlameResult{
			Query: query,
			Found: false,
		}, nil
	}

	index := buildIndex(scoped)
	narrowed := narrowSpecsForBlame(scoped, index, query, defaultBlameNarrowLimit)
	if len(narrowed) == 0 {
		return BlameResult{
			Query: query,
			Found: false,
		}, nil
	}

	provider := strings.ToLower(strings.TrimSpace(input.AIProvider))
	if provider == "" {
		provider = "codex"
	}
	response, err := resolveAIBlameResponse(root, provider, input.ResponseFile, query, input.Domain, narrowed, index)
	if err != nil {
		return BlameResult{}, err
	}

	specByID := make(map[string]Spec, len(scoped))
	for _, spec := range scoped {
		specByID[spec.ID] = spec
	}

	results := make([]BlameSpec, 0, len(response.Results))
	seen := map[string]struct{}{}
	for _, item := range response.Results {
		specID := strings.TrimSpace(item.SpecID)
		if specID == "" {
			continue
		}
		if _, ok := seen[specID]; ok {
			continue
		}
		spec, ok := specByID[specID]
		if !ok {
			continue
		}
		relevantLines := normalizeBlameLines(item.RelevantLine)
		results = append(results, BlameSpec{
			SpecID:        spec.ID,
			Title:         spec.Title,
			Domain:        spec.Domain,
			Confidence:    normalizeBlameConfidence(item.Confidence),
			Created:       spec.Created,
			RelevantLines: relevantLines,
		})
		seen[specID] = struct{}{}
	}

	found := len(results) > 0
	if !found {
		results = nil
	}
	return BlameResult{
		Query:   query,
		Found:   found,
		Results: results,
	}, nil
}

func filterSpecsForBlameDomain(specs []Spec, domain string) []Spec {
	target := strings.ToLower(strings.TrimSpace(domain))
	if target == "" {
		out := make([]Spec, len(specs))
		copy(out, specs)
		sortSpecsStable(out)
		return out
	}

	out := make([]Spec, 0, len(specs))
	for _, spec := range specs {
		if strings.EqualFold(strings.TrimSpace(spec.Domain), target) {
			out = append(out, spec)
			continue
		}
		for _, touched := range spec.TouchedDomains {
			if strings.EqualFold(strings.TrimSpace(touched), target) {
				out = append(out, spec)
				break
			}
		}
	}
	sortSpecsStable(out)
	return out
}

func narrowSpecsForBlame(specs []Spec, index Index, query string, maxSpecs int) []Spec {
	if len(specs) == 0 {
		return nil
	}
	if maxSpecs <= 0 {
		maxSpecs = defaultBlameNarrowLimit
	}

	ordered := make([]Spec, len(specs))
	copy(ordered, specs)
	sortSpecsStable(ordered)
	if len(ordered) <= maxSpecs {
		return ordered
	}

	queryTerms := blameQueryTerms(query)
	domainHints := detectBlameDomainHints(query, index)

	scored := make([]scoredBlameSpec, 0, len(ordered))
	for _, spec := range ordered {
		scored = append(scored, scoredBlameSpec{
			Spec:  spec,
			Score: scoreSpecForBlame(spec, queryTerms, domainHints),
		})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return specSortKey(scored[i].Spec) < specSortKey(scored[j].Spec)
		}
		return scored[i].Score > scored[j].Score
	})

	limit := maxSpecs
	if limit > len(scored) {
		limit = len(scored)
	}

	out := make([]Spec, 0, limit+blameTieBuffer)
	for i := 0; i < limit; i++ {
		out = append(out, scored[i].Spec)
	}
	cutoff := scored[limit-1].Score
	if cutoff > 0 {
		for i := limit; i < len(scored); i++ {
			if scored[i].Score != cutoff || len(out) >= limit+blameTieBuffer {
				break
			}
			out = append(out, scored[i].Spec)
		}
	}
	return out
}

func scoreSpecForBlame(spec Spec, queryTerms []string, domainHints map[string]struct{}) int {
	score := 0
	specDomain := strings.ToLower(strings.TrimSpace(spec.Domain))
	if _, ok := domainHints[specDomain]; ok {
		score += 4
	}
	for _, touched := range spec.TouchedDomains {
		if _, ok := domainHints[strings.ToLower(strings.TrimSpace(touched))]; ok {
			score += 2
			break
		}
	}

	text := strings.ToLower(strings.Join([]string{
		spec.ID,
		spec.Title,
		spec.Domain,
		strings.Join(spec.TouchedDomains, " "),
		spec.Body,
	}, "\n"))
	for _, term := range queryTerms {
		if strings.Contains(text, term) {
			score += 2
		}
	}
	return score
}

func blameQueryTerms(query string) []string {
	normalized := strings.ToLower(query)
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
		"_", " ",
		"?", " ",
		"!", " ",
	)
	normalized = replacer.Replace(normalized)

	stop := map[string]struct{}{
		"a": {}, "an": {}, "and": {}, "are": {}, "as": {}, "at": {}, "be": {},
		"by": {}, "for": {}, "from": {}, "in": {}, "is": {}, "it": {}, "of": {},
		"on": {}, "or": {}, "that": {}, "the": {}, "their": {}, "this": {},
		"to": {}, "was": {}, "were": {}, "with": {}, "must": {}, "should": {},
		"shall": {}, "after": {}, "before": {}, "when": {}, "where": {}, "why": {},
	}

	terms := make([]string, 0)
	seen := map[string]struct{}{}
	for _, field := range strings.Fields(normalized) {
		if len(field) < 3 {
			continue
		}
		if _, ok := stop[field]; ok {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		terms = append(terms, field)
	}
	return terms
}

func detectBlameDomainHints(query string, index Index) map[string]struct{} {
	hints := map[string]struct{}{}
	lowerQuery := strings.ToLower(query)
	queryWithSpaces := strings.ReplaceAll(lowerQuery, "-", " ")
	for domain := range index.Domains {
		normalized := strings.ToLower(strings.TrimSpace(domain))
		if normalized == "" {
			continue
		}
		if strings.Contains(lowerQuery, normalized) {
			hints[normalized] = struct{}{}
			continue
		}
		if strings.Contains(queryWithSpaces, strings.ReplaceAll(normalized, "-", " ")) {
			hints[normalized] = struct{}{}
		}
	}
	return hints
}

func resolveAIBlameResponse(root string, provider string, responseFile string, query string, domain string, specs []Spec, index Index) (aiBlameResponse, error) {
	if strings.TrimSpace(responseFile) != "" {
		return parseAIBlameResponse(root, responseFile)
	}
	if !aiProviderRuntimeReady(provider) {
		return aiBlameResponse{}, fmt.Errorf("ai provider %s is not runtime-ready", provider)
	}
	return runHeadlessAIBlame(provider, root, query, domain, specs, index)
}

func runHeadlessAIBlame(provider string, root string, query string, domain string, specs []Spec, index Index) (aiBlameResponse, error) {
	promptText := buildAIBlamePrompt(provider, query, domain, specs, index)
	schemaText := aiBlameJSONSchema()
	timeout := aiRenderTimeout()

	switch provider {
	case "codex":
		schemaFile, err := os.CreateTemp("", "canon-blame-schema-*.json")
		if err != nil {
			return aiBlameResponse{}, err
		}
		schemaPath := schemaFile.Name()
		defer func() {
			schemaFile.Close()
			_ = os.Remove(schemaPath)
		}()
		if _, err := schemaFile.WriteString(schemaText); err != nil {
			return aiBlameResponse{}, err
		}
		if err := schemaFile.Close(); err != nil {
			return aiBlameResponse{}, err
		}

		responseFile, err := os.CreateTemp("", "canon-blame-response-*.json")
		if err != nil {
			return aiBlameResponse{}, err
		}
		responsePath := responseFile.Name()
		responseFile.Close()
		defer func() { _ = os.Remove(responsePath) }()

		ctx, cancel := commandContext(timeout)
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
				return aiBlameResponse{}, fmt.Errorf("codex exec timed out after %s", timeout)
			}
			return aiBlameResponse{}, fmt.Errorf("codex exec failed: %w\n%s", err, strings.TrimSpace(string(output)))
		}

		responseBytes, err := os.ReadFile(responsePath)
		if err != nil {
			return aiBlameResponse{}, err
		}
		return decodeAIBlameResponse(responseBytes)

	case "claude":
		ctx, cancel := commandContext(timeout)
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
				return aiBlameResponse{}, fmt.Errorf("claude --print timed out after %s", timeout)
			}
			return aiBlameResponse{}, fmt.Errorf("claude --print failed: %w\n%s", err, strings.TrimSpace(string(output)))
		}
		return decodeAIBlameResponse(output)

	default:
		return aiBlameResponse{}, fmt.Errorf("unsupported ai provider: %s", provider)
	}
}

func buildAIBlamePrompt(provider string, query string, domain string, specs []Spec, index Index) string {
	lines := []string{
		"# Canon AI Blame",
		"",
		"Provider: " + provider,
		"",
		"Task:",
		"1. Match the behavior description to canonical specs that introduce or mandate it.",
		"2. Return only specs from the provided corpus.",
		"3. Extract exact requirement lines that justify each match.",
		"4. Assign confidence using this rubric:",
		"   - high: the spec explicitly states the behavior.",
		"   - medium: the behavior is implied by the spec.",
		"   - low: the behavior is a weak inference from the spec.",
		"5. If nothing matches, set found=false and results=[].",
		"6. Return JSON only with the schema below.",
		"",
		"Schema:",
		"{",
		`  "model": "string",`,
		`  "found": true|false,`,
		`  "results": [`,
		`    {`,
		`      "spec_id": "spec-id",`,
		`      "confidence": "high|medium|low",`,
		`      "relevant_lines": ["string"]`,
		"    }",
		"  ]",
		"}",
		"",
		"## Query",
		"",
		query,
		"",
	}

	if strings.TrimSpace(domain) != "" {
		lines = append(lines,
			"## Domain Filter",
			"",
			strings.TrimSpace(domain),
			"",
		)
	}

	lines = append(lines, "## Domain Index", "")
	domains := sortedKeys(index.Domains)
	for _, domainName := range domains {
		lines = append(lines,
			"- "+domainName+": "+renderList(index.Domains[domainName]),
		)
	}

	lines = append(lines, "", "## Canonical Specs", "")
	for _, spec := range specs {
		lines = append(lines,
			"### "+spec.ID,
			"",
			canonicalSpecText(spec),
			"",
		)
	}

	return strings.Join(lines, "\n")
}

func aiBlameJSONSchema() string {
	return `{
  "type": "object",
  "required": ["model", "found", "results"],
  "additionalProperties": false,
  "properties": {
    "model": {"type": "string"},
    "found": {"type": "boolean"},
    "results": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["spec_id", "confidence", "relevant_lines"],
        "additionalProperties": false,
        "properties": {
          "spec_id": {"type": "string"},
          "confidence": {"type": "string", "enum": ["high", "medium", "low"]},
          "relevant_lines": {
            "type": "array",
            "items": {"type": "string"}
          }
        }
      }
    }
  }
}`
}

func parseAIBlameResponse(root string, responseFile string) (aiBlameResponse, error) {
	b, err := readAIResponseFile(root, responseFile)
	if err != nil {
		if errors.Is(err, errAIResponseFilePathRequired) {
			return aiBlameResponse{}, fmt.Errorf("blame --response-file requires a path")
		}
		return aiBlameResponse{}, err
	}
	return decodeAIBlameResponse(b)
}

func decodeAIBlameResponse(b []byte) (aiBlameResponse, error) {
	response, err := decodeAIResponseJSON[aiBlameResponse](b, "invalid AI blame response JSON")
	if err != nil {
		return aiBlameResponse{}, err
	}
	return normalizeAIBlameResponse(response), nil
}

func normalizeAIBlameResponse(response aiBlameResponse) aiBlameResponse {
	if strings.TrimSpace(response.Model) == "" {
		response.Model = "headless-ai"
	}
	results := make([]aiBlameResult, 0, len(response.Results))
	for _, item := range response.Results {
		specID := strings.TrimSpace(item.SpecID)
		if specID == "" {
			continue
		}
		results = append(results, aiBlameResult{
			SpecID:       specID,
			Confidence:   normalizeBlameConfidence(item.Confidence),
			RelevantLine: normalizeBlameLines(item.RelevantLine),
		})
	}
	response.Results = results
	return response
}

func normalizeBlameLines(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	out := make([]string, 0, len(lines))
	seen := map[string]struct{}{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
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

func normalizeBlameConfidence(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "high":
		return "high"
	case "medium":
		return "medium"
	default:
		return "low"
	}
}
