package canon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResetDeletesNewerCanonArtifacts(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	mustIngestWithID(t, root, "spec-101", "Login", "auth", "Users can log in.")
	mustIngestWithID(t, root, "spec-102", "Sessions", "auth", "Sessions are valid for 7 days.")
	mustIngestWithID(t, root, "spec-103", "MFA", "auth", "MFA is required for admin users.")

	result, err := Reset(root, ResetInput{RefSpecID: "spec-102"})
	if err != nil {
		t.Fatalf("Reset failed: %v", err)
	}

	if result.KeptSpecID != "spec-102" {
		t.Fatalf("unexpected kept spec id: %s", result.KeptSpecID)
	}
	if result.LedgerDeleted != 1 || result.SpecDeleted != 1 || result.SourceDeleted != 1 {
		t.Fatalf("unexpected deletion counts: %+v", result)
	}

	entries, err := LoadLedger(root)
	if err != nil {
		t.Fatalf("LoadLedger failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 ledger entries after reset, got %d", len(entries))
	}
	if entries[0].SpecID != "spec-102" {
		t.Fatalf("expected newest ledger entry to be spec-102, got %s", entries[0].SpecID)
	}

	if _, err := os.Stat(filepath.Join(root, ".canon", "specs", "spec-103.spec.md")); !os.IsNotExist(err) {
		t.Fatalf("expected spec-103 canonical file to be removed, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".canon", "sources", "spec-103.source.md")); !os.IsNotExist(err) {
		t.Fatalf("expected spec-103 source file to be removed, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".canon", "specs", "spec-102.spec.md")); err != nil {
		t.Fatalf("expected spec-102 canonical file to remain: %v", err)
	}
}

func TestResetNoopWhenReferenceIsLatest(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	mustIngestWithID(t, root, "spec-201", "Billing Retry", "billing", "Retry billing jobs every day.")
	mustIngestWithID(t, root, "spec-202", "Billing Grace", "billing", "Grace period lasts 3 days.")

	result, err := Reset(root, ResetInput{RefSpecID: "spec-202"})
	if err != nil {
		t.Fatalf("Reset failed: %v", err)
	}
	if result.LedgerDeleted != 0 || result.SpecDeleted != 0 || result.SourceDeleted != 0 {
		t.Fatalf("expected no-op reset, got %+v", result)
	}
}

func TestResetReturnsErrorForUnknownSpecID(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	mustIngestWithID(t, root, "spec-301", "API Limits", "api", "Requests are limited per minute.")

	_, err := Reset(root, ResetInput{RefSpecID: "spec-999"})
	if err == nil {
		t.Fatalf("expected unknown spec id error")
	}
	if !strings.Contains(err.Error(), "unknown spec id") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResetBestEffortWhenFilesAreMissing(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	mustIngestWithID(t, root, "spec-401", "Auth", "auth", "Base auth behavior.")
	mustIngestWithID(t, root, "spec-402", "Tokens", "auth", "Tokens expire in 15 minutes.")
	mustIngestWithID(t, root, "spec-403", "Audit", "auth", "Audit every admin action.")

	if err := os.Remove(filepath.Join(root, ".canon", "specs", "spec-403.spec.md")); err != nil {
		t.Fatalf("failed removing spec file before reset: %v", err)
	}
	if err := os.Remove(filepath.Join(root, ".canon", "sources", "spec-403.source.md")); err != nil {
		t.Fatalf("failed removing source file before reset: %v", err)
	}

	result, err := Reset(root, ResetInput{RefSpecID: "spec-402"})
	if err != nil {
		t.Fatalf("Reset failed: %v", err)
	}
	if result.LedgerDeleted != 1 || result.SpecDeleted != 0 || result.SourceDeleted != 0 {
		t.Fatalf("unexpected best-effort deletion counts: %+v", result)
	}

	entries, err := LoadLedger(root)
	if err != nil {
		t.Fatalf("LoadLedger failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 ledger entries after reset, got %d", len(entries))
	}
}

func mustIngestWithID(t *testing.T, root string, specID string, title string, domain string, body string) {
	t.Helper()

	text := `---
id: ` + specID + `
type: feature
title: "` + title + `"
domain: ` + domain + `
created: 2026-02-19T12:00:00Z
depends_on: []
touched_domains: [` + domain + `]
---
` + body

	if _, err := Ingest(root, IngestInput{Text: text}); err != nil {
		t.Fatalf("Ingest failed for %s: %v", specID, err)
	}
}
