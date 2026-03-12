package canon

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	loggingAuditArtifactKindLayout = "layout"
	loggingAuditArtifactKindLedger = "ledger"
	loggingAuditArtifactKindSpec   = "spec"
	loggingAuditArtifactKindSource = "source"

	loggingAuditRuleMissingDirectory       = "missing-directory"
	loggingAuditRuleUnreadableArtifact     = "unreadable-artifact"
	loggingAuditRuleInvalidLedgerJSON      = "invalid-ledger-json"
	loggingAuditRuleMissingRequiredFields  = "missing-required-fields"
	loggingAuditRuleInvalidSequence        = "invalid-sequence"
	loggingAuditRuleInvalidIngestedAt      = "invalid-ingested-at"
	loggingAuditRuleInvalidSpecPath        = "invalid-spec-path"
	loggingAuditRuleInvalidSourcePath      = "invalid-source-path"
	loggingAuditRuleMissingSpecReference   = "missing-spec-reference"
	loggingAuditRuleMissingSourceReference = "missing-source-reference"
	loggingAuditRuleMissingParent          = "missing-parent"
	loggingAuditRuleDuplicateSequence      = "duplicate-sequence"
	loggingAuditRuleNonMonotonicSequence   = "non-monotonic-sequence"
	loggingAuditRuleSpecParseError         = "spec-parse-error"
	loggingAuditRuleSpecIDMismatch         = "spec-id-mismatch"
	loggingAuditRuleMetadataMismatch       = "metadata-mismatch"
	loggingAuditRuleHashMismatch           = "hash-mismatch"
	loggingAuditRuleUnknownSpecType        = "unknown-spec-type"
)

var loggingAuditKnownSpecTypes = map[string]struct{}{
	"feature":    {},
	"technical":  {},
	"resolution": {},
}

type loggingAuditLedgerRecord struct {
	Path        string
	Entry       LedgerEntry
	ParsedTime  time.Time
	TimeValid   bool
	SpecID      string
	Title       string
	Type        string
	Domain      string
	SpecPath    string
	SourcePath  string
	ContentHash string
}

type loggingAuditSpecArtifact struct {
	Path     string
	Readable bool
	Parsed   bool
	Text     string
	Spec     Spec
}

type loggingAuditSourceArtifact struct {
	Path     string
	Readable bool
}

