package canon

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSemanticDiffParsesDiffFileAndNormalizesExplanationsDeterministically(t *testing.T) {
	root := t.TempDir()
	diffPath := filepath.Join(root, "semantic.diff")
	diffText := `diff --git a/app/auth.go b/app/auth.go
index 1111111..2222222 100644
--- a/app/auth.go
+++ b/app/auth.go
@@ -10,2 +10,3 @@ func allowAccess() {
-allowGuest := true
+allowGuest := false
+requireMFA := true
 }
diff --git a/db/migrations/001_add_mfa.sql b/db/migrations/001_add_mfa.sql
new file mode 100644
index 0000000..3333333
--- /dev/null
+++ b/db/migrations/001_add_mfa.sql
@@ -0,0 +1,2 @@
+ALTER TABLE users ADD COLUMN mfa_enabled BOOLEAN NOT NULL DEFAULT FALSE;
+UPDATE users SET mfa_enabled = FALSE;
`
	if err := os.WriteFile(diffPath, []byte(diffText), 0o644); err != nil {
		t.Fatalf("failed writing diff fixture: %v", err)
	}

	responsePayload := map[string]any{
		"model":   "codex-headless",
		"summary": "Auth hardening and migration updates.",
		"explanations": []map[string]any{
			{
				"id":        "exp-high",
				"category":  "security",
				"impact":    "high",
				"summary":   "Guest access is now denied.",
				"rationale": "Authentication policy now enforces explicit access checks.",
				"evidence": []map[string]any{
					{"file": "app/auth.go", "kind": "hunk", "old_start": 10, "old_lines": 2, "new_start": 10, "new_lines": 3},
				},
			},
			{
				"id":        "duplicate-high",
				"category":  "security",
				"impact":    "high",
				"summary":   "Guest access is now denied.",
				"rationale": "The gate now routes requests through a stricter code path.",
				"evidence": []map[string]any{
					{"file": "app/auth.go", "kind": "hunk", "old_start": 10, "old_lines": 2, "new_start": 10, "new_lines": 3},
				},
			},
			{
				"id":        "exp-critical",
				"category":  "data",
				"impact":    "critical",
				"summary":   "A new non-null user column is introduced.",
				"rationale": "Existing writes and inserts must satisfy the new column semantics.",
				"evidence": []map[string]any{
					{"file": "db/migrations/001_add_mfa.sql", "kind": "hunk", "old_start": 0, "old_lines": 0, "new_start": 1, "new_lines": 2},
					{"file": "db/migrations/001_add_mfa.sql", "kind": "hunk", "old_start": 0, "old_lines": 0, "new_start": 1, "new_lines": 2},
				},
			},
			{
				"id":        "",
				"category":  "ops",
				"impact":    "urgent",
				"summary":   "Migration execution becomes part of deploy flow.",
				"rationale": "Rollout now includes a schema mutation step.",
				"evidence": []map[string]any{
					{"file": "db/migrations/001_add_mfa.sql", "kind": "file"},
				},
			},
			{
				"id":        "drop-me",
				"category":  "misc",
				"impact":    "medium",
				"summary":   " ",
				"rationale": " ",
				"evidence":  []map[string]any{},
			},
		},
	}
	responsePath := writeSemanticDiffResponseFixture(t, root, "semantic-response.json", responsePayload)

	wrappedBytes, err := os.ReadFile(responsePath)
	if err != nil {
		t.Fatalf("failed reading response fixture: %v", err)
	}
	wrappedPath := filepath.Join(root, "semantic-response-wrapped.json")
	wrapped := "model output:\n" + string(wrappedBytes) + "\nend"
	if err := os.WriteFile(wrappedPath, []byte(wrapped), 0o644); err != nil {
		t.Fatalf("failed writing wrapped response fixture: %v", err)
	}

	result, err := SemanticDiff(root, SemanticDiffOptions{
		DiffFile:     filepath.Base(diffPath),
		AIMode:       "from-response",
		ResponseFile: filepath.Base(wrappedPath),
	})
	if err != nil {
		t.Fatalf("SemanticDiff failed: %v", err)
	}

	expectedSource := filepath.ToSlash(diffPath)
	if result.DiffSource != expectedSource {
		t.Fatalf("expected diff source %q, got %q", expectedSource, result.DiffSource)
	}
	if result.ChangedFileCount != 2 {
		t.Fatalf("expected changed file count 2, got %d", result.ChangedFileCount)
	}
	if result.TotalAddedLines != 4 || result.TotalDeletedLines != 1 || result.TotalHunks != 2 {
		t.Fatalf("unexpected diff totals: +%d -%d hunks=%d", result.TotalAddedLines, result.TotalDeletedLines, result.TotalHunks)
	}

	expectedFiles := []SemanticDiffFileChange{
		{File: "app/auth.go", Status: "modified", AddedLines: 2, DeletedLines: 1, HunkCount: 1},
		{File: "db/migrations/001_add_mfa.sql", Status: "added", AddedLines: 2, DeletedLines: 0, HunkCount: 1},
	}
	if !reflect.DeepEqual(result.ChangedFiles, expectedFiles) {
		t.Fatalf("unexpected changed files:\nwant=%+v\n got=%+v", expectedFiles, result.ChangedFiles)
	}

	if len(result.Explanations) != 3 {
		t.Fatalf("expected 3 normalized explanations, got %d", len(result.Explanations))
	}
	if result.Explanations[0].Impact != SemanticDiffImpactCritical {
		t.Fatalf("expected first explanation critical, got %s", result.Explanations[0].Impact)
	}
	if result.Explanations[1].Impact != SemanticDiffImpactHigh {
		t.Fatalf("expected second explanation high, got %s", result.Explanations[1].Impact)
	}
	if result.Explanations[2].Impact != SemanticDiffImpactMedium {
		t.Fatalf("expected unknown impact to normalize to medium, got %s", result.Explanations[2].Impact)
	}
	securityCount := 0
	for _, explanation := range result.Explanations {
		if explanation.Category != "security" {
			continue
		}
		securityCount++
		if explanation.Rationale != "Authentication policy now enforces explicit access checks." {
			t.Fatalf("expected first security rationale to be retained, got %q", explanation.Rationale)
		}
	}
	if securityCount != 1 {
		t.Fatalf("expected one deduplicated security explanation, got %d", securityCount)
	}
	if !strings.HasPrefix(result.Explanations[2].ID, "exp-") {
		t.Fatalf("expected generated id prefix exp-, got %q", result.Explanations[2].ID)
	}

	if result.Summary.TotalExplanations != 3 {
		t.Fatalf("expected summary explanations 3, got %d", result.Summary.TotalExplanations)
	}
	if result.Summary.HighestImpact != SemanticDiffImpactCritical {
		t.Fatalf("expected highest impact critical, got %s", result.Summary.HighestImpact)
	}
	if result.Summary.ImpactCounts.Critical != 1 || result.Summary.ImpactCounts.High != 1 || result.Summary.ImpactCounts.Medium != 1 {
		t.Fatalf("unexpected impact counts: %+v", result.Summary.ImpactCounts)
	}
	expectedCategories := []SemanticDiffCategoryCount{
		{Category: "data", Count: 1},
		{Category: "ops", Count: 1},
		{Category: "security", Count: 1},
	}
	if !reflect.DeepEqual(result.Summary.CategoryCounts, expectedCategories) {
		t.Fatalf("unexpected category counts:\nwant=%+v\n got=%+v", expectedCategories, result.Summary.CategoryCounts)
	}

	again, err := SemanticDiff(root, SemanticDiffOptions{
		DiffFile:     filepath.Base(diffPath),
		AIMode:       "from-response",
		ResponseFile: filepath.Base(wrappedPath),
	})
	if err != nil {
		t.Fatalf("second SemanticDiff failed: %v", err)
	}
	if !reflect.DeepEqual(result, again) {
		t.Fatalf("semantic diff result changed between runs\nfirst=%+v\nsecond=%+v", result, again)
	}
}

