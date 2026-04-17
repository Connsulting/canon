package canon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitLayoutCreatesCanonSourceFolders(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	expected := []string{
		".canon/specs",
		".canon/ledger",
		".canon/sources",
		".canon/conflict-reports",
		".canon/archive/specs",
		".canon/archive/sources",
		"state/interactions",
	}

	for _, rel := range expected {
		if !isDir(filepath.Join(root, rel)) {
			t.Fatalf("missing directory: %s", rel)
		}
	}
}

func TestCheckLayoutReportsHealthy(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	report := CheckLayout(root)
	if report.Health != LayoutHealthy {
		t.Fatalf("expected healthy layout, got %s: %s", report.Health, report.ErrorMessage())
	}
	if len(report.MissingSupport) != 0 || len(report.Problems) != 0 {
		t.Fatalf("expected no layout problems, got missing=%v problems=%v", report.MissingSupport, report.Problems)
	}
}

func TestCheckLayoutReportsRepairableMissingSupport(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}
	if err := os.Remove(filepath.Join(root, ".canon", "conflict-reports")); err != nil {
		t.Fatalf("failed removing support dir: %v", err)
	}

	report := CheckLayout(root)
	if report.Health != LayoutRepairable {
		t.Fatalf("expected repairable layout, got %s: %s", report.Health, report.ErrorMessage())
	}
	if len(report.MissingSupport) != 1 || report.MissingSupport[0] != ".canon/conflict-reports" {
		t.Fatalf("unexpected missing support dirs: %v", report.MissingSupport)
	}
}

func TestCheckLayoutReportsInvalidMissingCore(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".canon", "sources"), 0o755); err != nil {
		t.Fatalf("failed creating core source dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".canon", "ledger"), 0o755); err != nil {
		t.Fatalf("failed creating core ledger dir: %v", err)
	}

	report := CheckLayout(root)
	if report.Health != LayoutInvalid {
		t.Fatalf("expected invalid layout, got %s", report.Health)
	}
	if len(report.Problems) == 0 || report.Problems[0].Kind != LayoutProblemMissingCore {
		t.Fatalf("expected missing core problem, got %v", report.Problems)
	}
}

func TestCheckLayoutReportsInvalidNonDirectory(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}
	if err := os.Remove(filepath.Join(root, ".canon", "specs")); err != nil {
		t.Fatalf("failed removing specs dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".canon", "specs"), []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("failed writing specs file: %v", err)
	}

	report := CheckLayout(root)
	if report.Health != LayoutInvalid {
		t.Fatalf("expected invalid layout, got %s", report.Health)
	}
	if len(report.Problems) != 1 || report.Problems[0].Kind != LayoutProblemNotDirectory {
		t.Fatalf("expected non-directory problem, got %v", report.Problems)
	}
}

func TestIngestFreeformAndLogNewestFirst(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	first, err := Ingest(root, IngestInput{
		Text:   "Users can sign in with email and password.",
		Title:  "Authentication",
		Domain: "auth",
	})
	if err != nil {
		t.Fatalf("Ingest first failed: %v", err)
	}

	second, err := Ingest(root, IngestInput{
		Text:   "Each user gets per minute API limits.",
		Title:  "Rate Limits",
		Domain: "api",
	})
	if err != nil {
		t.Fatalf("Ingest second failed: %v", err)
	}

	entries, err := LoadLedger(root)
	if err != nil {
		t.Fatalf("LoadLedger failed: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 ledger entries, got %d", len(entries))
	}

	if entries[0].SpecID != second.SpecID {
		t.Fatalf("expected newest first in log order, got first=%s second=%s", entries[0].SpecID, second.SpecID)
	}
	if entries[0].Title == "" {
		t.Fatalf("expected ledger entry to include title")
	}

	if entries[1].SpecID != first.SpecID {
		t.Fatalf("expected older second in log order, got %s", entries[1].SpecID)
	}
}

func TestIngestInheritsParentsIntoDependsOn(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	first, err := Ingest(root, IngestInput{
		Text:   "Base requirement",
		Title:  "Base",
		Domain: "core",
	})
	if err != nil {
		t.Fatalf("first ingest failed: %v", err)
	}

	second, err := Ingest(root, IngestInput{
		Text:   "Next requirement",
		Title:  "Next",
		Domain: "core",
	})
	if err != nil {
		t.Fatalf("second ingest failed: %v", err)
	}

	secondSpec, err := loadSpecByID(root, second.SpecID)
	if err != nil {
		t.Fatalf("loadSpecByID failed: %v", err)
	}
	if len(secondSpec.DependsOn) != 1 || secondSpec.DependsOn[0] != first.SpecID {
		t.Fatalf("expected second depends_on to include parent %s, got %v", first.SpecID, secondSpec.DependsOn)
	}
}