func LoggingAudit(root string, opts LoggingAuditOptions) (LoggingAuditResult, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return LoggingAuditResult{}, err
	}

	info, err := os.Stat(absRoot)
	if err != nil {
		return LoggingAuditResult{}, err
	}
	if !info.IsDir() {
		return LoggingAuditResult{}, fmt.Errorf("root is not a directory: %s", filepath.ToSlash(absRoot))
	}

	result := LoggingAuditResult{
		Root: absRoot,
		Directories: LoggingAuditDirectories{
			Ledger:  filepath.Join(absRoot, ".canon", "ledger"),
			Specs:   filepath.Join(absRoot, ".canon", "specs"),
			Sources: filepath.Join(absRoot, ".canon", "sources"),
		},
	}

	findings := make([]LoggingAuditFinding, 0)
	addFinding := func(ruleID string, severity LoggingAuditSeverity, artifactKind string, relPath string, specID string, message string) {
		findings = append(findings, LoggingAuditFinding{
			RuleID:       ruleID,
			Severity:     severity,
			ArtifactKind: artifactKind,
			Path:         relPath,
			SpecID:       strings.TrimSpace(specID),
			Message:      strings.TrimSpace(message),
		})
	}

	ledgerPaths, err := loggingAuditListArtifactPaths(absRoot, ".canon/ledger", "*.json")
	if err != nil {
		return LoggingAuditResult{}, err
	}
	specPaths, err := loggingAuditListArtifactPaths(absRoot, ".canon/specs", "*.spec.md")
	if err != nil {
		return LoggingAuditResult{}, err
	}
	sourcePaths, err := loggingAuditListArtifactPaths(absRoot, ".canon/sources", "*.source.md")
	if err != nil {
		return LoggingAuditResult{}, err
	}

	result.ArtifactCounts = LoggingAuditArtifactCounts{
		LedgerFiles: len(ledgerPaths),
		SpecFiles:   len(specPaths),
		SourceFiles: len(sourcePaths),
	}

	loggingAuditCheckDirectory(absRoot, ".canon/ledger", addFinding)
	loggingAuditCheckDirectory(absRoot, ".canon/specs", addFinding)
	loggingAuditCheckDirectory(absRoot, ".canon/sources", addFinding)

	specArtifacts := make(map[string]loggingAuditSpecArtifact, len(specPaths))
	specsByID := make(map[string]loggingAuditSpecArtifact, len(specPaths))
	for _, relPath := range specPaths {
		artifact := loggingAuditSpecArtifact{Path: relPath}
		absPath := filepath.Join(absRoot, filepath.FromSlash(relPath))

		content, readErr := os.ReadFile(absPath)
		if readErr != nil {
			addFinding(
				loggingAuditRuleUnreadableArtifact,
				LoggingAuditSeverityCritical,
				loggingAuditArtifactKindSpec,
				relPath,
				"",
				fmt.Sprintf("cannot read spec artifact: %v", readErr),
			)
			specArtifacts[relPath] = artifact
			continue
		}

		artifact.Readable = true
		artifact.Text = string(content)

		spec, parseErr := parseSpecText(artifact.Text, relPath)
		if parseErr != nil {
			addFinding(
				loggingAuditRuleSpecParseError,
				LoggingAuditSeverityCritical,
				loggingAuditArtifactKindSpec,
				relPath,
				"",
				fmt.Sprintf("cannot parse spec frontmatter: %v", parseErr),
			)
			specArtifacts[relPath] = artifact
			continue
		}

		artifact.Parsed = true
		artifact.Spec = spec
		specArtifacts[relPath] = artifact
		if _, ok := specsByID[spec.ID]; !ok {
			specsByID[spec.ID] = artifact
		}
		if !loggingAuditIsKnownSpecType(spec.Type) {
			addFinding(
				loggingAuditRuleUnknownSpecType,
				LoggingAuditSeverityMedium,
				loggingAuditArtifactKindSpec,
				relPath,
				spec.ID,
				fmt.Sprintf("unknown spec type %q", spec.Type),
			)
		}
	}

	sourceArtifacts := make(map[string]loggingAuditSourceArtifact, len(sourcePaths))
	for _, relPath := range sourcePaths {
		artifact := loggingAuditSourceArtifact{Path: relPath}
		absPath := filepath.Join(absRoot, filepath.FromSlash(relPath))
		if _, readErr := os.ReadFile(absPath); readErr != nil {
			addFinding(
				loggingAuditRuleUnreadableArtifact,
				LoggingAuditSeverityCritical,
				loggingAuditArtifactKindSource,
				relPath,
				"",
				fmt.Sprintf("cannot read source artifact: %v", readErr),
			)
			sourceArtifacts[relPath] = artifact
			continue
		}
		artifact.Readable = true
		sourceArtifacts[relPath] = artifact
	}

	parsedLedgers := make([]loggingAuditLedgerRecord, 0, len(ledgerPaths))
	ledgerSpecIDs := make(map[string]struct{}, len(ledgerPaths))
	for _, relPath := range ledgerPaths {
		absPath := filepath.Join(absRoot, filepath.FromSlash(relPath))
		content, readErr := os.ReadFile(absPath)
		if readErr != nil {
			addFinding(
				loggingAuditRuleUnreadableArtifact,
				LoggingAuditSeverityCritical,
				loggingAuditArtifactKindLedger,
				relPath,
				"",
				fmt.Sprintf("cannot read ledger artifact: %v", readErr),
			)
			continue
		}

		var entry LedgerEntry
		if err := json.Unmarshal(content, &entry); err != nil {
			addFinding(
				loggingAuditRuleInvalidLedgerJSON,
				LoggingAuditSeverityCritical,
				loggingAuditArtifactKindLedger,
				relPath,
				"",
				fmt.Sprintf("cannot decode ledger JSON: %v", err),
			)
			continue
		}

		record := loggingAuditLedgerRecord{
			Path:        relPath,
			Entry:       entry,
			SpecID:      strings.TrimSpace(entry.SpecID),
			Title:       strings.TrimSpace(entry.Title),
			Type:        strings.TrimSpace(entry.Type),
			Domain:      strings.TrimSpace(entry.Domain),
			SpecPath:    strings.TrimSpace(entry.SpecPath),
			SourcePath:  strings.TrimSpace(entry.SourcePath),
			ContentHash: strings.TrimSpace(entry.ContentHash),
		}

		missingFields := make([]string, 0, 8)
		if record.SpecID == "" {
			missingFields = append(missingFields, "spec_id")
		}
		if record.Title == "" {
			missingFields = append(missingFields, "title")
		}
		if record.Type == "" {
			missingFields = append(missingFields, "type")
		}
		if record.Domain == "" {
			missingFields = append(missingFields, "domain")
		}
		if record.ContentHash == "" {
			missingFields = append(missingFields, "content_hash")
		}
		if record.SpecPath == "" {
			missingFields = append(missingFields, "spec_path")
		}
		if record.SourcePath == "" {
			missingFields = append(missingFields, "source_path")
		}
		if len(missingFields) > 0 {
			addFinding(
				loggingAuditRuleMissingRequiredFields,
				LoggingAuditSeverityHigh,
				loggingAuditArtifactKindLedger,
				relPath,
				record.SpecID,
				fmt.Sprintf("missing required ledger fields: %s", strings.Join(missingFields, ", ")),
			)
		}

		if entry.Sequence <= 0 {
			addFinding(
				loggingAuditRuleInvalidSequence,
				LoggingAuditSeverityHigh,
				loggingAuditArtifactKindLedger,
				relPath,
				record.SpecID,
				fmt.Sprintf("sequence must be positive, got %d", entry.Sequence),
			)
		}

		if strings.TrimSpace(entry.IngestedAt) == "" {
			addFinding(
				loggingAuditRuleMissingRequiredFields,
				LoggingAuditSeverityHigh,
				loggingAuditArtifactKindLedger,
				relPath,
				record.SpecID,
				"missing required ledger fields: ingested_at",
			)
		} else {
			parsedTime, timeErr := time.Parse(time.RFC3339, strings.TrimSpace(entry.IngestedAt))
			if timeErr != nil {
				addFinding(
					loggingAuditRuleInvalidIngestedAt,
					LoggingAuditSeverityHigh,
					loggingAuditArtifactKindLedger,
					relPath,
					record.SpecID,
					fmt.Sprintf("ingested_at must be RFC3339, got %q", strings.TrimSpace(entry.IngestedAt)),
				)
			} else {
				record.TimeValid = true
				record.ParsedTime = parsedTime.UTC()
			}
		}

		expectedSpecPath := loggingAuditExpectedSpecPath(record.SpecID)
		if record.SpecPath != "" {
			if err := loggingAuditValidateCanonicalPath(record.SpecPath, ".canon/specs", expectedSpecPath); err != nil {
				addFinding(
					loggingAuditRuleInvalidSpecPath,
					LoggingAuditSeverityHigh,
					loggingAuditArtifactKindLedger,
					relPath,
					record.SpecID,
					err.Error(),
				)
			}
		}

		expectedSourcePath := loggingAuditExpectedSourcePath(record.SpecID)
		if record.SourcePath != "" {
			if err := loggingAuditValidateCanonicalPath(record.SourcePath, ".canon/sources", expectedSourcePath); err != nil {
				addFinding(
					loggingAuditRuleInvalidSourcePath,
					LoggingAuditSeverityHigh,
					loggingAuditArtifactKindLedger,
					relPath,
					record.SpecID,
					err.Error(),
				)
			}
		}

		if record.SpecPath != "" {
			if _, ok := specArtifacts[record.SpecPath]; !ok {
				addFinding(
					loggingAuditRuleMissingSpecReference,
					LoggingAuditSeverityHigh,
					loggingAuditArtifactKindLedger,
					relPath,
					record.SpecID,
					fmt.Sprintf("referenced spec artifact does not exist: %s", record.SpecPath),
				)
			}
		}

		if record.SourcePath != "" {
			if _, ok := sourceArtifacts[record.SourcePath]; !ok {
				addFinding(
					loggingAuditRuleMissingSourceReference,
					LoggingAuditSeverityHigh,
					loggingAuditArtifactKindLedger,
					relPath,
					record.SpecID,
					fmt.Sprintf("referenced source artifact does not exist: %s", record.SourcePath),
				)
			}
		}

		if record.SpecID != "" {
			ledgerSpecIDs[record.SpecID] = struct{}{}
		}
		parsedLedgers = append(parsedLedgers, record)
	}

	for _, record := range parsedLedgers {
		for _, parent := range record.Entry.Parents {
			trimmedParent := strings.TrimSpace(parent)
			if trimmedParent == "" {
				continue
			}
			if _, ok := ledgerSpecIDs[trimmedParent]; ok {
				continue
			}
			addFinding(
				loggingAuditRuleMissingParent,
				LoggingAuditSeverityHigh,
				loggingAuditArtifactKindLedger,
				record.Path,
				record.SpecID,
				fmt.Sprintf("parent spec id %q does not resolve to any ledger entry", trimmedParent),
			)
		}

		specArtifact, ok := loggingAuditResolveSpecArtifact(record, specArtifacts, specsByID)
		if !ok {
			if record.Type != "" && !loggingAuditIsKnownSpecType(record.Type) {
				addFinding(
					loggingAuditRuleUnknownSpecType,
					LoggingAuditSeverityMedium,
					loggingAuditArtifactKindLedger,
					record.Path,
					record.SpecID,
					fmt.Sprintf("unknown spec type %q", record.Type),
				)
			}
			continue
		}

		if specArtifact.Readable && record.ContentHash != "" {
			currentHash := checksum(specArtifact.Text)
			if currentHash != record.ContentHash {
				addFinding(
					loggingAuditRuleHashMismatch,
					LoggingAuditSeverityHigh,
					loggingAuditArtifactKindLedger,
					record.Path,
					record.SpecID,
					fmt.Sprintf("content hash mismatch: ledger=%s current=%s", record.ContentHash, currentHash),
				)
			}
		}

		if !specArtifact.Parsed {
			continue
		}

		spec := specArtifact.Spec
		if record.SpecID != "" && spec.ID != record.SpecID {
			addFinding(
				loggingAuditRuleSpecIDMismatch,
				LoggingAuditSeverityHigh,
				loggingAuditArtifactKindLedger,
				record.Path,
				record.SpecID,
				fmt.Sprintf("ledger spec_id %q does not match spec frontmatter id %q", record.SpecID, spec.ID),
			)
		}

		mismatches := make([]string, 0, 3)
		if record.Type != "" && spec.Type != record.Type {
			mismatches = append(mismatches, fmt.Sprintf("type ledger=%q spec=%q", record.Type, spec.Type))
		}
		if record.Title != "" && spec.Title != record.Title {
			mismatches = append(mismatches, fmt.Sprintf("title ledger=%q spec=%q", record.Title, spec.Title))
		}
		if record.Domain != "" && spec.Domain != record.Domain {
			mismatches = append(mismatches, fmt.Sprintf("domain ledger=%q spec=%q", record.Domain, spec.Domain))
		}
		if len(mismatches) > 0 {
			addFinding(
				loggingAuditRuleMetadataMismatch,
				LoggingAuditSeverityMedium,
				loggingAuditArtifactKindLedger,
				record.Path,
				record.SpecID,
				fmt.Sprintf("ledger metadata does not match spec frontmatter: %s", strings.Join(mismatches, "; ")),
			)
		}
	}

	loggingAuditAppendSequenceFindings(parsedLedgers, addFinding)
	sortLoggingAuditFindings(findings)

	result.Findings = findings
	result.Summary = summarizeLoggingAuditFindings(findings)

	if strings.TrimSpace(string(opts.FailOn)) != "" {
		failOn, err := parseLoggingAuditSeverity(string(opts.FailOn))
		if err != nil {
			return LoggingAuditResult{}, err
		}
		if failOn != LoggingAuditSeverityNone {
			result.FailOn = failOn
			result.ThresholdExceeded = loggingAuditExceedsThreshold(result, failOn)
		}
	}

	return result, nil
}

