package canon

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func splitOptionalFrontmatter(text string) (map[string]any, string, bool, error) {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, text, false, nil
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return nil, "", false, fmt.Errorf("missing YAML frontmatter end delimiter")
	}

	frontmatterLines := lines[1:end]
	body := strings.Join(lines[end+1:], "\n")
	meta, err := parseFrontmatter(frontmatterLines)
	if err != nil {
		return nil, "", false, err
	}
	return meta, body, true, nil
}

func parseFrontmatter(lines []string) (map[string]any, error) {
	out := make(map[string]any)
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid frontmatter line: %s", raw)
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		parsed := parseScalarOrList(val)
		out[key] = parsed
	}
	return out, nil
}

func parseScalarOrList(value string) any {
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"))
		if inner == "" {
			return []string{}
		}
		parts := strings.Split(inner, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			item := stripQuotes(strings.TrimSpace(part))
			if item != "" {
				out = append(out, item)
			}
		}
		return out
	}

	if i, err := strconv.Atoi(value); err == nil {
		return i
	}

	lower := strings.ToLower(value)
	if lower == "true" {
		return true
	}
	if lower == "false" {
		return false
	}

	return stripQuotes(value)
}

func stripQuotes(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) >= 2 {
		if (trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"') || (trimmed[0] == '\'' && trimmed[len(trimmed)-1] == '\'') {
			return trimmed[1 : len(trimmed)-1]
		}
	}
	return trimmed
}

func listString(meta map[string]any, key string) []string {
	v, ok := meta[key]
	if !ok {
		return nil
	}
	switch typed := v.(type) {
	case []string:
		return normalizeList(typed)
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []string{strings.TrimSpace(typed)}
	default:
		return nil
	}
}

func scalarString(meta map[string]any, key string) string {
	v, ok := meta[key]
	if !ok {
		return ""
	}
	switch typed := v.(type) {
	case string:
		return strings.TrimSpace(typed)
	case int:
		return strconv.Itoa(typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", typed))
	}
}

func parseSpecText(text string, sourcePath string) (Spec, error) {
	meta, body, hasFrontmatter, err := splitOptionalFrontmatter(text)
	if err != nil {
		return Spec{}, err
	}
	if !hasFrontmatter {
		return Spec{}, fmt.Errorf("missing YAML frontmatter in %s", sourcePath)
	}

	s := Spec{
		ID:             scalarString(meta, "id"),
		Type:           scalarString(meta, "type"),
		Title:          scalarString(meta, "title"),
		Domain:         scalarString(meta, "domain"),
		Created:        scalarString(meta, "created"),
		DependsOn:      listString(meta, "depends_on"),
		TouchedDomains: listString(meta, "touched_domains"),
		Path:           sourcePath,
		Body:           strings.TrimSpace(body),
	}

	if s.ID == "" {
		return Spec{}, fmt.Errorf("missing required field `id` in %s", sourcePath)
	}
	if s.Domain == "" {
		return Spec{}, fmt.Errorf("missing required field `domain` in %s", sourcePath)
	}
	if s.Type == "" {
		s.Type = "feature"
	}
	if s.Title == "" {
		s.Title = s.ID
	}
	if s.Created == "" {
		s.Created = nowUTC().Format(timeRFC3339)
	}
	s.TouchedDomains = mustInclude(s.TouchedDomains, s.Domain)
	s.DependsOn = normalizeList(s.DependsOn)
	return s, nil
}

const timeRFC3339 = "2006-01-02T15:04:05Z07:00"

func canonicalSpecText(spec Spec) string {
	depends := renderList(spec.DependsOn)
	touches := renderList(spec.TouchedDomains)
	lines := []string{
		"---",
		"id: " + spec.ID,
		"type: " + spec.Type,
		"title: " + yamlScalar(spec.Title),
		"domain: " + spec.Domain,
		"created: " + spec.Created,
		"depends_on: " + depends,
		"touched_domains: " + touches,
		"---",
		strings.TrimSpace(spec.Body),
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
}

func yamlScalar(value string) string {
	v := strings.TrimSpace(value)
	if v == "" {
		return "''"
	}
	for _, ch := range v {
		if !(ch == '-' || ch == '_' || ch == '.' || ch == '/' || ch == ':' || ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z') {
			return strconv.Quote(v)
		}
	}
	return v
}

func renderList(values []string) string {
	values = normalizeList(values)
	if len(values) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, yamlScalar(v))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func specSortKey(spec Spec) string {
	return spec.Created + "|" + spec.ID + "|" + spec.Path
}

func sortSpecsStable(specs []Spec) {
	sort.Slice(specs, func(i, j int) bool {
		return specSortKey(specs[i]) < specSortKey(specs[j])
	})
}

func specFileName(id string) string {
	return filepath.Base(id) + ".spec.md"
}