func TestIngestGeneratedSpecIDUsesCanonPrefix(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	result, err := Ingest(root, IngestInput{
		Text:   "Generated id behavior.",
		Title:  "ID Policy",
		Domain: "canon-cli",
	})
	if err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}
	if len(result.SpecID) != 7 {
		t.Fatalf("expected generated spec id length 7, got %d (%s)", len(result.SpecID), result.SpecID)
	}
	for _, ch := range result.SpecID {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			t.Fatalf("expected hex generated spec id, got %s", result.SpecID)
		}
	}
}

func TestRenderDeterministic(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	specs := []IngestInput{
		{
			Text: `---
id: spec-101
type: feature
title: Authentication
domain: auth
status: active
created: 2026-02-19T10:00:00Z
depends_on: []
touched_domains: [auth]
---
## Behaviors
Users can sign in with email and password.`,
		},
		{
			Text: `---
id: spec-102
type: feature
title: API Rate Limits
domain: api
status: active
created: 2026-02-19T10:10:00Z
depends_on: [spec-101]
touched_domains: [api, auth]
---
## Behaviors
Each authenticated user has per minute request limits by tier.`,
		},
		{
			Text: `---
id: spec-103
type: feature
title: Billing Grace Period
domain: billing
status: active
created: 2026-02-19T10:20:00Z
depends_on: [spec-102]
touched_domains: [billing, api]
---
## Behaviors
After payment failure the account enters a three day grace period.`,
		},
		{
			Text: `---
id: spec-104
type: technical
title: API Cache Policy
domain: api
status: active
created: 2026-02-19T10:30:00Z
depends_on: [spec-102]
touched_domains: [api]
---
## Behaviors
Cache successful GET responses for thirty seconds.`,
		},
	}

	for _, in := range specs {
		if _, err := Ingest(root, in); err != nil {
			t.Fatalf("Ingest failed: %v", err)
		}
	}

	first, err := Render(root, RenderOptions{Write: true})
	if err != nil {
		t.Fatalf("first Render failed: %v", err)
	}

	second, err := Render(root, RenderOptions{Write: true})
	if err != nil {
		t.Fatalf("second Render failed: %v", err)
	}

	if first.FilesWritten < 5 {
		t.Fatalf("expected initial write to create files, got %d", first.FilesWritten)
	}

	if second.FilesWritten != 0 {
		t.Fatalf("expected deterministic no-op second render, got %d", second.FilesWritten)
	}

	apiState := readFile(t, filepath.Join(root, "state/api.md"))
	if !strings.Contains(apiState, "Contributing specs: spec-102, spec-103, spec-104") {
		t.Fatalf("api state missing contributors header")
	}

	manifest := readFile(t, filepath.Join(root, "state/manifest.yaml"))
	if !strings.Contains(manifest, "total_specs: 4") {
		t.Fatalf("manifest missing total spec count")
	}
}

func TestShowSpecReturnsCanonicalContent(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	result, err := Ingest(root, IngestInput{
		Text:   "Voice dictated behavior for a new workflow.",
		Title:  "Voice Capture",
		Domain: "product",
	})
	if err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}

	relPath, text, err := ShowSpec(root, result.SpecID)
	if err != nil {
		t.Fatalf("ShowSpec failed: %v", err)
	}

	if !strings.HasPrefix(relPath, ".canon/specs/") {
		t.Fatalf("unexpected canonical path: %s", relPath)
	}
	if !strings.Contains(text, "id: "+result.SpecID) {
		t.Fatalf("show output missing spec id")
	}
	if !strings.Contains(text, "domain: product") {
		t.Fatalf("show output missing domain")
	}
}