func TestSemanticDiffChangedFilesParserHandlesPathsWithSpaces(t *testing.T) {
	diffText := `diff --git "a/app/auth policy.go" "b/app/auth policy.go"
index 1111111..2222222 100644
--- "a/app/auth policy.go"
+++ "b/app/auth policy.go"
@@ -1 +1 @@
-before
+after
diff --git "a/db/old policy.sql" "b/db/new policy.sql"
similarity index 95%
rename from db/old policy.sql
rename to db/new policy.sql
index 3333333..4444444 100644
--- "a/db/old policy.sql"
+++ "b/db/new policy.sql"
@@ -1 +1 @@
-old
+new
`

	got := parseSemanticDiffChangedFiles(diffText)
	want := []SemanticDiffFileChange{
		{File: "app/auth policy.go", Status: "modified", AddedLines: 1, DeletedLines: 1, HunkCount: 1},
		{File: "db/new policy.sql", Status: "renamed", AddedLines: 1, DeletedLines: 1, HunkCount: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected parsed changed files:\nwant=%+v\n got=%+v", want, got)
	}
}

func TestSemanticDiffReadsDefaultGitDiffWhenDiffFileNotProvided(t *testing.T) {
	root := t.TempDir()
	runSemanticGit(t, root, "init")
	runSemanticGit(t, root, "config", "user.email", "semantic-diff@example.com")
	runSemanticGit(t, root, "config", "user.name", "Semantic Diff")

	targetPath := filepath.Join(root, "service.txt")
	if err := os.WriteFile(targetPath, []byte("before\n"), 0o644); err != nil {
		t.Fatalf("failed writing seed file: %v", err)
	}
	runSemanticGit(t, root, "add", "service.txt")
	runSemanticGit(t, root, "commit", "-m", "seed")

	if err := os.WriteFile(targetPath, []byte("after\nextra\n"), 0o644); err != nil {
		t.Fatalf("failed writing changed file: %v", err)
	}

	responsePath := writeSemanticDiffResponseFixture(t, root, "semantic-response.json", map[string]any{
		"model":        "codex-headless",
		"summary":      "No meaningful semantic impact.",
		"explanations": []map[string]any{},
	})

	result, err := SemanticDiff(root, SemanticDiffOptions{
		AIMode:       "from-response",
		ResponseFile: responsePath,
	})
	if err != nil {
		t.Fatalf("SemanticDiff failed: %v", err)
	}

	if result.DiffSource != "git" {
		t.Fatalf("expected git diff source, got %q", result.DiffSource)
	}
	if result.ChangedFileCount != 1 {
		t.Fatalf("expected one changed file, got %d", result.ChangedFileCount)
	}
	if result.ChangedFiles[0].File != "service.txt" {
		t.Fatalf("expected service.txt changed file, got %+v", result.ChangedFiles[0])
	}
	if result.TotalAddedLines == 0 || result.TotalDeletedLines == 0 {
		t.Fatalf("expected non-zero line deltas, got +%d -%d", result.TotalAddedLines, result.TotalDeletedLines)
	}
}

func TestSemanticDiffValidationAndErrorPaths(t *testing.T) {
	root := t.TempDir()
	diffPath := filepath.Join(root, "changes.diff")
	if err := os.WriteFile(diffPath, []byte("diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-old\n+new\n"), 0o644); err != nil {
		t.Fatalf("failed writing diff fixture: %v", err)
	}
	responsePath := writeSemanticDiffResponseFixture(t, root, "semantic-response.json", map[string]any{
		"model":        "codex-headless",
		"summary":      "summary",
		"explanations": []map[string]any{},
	})

	if _, err := SemanticDiff(root, SemanticDiffOptions{DiffFile: diffPath, AIMode: "off", ResponseFile: responsePath}); err == nil {
		t.Fatalf("expected unsupported mode error")
	} else if !strings.Contains(err.Error(), "unsupported semantic-diff ai mode") {
		t.Fatalf("unexpected unsupported mode error: %v", err)
	}

	if _, err := SemanticDiff(root, SemanticDiffOptions{DiffFile: diffPath, AIMode: "from-response"}); err == nil {
		t.Fatalf("expected missing response-file error")
	} else if !strings.Contains(err.Error(), "requires --response-file") {
		t.Fatalf("unexpected from-response missing file error: %v", err)
	}

	emptyDiffPath := filepath.Join(root, "empty.diff")
	if err := os.WriteFile(emptyDiffPath, []byte("\n\n"), 0o644); err != nil {
		t.Fatalf("failed writing empty diff fixture: %v", err)
	}
	if _, err := SemanticDiff(root, SemanticDiffOptions{DiffFile: emptyDiffPath, AIMode: "from-response", ResponseFile: responsePath}); err == nil {
		t.Fatalf("expected empty diff error")
	} else if !strings.Contains(err.Error(), "no diff content found") {
		t.Fatalf("unexpected empty diff error: %v", err)
	}

	invalidResponsePath := filepath.Join(root, "bad-response.json")
	if err := os.WriteFile(invalidResponsePath, []byte("not json"), 0o644); err != nil {
		t.Fatalf("failed writing invalid response fixture: %v", err)
	}
	if _, err := SemanticDiff(root, SemanticDiffOptions{DiffFile: diffPath, AIMode: "from-response", ResponseFile: invalidResponsePath}); err == nil {
		t.Fatalf("expected invalid response json error")
	} else if !strings.Contains(err.Error(), "invalid AI semantic-diff response JSON") {
		t.Fatalf("unexpected invalid response error: %v", err)
	}

	if _, err := SemanticDiff(root, SemanticDiffOptions{DiffFile: diffPath, AIMode: "auto", AIProvider: "unsupported-provider"}); err == nil {
		t.Fatalf("expected runtime-ready provider error")
	} else if !strings.Contains(err.Error(), "is not runtime-ready") {
		t.Fatalf("unexpected runtime-ready provider error: %v", err)
	}
}

func writeSemanticDiffResponseFixture(t *testing.T, root string, name string, payload map[string]any) string {
	t.Helper()
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("failed marshaling semantic diff response: %v", err)
	}
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("failed writing semantic diff response: %v", err)
	}
	return path
}

func runSemanticGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
}
