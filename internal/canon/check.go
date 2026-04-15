package canon

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type aiCheckResponse struct {
	Model         string              `json:"model"`
	ConflictCheck aiCheckConflictPack `json:"conflict_check"`
}

type aiCheckConflictPack struct {
	HasConflicts bool                  `json:"has_conflicts"`
	Summary      string                `json:"summary"`
	Conflicts    []aiCheckConflictItem `json:"conflicts"`
}

type aiCheckConflictItem struct {
	SpecA        string `json:"spec_a"`
	SpecB        string `json:"spec_b"`
	Domain       string `json:"domain"`
	StatementKey string `json:"statement_key"`
	LineA        string `json:"line_a"`
	LineB        string `json:"line_b"`
	Reason       string `json:"reason"`
}

func Check(root string, opts CheckOptions) (CheckResult, error) {
	if err := EnsureLayout(root, false); err != nil {
		return CheckResult{}, err
	}

	specs, err := loadSpecs(root)
	if err != nil {
		return CheckResult{}, err
	}

	var candidate *Spec
	if strings.TrimSpace(opts.CandidateFile) != "" {
		prepared, err := prepareCheckCandidateSpec(root, opts.CandidateFile)
		if err != nil {
			return CheckResult{}, err
		}
		candidate = &prepared
	}

	scoped := filterCheckSpecsByDomain(specs, opts.Domain)
	checkSpecs := scoped
	if candidate != nil {
		for _, spec := range specs {
			if spec.ID == candidate.ID {
				return CheckResult{}, fmt.Errorf("candidate spec id already exists: %s", candidate.ID)
			}
		}
		checkSpecs = append([]Spec{}, scoped...)
		checkSpecs = append(checkSpecs, *candidate)
		sort.Slice(checkSpecs, func(i, j int) bool {
			return checkSpecs[i].ID < checkSpecs[j].ID
		})
	}

	result := CheckResult{
		Passed:             true,
		TotalSpecs:         len(checkSpecs),
		TotalConflicts:     0,
		TotalReadinessGaps: 0,
		Conflicts:          []CheckConflict{},
		ReadinessGaps:      []CheckReadinessGap{},
	}
	if candidate != nil {
		result.Candidate = &CheckCandidate{
			SpecID: candidate.ID,
			Title:  candidate.Title,
			Domain: candidate.Domain,
			Path:   candidate.Path,
		}
	}

	targetID := strings.TrimSpace(opts.SpecID)
	if candidate != nil {
		if targetID != "" {
			return CheckResult{}, errors.New("use either --spec or --file, not both")
		}
		targetID = candidate.ID
	}
	if len(checkSpecs) == 0 {
		if targetID != "" {
			return CheckResult{}, fmt.Errorf("spec not found in check scope: %s", targetID)
		}
		return result, nil
	}

	specByID := make(map[string]Spec, len(checkSpecs))
	for _, spec := range checkSpecs {
		specByID[spec.ID] = spec
	}

	if targetID != "" {
		if _, ok := specByID[targetID]; !ok {
			return CheckResult{}, fmt.Errorf("spec not found in check scope: %s", targetID)
		}
	}

	mode := strings.ToLower(strings.TrimSpace(opts.AIMode))
	if mode == "" {
		mode = "auto"
	}
	responseFile := strings.TrimSpace(opts.ResponseFile)
	if responseFile != "" && mode == "auto" {
		mode = "from-response"
	}

	provider := strings.ToLower(strings.TrimSpace(opts.AIProvider))
	if provider == "" {
		provider = "codex"
	}

	var response aiCheckResponse
	switch mode {
	case "auto":
		if !aiProviderRuntimeReady(provider) {
			return CheckResult{}, fmt.Errorf("ai provider %s is not runtime-ready", provider)
		}
		response, err = runHeadlessAICheck(provider, root, checkSpecs, targetID)
	case "from-response":
		response, err = parseAICheckResponse(root, responseFile)
	default:
		return CheckResult{}, fmt.Errorf("unsupported AI check mode: %s", mode)
	}
	if err != nil {
		return CheckResult{}, err
	}

	conflicts, err := checkConflictsFromAIResponse(response, specByID, targetID)
	if err != nil {
		return CheckResult{}, err
	}
	sort.Slice(conflicts, func(i, j int) bool {
		if conflicts[i].SpecA == conflicts[j].SpecA {
			if conflicts[i].SpecB == conflicts[j].SpecB {
				if conflicts[i].StatementKey == conflicts[j].StatementKey {
					if conflicts[i].LineA == conflicts[j].LineA {
						return conflicts[i].LineB < conflicts[j].LineB
					}
					return conflicts[i].LineA < conflicts[j].LineA
				}
				return conflicts[i].StatementKey < conflicts[j].StatementKey
			}
			return conflicts[i].SpecB < conflicts[j].SpecB
		}
		return conflicts[i].SpecA < conflicts[j].SpecA
	})

	readinessScope := scoped
	if targetID != "" {
		readinessScope = []Spec{specByID[targetID]}
	}

	result.Conflicts = conflicts
	result.TotalConflicts = len(conflicts)
	result.ReadinessGaps = collectReadinessGaps(readinessScope)
	result.TotalReadinessGaps = len(result.ReadinessGaps)
	result.Passed = result.TotalConflicts == 0 && result.TotalReadinessGaps == 0

	if opts.Write && result.TotalConflicts > 0 {
		reportBuckets := map[string][]specConflict{}
		for _, conflict := range result.Conflicts {
			reportBuckets[conflict.SpecB] = append(reportBuckets[conflict.SpecB], specConflict{
				ExistingSpecID: conflict.SpecA,
				CandidateSpec:  conflict.SpecB,
				StatementKey:   conflict.StatementKey,
				ExistingLine:   conflict.LineA,
				CandidateLine:  conflict.LineB,
			})
		}
		candidates := make([]string, 0, len(reportBuckets))
		for id := range reportBuckets {
			candidates = append(candidates, id)
		}
		sort.Strings(candidates)

		paths := make([]string, 0, len(candidates))
		for _, candidateID := range candidates {
			candidate := specByID[candidateID]
			reportPath, writeErr := writeConflictReport(root, candidate, reportBuckets[candidateID])
			if writeErr != nil {
				return CheckResult{}, writeErr
			}
			paths = append(paths, reportPath)
		}
		result.ReportPaths = paths
	}

	return result, nil
}

