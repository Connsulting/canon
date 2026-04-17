package canon

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitFromResponseFileAcceptAllIngestsSpecs(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Sample\n"), 0o644); err != nil {
		t.Fatalf("write README failed: %v", err)
	}

	responsePath := filepath.Join(root, "init-response.json")
	writeInitResponse(t, responsePath, map[string]any{
		"model":           "test-model",
		"project_summary": "Sample project.",
		"specs": []map[string]any{
			{
				"id":              "aa11bb2",
				"type":            "feature",
				"title":           "Authentication",
				"domain":          "auth",
				"depends_on":      []string{},
				"touched_domains": []string{"auth"},
				"body":            "Users can sign in.",
				"review_hint":     "Auth behavior.",
			},
			{
				"id":              "cc33dd4",
				"type":            "technical",
				"title":           "API Layer",
				"domain":          "api",
				"depends_on":      []string{"aa11bb2"},
				"touched_domains": []string{"api", "auth"},
				"body":            "API validates auth tokens.",
				"review_hint":     "API behavior.",
			},
		},
	})

	out := &bytes.Buffer{}
	result, err := Init(root, InitOptions{
		AIMode:       "auto",
		AIProvider:   "codex",
		ResponseFile: responsePath,
		Interactive:  false,
		MaxSpecs:     10,
		ContextLimit: 100,
		Out:          out,
	})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if result.GeneratedSpecs != 2 {
		t.Fatalf("expected 2 generated specs, got %d", result.GeneratedSpecs)
	}
	if result.AcceptedSpecs != 2 {
		t.Fatalf("expected 2 accepted specs, got %d", result.AcceptedSpecs)
	}

	specs, err := loadSpecs(root)
	if err != nil {
		t.Fatalf("loadSpecs failed: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("expected 2 canonical specs, got %d", len(specs))
	}
	entries, err := LoadLedger(root)
	if err != nil {
		t.Fatalf("LoadLedger failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 ledger entries, got %d", len(entries))
	}

	if strings.Contains(out.String(), "Starting interactive review") {
		t.Fatalf("did not expect interactive output when Interactive=false")
	}
}

