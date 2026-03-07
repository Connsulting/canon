package canon

import (
	"bytes"
	"encoding/json"
	"os"
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

func TestScanProjectForInitSkipsSensitiveFilesByDefault(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"README.md":                  "# Sample\n",
		".env":                       "OPENAI_API_KEY=TEST_KEY_ONLY\n",
		".env.local":                 "DB_PASSWORD=example_local\n",
		".env.example":               "DB_PASSWORD=example\n",
		"keys/private.key":           "private-key\n",
		"certs/server.pem":           "pem\n",
		"credentials.dev":            "token=abc\n",
		"secrets.prod":               "token=prod\n",
		"exports/users_export.csv":   "email,name\nredacted_email,SampleUser\n",
		"db/dumps/customers.sql":     "insert into users values ('redacted_value');\n",
		"backups/users_dump.xlsx":    "binary-ish\n",
		"db/migrations/001_init.sql": "create table users(id text);\n",
	}
	for rel, content := range files {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir for %s failed: %v", rel, err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s failed: %v", rel, err)
		}
	}

	scan, err := scanProjectForInit(root, initScanOptions{
		ContextLimitBytes: 100 * 1024,
		MaxFileBytes:      initDefaultMaxFileBytes,
	})
	if err != nil {
		t.Fatalf("scanProjectForInit failed: %v", err)
	}

	for _, mustInclude := range []string{"README.md", ".env.example", "db/migrations/001_init.sql"} {
		if !strings.Contains(scan.Context, "## File: "+mustInclude+"\n") {
			t.Fatalf("expected %s to be included in context", mustInclude)
		}
	}
	for _, mustExclude := range []string{
		".env",
		".env.local",
		"keys/private.key",
		"certs/server.pem",
		"credentials.dev",
		"secrets.prod",
		"exports/users_export.csv",
		"db/dumps/customers.sql",
		"backups/users_dump.xlsx",
	} {
		if strings.Contains(scan.Context, "## File: "+mustExclude+"\n") {
			t.Fatalf("expected %s to be excluded from context", mustExclude)
		}
	}
}

func TestScanProjectForInitAllowsSensitiveFilesWhenExplicitlyIncluded(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Sample\n"), 0o644); err != nil {
		t.Fatalf("write README failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("OPENAI_API_KEY=EXPLICIT_TEST_VALUE\n"), 0o644); err != nil {
		t.Fatalf("write .env failed: %v", err)
	}

	scan, err := scanProjectForInit(root, initScanOptions{
		ContextLimitBytes: 100 * 1024,
		MaxFileBytes:      initDefaultMaxFileBytes,
		Include:           []string{".env"},
	})
	if err != nil {
		t.Fatalf("scanProjectForInit failed: %v", err)
	}
	if !strings.Contains(scan.Context, "## File: .env\n") {
		t.Fatalf("expected explicit include to allow .env in context")
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