func loggingAuditListArtifactPaths(root string, relDir string, pattern string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(relDir), pattern))
	if err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(matches))
	for _, absPath := range matches {
		relPath, err := filepath.Rel(root, absPath)
		if err != nil {
			return nil, err
		}
		paths = append(paths, filepath.ToSlash(relPath))
	}
	sort.Strings(paths)
	return paths, nil
}

func loggingAuditCheckDirectory(root string, relDir string, addFinding func(string, LoggingAuditSeverity, string, string, string, string)) {
	absDir := filepath.Join(root, filepath.FromSlash(relDir))
	info, err := os.Stat(absDir)
	if err != nil {
		if os.IsNotExist(err) {
			addFinding(
				loggingAuditRuleMissingDirectory,
				LoggingAuditSeverityHigh,
				loggingAuditArtifactKindLayout,
				relDir,
				"",
				fmt.Sprintf("required Canon directory is missing: %s", relDir),
			)
			return
		}
		addFinding(
			loggingAuditRuleUnreadableArtifact,
			LoggingAuditSeverityCritical,
			loggingAuditArtifactKindLayout,
			relDir,
			"",
			fmt.Sprintf("cannot inspect Canon directory: %v", err),
		)
		return
	}
	if info.IsDir() {
		return
	}
	addFinding(
		loggingAuditRuleMissingDirectory,
		LoggingAuditSeverityHigh,
		loggingAuditArtifactKindLayout,
		relDir,
		"",
		fmt.Sprintf("expected Canon directory but found non-directory path: %s", relDir),
	)
}

