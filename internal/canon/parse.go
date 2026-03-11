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
		parsed, err := parseScalarOrList(val)
		if err != nil {
			return nil, fmt.Errorf("invalid frontmatter value for %s: %w", key, err)
		}
		out[key] = parsed
	}
	return out, nil
}

func parseScalarOrList(value string) (any, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}

	if strings.HasPrefix(trimmed, "[") || strings.HasSuffix(trimmed, "]") {
		if !strings.HasPrefix(trimmed, "[") || !strings.HasSuffix(trimmed, "]") {
			return nil, fmt.Errorf("malformed list")
		}
		out, err := parseBracketList(trimmed)
		if err != nil {
			return nil, err
		}
		return out, nil
	}

	if hasQuoteBoundary(trimmed) {
		return parseQuotedScalar(trimmed)
	}

	// Canon emits lowercase boolean literals when it intends booleans.
	// Mixed-case words such as "True" must remain strings.
	if trimmed == "true" {
		return true, nil
	}
	if trimmed == "false" {
		return false, nil
	}

	if i, err := strconv.Atoi(trimmed); err == nil {
		if hasAmbiguousLeadingZero(trimmed) {
			return trimmed, nil
		}
		return i, nil
	}

	return trimmed, nil
}

func parseBracketList(value string) ([]string, error) {
	inner := strings.TrimSpace(value[1 : len(value)-1])
	if inner == "" {
		return []string{}, nil
	}

	out := make([]string, 0, 4)
	for inner != "" {
		item, rest, err := consumeListItem(inner)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
		rest = strings.TrimSpace(rest)
		if rest == "" {
			break
		}
		if rest[0] != ',' {
			return nil, fmt.Errorf("expected comma separator in list")
		}
		inner = strings.TrimSpace(rest[1:])
		if inner == "" {
			return nil, fmt.Errorf("trailing comma in list")
		}
	}
	return out, nil
}

func consumeListItem(value string) (string, string, error) {
	if value == "" {
		return "", "", fmt.Errorf("empty list item")
	}

	switch value[0] {
	case '"', '\'':
		token, rest, err := consumeQuotedToken(value)
		if err != nil {
			return "", "", err
		}
		parsed, err := parseQuotedScalar(token)
		if err != nil {
			return "", "", err
		}
		item, ok := parsed.(string)
		if !ok {
			return "", "", fmt.Errorf("quoted list item must decode to string")
		}
		return item, rest, nil
	default:
		end := strings.IndexByte(value, ',')
		if end == -1 {
			end = len(value)
		}
		item := strings.TrimSpace(value[:end])
		if item == "" {
			return "", "", fmt.Errorf("empty list item")
		}
		if strings.ContainsAny(item, `"'`) {
			return "", "", fmt.Errorf("unexpected quote in bare list item")
		}
		return item, value[end:], nil
	}
}

func consumeQuotedToken(value string) (string, string, error) {
	if value == "" {
		return "", "", fmt.Errorf("empty quoted scalar")
	}

	quote := value[0]
	escaped := false
	for i := 1; i < len(value); i++ {
		ch := value[i]
		if quote == '"' && escaped {
			escaped = false
			continue
		}
		if quote == '"' && ch == '\\' {
			escaped = true
			continue
		}
		if ch == quote {
			return value[:i+1], value[i+1:], nil
		}
	}
	return "", "", fmt.Errorf("unterminated quoted scalar")
}

func parseQuotedScalar(value string) (any, error) {
	if len(value) < 2 {
		return nil, fmt.Errorf("malformed quoted scalar")
	}

	switch value[0] {
	case '"':
		if value[len(value)-1] != '"' {
			return nil, fmt.Errorf("malformed quoted scalar")
		}
		unquoted, err := strconv.Unquote(value)
		if err != nil {
			return nil, fmt.Errorf("invalid quoted scalar: %w", err)
		}
		return unquoted, nil
	case '\'':
		if value[len(value)-1] != '\'' {
			return nil, fmt.Errorf("malformed quoted scalar")
		}
		inner := value[1 : len(value)-1]
		if strings.Contains(inner, "'") {
			return nil, fmt.Errorf("invalid quoted scalar")
		}
		return inner, nil
	default:
		return nil, fmt.Errorf("malformed quoted scalar")
	}
}

func hasQuoteBoundary(value string) bool {
	if len(value) == 0 {
		return false
	}
	startsQuoted := value[0] == '"' || value[0] == '\''
	endsQuoted := value[len(value)-1] == '"' || value[len(value)-1] == '\''
	return startsQuoted || endsQuoted
}

func hasAmbiguousLeadingZero(value string) bool {
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(trimmed, "-") {
		trimmed = trimmed[1:]
	}
	return len(trimmed) > 1 && strings.HasPrefix(trimmed, "0")
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
		Consolidates:   listString(meta, "consolidates"),
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
	}
	if len(spec.Consolidates) > 0 {
		lines = append(lines, "consolidates: "+renderList(spec.Consolidates))
	}
	lines = append(lines,
		"---",
		strings.TrimSpace(spec.Body),
	)
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
