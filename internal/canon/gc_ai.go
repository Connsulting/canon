package canon

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

type aiGCResponse struct {
	Model             string     `json:"model"`
	Summary           string     `json:"summary"`
	ConsolidatedSpecs []aiGCSpec `json:"consolidated_specs"`
}

type aiGCSpec struct {
	ID             string   `json:"id"`
	Type           string   `json:"type"`
	Title          string   `json:"title"`
	Domain         string   `json:"domain"`
	Created        string   `json:"created"`
	DependsOn      []string `json:"depends_on"`
	TouchedDomains []string `json:"touched_domains"`
	Consolidates   []string `json:"consolidates"`
	Body           string   `json:"body"`
}

func gcRunAIConsolidation(root string, target []Spec, mode string, aiProvider string, responseFile string) ([]aiGCSpec, error) {
	provider := strings.ToLower(strings.TrimSpace(aiProvider))
	if provider == "" {
		provider = "codex"
	}

	promptMode := strings.ToLower(strings.TrimSpace(mode))
	if promptMode == "" {
		promptMode = "auto"
	}

	var response aiGCResponse
	var err error
	switch promptMode {
	case "from-response":
		response, err = parseAIGCResponse(root, responseFile)
	case "auto":
		if !aiProviderRuntimeReady(provider) {
			return nil, fmt.Errorf("ai provider %s is not runtime-ready", provider)
		}
		response, err = runHeadlessGCAI(provider, root, target)
	default:
		return nil, fmt.Errorf("unsupported gc ai mode: %s", promptMode)
	}
	if err != nil {
		return nil, err
	}

	if len(response.ConsolidatedSpecs) == 0 {
		return nil, fmt.Errorf("ai gc response did not include consolidated specs")
	}
	return response.ConsolidatedSpecs, nil
}

func runHeadlessGCAI(provider string, root string, target []Spec) (aiGCResponse, error) {
	promptText := buildGCAPIPrompt(provider, target)
	schemaText := gcConsolidationJSONSchema()
	switch provider {
	case "codex":
		schemaFile, err := os.CreateTemp("", "canon-gc-schema-*.json")
		if err != nil {
			return aiGCResponse{}, err
		}
		schemaPath := schemaFile.Name()
		defer func() {
			schemaFile.Close()
			_ = os.Remove(schemaPath)
		}()
		if _, err := schemaFile.WriteString(schemaText); err != nil {
			return aiGCResponse{}, err
		}
		if err := schemaFile.Close(); err != nil {
			return aiGCResponse{}, err
		}

		responseFile, err := os.CreateTemp("", "canon-gc-response-*.json")
		if err != nil {
			return aiGCResponse{}, err
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
			return aiGCResponse{}, fmt.Errorf("codex exec failed: %w\n%s", err, strings.TrimSpace(string(output)))
		}

		responseBytes, err := os.ReadFile(responsePath)
		if err != nil {
			return aiGCResponse{}, err
		}
		return parseAIGCResponseBytes(responseBytes)

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
			return aiGCResponse{}, fmt.Errorf("claude --print failed: %w\n%s", err, strings.TrimSpace(string(output)))
		}
		return parseAIGCResponseBytes(output)

	default:
		return aiGCResponse{}, fmt.Errorf("unsupported ai provider: %s", provider)
	}
}

func parseAIGCResponse(root string, responseFile string) (aiGCResponse, error) {
	b, err := readAIResponseFile(root, responseFile)
	if err != nil {
		if errors.Is(err, errAIResponseFilePathRequired) {
			return aiGCResponse{}, fmt.Errorf("from-response gc mode requires --response-file")
		}
		return aiGCResponse{}, err
	}
	return parseAIGCResponseBytes(b)
}

func parseAIGCResponseBytes(b []byte) (aiGCResponse, error) {
	response, err := decodeAIResponseJSON[aiGCResponse](b, "invalid AI gc response JSON")
	if err != nil {
		return aiGCResponse{}, err
	}
	sort.Slice(response.ConsolidatedSpecs, func(i, j int) bool {
		left := strings.TrimSpace(response.ConsolidatedSpecs[i].ID)
		right := strings.TrimSpace(response.ConsolidatedSpecs[j].ID)
		return left < right
	})
	return response, nil
}

func buildGCAPIPrompt(provider string, target []Spec) string {
	lines := []string{
		"# Canon AI GC",
		"",
		"Provider: " + provider,
		"",
		"Task:",
		"1. Consolidate the provided specs into a minimal set of canonical specs.",
		"2. Preserve active, non superseded requirements and remove redundant text.",
		"3. Resolve contradictions by choosing the most recent created spec.",
		"4. Preserve required dependencies to specs outside the consolidation scope.",
		"5. Return JSON only with the schema below.",
		"",
		"Schema:",
		"{",
		`  "model": "string",`,
		`  "summary": "string",`,
		`  "consolidated_specs": [`,
		`    {`,
		`      "id": "string",`,
		`      "type": "feature|technical|resolution",`,
		`      "title": "string",`,
		`      "domain": "string",`,
		`      "created": "RFC3339 timestamp",`,
		`      "depends_on": ["spec-id"],`,
		`      "touched_domains": ["domain"],`,
		`      "consolidates": ["source-spec-id"],`,
		`      "body": "markdown"`,
		`    }`,
		"  ]",
		"}",
		"",
		"Requirements:",
		"- Every original spec id from the consolidation set must appear exactly once in one of the consolidates arrays.",
		"- Keep domain metadata and type metadata where sensible for each consolidated spec.",
		"- Preserve all external depends_on entries from source specs into consolidated specs.",
		"",
		"## Canonical Specs To Consolidate",
	}

	sorted := make([]Spec, len(target))
	copy(sorted, target)
	sortSpecsStable(sorted)
	for _, spec := range sorted {
		lines = append(lines,
			"### "+spec.ID,
			"",
			"Type: "+spec.Type,
			"Domain: "+spec.Domain,
			"Created: "+spec.Created,
			"DependsOn: "+renderList(spec.DependsOn),
			"TouchedDomains: "+renderList(spec.TouchedDomains),
			"",
			strings.TrimSpace(spec.Body),
			"",
		)
	}

	return strings.Join(lines, "\n")
}

func gcConsolidationJSONSchema() string {
	return `{
  "type": "object",
  "required": ["model", "summary", "consolidated_specs"],
  "additionalProperties": false,
  "properties": {
    "model": {"type": "string"},
    "summary": {"type": "string"},
    "consolidated_specs": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["id", "type", "title", "domain", "created", "depends_on", "touched_domains", "consolidates", "body"],
        "additionalProperties": false,
        "properties": {
          "id": {"type": "string"},
          "type": {"type": "string"},
          "title": {"type": "string"},
          "domain": {"type": "string"},
          "created": {"type": "string"},
          "depends_on": {"type": "array", "items": {"type": "string"}},
          "touched_domains": {"type": "array", "items": {"type": "string"}},
          "consolidates": {"type": "array", "items": {"type": "string"}},
          "body": {"type": "string"}
        }
      }
    }
  }
}`
}