func TestInitInteractiveSkipWritesDraftAndAcceptIngests(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Sample\n"), 0o644); err != nil {
		t.Fatalf("write README failed: %v", err)
	}

	responsePath := filepath.Join(root, "init-response.json")
	writeInitResponse(t, responsePath, map[string]any{
		"model":           "test-model",
		"project_summary": "Sample project.",
		"specs": []map[string]any{
			{
				"id":              "1111111",
				"type":            "feature",
				"title":           "Auth",
				"domain":          "auth",
				"depends_on":      []string{},
				"touched_domains": []string{"auth"},
				"body":            "Auth behavior.",
				"review_hint":     "Auth hint.",
			},
			{
				"id":              "2222222",
				"type":            "feature",
				"title":           "Billing",
				"domain":          "billing",
				"depends_on":      []string{},
				"touched_domains": []string{"billing"},
				"body":            "Billing behavior.",
				"review_hint":     "Billing hint.",
			},
		},
	})

	in := strings.NewReader("s\na\n")
	result, err := Init(root, InitOptions{
		AIMode:       "auto",
		AIProvider:   "codex",
		ResponseFile: responsePath,
		Interactive:  true,
		MaxSpecs:     10,
		ContextLimit: 100,
		In:           in,
		Out:          &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if result.SkippedSpecs != 1 {
		t.Fatalf("expected 1 skipped spec, got %d", result.SkippedSpecs)
	}
	if result.AcceptedSpecs != 1 {
		t.Fatalf("expected 1 accepted spec, got %d", result.AcceptedSpecs)
	}

	draftFiles, err := filepath.Glob(filepath.Join(root, "specs", "init-drafts", "*.md"))
	if err != nil {
		t.Fatalf("glob drafts failed: %v", err)
	}
	if len(draftFiles) != 1 {
		t.Fatalf("expected 1 draft file, got %d", len(draftFiles))
	}
	specs, err := loadSpecs(root)
	if err != nil {
		t.Fatalf("loadSpecs failed: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 ingested spec, got %d", len(specs))
	}
}

func TestInitInteractiveQuitDefersRemainingSpecs(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Sample\n"), 0o644); err != nil {
		t.Fatalf("write README failed: %v", err)
	}

	responsePath := filepath.Join(root, "init-response.json")
	writeInitResponse(t, responsePath, map[string]any{
		"model":           "test-model",
		"project_summary": "Sample project.",
		"specs": []map[string]any{
			{
				"id":              "aaaaaaa",
				"type":            "feature",
				"title":           "Auth",
				"domain":          "auth",
				"depends_on":      []string{},
				"touched_domains": []string{"auth"},
				"body":            "Auth behavior.",
				"review_hint":     "Auth hint.",
			},
			{
				"id":              "bbbbbbb",
				"type":            "feature",
				"title":           "Billing",
				"domain":          "billing",
				"depends_on":      []string{},
				"touched_domains": []string{"billing"},
				"body":            "Billing behavior.",
				"review_hint":     "Billing hint.",
			},
		},
	})

	result, err := Init(root, InitOptions{
		AIMode:       "auto",
		AIProvider:   "codex",
		ResponseFile: responsePath,
		Interactive:  true,
		MaxSpecs:     10,
		ContextLimit: 100,
		In:           strings.NewReader("q\n"),
		Out:          &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if result.DeferredSpecs != 2 {
		t.Fatalf("expected 2 deferred specs, got %d", result.DeferredSpecs)
	}

	draftFiles, err := filepath.Glob(filepath.Join(root, "specs", "init-drafts", "*.md"))
	if err != nil {
		t.Fatalf("glob drafts failed: %v", err)
	}
	if len(draftFiles) != 2 {
		t.Fatalf("expected 2 draft files, got %d", len(draftFiles))
	}
	specs, err := loadSpecs(root)
	if err != nil {
		t.Fatalf("loadSpecs failed: %v", err)
	}
	if len(specs) != 0 {
		t.Fatalf("expected 0 ingested specs, got %d", len(specs))
	}
}

func TestInitFromResponseFileMalformedJSONReturnsError(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Sample\n"), 0o644); err != nil {
		t.Fatalf("write README failed: %v", err)
	}

	responsePath := filepath.Join(root, "bad-response.json")
	if err := os.WriteFile(responsePath, []byte("not-json"), 0o644); err != nil {
		t.Fatalf("write response failed: %v", err)
	}

	_, err := Init(root, InitOptions{
		AIMode:       "auto",
		AIProvider:   "codex",
		ResponseFile: responsePath,
		Interactive:  false,
		MaxSpecs:     10,
		ContextLimit: 100,
		Out:          &bytes.Buffer{},
	})
	if err == nil {
		t.Fatalf("expected malformed response error")
	}
}

func TestInitRejectsUnsupportedCrawlMode(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Sample\n"), 0o644); err != nil {
		t.Fatalf("write README failed: %v", err)
	}

	_, err := Init(root, InitOptions{
		AIMode:       "auto",
		AIProvider:   "codex",
		CrawlMode:    "weird",
		Interactive:  false,
		MaxSpecs:     10,
		ContextLimit: 100,
		Out:          &bytes.Buffer{},
	})
	if err == nil {
		t.Fatalf("expected unsupported crawl mode error")
	}
	if !strings.Contains(err.Error(), "unsupported init crawl mode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInitAgenticCrawlUsesSeedInventoryPrompt(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Sample\n\nImportant behavior.\n"), 0o644); err != nil {
		t.Fatalf("write README failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "cmd"), 0o755); err != nil {
		t.Fatalf("mkdir cmd failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "cmd", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write cmd/main.go failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{\"name\":\"sample\"}\n"), 0o644); err != nil {
		t.Fatalf("write package.json failed: %v", err)
	}

	binDir := t.TempDir()
	promptPath := filepath.Join(root, "captured-prompt.txt")
	writeFakeInitCodex(t, filepath.Join(binDir, "codex"))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PROMPT_CAPTURE", promptPath)

	out := &bytes.Buffer{}
	result, err := Init(root, InitOptions{
		AIMode:       "auto",
		AIProvider:   "codex",
		CrawlMode:    "agentic",
		Interactive:  false,
		MaxSpecs:     10,
		ContextLimit: 100,
		Out:          out,
	})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if result.AcceptedSpecs != 1 {
		t.Fatalf("expected 1 accepted spec, got %d", result.AcceptedSpecs)
	}

	promptBytes, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("read prompt failed: %v", err)
	}
	prompt := string(promptBytes)
	if !strings.Contains(prompt, "Crawl mode: agentic") {
		t.Fatalf("expected agentic crawl marker in prompt")
	}
	if !strings.Contains(prompt, "inspect the repository directly using local tools as needed") {
		t.Fatalf("expected direct inspection instructions in prompt")
	}
	if !strings.Contains(prompt, "## Seed Inventory") {
		t.Fatalf("expected seed inventory section in prompt")
	}
	if strings.Contains(prompt, "## File: ") {
		t.Fatalf("did not expect bundled file-content sections in agentic prompt")
	}

	output := out.String()
	if !strings.Contains(output, "included in seed inventory") {
		t.Fatalf("expected seed inventory output, got:\n%s", output)
	}
	if !strings.Contains(output, "Agentic mode may inspect additional repository files directly during AI decomposition.") {
		t.Fatalf("expected agentic crawl messaging, got:\n%s", output)
	}
}