func loggingAuditExpectedSpecPath(specID string) string {
	trimmed := strings.TrimSpace(specID)
	if trimmed == "" {
		return ""
	}
	return path.Join(".canon", "specs", specFileName(trimmed))
}

func loggingAuditExpectedSourcePath(specID string) string {
	trimmed := strings.TrimSpace(specID)
	if trimmed == "" {
		return ""
	}
	return path.Join(".canon", "sources", trimmed+".source.md")
}

func loggingAuditValidateCanonicalPath(value string, requiredDir string, expectedPath string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("path is empty")
	}
	if trimmed != filepath.ToSlash(trimmed) {
		return fmt.Errorf("path must use canonical forward slashes: %s", trimmed)
	}
	if filepath.IsAbs(trimmed) || path.IsAbs(trimmed) {
		return fmt.Errorf("path must be relative to repository root: %s", trimmed)
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("path escapes canonical artifact directories: %s", trimmed)
	}
	if cleaned != trimmed {
		return fmt.Errorf("path must be canonical and clean: %s", trimmed)
	}
	requiredPrefix := strings.TrimSuffix(requiredDir, "/") + "/"
	if !strings.HasPrefix(cleaned, requiredPrefix) {
		return fmt.Errorf("path must stay under %s: %s", requiredDir, trimmed)
	}
	if expectedPath != "" && cleaned != expectedPath {
		return fmt.Errorf("path must equal %s, got %s", expectedPath, trimmed)
	}
	return nil
}