func prepareCheckCandidateSpec(root string, candidateFile string) (Spec, error) {
	path := strings.TrimSpace(candidateFile)
	if path == "" {
		return Spec{}, errors.New("candidate file path is required")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Spec{}, err
	}
	spec, err := provisionalSpecFromInput(string(raw), IngestInput{
		IngestKind: "file",
		File:       path,
	}, nowUTC())
	if err != nil {
		return Spec{}, err
	}
	rel, relErr := filepath.Rel(root, path)
	if relErr == nil && !strings.HasPrefix(rel, "..") && rel != "." {
		spec.Path = filepath.ToSlash(rel)
	} else {
		spec.Path = filepath.ToSlash(path)
	}
	return spec, nil
}

func runHeadlessAICheck(provider string, root string, specs []Spec, targetID string) (aiCheckResponse, error) {
	promptText := buildAICheckPrompt(provider, specs, targetID)
	schemaText := aiCheckJSONSchema()

	switch provider {
	case "codex":
		schemaFile, err := os.CreateTemp("", "canon-check-schema-*.json")
		if err != nil {
			return aiCheckResponse{}, err
		}
		schemaPath := schemaFile.Name()
		defer func() {
			schemaFile.Close()
			_ = os.Remove(schemaPath)
		}()
		if _, err := schemaFile.WriteString(schemaText); err != nil {
			return aiCheckResponse{}, err
		}
		if err := schemaFile.Close(); err != nil {
			return aiCheckResponse{}, err
		}

		responseFile, err := os.CreateTemp("", "canon-check-response-*.json")
		if err != nil {
			return aiCheckResponse{}, err
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
			return aiCheckResponse{}, fmt.Errorf("codex exec failed: %w\n%s", err, strings.TrimSpace(string(output)))
		}

		responseBytes, err := os.ReadFile(responsePath)
		if err != nil {
			return aiCheckResponse{}, err
		}
		return decodeAICheckResponse(responseBytes)

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
			return aiCheckResponse{}, fmt.Errorf("claude --print failed: %w\n%s", err, strings.TrimSpace(string(output)))
		}
		return decodeAICheckResponse(output)

	default:
		return aiCheckResponse{}, fmt.Errorf("unsupported ai provider: %s", provider)
	}
}

