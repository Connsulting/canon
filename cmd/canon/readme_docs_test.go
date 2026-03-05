package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestREADMEIncludesAllDocumentedCLICommands(t *testing.T) {
	readme := loadREADMEForDocsTest(t)

	required := []string{
		"go run ./cmd/canon init",
		"go run ./cmd/canon ingest <spec-file>",
		"go run ./cmd/canon import <spec-file>",
		"go run ./cmd/canon raw",
		"go run ./cmd/canon log",
		"go run ./cmd/canon check",
		"go run ./cmd/canon show <spec-id>",
		"go run ./cmd/canon reset <spec-id>",
		"go run ./cmd/canon index",
		"go run ./cmd/canon render --write",
		"go run ./cmd/canon blame \"<behavior description>\"",
		"go run ./cmd/canon deps-risk",
		"go run ./cmd/canon status",
		"go run ./cmd/canon gc",
		"go run ./cmd/canon version",
	}

	for _, snippet := range required {
		if !strings.Contains(readme, snippet) {
			t.Fatalf("README missing command documentation snippet %q", snippet)
		}
	}
}

func TestREADMEIncludesCriticalFlagParityNotes(t *testing.T) {
	readme := loadREADMEForDocsTest(t)

	required := []string{
		"Log options:",
		"--all` include all disconnected heads (default: `true`)",
		"--show-tags` include `[type/domain]` tags",
		"GC options:",
		"--root <path>` repository root (default: `.`)",
		"Version options:",
		"--short` print version only",
	}

	for _, snippet := range required {
		if !strings.Contains(readme, snippet) {
			t.Fatalf("README missing critical CLI parity snippet %q", snippet)
		}
	}

	if strings.Contains(readme, "default scopes to primary head") {
		t.Fatalf("README still contains stale --all default description")
	}
}

func loadREADMEForDocsTest(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}
	readmePath := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", "README.md"))

	content, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read README at %s: %v", readmePath, err)
	}
	return string(content)
}
