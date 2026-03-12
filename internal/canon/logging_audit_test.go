package canon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLoggingAuditCleanArtifacts(t *testing.T) {
	root := t.TempDir()
	ensureLoggingAuditLayout(t, root)

	ingestLoggingAuditSpec(t, root, "log0001", "Logging Audit Base", "canon-cli", "feature", "2026-03-01T10:00:00Z")
	ingestLoggingAuditSpec(t, root, "log0002", "Logging Audit Followup", "canon-cli", "technical", "2026-03-01T10:05:00Z")

	result, err := LoggingAudit(root, LoggingAuditOptions{})
	if err != nil {
		t.Fatalf("LoggingAudit failed: %v", err)
	}

	if result.ArtifactCounts.LedgerFiles != 2 || result.ArtifactCounts.SpecFiles != 2 || result.ArtifactCounts.SourceFiles != 2 {
		t.Fatalf("unexpected artifact counts: %+v", result.ArtifactCounts)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings, got %d", len(result.Findings))
	}
	if result.Summary.TotalFindings != 0 || result.Summary.HighestSeverity != LoggingAuditSeverityNone {
		t.Fatalf("unexpected summary: %+v", result.Summary)
	}
}

func TestLoggingAuditAcceptsNestedSourceArtifacts(t *testing.T) {
	root := t.TempDir()
	ensureLoggingAuditLayout(t, root)

	item := ingestLoggingAuditSpec(t, root, "nested/log0001", "Nested Logging Audit", "canon-cli", "feature", "2026-03-01T10:00:00Z")
	entry := readLoggingAuditLedgerEntry(t, root, item.LedgerPath)
	if got, want := entry.SourcePath, ".canon/sources/nested/log0001.source.md"; got != want {
		t.Fatalf("unexpected source path: got=%q want=%q", got, want)
	}

	result, err := LoggingAudit(root, LoggingAuditOptions{})
	if err != nil {
		t.Fatalf("LoggingAudit failed: %v", err)
	}

	if result.ArtifactCounts.SourceFiles != 1 {
		t.Fatalf("expected nested source artifact to be counted, got %+v", result.ArtifactCounts)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings for valid nested source artifact, got %+v", result.Findings)
	}
}

