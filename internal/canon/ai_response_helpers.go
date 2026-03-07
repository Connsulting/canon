package canon

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var errAIResponseFilePathRequired = errors.New("response-file path is required")

func normalizeAIResponseFilePath(root string, responseFile string) (string, error) {
	path := strings.TrimSpace(responseFile)
	if path == "" {
		return "", errAIResponseFilePathRequired
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	return path, nil
}

func readAIResponseFile(root string, responseFile string) ([]byte, error) {
	path, err := normalizeAIResponseFilePath(root, responseFile)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func decodeAIResponseJSON[T any](b []byte, invalidJSONMessage string) (T, error) {
	var response T
	if err := json.Unmarshal(b, &response); err == nil {
		return response, nil
	}
	fragment, ok := extractFirstJSONObjectFragment(b)
	if !ok {
		var zero T
		return zero, errors.New(invalidJSONMessage)
	}
	if err := json.Unmarshal(fragment, &response); err != nil {
		var zero T
		return zero, fmt.Errorf("%s: %w", invalidJSONMessage, err)
	}
	return response, nil
}

func extractFirstJSONObjectFragment(b []byte) ([]byte, bool) {
	trimmed := bytes.TrimSpace(b)
	start := bytes.IndexByte(trimmed, '{')
	if start == -1 {
		return nil, false
	}

	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(trimmed); i++ {
		switch ch := trimmed[i]; {
		case inString:
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
		case ch == '"':
			inString = true
		case ch == '{':
			depth++
		case ch == '}':
			depth--
			if depth == 0 {
				return trimmed[start : i+1], true
			}
			if depth < 0 {
				return nil, false
			}
		}
	}
	return nil, false
}
