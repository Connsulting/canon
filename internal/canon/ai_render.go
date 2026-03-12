package canon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

type aiRenderResponse struct {
	Model           string                   `json:"model"`
	DomainDocs      []aiRenderDomainDoc      `json:"domain_docs"`
	InteractionDocs []aiRenderInteractionDoc `json:"interaction_docs"`
	Gaps            string                   `json:"gaps"`
}

type aiRenderDomainDoc struct {
	Domain string `json:"domain"`
	Body   string `json:"body"`
}

type aiRenderInteractionDoc struct {
	Name string `json:"name"`
	Body string `json:"body"`
}

func applyAIRenderOverrides(root string, index Index, domainDocs map[string]string, interactionDocs map[string]string, gaps string, options RenderOptions) (map[string]string, map[string]string, string, bool, error) {
	mode := strings.ToLower(strings.TrimSpace(options.AIMode))
	if mode == "" || mode == "off" {
		return copyStringMap(domainDocs), copyStringMap(interactionDocs), gaps, false, nil
	}

	provider := strings.ToLower(strings.TrimSpace(options.AIProvider))
	if provider == "" {
		provider = "codex"
	}

	var response aiRenderResponse
	var err error
	switch mode {
	case "from-response":
		response, err = parseAIRenderResponse(root, options.ResponseFile)
	case "auto":
		if !aiProviderRuntimeReady(provider) {
			return nil, nil, "", false, fmt.Errorf("ai provider %s is not runtime-ready", provider)
		}
		response, err = runHeadlessAIRender(provider, index, domainDocs, interactionDocs, gaps, root)
	default:
		return nil, nil, "", false, fmt.Errorf("unsupported AI render mode: %s", mode)
	}
	if err != nil {
		return nil, nil, "", false, err
	}

	updatedDomainDocs := copyStringMap(domainDocs)
	updatedInteractionDocs := copyStringMap(interactionDocs)
	updatedGaps := gaps

	for _, doc := range response.DomainDocs {
		domain := strings.TrimSpace(doc.Domain)
		if domain == "" {
			continue
		}
		if _, ok := updatedDomainDocs[domain]; !ok {
			continue
		}
		body := strings.TrimSpace(doc.Body)
		if body == "" {
			continue
		}
		updatedDomainDocs[domain] = strings.TrimRight(body, "\n") + "\n"
	}

	for _, doc := range response.InteractionDocs {
		name := strings.TrimSpace(doc.Name)
		if name == "" {
			continue
		}
		if _, ok := updatedInteractionDocs[name]; !ok {
			continue
		}
		body := strings.TrimSpace(doc.Body)
		if body == "" {
			continue
		}
		updatedInteractionDocs[name] = strings.TrimRight(body, "\n") + "\n"
	}

	if strings.TrimSpace(response.Gaps) != "" {
		updatedGaps = strings.TrimRight(strings.TrimSpace(response.Gaps), "\n") + "\n"
	}

	return updatedDomainDocs, updatedInteractionDocs, updatedGaps, true, nil
}

func runHeadlessAIRender(provider string, index Index, domainDocs map[string]string, interactionDocs map[string]string, gaps string, root string) (aiRenderResponse, error) {
	promptText := buildAIRenderPrompt(provider, index, domainDocs, interactionDocs, gaps)
	schemaText := aiRenderJSONSchema()
	timeout := aiRenderTimeout()

	switch provider {
	case "codex":
		schemaFile, err := os.CreateTemp("", "canon-render-schema-*.json")
		if err != nil {
			return aiRenderResponse{}, err
		}
		schemaPath := schemaFile.Name()
		defer func() {
			schemaFile.Close()
			_ = os.Remove(schemaPath)
		}()
		if _, err := schemaFile.WriteString(schemaText); err != nil {
			return aiRenderResponse{}, err
		}
		if err := schemaFile.Close(); err != nil {
			return aiRenderResponse{}, err
		}

		responseFile, err := os.CreateTemp("", "canon-render-response-*.json")
		if err != nil {
			return aiRenderResponse{}, err
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
				return aiRenderResponse{}, fmt.Errorf("codex exec timed out after %s", timeout)
			}
			return aiRenderResponse{}, fmt.Errorf("codex exec failed: %w\n%s", err, strings.TrimSpace(string(output)))
		}

		responseBytes, err := os.ReadFile(responsePath)
		if err != nil {
			return aiRenderResponse{}, err
		}
		return decodeAIRenderResponse(responseBytes)

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
				return aiRenderResponse{}, fmt.Errorf("claude --print timed out after %s", timeout)
			}
			return aiRenderResponse{}, fmt.Errorf("claude --print failed: %w\n%s", err, strings.TrimSpace(string(output)))
		}
		return decodeAIRenderResponse(output)

	default:
		return aiRenderResponse{}, fmt.Errorf("unsupported ai provider: %s", provider)
	}
}