func TestLoggingAuditReportsCorruptedArtifactsInDeterministicOrder(t *testing.T) {
	root := t.TempDir()
	ensureLoggingAuditLayout(t, root)
	ingestLoggingAuditSpec(t, root, "log0001", "Logging Audit Base", "canon-cli", "feature", "2026-03-01T10:00:00Z")

	if err := os.WriteFile(filepath.Join(root, ".canon", "ledger", "a-invalid.json"), []byte("{"), 0o644); err != nil {
		t.Fatalf("failed writing malformed ledger artifact: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, ".canon", "ledger", "b-unreadable.json"), 0o755); err != nil {
		t.Fatalf("failed creating unreadable ledger artifact: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".canon", "specs", "c-broken.spec.md"), []byte("---\nid: broken\n---\n"), 0o644); err != nil {
		t.Fatalf("failed writing broken spec artifact: %v", err)
	}

	result, err := LoggingAudit(root, LoggingAuditOptions{})
	if err != nil {
		t.Fatalf("LoggingAudit failed: %v", err)
	}

	if got, want := len(result.Findings), 3; got != want {
		t.Fatalf("expected %d findings, got %d", want, got)
	}

	gotOrder := []string{
		result.Findings[0].RuleID + "@" + result.Findings[0].Path,
		result.Findings[1].RuleID + "@" + result.Findings[1].Path,
		result.Findings[2].RuleID + "@" + result.Findings[2].Path,
	}
	wantOrder := []string{
		loggingAuditRuleInvalidLedgerJSON + "@.canon/ledger/a-invalid.json",
		loggingAuditRuleUnreadableArtifact + "@.canon/ledger/b-unreadable.json",
		loggingAuditRuleSpecParseError + "@.canon/specs/c-broken.spec.md",
	}
	if !slices.Equal(gotOrder, wantOrder) {
		t.Fatalf("unexpected finding order: got=%v want=%v", gotOrder, wantOrder)
	}

	if result.Summary.TotalFindings != 3 || result.Summary.HighestSeverity != LoggingAuditSeverityCritical {
		t.Fatalf("unexpected summary: %+v", result.Summary)
	}
	if result.Summary.FindingsBySeverity.Critical != 3 {
		t.Fatalf("expected 3 critical findings, got %+v", result.Summary.FindingsBySeverity)
	}
}

func TestLoggingAuditReportsMissingReferencesAndBadParents(t *testing.T) {
	root := t.TempDir()
	ensureLoggingAuditLayout(t, root)
	item := ingestLoggingAuditSpec(t, root, "log0001", "Logging Audit Base", "canon-cli", "feature", "2026-03-01T10:00:00Z")

	if err := os.Remove(filepath.Join(root, filepath.FromSlash(item.SpecPath))); err != nil {
		t.Fatalf("failed removing spec artifact: %v", err)
	}
	if err := os.Remove(filepath.Join(root, ".canon", "sources", item.SpecID+".source.md")); err != nil {
		t.Fatalf("failed removing source artifact: %v", err)
	}

	entry := readLoggingAuditLedgerEntry(t, root, item.LedgerPath)
	entry.Parents = []string{"ghost-parent"}
	writeLoggingAuditLedgerEntry(t, root, item.LedgerPath, entry)

	result, err := LoggingAudit(root, LoggingAuditOptions{})
	if err != nil {
		t.Fatalf("LoggingAudit failed: %v", err)
	}

	assertLoggingAuditRuleIDs(t, result.Findings, []string{
		loggingAuditRuleMissingSpecReference,
		loggingAuditRuleMissingSourceReference,
		loggingAuditRuleMissingParent,
	})
	if result.Summary.TotalFindings != 3 || result.Summary.HighestSeverity != LoggingAuditSeverityHigh {
		t.Fatalf("unexpected summary: %+v", result.Summary)
	}
}

func TestLoggingAuditReportsSequenceQualityIssues(t *testing.T) {
	root := t.TempDir()
	ensureLoggingAuditLayout(t, root)

	first := ingestLoggingAuditSpec(t, root, "log0001", "Logging Audit Base", "canon-cli", "feature", "2026-03-01T10:00:00Z")
	second := ingestLoggingAuditSpec(t, root, "log0002", "Logging Audit Mid", "canon-cli", "feature", "2026-03-01T10:05:00Z")
	third := ingestLoggingAuditSpec(t, root, "log0003", "Logging Audit Late", "canon-cli", "feature", "2026-03-01T10:10:00Z")

	entry1 := readLoggingAuditLedgerEntry(t, root, first.LedgerPath)
	entry1.Sequence = 100
	entry1.IngestedAt = "2026-03-01T10:00:00Z"
	writeLoggingAuditLedgerEntry(t, root, first.LedgerPath, entry1)

	entry2 := readLoggingAuditLedgerEntry(t, root, second.LedgerPath)
	entry2.Sequence = 100
	entry2.IngestedAt = "2026-03-01T10:01:00Z"
	writeLoggingAuditLedgerEntry(t, root, second.LedgerPath, entry2)

	entry3 := readLoggingAuditLedgerEntry(t, root, third.LedgerPath)
	entry3.Sequence = 99
	entry3.IngestedAt = "2026-03-01T10:02:00Z"
	writeLoggingAuditLedgerEntry(t, root, third.LedgerPath, entry3)

	result, err := LoggingAudit(root, LoggingAuditOptions{})
	if err != nil {
		t.Fatalf("LoggingAudit failed: %v", err)
	}

	assertLoggingAuditRuleIDs(t, result.Findings, []string{
		loggingAuditRuleDuplicateSequence,
		loggingAuditRuleNonMonotonicSequence,
	})
	if result.Summary.TotalFindings != 2 || result.Summary.HighestSeverity != LoggingAuditSeverityMedium {
		t.Fatalf("unexpected summary: %+v", result.Summary)
	}
}

func TestLoggingAuditReportsMetadataHashAndUnknownTypeIssues(t *testing.T) {
	root := t.TempDir()
	ensureLoggingAuditLayout(t, root)
	item := ingestLoggingAuditSpec(t, root, "log0001", "Logging Audit Base", "canon-cli", "feature", "2026-03-01T10:00:00Z")

	corrupted := Spec{
		ID:             item.SpecID,
		Type:           "mystery",
		Title:          "Mutated Title",
		Domain:         "ops",
		Created:        "2026-03-01T10:00:00Z",
		TouchedDomains: []string{"ops"},
		Body:           "Mutated body.",
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(item.SpecPath)), []byte(canonicalSpecText(corrupted)), 0o644); err != nil {
		t.Fatalf("failed mutating spec artifact: %v", err)
	}

	result, err := LoggingAudit(root, LoggingAuditOptions{})
	if err != nil {
		t.Fatalf("LoggingAudit failed: %v", err)
	}

	assertLoggingAuditRuleIDs(t, result.Findings, []string{
		loggingAuditRuleHashMismatch,
		loggingAuditRuleMetadataMismatch,
		loggingAuditRuleUnknownSpecType,
	})
	if result.Summary.HighestSeverity != LoggingAuditSeverityHigh {
		t.Fatalf("expected highest severity high, got %s", result.Summary.HighestSeverity)
	}
	if result.Summary.FindingsBySeverity.High != 1 || result.Summary.FindingsBySeverity.Medium != 2 {
		t.Fatalf("unexpected severity counts: %+v", result.Summary.FindingsBySeverity)
	}
}

func TestLoggingAuditThresholdBehavior(t *testing.T) {
	root := t.TempDir()
	ensureLoggingAuditLayout(t, root)
	item := ingestLoggingAuditSpec(t, root, "log0001", "Logging Audit Base", "canon-cli", "feature", "2026-03-01T10:00:00Z")

	if err := os.Remove(filepath.Join(root, filepath.FromSlash(item.SpecPath))); err != nil {
		t.Fatalf("failed removing spec artifact: %v", err)
	}

	baseResult, err := LoggingAudit(root, LoggingAuditOptions{})
	if err != nil {
		t.Fatalf("LoggingAudit without fail-on failed: %v", err)
	}
	if baseResult.FailOn != "" || baseResult.ThresholdExceeded != nil {
		t.Fatalf("expected threshold metadata to be omitted without fail-on, got %+v", baseResult)
	}

	highResult, err := LoggingAudit(root, LoggingAuditOptions{FailOn: LoggingAuditSeverityHigh})
	if err != nil {
		t.Fatalf("LoggingAudit with high fail-on failed: %v", err)
	}
	if highResult.FailOn != LoggingAuditSeverityHigh || highResult.ThresholdExceeded == nil || !*highResult.ThresholdExceeded {
		t.Fatalf("expected high threshold to be exceeded, got %+v", highResult)
	}

	criticalResult, err := LoggingAudit(root, LoggingAuditOptions{FailOn: LoggingAuditSeverityCritical})
	if err != nil {
		t.Fatalf("LoggingAudit with critical fail-on failed: %v", err)
	}
	if criticalResult.ThresholdExceeded == nil {
		t.Fatalf("expected threshold metadata when fail-on is provided")
	}
	if *criticalResult.ThresholdExceeded {
		t.Fatalf("did not expect critical threshold to be exceeded")
	}

	if _, err := LoggingAudit(root, LoggingAuditOptions{FailOn: LoggingAuditSeverity("urgent")}); err == nil {
		t.Fatalf("expected unsupported fail-on severity error")
	}
}

func ensureLoggingAuditLayout(t *testing.T, root string) {
	t.Helper()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}
}