func TestIngestFromResponseBlocksConflictAndWritesReport(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	base, err := Ingest(root, IngestInput{
		Text:   "Existing auth behavior. Users must retain access to audit history.",
		Title:  "Audit Access",
		Domain: "auth",
	})
	if err != nil {
		t.Fatalf("base ingest failed: %v", err)
	}

	responsePath := filepath.Join(root, "test-response.json")
	response := `{
  "model": "claude-headless",
  "canonical_spec": {
    "id": "spec-200",
    "type": "feature",
    "title": "Audit Restrictions",
    "domain": "auth",
    "status": "active",
    "created": "2026-02-19T16:00:00Z",
    "depends_on": [],
    "touched_domains": ["auth"],
    "body": "Users must not retain access to audit history."
  },
  "conflict_check": {
    "has_conflicts": true,
    "summary": "Conflicts with existing immutable audit access behavior.",
    "conflicts": [
      {"existing_spec_id": "` + base.SpecID + `", "reason": "Direct contradiction on audit access retention."}
    ]
  }
}`
	if err := os.WriteFile(responsePath, []byte(response), 0o644); err != nil {
		t.Fatalf("failed writing response file: %v", err)
	}

	_, err = Ingest(root, IngestInput{
		Text:         "AI candidate spec input",
		ConflictMode: "from-response",
		ResponseFile: responsePath,
		AIProvider:   "claude",
	})
	if err == nil {
		t.Fatalf("expected merge conflict error from AI response")
	}
	if !strings.Contains(err.Error(), "merge conflict detected") {
		t.Fatalf("unexpected error: %v", err)
	}

	reportFiles, err := filepath.Glob(filepath.Join(root, ".canon", "conflict-reports", "*.yaml"))
	if err != nil {
		t.Fatalf("glob conflict reports failed: %v", err)
	}
	if len(reportFiles) == 0 {
		t.Fatalf("expected conflict report to be written")
	}
}

func TestIngestPreservesSourceWhenAIBodyIsCompressed(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	raw := strings.Repeat("This requirement must be preserved with detail and context. ", 40)
	responsePath := filepath.Join(root, "compress-response.json")
	response := `{
  "model": "codex-headless",
  "canonical_spec": {
    "id": "spec-300",
    "type": "feature",
    "title": "Compressed Spec",
    "domain": "api",
    "status": "active",
    "created": "2026-02-19T17:00:00Z",
    "depends_on": [],
    "touched_domains": ["api"],
    "body": "Too short."
  },
  "conflict_check": {
    "has_conflicts": false,
    "summary": "No conflict",
    "conflicts": []
  }
}`
	if err := os.WriteFile(responsePath, []byte(response), 0o644); err != nil {
		t.Fatalf("failed writing response file: %v", err)
	}

	result, err := Ingest(root, IngestInput{
		IngestKind:   "file",
		Text:         raw,
		ConflictMode: "from-response",
		ResponseFile: responsePath,
		AIProvider:   "codex",
	})
	if err != nil {
		t.Fatalf("expected ingest success with source preservation: %v", err)
	}
	specText := readFile(t, filepath.Join(root, ".canon", "specs", "spec-300.spec.md"))
	if !strings.Contains(specText, "This requirement must be preserved with detail and context.") {
		t.Fatalf("expected canonical spec to preserve source body")
	}
	if !strings.Contains(specText, "## AI Enhancements") {
		t.Fatalf("expected AI enhancement section when AI adds extra lines")
	}
	if result.SpecPath == "" {
		t.Fatalf("expected spec path in ingest result")
	}
}

func TestIngestFromResponsePreservesSourceCreatedTimestamp(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	responsePath := filepath.Join(root, "created-response.json")
	response := `{
  "model": "codex-headless",
  "canonical_spec": {
    "id": "spec-400",
    "type": "technical",
    "title": "Created Timestamp Check",
    "domain": "canon-cli",
    "status": "active",
    "created": "2026-02-19T00:00:00Z",
    "depends_on": [],
    "touched_domains": ["canon-cli"],
    "body": "Body from AI."
  },
  "conflict_check": {
    "has_conflicts": false,
    "summary": "No conflict",
    "conflicts": []
  }
}`
	if err := os.WriteFile(responsePath, []byte(response), 0o644); err != nil {
		t.Fatalf("failed writing response file: %v", err)
	}

	source := `---
id: spec-400
type: technical
title: "Created Timestamp Check"
domain: canon-cli
status: active
created: 2026-02-19T17:30:45Z
depends_on: []
touched_domains: [canon-cli]
---
## Notes
Keep source created timestamp exact.`

	if _, err := Ingest(root, IngestInput{
		IngestKind:   "file",
		Text:         source,
		ConflictMode: "from-response",
		ResponseFile: responsePath,
		AIProvider:   "codex",
	}); err != nil {
		t.Fatalf("ingest failed: %v", err)
	}

	specText := readFile(t, filepath.Join(root, ".canon", "specs", "spec-400.spec.md"))
	if !strings.Contains(specText, "created: 2026-02-19T17:30:45Z") {
		t.Fatalf("expected canonical spec to preserve source created timestamp")
	}
	if strings.Contains(specText, "created: 2026-02-19T00:00:00Z") {
		t.Fatalf("expected canonical spec to reject AI-rounded midnight created timestamp")
	}
}