func buildAICheckPrompt(provider string, specs []Spec, targetID string) string {
	lines := []string{
		"# Canon AI Check",
		"",
		"Provider: " + provider,
		"",
		"Task:",
		"1. Review all provided canonical specs.",
		"2. Find semantic contradictions between specs.",
		"3. Return JSON only using the schema below.",
		"",
		"Rules:",
		"- Only report a conflict when the semantics truly contradict.",
		"- Return each spec pair at most once for each unique conflict statement.",
		"- Use exact spec ids from the provided corpus.",
		"- Include one concise reason and concrete contradictory lines from each spec body.",
		"",
		"Schema:",
		"{",
		`  "model": "string",`,
		`  "conflict_check": {`,
		`    "has_conflicts": true|false,`,
		`    "summary": "string",`,
		`    "conflicts": [`,
		`      {`,
		`        "spec_a": "id",`,
		`        "spec_b": "id",`,
		`        "domain": "string",`,
		`        "statement_key": "string",`,
		`        "line_a": "string",`,
		`        "line_b": "string",`,
		`        "reason": "string"`,
		`      }`,
		"    ]",
		"  }",
		"}",
		"",
	}

	if strings.TrimSpace(targetID) != "" {
		lines = append(lines,
			"Focus Spec:",
			"- id: "+targetID,
			"- Only include conflicts where spec_a or spec_b equals this focus spec id.",
			"",
		)
	}

	lines = append(lines, "## Canonical Specs", "")
	ordered := make([]Spec, len(specs))
	copy(ordered, specs)
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

func aiCheckJSONSchema() string {
	return `{
  "type": "object",
  "required": ["model", "conflict_check"],
  "additionalProperties": false,
  "properties": {
    "model": {"type": "string"},
    "conflict_check": {
      "type": "object",
      "required": ["has_conflicts", "summary", "conflicts"],
      "additionalProperties": false,
      "properties": {
        "has_conflicts": {"type": "boolean"},
        "summary": {"type": "string"},
        "conflicts": {
          "type": "array",
          "items": {
            "type": "object",
            "required": ["spec_a", "spec_b", "domain", "statement_key", "line_a", "line_b", "reason"],
            "additionalProperties": false,
            "properties": {
              "spec_a": {"type": "string"},
              "spec_b": {"type": "string"},
              "domain": {"type": "string"},
              "statement_key": {"type": "string"},
              "line_a": {"type": "string"},
              "line_b": {"type": "string"},
              "reason": {"type": "string"}
            }
          }
        }
      }
    }
  }
}`
}

func parseAICheckResponse(root string, responseFile string) (aiCheckResponse, error) {
	b, err := readAIResponseFile(root, responseFile)
	if err != nil {
		if errors.Is(err, errAIResponseFilePathRequired) {
			return aiCheckResponse{}, fmt.Errorf("from-response check mode requires --response-file")
		}
		return aiCheckResponse{}, err
	}
	return decodeAICheckResponse(b)
}

func decodeAICheckResponse(b []byte) (aiCheckResponse, error) {
	return decodeAIResponseJSON[aiCheckResponse](b, "invalid AI check response JSON")
}

func checkConflictsFromAIResponse(response aiCheckResponse, specByID map[string]Spec, targetID string) ([]CheckConflict, error) {
	conflicts := make([]CheckConflict, 0, len(response.ConflictCheck.Conflicts))
	seen := map[string]struct{}{}
	for _, item := range response.ConflictCheck.Conflicts {
		left := strings.TrimSpace(item.SpecA)
		right := strings.TrimSpace(item.SpecB)
		if left == "" || right == "" || left == right {
			continue
		}
		leftSpec, ok := specByID[left]
		if !ok {
			return nil, fmt.Errorf("AI check conflict references unknown spec id: %s", left)
		}
		rightSpec, ok := specByID[right]
		if !ok {
			return nil, fmt.Errorf("AI check conflict references unknown spec id: %s", right)
		}

		if targetID != "" && left != targetID && right != targetID {
			continue
		}

		lineA := strings.TrimSpace(item.LineA)
		lineB := strings.TrimSpace(item.LineB)
		reason := strings.TrimSpace(item.Reason)
		if lineA == "" {
			lineA = reason
		}
		if lineB == "" {
			lineB = reason
		}
		statementKey := strings.TrimSpace(item.StatementKey)
		if statementKey == "" {
			statementKey = "ai_adjudicated_conflict"
		}

		if targetID != "" && left == targetID {
			left, right = right, left
			leftSpec, rightSpec = rightSpec, leftSpec
			lineA, lineB = lineB, lineA
		} else if targetID == "" && left > right {
			left, right = right, left
			leftSpec, rightSpec = rightSpec, leftSpec
			lineA, lineB = lineB, lineA
		}

		domain := strings.TrimSpace(item.Domain)
		overlap := sharedDomainsForCheck(leftSpec, rightSpec)
		if domain == "" && len(overlap) > 0 {
			domain = overlap[0]
		}
		if domain == "" {
			domain = leftSpec.Domain
		}

		sig := left + "|" + right + "|" + statementKey + "|" + lineA + "|" + lineB
		if _, ok := seen[sig]; ok {
			continue
		}
		seen[sig] = struct{}{}

		conflicts = append(conflicts, CheckConflict{
			SpecA:          left,
			TitleA:         leftSpec.Title,
			DomainA:        leftSpec.Domain,
			SpecB:          right,
			TitleB:         rightSpec.Title,
			DomainB:        rightSpec.Domain,
			Domain:         domain,
			StatementKey:   statementKey,
			LineA:          lineA,
			LineB:          lineB,
			Reason:         reason,
			OverlapDomains: overlap,
		})
	}
	if response.ConflictCheck.HasConflicts && len(conflicts) == 0 {
		return nil, fmt.Errorf("AI check reported conflicts but none were parseable")
	}
	return conflicts, nil
}

func filterCheckSpecsByDomain(specs []Spec, domain string) []Spec {
	filter := strings.TrimSpace(domain)
	out := make([]Spec, 0, len(specs))
	if filter == "" {
		out = append(out, specs...)
		sort.Slice(out, func(i, j int) bool {
			return out[i].ID < out[j].ID
		})
		return out
	}
	for _, spec := range specs {
		for _, specDomain := range mustInclude(spec.TouchedDomains, spec.Domain) {
			if specDomain == filter {
				out = append(out, spec)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func sharedDomainsForCheck(left Spec, right Spec) []string {
	leftDomains := mustInclude(left.TouchedDomains, left.Domain)
	rightDomains := mustInclude(right.TouchedDomains, right.Domain)

	leftSet := make(map[string]struct{}, len(leftDomains))
	for _, domain := range leftDomains {
		leftSet[domain] = struct{}{}
	}

	out := make([]string, 0, len(rightDomains))
	for _, domain := range rightDomains {
		if _, ok := leftSet[domain]; ok {
			out = append(out, domain)
		}
	}
	return normalizeList(out)
}