func ingestLoggingAuditSpec(t *testing.T, root string, id string, title string, domain string, typ string, created string) IngestResult {
	t.Helper()
	result, err := Ingest(root, IngestInput{
		Text:          "Body for " + id + ".",
		ID:            id,
		Title:         title,
		Domain:        domain,
		Type:          typ,
		Created:       created,
		NoAutoParents: true,
		ConflictMode:  "off",
	})
	if err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}
	return result
}

func readLoggingAuditLedgerEntry(t *testing.T, root string, ledgerRelPath string) LedgerEntry {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ledgerRelPath)))
	if err != nil {
		t.Fatalf("failed reading ledger artifact: %v", err)
	}

	var entry LedgerEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("failed decoding ledger artifact: %v", err)
	}
	return entry
}

func writeLoggingAuditLedgerEntry(t *testing.T, root string, ledgerRelPath string, entry LedgerEntry) {
	t.Helper()
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		t.Fatalf("failed encoding ledger artifact: %v", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(ledgerRelPath)), data, 0o644); err != nil {
		t.Fatalf("failed writing ledger artifact: %v", err)
	}
}

func assertLoggingAuditRuleIDs(t *testing.T, findings []LoggingAuditFinding, want []string) {
	t.Helper()
	got := make([]string, 0, len(findings))
	for _, finding := range findings {
		got = append(got, finding.RuleID)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected rule ids: got=%v want=%v", got, want)
	}
}