func TestInitMultipassCrawlUsesManagedAreaPrompts(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Sample\n\nImportant behavior.\n"), 0o644); err != nil {
		t.Fatalf("write README failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "app"), 0o755); err != nil {
		t.Fatalf("mkdir app failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "app", "main.go"), []byte("package app\n"), 0o644); err != nil {
		t.Fatalf("write app/main.go failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "architecture.md"), []byte("# Architecture\n"), 0o644); err != nil {
		t.Fatalf("write docs/architecture.md failed: %v", err)
	}

	binDir := t.TempDir()
	captureDir := filepath.Join(root, "prompts")
	if err := os.MkdirAll(captureDir, 0o755); err != nil {
		t.Fatalf("mkdir capture dir failed: %v", err)
	}
	writeFakeInitMultipassCodex(t, filepath.Join(binDir, "codex"))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PROMPT_CAPTURE_DIR", captureDir)

	out := &bytes.Buffer{}
	result, err := Init(root, InitOptions{
		AIMode:       "auto",
		AIProvider:   "codex",
		CrawlMode:    "multipass",
		Interactive:  false,
		MaxSpecs:     10,
		ContextLimit: 100,
		Out:          out,
	})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if result.AcceptedSpecs != 1 {
		t.Fatalf("expected 1 accepted spec, got %d", result.AcceptedSpecs)
	}

	prompts, err := filepath.Glob(filepath.Join(captureDir, "prompt-*.txt"))
	if err != nil {
		t.Fatalf("glob prompts failed: %v", err)
	}
	if len(prompts) < 2 {
		t.Fatalf("expected multiple managed provider prompts, got %d", len(prompts))
	}

	combined := strings.Builder{}
	for _, promptPath := range prompts {
		b, err := os.ReadFile(promptPath)
		if err != nil {
			t.Fatalf("read prompt failed: %v", err)
		}
		combined.Write(b)
		combined.WriteString("\n--- prompt boundary ---\n")
	}
	promptText := combined.String()
	if !strings.Contains(promptText, "# Canon Init Area Analysis") {
		t.Fatalf("expected per-area analysis prompt, got:\n%s", promptText)
	}
	if !strings.Contains(promptText, "Crawl mode: multipass") {
		t.Fatalf("expected multipass prompt marker, got:\n%s", promptText)
	}
	if !strings.Contains(promptText, "## Area Analyses") {
		t.Fatalf("expected synthesis prompt to include area analyses, got:\n%s", promptText)
	}

	output := out.String()
	if !strings.Contains(output, "Planning managed crawl") {
		t.Fatalf("expected multipass planning output, got:\n%s", output)
	}
	if !strings.Contains(output, "Analyzing area") {
		t.Fatalf("expected multipass area output, got:\n%s", output)
	}
	if !strings.Contains(output, "Synthesizing specs from area analyses") {
		t.Fatalf("expected multipass synthesis output, got:\n%s", output)
	}
}

func TestInitAutoFallbackUsesReadmeWhenProviderUnavailable(t *testing.T) {
	root := t.TempDir()
	readme := "# Example Project\n\nCurrent behavior documentation."
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte(readme), 0o644); err != nil {
		t.Fatalf("write README failed: %v", err)
	}

	result, err := Init(root, InitOptions{
		AIMode:       "auto",
		AIProvider:   "unsupported-provider",
		Interactive:  false,
		MaxSpecs:     10,
		ContextLimit: 100,
		Out:          &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if !result.FallbackUsed {
		t.Fatalf("expected fallback to be used")
	}
	if result.AcceptedSpecs != 1 {
		t.Fatalf("expected fallback to ingest 1 spec, got %d", result.AcceptedSpecs)
	}

	specs, err := loadSpecs(root)
	if err != nil {
		t.Fatalf("loadSpecs failed: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 fallback spec, got %d", len(specs))
	}
	if !strings.Contains(specs[0].Body, "Current behavior documentation.") {
		t.Fatalf("expected README body in fallback spec, got:\n%s", specs[0].Body)
	}
}

func TestBuildInitCrawlAreasGroupsRootAndTopLevelDeterministically(t *testing.T) {
	files := []string{
		"scripts/setup.sh",
		"app/main.go",
		"README.md",
		"docs/architecture.md",
		"package.json",
		"e2e/login.spec.ts",
	}

	areas := buildInitCrawlAreas(files, 0)
	names := make([]string, 0, len(areas))
	for _, area := range areas {
		names = append(names, area.Name)
	}
	expected := []string{"root", "docs", "app", "e2e", "scripts"}
	if strings.Join(names, ",") != strings.Join(expected, ",") {
		t.Fatalf("expected areas %v, got %v", expected, names)
	}

	capped := buildInitCrawlAreas(files, 3)
	if len(capped) != 3 {
		t.Fatalf("expected 3 capped areas, got %d", len(capped))
	}
	if capped[0].Name != "root" || capped[1].Name != "docs" || capped[2].Name != "other" {
		t.Fatalf("expected capped areas [root docs other], got [%s %s %s]", capped[0].Name, capped[1].Name, capped[2].Name)
	}
}

func TestBuildInitAreaEvidencePackSkipsBinaryAndOversizedContent(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{\"name\":\"sample\"}\n"), 0o644); err != nil {
		t.Fatalf("write package.json failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main.go failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "image.png"), []byte{0, 1, 2, 3}, 0o644); err != nil {
		t.Fatalf("write image.png failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "large.txt"), []byte(strings.Repeat("x", initDefaultMaxFileBytes+1)), 0o644); err != nil {
		t.Fatalf("write large.txt failed: %v", err)
	}

	area := initCrawlArea{
		Name:  "root",
		Files: []string{"image.png", "large.txt", "main.go", "package.json"},
	}
	pack, selected, err := buildInitAreaEvidencePack(root, area, 100*1024)
	if err != nil {
		t.Fatalf("buildInitAreaEvidencePack failed: %v", err)
	}
	if !strings.Contains(pack, "### package.json") {
		t.Fatalf("expected package.json excerpt, got:\n%s", pack)
	}
	if !strings.Contains(pack, "### main.go") {
		t.Fatalf("expected main.go excerpt, got:\n%s", pack)
	}
	if strings.Contains(pack, "### image.png") {
		t.Fatalf("did not expect binary image excerpt, got:\n%s", pack)
	}
	if strings.Contains(pack, "### large.txt") {
		t.Fatalf("did not expect oversized file excerpt, got:\n%s", pack)
	}
	if strings.Join(selected, ",") != "package.json,main.go" {
		t.Fatalf("expected selected evidence [package.json main.go], got %v", selected)
	}
}

func TestBuildInitDraftsRegeneratesCollidingIDs(t *testing.T) {
	existing := []Spec{{ID: "deadbee", Domain: "auth", Type: "feature", Title: "Existing"}}
	response := aiInitResponse{
		Model:          "test-model",
		ProjectSummary: "summary",
		Specs: []aiInitSpec{
			{
				ID:             "deadbee",
				Type:           "feature",
				Title:          "Auth",
				Domain:         "auth",
				DependsOn:      []string{},
				TouchedDomains: []string{"auth"},
				Body:           "Auth body.",
				ReviewHint:     "hint",
			},
			{
				ID:             "deadbee",
				Type:           "technical",
				Title:          "API",
				Domain:         "api",
				DependsOn:      []string{},
				TouchedDomains: []string{"api"},
				Body:           "API body.",
				ReviewHint:     "hint",
			},
		},
	}

	drafts := buildInitDrafts(response, existing, 10)
	if len(drafts) != 2 {
		t.Fatalf("expected 2 drafts, got %d", len(drafts))
	}
	if drafts[0].Spec.ID == "deadbee" || drafts[1].Spec.ID == "deadbee" {
		t.Fatalf("expected colliding ids to be regenerated, got %q and %q", drafts[0].Spec.ID, drafts[1].Spec.ID)
	}
	if drafts[0].Spec.ID == drafts[1].Spec.ID {
		t.Fatalf("expected unique regenerated ids, got duplicate %q", drafts[0].Spec.ID)
	}
}

func TestBuildInitDraftsAppliesDependencyOrderBeforeMaxSpecs(t *testing.T) {
	response := aiInitResponse{
		Model:          "test-model",
		ProjectSummary: "summary",
		Specs: []aiInitSpec{
			{
				ID:             "bbbbbbb",
				Type:           "feature",
				Title:          "Dependent",
				Domain:         "api",
				DependsOn:      []string{"aaaaaaa"},
				TouchedDomains: []string{"api"},
				Body:           "Depends on base.",
				ReviewHint:     "dependent",
			},
			{
				ID:             "aaaaaaa",
				Type:           "feature",
				Title:          "Base",
				Domain:         "auth",
				DependsOn:      []string{},
				TouchedDomains: []string{"auth"},
				Body:           "Base behavior.",
				ReviewHint:     "base",
			},
			{
				ID:             "ccccccc",
				Type:           "technical",
				Title:          "Other",
				Domain:         "ops",
				DependsOn:      []string{},
				TouchedDomains: []string{"ops"},
				Body:           "Other behavior.",
				ReviewHint:     "other",
			},
		},
	}

	drafts := buildInitDrafts(response, nil, 2)
	if len(drafts) != 2 {
		t.Fatalf("expected truncation to 2 specs, got %d", len(drafts))
	}
	if drafts[0].Spec.ID != "aaaaaaa" || drafts[1].Spec.ID != "bbbbbbb" {
		t.Fatalf("expected dependency-ordered truncation [aaaaaaa, bbbbbbb], got [%s, %s]", drafts[0].Spec.ID, drafts[1].Spec.ID)
	}
}

func TestScanProjectForInitRespectsGitignore(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignore.md\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Sample\n"), 0o644); err != nil {
		t.Fatalf("write README failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "ignore.md"), []byte("skip me\n"), 0o644); err != nil {
		t.Fatalf("write ignored file failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "cmd"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "cmd", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write cmd/main.go failed: %v", err)
	}

	scan, err := scanProjectForInit(root, initScanOptions{
		ContextLimitBytes: 100 * 1024,
		MaxFileBytes:      initDefaultMaxFileBytes,
	})
	if err != nil {
		t.Fatalf("scanProjectForInit failed: %v", err)
	}
	if scan.IncludedFiles == 0 {
		t.Fatalf("expected included files in scan")
	}
	if strings.Contains(scan.Context, "## File: ignore.md") {
		t.Fatalf("expected ignored file to be excluded from context")
	}
	if !strings.Contains(scan.Context, "## File: README.md") {
		t.Fatalf("expected README to be included in context")
	}
}

func TestScanProjectForInitUsesGitIgnoreRulesInGitRepo(t *testing.T) {
	root := t.TempDir()
	mustInitGitRepo(t, root)
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".vscode/*\n!.vscode/extensions.json\nfoo/\n!foo/keep.log\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Sample\n"), 0o644); err != nil {
		t.Fatalf("write README failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".vscode"), 0o755); err != nil {
		t.Fatalf("mkdir .vscode failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".vscode", "settings.json"), []byte("{\"theme\":\"dark\"}\n"), 0o644); err != nil {
		t.Fatalf("write settings.json failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".vscode", "extensions.json"), []byte("{\"recommendations\":[]}\n"), 0o644); err != nil {
		t.Fatalf("write extensions.json failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "foo"), 0o755); err != nil {
		t.Fatalf("mkdir foo failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "foo", "a.txt"), []byte("ignored\n"), 0o644); err != nil {
		t.Fatalf("write foo/a.txt failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "foo", "keep.log"), []byte("still ignored\n"), 0o644); err != nil {
		t.Fatalf("write foo/keep.log failed: %v", err)
	}

	scan, err := scanProjectForInit(root, initScanOptions{
		ContextLimitBytes: 100 * 1024,
		MaxFileBytes:      initDefaultMaxFileBytes,
	})
	if err != nil {
		t.Fatalf("scanProjectForInit failed: %v", err)
	}
	if strings.Contains(scan.Context, "## File: .vscode/settings.json") {
		t.Fatalf("expected ignored .vscode/settings.json to be excluded from context")
	}
	if !strings.Contains(scan.Context, "## File: .vscode/extensions.json") {
		t.Fatalf("expected negated .vscode/extensions.json to be included in context")
	}
	if strings.Contains(scan.Context, "## File: foo/keep.log") {
		t.Fatalf("expected file under ignored parent directory to stay excluded")
	}
	if strings.Contains(scan.Tree, "settings.json") {
		t.Fatalf("expected ignored .vscode/settings.json to be excluded from tree")
	}
	if !strings.Contains(scan.Tree, "extensions.json") {
		t.Fatalf("expected negated .vscode/extensions.json to appear in tree")
	}
	if strings.Contains(scan.Tree, "foo") {
		t.Fatalf("expected ignored foo directory to be excluded from tree")
	}
}

func TestScanProjectForInitUsesNestedGitignoreInGitRepo(t *testing.T) {
	root := t.TempDir()
	mustInitGitRepo(t, root)
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Sample\n"), 0o644); err != nil {
		t.Fatalf("write README failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir nested failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", ".gitignore"), []byte("secret.txt\n"), 0o644); err != nil {
		t.Fatalf("write nested .gitignore failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "secret.txt"), []byte("hidden\n"), 0o644); err != nil {
		t.Fatalf("write nested/secret.txt failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "visible.txt"), []byte("shown\n"), 0o644); err != nil {
		t.Fatalf("write nested/visible.txt failed: %v", err)
	}

	scan, err := scanProjectForInit(root, initScanOptions{
		ContextLimitBytes: 100 * 1024,
		MaxFileBytes:      initDefaultMaxFileBytes,
	})
	if err != nil {
		t.Fatalf("scanProjectForInit failed: %v", err)
	}
	if strings.Contains(scan.Context, "## File: nested/secret.txt") {
		t.Fatalf("expected nested gitignored file to be excluded from context")
	}
	if !strings.Contains(scan.Context, "## File: nested/visible.txt") {
		t.Fatalf("expected nested visible file to be included in context")
	}
	if strings.Contains(scan.Tree, "secret.txt") {
		t.Fatalf("expected nested gitignored file to be excluded from tree")
	}
	if !strings.Contains(scan.Tree, "visible.txt") {
		t.Fatalf("expected nested visible file to be included in tree")
	}
}

func TestScanProjectForInitIncludeOverridesGitignoreInGitRepo(t *testing.T) {
	root := t.TempDir()
	mustInitGitRepo(t, root)
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Sample\n"), 0o644); err != nil {
		t.Fatalf("write README failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.log"), []byte("retain me\n"), 0o644); err != nil {
		t.Fatalf("write app.log failed: %v", err)
	}

	scan, err := scanProjectForInit(root, initScanOptions{
		ContextLimitBytes: 100 * 1024,
		MaxFileBytes:      initDefaultMaxFileBytes,
		Include:           []string{"app.log"},
	})
	if err != nil {
		t.Fatalf("scanProjectForInit failed: %v", err)
	}
	if !strings.Contains(scan.Context, "## File: app.log") {
		t.Fatalf("expected --include to override gitignore for app.log")
	}
	if !strings.Contains(scan.Tree, "app.log") {
		t.Fatalf("expected --include to restore app.log in the tree")
	}
}

func TestScanProjectForInitSkipsCacheAndVenvArtifacts(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Sample\n"), 0o644); err != nil {
		t.Fatalf("write README failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "__pycache__"), 0o755); err != nil {
		t.Fatalf("mkdir __pycache__ failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "__pycache__", "x.pyc"), []byte{0, 1, 2, 3}, 0o644); err != nil {
		t.Fatalf("write pyc failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".venv", "lib"), 0o755); err != nil {
		t.Fatalf("mkdir .venv failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".venv", "lib", "x.py"), []byte("print('x')\n"), 0o644); err != nil {
		t.Fatalf("write venv file failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatalf("mkdir src failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatalf("write main.py failed: %v", err)
	}

	scan, err := scanProjectForInit(root, initScanOptions{
		ContextLimitBytes: 100 * 1024,
		MaxFileBytes:      initDefaultMaxFileBytes,
	})
	if err != nil {
		t.Fatalf("scanProjectForInit failed: %v", err)
	}
	if strings.Contains(scan.Context, "__pycache__") {
		t.Fatalf("expected __pycache__ artifacts to be excluded from context")
	}
	if strings.Contains(scan.Context, ".venv") {
		t.Fatalf("expected .venv artifacts to be excluded from context")
	}
	if !strings.Contains(scan.Context, "## File: src/main.py") {
		t.Fatalf("expected regular source file to remain in context")
	}
}

func TestScanProjectForInitSkipsSymlinkEntries(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Sample\n"), 0o644); err != nil {
		t.Fatalf("write README failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatalf("mkdir src failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write src/main.go failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "real-dir"), 0o755); err != nil {
		t.Fatalf("mkdir real-dir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "real-dir", "note.txt"), []byte("linked\n"), 0o644); err != nil {
		t.Fatalf("write real-dir/note.txt failed: %v", err)
	}

	mustSymlinkOrSkip(t, "real-dir", filepath.Join(root, "linked-dir"))
	mustSymlinkOrSkip(t, "README.md", filepath.Join(root, "readme-link.md"))
	mustSymlinkOrSkip(t, "missing-target.txt", filepath.Join(root, "broken-link.md"))

	scan, err := scanProjectForInit(root, initScanOptions{
		ContextLimitBytes: 100 * 1024,
		MaxFileBytes:      initDefaultMaxFileBytes,
	})
	if err != nil {
		t.Fatalf("scanProjectForInit failed: %v", err)
	}
	if !strings.Contains(scan.Context, "## File: README.md") {
		t.Fatalf("expected README to be included in context")
	}
	if strings.Contains(scan.Context, "## File: linked-dir") {
		t.Fatalf("expected symlinked directory to be excluded from context")
	}
	if strings.Contains(scan.Context, "## File: readme-link.md") {
		t.Fatalf("expected symlinked file to be excluded from context")
	}
	if strings.Contains(scan.Context, "## File: broken-link.md") {
		t.Fatalf("expected broken symlink to be excluded from context")
	}
}

func mustSymlinkOrSkip(t *testing.T, target string, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if os.IsPermission(err) || strings.Contains(strings.ToLower(err.Error()), "privilege") {
			t.Skipf("symlinks not supported in this environment: %v", err)
		}
		t.Fatalf("create symlink %s -> %s failed: %v", link, target, err)
	}
}

func mustInitGitRepo(t *testing.T, root string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not available: %v", err)
	}
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, strings.TrimSpace(string(output)))
	}
}

func writeInitResponse(t *testing.T, path string, payload map[string]any) {
	t.Helper()
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("marshal init response failed: %v", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write init response failed: %v", err)
	}
}

func writeFakeInitCodex(t *testing.T, path string) {
	t.Helper()
	script := `#!/bin/sh
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    out="$2"
    shift 2
    continue
  fi
  shift
done

if [ -z "$out" ]; then
  echo "missing output path" >&2
  exit 2
fi

cat > "$PROMPT_CAPTURE"

cat <<'JSON' > "$out"
{
  "model": "codex-headless",
  "project_summary": "sample",
  "specs": [
    {
      "id": "abc1234",
      "type": "technical",
      "title": "Project Overview",
      "domain": "general",
      "depends_on": [],
      "touched_domains": ["general"],
      "body": "The project exposes current behavior.",
      "review_hint": "overview"
    }
  ]
}
JSON
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex failed: %v", err)
	}
}

func writeFakeInitMultipassCodex(t *testing.T, path string) {
	t.Helper()
	script := `#!/bin/sh
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    out="$2"
    shift 2
    continue
  fi
  shift
done

if [ -z "$out" ]; then
  echo "missing output path" >&2
  exit 2
fi

prompt="$(cat)"
if [ -n "$PROMPT_CAPTURE_DIR" ]; then
  n=$(ls "$PROMPT_CAPTURE_DIR"/prompt-*.txt 2>/dev/null | wc -l)
  n=$((n + 1))
  printf '%s' "$prompt" > "$PROMPT_CAPTURE_DIR/prompt-$n.txt"
elif [ -n "$PROMPT_CAPTURE" ]; then
  printf '%s' "$prompt" > "$PROMPT_CAPTURE"
fi

if printf '%s' "$prompt" | grep -q "Canon Init Area Analysis"; then
  cat <<'JSON' > "$out"
{
  "model": "codex-headless",
  "area": "sample-area",
  "summary": "Area contains current behavior.",
  "components": ["component"],
  "user_facing_features": ["feature"],
  "technical_behaviors": ["technical behavior"],
  "runtime_wiring": ["runtime wiring"],
  "risks_or_gaps": [],
  "evidence_files": ["README.md"],
  "support_only": false,
  "omission_reason": ""
}
JSON
  exit 0
fi

cat <<'JSON' > "$out"
{
  "model": "codex-headless",
  "project_summary": "sample",
  "specs": [
    {
      "id": "def5678",
      "type": "technical",
      "title": "Managed Crawl Overview",
      "domain": "general",
      "depends_on": [],
      "touched_domains": ["general"],
      "body": "The project exposes current behavior discovered through managed crawl summaries.",
      "review_hint": "overview"
    }
  ]
}
JSON
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake multipass codex failed: %v", err)
	}
}
