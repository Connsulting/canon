package canon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func nowUTC() time.Time {
	return time.Now().UTC()
}

func commandContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		return context.WithTimeout(context.Background(), timeout)
	}
	return context.WithCancel(context.Background())
}

func normalizeList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		v := strings.TrimSpace(value)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func mustInclude(values []string, required string) []string {
	required = strings.TrimSpace(required)
	if required == "" {
		return normalizeList(values)
	}
	list := append([]string{}, values...)
	list = append(list, required)
	return normalizeList(list)
}

func checksum(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func writeTextIfChanged(path string, content string) (bool, error) {
	current, err := os.ReadFile(path)
	if err == nil {
		if string(current) == content {
			return false, nil
		}
	} else if !os.IsNotExist(err) {
		return false, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func slugify(value string) string {
	clean := strings.ToLower(strings.TrimSpace(value))
	clean = nonAlnum.ReplaceAllString(clean, "-")
	clean = strings.Trim(clean, "-")
	if clean == "" {
		return "spec"
	}
	if len(clean) > 48 {
		clean = clean[:48]
		clean = strings.Trim(clean, "-")
	}
	if clean == "" {
		return "spec"
	}
	return clean
}

func inferTitle(body string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			t := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			if t != "" {
				return t
			}
		}
	}
	return "Untitled Spec"
}

func parseRFC3339OrNow(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nowUTC().Format(time.RFC3339), nil
	}
	t, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid RFC3339 timestamp: %s", trimmed)
	}
	return t.UTC().Format(time.RFC3339), nil
}