func buildAIRenderPrompt(provider string, index Index, domainDocs map[string]string, interactionDocs map[string]string, gaps string) string {
	lines := []string{
		"# Canon AI Render",
		"",
		"Provider: " + provider,
		"",
		"Task:",
		"1. Synthesize effective state from the provided deterministic draft documents.",
		"2. Collapse overlapping historical prose into concise current-state statements.",
		"3. Keep file structure stable. Only update body content for existing files.",
		"4. Return JSON only with the schema below.",
		"",
		"Schema:",
		"{",
		`  "model": "string",`,
		`  "domain_docs": [`,
		`    {"domain":"string","body":"markdown"}`,
		"  ],",
		`  "interaction_docs": [`,
		`    {"name":"left-right","body":"markdown"}`,
		"  ],",
		`  "gaps": "markdown"`,
		"}",
		"",
		"## Canonical Specs",
		"",
	}

	specIDs := make([]string, 0, len(index.Specs))
	for id := range index.Specs {
		specIDs = append(specIDs, id)
	}
	sort.Strings(specIDs)
	for _, id := range specIDs {
		spec := index.Specs[id]
		lines = append(lines,
			"### "+spec.ID,
			"",
			"Type: "+spec.Type,
			"Domain: "+spec.Domain,
			"Created: "+spec.Created,
			"DependsOn: "+renderList(spec.DependsOn),
			"TouchedDomains: "+renderList(spec.TouchedDomains),
			"",
			spec.Body,
			"",
		)
	}

	lines = append(lines, "## Target Domain Files", "")
	domains := sortedKeys(domainDocs)
	for _, domain := range domains {
		lines = append(lines,
			"- domain: "+domain,
			"  path: state/"+domain+".md",
			"  contributing_specs: "+renderList(index.Domains[domain]),
		)
	}

	lines = append(lines, "", "## Target Interaction Files", "")
	interactions := sortedKeys(interactionDocs)
	for _, name := range interactions {
		lines = append(lines,
			"- name: "+name,
			"  path: state/interactions/"+name+".md",
		)
	}

	lines = append(lines,
		"",
		"## Gaps Draft",
		"",
		gaps,
		"",
		"## Canonical Index",
		"",
		serializeIndexYAML(index),
		"",
	)

	return strings.Join(lines, "\n")
}

func aiRenderJSONSchema() string {
	return `{
  "type": "object",
  "required": ["model", "domain_docs", "interaction_docs", "gaps"],
  "additionalProperties": false,
  "properties": {
    "model": {"type": "string"},
    "domain_docs": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["domain", "body"],
        "additionalProperties": false,
        "properties": {
          "domain": {"type": "string"},
          "body": {"type": "string"}
        }
      }
    },
    "interaction_docs": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["name", "body"],
        "additionalProperties": false,
        "properties": {
          "name": {"type": "string"},
          "body": {"type": "string"}
        }
      }
    },
    "gaps": {"type": "string"}
  }
}`
}

func parseAIRenderResponse(root string, responseFile string) (aiRenderResponse, error) {
	b, err := readAIResponseFile(root, responseFile)
	if err != nil {
		if errors.Is(err, errAIResponseFilePathRequired) {
			return aiRenderResponse{}, fmt.Errorf("from-response render mode requires --response-file")
		}
		return aiRenderResponse{}, err
	}
	return decodeAIRenderResponse(b)
}

func decodeAIRenderResponse(b []byte) (aiRenderResponse, error) {
	response, err := decodeAIResponseJSON[aiRenderResponse](b, "invalid AI render response JSON")
	if err != nil {
		return aiRenderResponse{}, err
	}
	sort.Slice(response.DomainDocs, func(i, j int) bool {
		return response.DomainDocs[i].Domain < response.DomainDocs[j].Domain
	})
	sort.Slice(response.InteractionDocs, func(i, j int) bool {
		return response.InteractionDocs[i].Name < response.InteractionDocs[j].Name
	})
	return response, nil
}

func copyStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func aiProviderRuntimeReady(provider string) bool {
	switch provider {
	case "codex", "claude":
		_, err := exec.LookPath(provider)
		return err == nil
	default:
		return false
	}
}

func aiRenderTimeout() time.Duration {
	const defaultSeconds = 600
	value := strings.TrimSpace(os.Getenv("CANON_AI_RENDER_TIMEOUT_SECONDS"))
	if value == "" {
		return defaultSeconds * time.Second
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 0 {
		return defaultSeconds * time.Second
	}
	if seconds == 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