func TestRenderWritesStateFileMap(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	if _, err := Ingest(root, IngestInput{
		Text: `---
id: spec-501
type: feature
title: API Availability
domain: api
created: 2026-02-19T12:00:00Z
depends_on: []
touched_domains: [api]
---
Services must publish a health endpoint.`,
	}); err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}

	if _, err := Render(root, RenderOptions{Write: true}); err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	fileMap := readFile(t, filepath.Join(root, "state", "file-map.json"))
	if !strings.Contains(fileMap, `"path": "state/api.md"`) {
		t.Fatalf("file-map missing api state file entry")
	}
	if !strings.Contains(fileMap, `"contributing_specs": [`) {
		t.Fatalf("file-map missing contributing specs section")
	}
}

func TestRenderFromResponseAppliesAIDocCompression(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	if _, err := Ingest(root, IngestInput{
		Text: `---
id: spec-601
type: feature
title: Legacy Session Window
domain: auth
created: 2026-02-19T09:00:00Z
depends_on: []
touched_domains: [auth]
---
Users must keep sessions for 30 days.`,
	}); err != nil {
		t.Fatalf("Ingest old spec failed: %v", err)
	}

	if _, err := Ingest(root, IngestInput{
		Text: `---
id: spec-602
type: resolution
title: Session Window Update
domain: auth
created: 2026-02-19T10:00:00Z
depends_on: [spec-601]
touched_domains: [auth]
---
Users must keep sessions for 7 days.`,
	}); err != nil {
		t.Fatalf("Ingest new spec failed: %v", err)
	}

	responsePath := filepath.Join(root, "render-response.json")
	response := `{
  "model": "codex-headless",
  "domain_docs": [
    {
      "domain": "auth",
      "body": "<!-- AUTO-GENERATED by canon. Do not edit directly. -->\n<!-- Domain: auth -->\n<!-- Contributing specs: spec-601, spec-602 -->\n\n# AUTH\n\n## Effective State\n\n- Users must keep sessions for 7 days. _(from spec-602)_\n"
    }
  ],
  "interaction_docs": [],
  "gaps": "<!-- AUTO-GENERATED by canon. Do not edit directly. -->\n# Known Gaps\n\nNo unresolved gaps.\n"
}`
	if err := os.WriteFile(responsePath, []byte(response), 0o644); err != nil {
		t.Fatalf("failed writing render response file: %v", err)
	}

	result, err := Render(root, RenderOptions{
		Write:        true,
		AIMode:       "from-response",
		AIProvider:   "codex",
		ResponseFile: responsePath,
	})
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if !result.AIUsed {
		t.Fatalf("expected AI render override usage")
	}

	authState := readFile(t, filepath.Join(root, "state", "auth.md"))
	if !strings.Contains(authState, "7 days") {
		t.Fatalf("expected compressed current state to include latest requirement")
	}
	if strings.Contains(authState, "30 days") {
		t.Fatalf("expected compressed output to drop superseded requirement")
	}
}

func TestRenderAutoFallsBackWhenAIProviderFails(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	if _, err := Ingest(root, IngestInput{
		Text:   "API clients must include tenant headers.",
		Title:  "Tenant Headers",
		Domain: "api",
	}); err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}

	result, err := Render(root, RenderOptions{
		Write:      true,
		AIMode:     "auto",
		AIProvider: "unsupported-provider",
	})
	if err != nil {
		t.Fatalf("Render should fall back on AI failure: %v", err)
	}
	if !result.AIFallback {
		t.Fatalf("expected AI fallback marker when provider fails")
	}
	if result.AIUsed {
		t.Fatalf("did not expect AI used marker during fallback")
	}

	manifest := readFile(t, filepath.Join(root, "state", "manifest.yaml"))
	if !strings.Contains(manifest, "total_specs: 1") {
		t.Fatalf("expected deterministic fallback output")
	}
}

func TestRenderRebuildsWhenStateDirectoryIsMissing(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	if _, err := Ingest(root, IngestInput{
		Text:   "Billing retries must happen daily.",
		Title:  "Billing Retry",
		Domain: "billing",
	}); err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}

	if _, err := Render(root, RenderOptions{Write: true}); err != nil {
		t.Fatalf("initial Render failed: %v", err)
	}

	if err := os.RemoveAll(filepath.Join(root, "state")); err != nil {
		t.Fatalf("failed to remove state directory: %v", err)
	}

	if _, err := Render(root, RenderOptions{
		Write:      true,
		AIMode:     "auto",
		AIProvider: "unsupported-provider",
	}); err != nil {
		t.Fatalf("Render failed to rebuild from scratch: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "state", "billing.md")); err != nil {
		t.Fatalf("expected billing state file after rebuild: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "state", "manifest.yaml")); err != nil {
		t.Fatalf("expected manifest after rebuild: %v", err)
	}
}
