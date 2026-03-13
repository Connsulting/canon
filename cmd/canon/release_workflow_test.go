package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseWorkflowBuildOptimizationGuardrails(t *testing.T) {
	workflowPath := filepath.Join("..", "..", ".github", "workflows", "release.yml")
	content, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}

	text := string(content)
	for _, snippet := range []string{
		"\n  version:\n",
		"\n  test:\n",
		"\n  vet:\n",
		"needs: [version, test, vet]",
		"cache-dependency-path: go.mod",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("release workflow missing %q", snippet)
		}
	}

	if strings.Contains(text, "\n  verify:\n") {
		t.Fatalf("release workflow reverted to a single verify job")
	}

	if got := strings.Count(text, "cache-dependency-path: go.mod"); got != 3 {
		t.Fatalf("expected 3 explicit Go cache dependency paths, got %d", got)
	}
}