func loggingAuditResolveSpecArtifact(record loggingAuditLedgerRecord, specArtifacts map[string]loggingAuditSpecArtifact, specsByID map[string]loggingAuditSpecArtifact) (loggingAuditSpecArtifact, bool) {
	if record.SpecPath != "" {
		if artifact, ok := specArtifacts[record.SpecPath]; ok {
			return artifact, true
		}
	}
	if expected := loggingAuditExpectedSpecPath(record.SpecID); expected != "" {
		if artifact, ok := specArtifacts[expected]; ok {
			return artifact, true
		}
	}
	if record.SpecID != "" {
		if artifact, ok := specsByID[record.SpecID]; ok {
			return artifact, true
		}
	}
	return loggingAuditSpecArtifact{}, false
}

func loggingAuditAppendSequenceFindings(records []loggingAuditLedgerRecord, addFinding func(string, LoggingAuditSeverity, string, string, string, string)) {
	if len(records) == 0 {
		return
	}

	bySequence := make(map[int64][]loggingAuditLedgerRecord)
	for _, record := range records {
		if record.Entry.Sequence <= 0 {
			continue
		}
		bySequence[record.Entry.Sequence] = append(bySequence[record.Entry.Sequence], record)
	}

	sequences := make([]int64, 0, len(bySequence))
	for sequence, items := range bySequence {
		if len(items) <= 1 {
			continue
		}
		sequences = append(sequences, sequence)
	}
	sort.Slice(sequences, func(i, j int) bool {
		return sequences[i] < sequences[j]
	})

	for _, sequence := range sequences {
		items := bySequence[sequence]
		sort.Slice(items, func(i, j int) bool {
			return loggingAuditLedgerOrderKey(items[i]) < loggingAuditLedgerOrderKey(items[j])
		})

		paths := make([]string, 0, len(items))
		for _, item := range items {
			paths = append(paths, item.Path)
		}
		addFinding(
			loggingAuditRuleDuplicateSequence,
			LoggingAuditSeverityMedium,
			loggingAuditArtifactKindLedger,
			items[0].Path,
			items[0].SpecID,
			fmt.Sprintf("sequence %d is duplicated across ledger artifacts: %s", sequence, strings.Join(paths, ", ")),
		)
	}

	ordered := make([]loggingAuditLedgerRecord, 0, len(records))
	for _, record := range records {
		if !record.TimeValid || record.Entry.Sequence <= 0 {
			continue
		}
		ordered = append(ordered, record)
	}
	sort.Slice(ordered, func(i, j int) bool {
		left := ordered[i]
		right := ordered[j]
		if !left.ParsedTime.Equal(right.ParsedTime) {
			return left.ParsedTime.Before(right.ParsedTime)
		}
		return loggingAuditLedgerOrderKey(left) < loggingAuditLedgerOrderKey(right)
	})

	for i := 1; i < len(ordered); i++ {
		prev := ordered[i-1]
		curr := ordered[i]
		if curr.Entry.Sequence >= prev.Entry.Sequence {
			continue
		}
		addFinding(
			loggingAuditRuleNonMonotonicSequence,
			LoggingAuditSeverityMedium,
			loggingAuditArtifactKindLedger,
			curr.Path,
			curr.SpecID,
			fmt.Sprintf(
				"sequence %d is lower than prior sequence %d for later ingest time %s",
				curr.Entry.Sequence,
				prev.Entry.Sequence,
				curr.ParsedTime.Format(time.RFC3339),
			),
		)
	}
}

func loggingAuditLedgerOrderKey(record loggingAuditLedgerRecord) string {
	return fmt.Sprintf("%020d|%s|%s|%s", record.Entry.Sequence, record.Entry.IngestedAt, record.SpecID, record.Path)
}

func sortLoggingAuditFindings(findings []LoggingAuditFinding) {
	sort.Slice(findings, func(i, j int) bool {
		left := findings[i]
		right := findings[j]

		leftRank := loggingAuditSeverityRank(left.Severity)
		rightRank := loggingAuditSeverityRank(right.Severity)
		if leftRank != rightRank {
			return leftRank > rightRank
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.ArtifactKind != right.ArtifactKind {
			return left.ArtifactKind < right.ArtifactKind
		}
		if left.SpecID != right.SpecID {
			return left.SpecID < right.SpecID
		}
		if left.RuleID != right.RuleID {
			return left.RuleID < right.RuleID
		}
		return left.Message < right.Message
	})
}

func summarizeLoggingAuditFindings(findings []LoggingAuditFinding) LoggingAuditSummary {
	summary := LoggingAuditSummary{
		TotalFindings:      len(findings),
		HighestSeverity:    LoggingAuditSeverityNone,
		FindingsBySeverity: LoggingAuditSeverityCounts{},
	}

	for _, finding := range findings {
		switch finding.Severity {
		case LoggingAuditSeverityLow:
			summary.FindingsBySeverity.Low++
		case LoggingAuditSeverityMedium:
			summary.FindingsBySeverity.Medium++
		case LoggingAuditSeverityHigh:
			summary.FindingsBySeverity.High++
		case LoggingAuditSeverityCritical:
			summary.FindingsBySeverity.Critical++
		}

		if loggingAuditSeverityRank(finding.Severity) > loggingAuditSeverityRank(summary.HighestSeverity) {
			summary.HighestSeverity = finding.Severity
		}
	}

	return summary
}

func parseLoggingAuditSeverity(value string) (LoggingAuditSeverity, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "", string(LoggingAuditSeverityNone):
		return LoggingAuditSeverityNone, nil
	case string(LoggingAuditSeverityLow):
		return LoggingAuditSeverityLow, nil
	case string(LoggingAuditSeverityMedium):
		return LoggingAuditSeverityMedium, nil
	case string(LoggingAuditSeverityHigh):
		return LoggingAuditSeverityHigh, nil
	case string(LoggingAuditSeverityCritical):
		return LoggingAuditSeverityCritical, nil
	default:
		return "", fmt.Errorf("unsupported severity %q (expected one of: low, medium, high, critical)", strings.TrimSpace(value))
	}
}

func loggingAuditExceedsThreshold(result LoggingAuditResult, threshold LoggingAuditSeverity) bool {
	thresholdRank := loggingAuditSeverityRank(threshold)
	if thresholdRank <= loggingAuditSeverityRank(LoggingAuditSeverityNone) {
		return false
	}
	return loggingAuditSeverityRank(result.Summary.HighestSeverity) >= thresholdRank
}

func loggingAuditSeverityRank(severity LoggingAuditSeverity) int {
	switch severity {
	case LoggingAuditSeverityNone:
		return 0
	case LoggingAuditSeverityLow:
		return 1
	case LoggingAuditSeverityMedium:
		return 2
	case LoggingAuditSeverityHigh:
		return 3
	case LoggingAuditSeverityCritical:
		return 4
	default:
		return -1
	}
}

func loggingAuditIsKnownSpecType(value string) bool {
	_, ok := loggingAuditKnownSpecTypes[strings.TrimSpace(value)]
	return ok
}
