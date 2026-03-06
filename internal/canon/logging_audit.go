package canon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	loggingAuditCategoryCompleteness = "completeness"
	loggingAuditCategoryIntegrity    = "integrity"
	loggingAuditCategoryQuality      = "quality"

	loggingAuditRuleLedgerUnreadable     = "ledger-unreadable"
	loggingAuditRuleLedgerInvalidJSON    = "ledger-invalid-json"
	loggingAuditRuleSpecUnreadable       = "spec-unreadable"
	loggingAuditRuleSourceUnreadable     = "source-unreadable"
	loggingAuditRuleSpecParseError       = "spec-parse-error"
	loggingAuditRuleDuplicateSpecID      = "duplicate-spec-id"
	loggingAuditRuleMissingRequiredField = "missing-required-field"
	loggingAuditRuleInvalidSequence      = "invalid-sequence"
	loggingAuditRuleInvalidTimestamp     = "invalid-timestamp"
	loggingAuditRuleInvalidPath          = "invalid-path"
	loggingAuditRuleMissingSpecFile      = "missing-spec-file"
	loggingAuditRuleMissingSourceFile    = "missing-source-file"
	loggingAuditRuleMissingParent        = "missing-parent-reference"
	loggingAuditRuleDuplicateSequence    = "duplicate-sequence"
	loggingAuditRuleNonMonotonicSequence = "non-monotonic-sequence"
	loggingAuditRuleSpecIDMismatch       = "spec-id-mismatch"
	loggingAuditRuleMetadataMismatch     = "metadata-mismatch"
	loggingAuditRuleContentHashMismatch  = "content-hash-mismatch"
	loggingAuditRuleUnknownSpecType      = "unknown-spec-type"
	loggingAuditRuleSourceNamingMismatch = "source-naming-mismatch"
	loggingAuditRuleSpecNamingMismatch   = "spec-naming-mismatch"
	loggingAuditRuleDuplicateParent      = "duplicate-parent"
	loggingAuditRuleSelfParent           = "self-parent"
	loggingAuditRuleWeakTitle            = "weak-title"
	loggingAuditRuleEmptySource          = "empty-source"
)

type loggingAuditSpecRecord struct {
	PathAbs  string
	PathRel  string
	Raw      string
	Spec     Spec
	ParseErr error
}

type loggingAuditSourceRecord struct {
	PathAbs string
	PathRel string
	Size    int64
}

type loggingAuditLedgerRecord struct {
	PathAbs         string
	PathRel         string
	Entry           LedgerEntry
	IngestedAt      time.Time
	IngestedAtValid bool
}

func LoggingAudit(root string, opts LoggingAuditOptions) (LoggingAuditResult, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return LoggingAuditResult{}, err
	}

	ledgerDir := filepath.Join(absRoot, ".canon", "ledger")
	specsDir := filepath.Join(absRoot, ".canon", "specs")
	sourcesDir := filepath.Join(absRoot, ".canon", "sources")

	findings := make([]LoggingAuditFinding, 0)

	specFileCount, specRecords, specByID, specFindings, err := collectLoggingAuditSpecRecords(absRoot)
	if err != nil {
		return LoggingAuditResult{}, err
	}
	findings = append(findings, specFindings...)

	sourceFileCount, sourceRecords, sourceFindings, err := collectLoggingAuditSourceRecords(absRoot)
	if err != nil {
		return LoggingAuditResult{}, err
	}
	findings = append(findings, sourceFindings...)

	ledgerEntryCount, ledgerRecords, ledgerFindings, err := collectLoggingAuditLedgerRecords(absRoot)
	if err != nil {
		return LoggingAuditResult{}, err
	}
	findings = append(findings, ledgerFindings...)

	ledgerSpecIDs := make(map[string]struct{}, len(ledgerRecords))
	sequenceToLedgerPaths := make(map[int64][]string)
	validSequenceRecords := make([]loggingAuditLedgerRecord, 0, len(ledgerRecords))

	for _, record := range ledgerRecords {
		entry := record.Entry
		specID := strings.TrimSpace(entry.SpecID)

		if specID != "" {
			ledgerSpecIDs[specID] = struct{}{}
		}
		if entry.Sequence > 0 {
			sequenceToLedgerPaths[entry.Sequence] = append(sequenceToLedgerPaths[entry.Sequence], record.PathRel)
		}
		if entry.Sequence > 0 && record.IngestedAtValid {
			validSequenceRecords = append(validSequenceRecords, record)
		}

		entryType := strings.ToLower(strings.TrimSpace(entry.Type))
		if entryType != "" && !isKnownSpecType(entryType) {
			findings = append(findings, LoggingAuditFinding{
				RuleID:     loggingAuditRuleUnknownSpecType,
				Category:   loggingAuditCategoryQuality,
				Severity:   LoggingAuditSeverityMedium,
				SpecID:     specID,
				LedgerPath: record.PathRel,
				Field:      "type",
				Message:    fmt.Sprintf("unknown spec type %q", strings.TrimSpace(entry.Type)),
			})
		}

		title := strings.TrimSpace(entry.Title)
		if title != "" && len([]rune(title)) < 4 {
			findings = append(findings, LoggingAuditFinding{
				RuleID:     loggingAuditRuleWeakTitle,
				Category:   loggingAuditCategoryQuality,
				Severity:   LoggingAuditSeverityLow,
				SpecID:     specID,
				LedgerPath: record.PathRel,
				Field:      "title",
				Message:    "title is unusually short",
			})
		}

		specPath, specPathErr := normalizeLoggingAuditArtifactPath(entry.SpecPath, ".canon/specs", ".spec.md")
		if specPathErr != nil {
			findings = append(findings, LoggingAuditFinding{
				RuleID:     loggingAuditRuleInvalidPath,
				Category:   loggingAuditCategoryIntegrity,
				Severity:   LoggingAuditSeverityHigh,
				SpecID:     specID,
				LedgerPath: record.PathRel,
				Field:      "spec_path",
				Message:    specPathErr.Error(),
			})
		} else {
			specAbsPath := filepath.Join(absRoot, filepath.FromSlash(specPath))
			info, err := os.Stat(specAbsPath)
			if err != nil {
				if os.IsNotExist(err) {
					findings = append(findings, LoggingAuditFinding{
						RuleID:     loggingAuditRuleMissingSpecFile,
						Category:   loggingAuditCategoryIntegrity,
						Severity:   LoggingAuditSeverityHigh,
						SpecID:     specID,
						LedgerPath: record.PathRel,
						SpecPath:   specPath,
						Message:    "ledger spec_path does not exist",
					})
				} else {
					return LoggingAuditResult{}, err
				}
			} else if info.IsDir() {
				findings = append(findings, LoggingAuditFinding{
					RuleID:     loggingAuditRuleInvalidPath,
					Category:   loggingAuditCategoryIntegrity,
					Severity:   LoggingAuditSeverityHigh,
					SpecID:     specID,
					LedgerPath: record.PathRel,
					SpecPath:   specPath,
					Field:      "spec_path",
					Message:    "spec_path points to a directory",
				})
			} else {
				if specID != "" {
					expectedSpecPath := filepath.ToSlash(filepath.Join(".canon", "specs", specFileName(specID)))
					if specPath != expectedSpecPath {
						findings = append(findings, LoggingAuditFinding{
							RuleID:     loggingAuditRuleSpecNamingMismatch,
							Category:   loggingAuditCategoryQuality,
							Severity:   LoggingAuditSeverityLow,
							SpecID:     specID,
							LedgerPath: record.PathRel,
							SpecPath:   specPath,
							Message:    fmt.Sprintf("spec_path does not match canonical naming for spec id %q", specID),
						})
					}
				}

				rec, ok := specRecords[specPath]
				if ok && rec.ParseErr == nil {
					if specID != "" && strings.TrimSpace(rec.Spec.ID) != "" && rec.Spec.ID != specID {
						findings = append(findings, LoggingAuditFinding{
							RuleID:     loggingAuditRuleSpecIDMismatch,
							Category:   loggingAuditCategoryIntegrity,
							Severity:   LoggingAuditSeverityHigh,
							SpecID:     specID,
							LedgerPath: record.PathRel,
							SpecPath:   specPath,
							Field:      "spec_id",
							Message:    fmt.Sprintf("ledger spec_id %q does not match spec frontmatter id %q", specID, rec.Spec.ID),
						})
					}
					if strings.TrimSpace(entry.Title) != "" && strings.TrimSpace(rec.Spec.Title) != "" && strings.TrimSpace(entry.Title) != strings.TrimSpace(rec.Spec.Title) {
						findings = append(findings, LoggingAuditFinding{
							RuleID:     loggingAuditRuleMetadataMismatch,
							Category:   loggingAuditCategoryQuality,
							Severity:   LoggingAuditSeverityMedium,
							SpecID:     specID,
							LedgerPath: record.PathRel,
							SpecPath:   specPath,
							Field:      "title",
							Message:    fmt.Sprintf("ledger title %q does not match spec title %q", strings.TrimSpace(entry.Title), strings.TrimSpace(rec.Spec.Title)),
						})
					}
					if strings.TrimSpace(entry.Domain) != "" && strings.TrimSpace(rec.Spec.Domain) != "" && strings.TrimSpace(entry.Domain) != strings.TrimSpace(rec.Spec.Domain) {
						findings = append(findings, LoggingAuditFinding{
							RuleID:     loggingAuditRuleMetadataMismatch,
							Category:   loggingAuditCategoryQuality,
							Severity:   LoggingAuditSeverityMedium,
							SpecID:     specID,
							LedgerPath: record.PathRel,
							SpecPath:   specPath,
							Field:      "domain",
							Message:    fmt.Sprintf("ledger domain %q does not match spec domain %q", strings.TrimSpace(entry.Domain), strings.TrimSpace(rec.Spec.Domain)),
						})
					}
					if strings.TrimSpace(entry.Type) != "" && strings.TrimSpace(rec.Spec.Type) != "" && strings.TrimSpace(entry.Type) != strings.TrimSpace(rec.Spec.Type) {
						findings = append(findings, LoggingAuditFinding{
							RuleID:     loggingAuditRuleMetadataMismatch,
							Category:   loggingAuditCategoryQuality,
							Severity:   LoggingAuditSeverityMedium,
							SpecID:     specID,
							LedgerPath: record.PathRel,
							SpecPath:   specPath,
							Field:      "type",
							Message:    fmt.Sprintf("ledger type %q does not match spec type %q", strings.TrimSpace(entry.Type), strings.TrimSpace(rec.Spec.Type)),
						})
					}

					entryHash := strings.TrimSpace(entry.ContentHash)
					if entryHash != "" {
						currentHash := checksum(rec.Raw)
						if entryHash != currentHash {
							findings = append(findings, LoggingAuditFinding{
								RuleID:     loggingAuditRuleContentHashMismatch,
								Category:   loggingAuditCategoryIntegrity,
								Severity:   LoggingAuditSeverityHigh,
								SpecID:     specID,
								LedgerPath: record.PathRel,
								SpecPath:   specPath,
								Field:      "content_hash",
								Message:    "ledger content_hash does not match spec file contents",
							})
						}
					}
				}
			}
		}

		sourcePath, sourcePathErr := normalizeLoggingAuditArtifactPath(entry.SourcePath, ".canon/sources", ".source.md")
		if sourcePathErr != nil {
			findings = append(findings, LoggingAuditFinding{
				RuleID:     loggingAuditRuleInvalidPath,
				Category:   loggingAuditCategoryIntegrity,
				Severity:   LoggingAuditSeverityHigh,
				SpecID:     specID,
				LedgerPath: record.PathRel,
				Field:      "source_path",
				Message:    sourcePathErr.Error(),
			})
		} else {
			sourceAbsPath := filepath.Join(absRoot, filepath.FromSlash(sourcePath))
			info, err := os.Stat(sourceAbsPath)
			if err != nil {
				if os.IsNotExist(err) {
					findings = append(findings, LoggingAuditFinding{
						RuleID:     loggingAuditRuleMissingSourceFile,
						Category:   loggingAuditCategoryIntegrity,
						Severity:   LoggingAuditSeverityHigh,
						SpecID:     specID,
						LedgerPath: record.PathRel,
						SourcePath: sourcePath,
						Message:    "ledger source_path does not exist",
					})
				} else {
					return LoggingAuditResult{}, err
				}
			} else if info.IsDir() {
				findings = append(findings, LoggingAuditFinding{
					RuleID:     loggingAuditRuleInvalidPath,
					Category:   loggingAuditCategoryIntegrity,
					Severity:   LoggingAuditSeverityHigh,
					SpecID:     specID,
					LedgerPath: record.PathRel,
					SourcePath: sourcePath,
					Field:      "source_path",
					Message:    "source_path points to a directory",
				})
			} else {
				if specID != "" {
					expectedSourcePath := filepath.ToSlash(filepath.Join(".canon", "sources", specID+".source.md"))
					if sourcePath != expectedSourcePath {
						findings = append(findings, LoggingAuditFinding{
							RuleID:     loggingAuditRuleSourceNamingMismatch,
							Category:   loggingAuditCategoryQuality,
							Severity:   LoggingAuditSeverityLow,
							SpecID:     specID,
							LedgerPath: record.PathRel,
							SourcePath: sourcePath,
							Message:    fmt.Sprintf("source_path does not match canonical naming for spec id %q", specID),
						})
					}
				}

				rec, ok := sourceRecords[sourcePath]
				if ok && rec.Size == 0 {
					findings = append(findings, LoggingAuditFinding{
						RuleID:     loggingAuditRuleEmptySource,
						Category:   loggingAuditCategoryQuality,
						Severity:   LoggingAuditSeverityLow,
						SpecID:     specID,
						LedgerPath: record.PathRel,
						SourcePath: sourcePath,
						Message:    "source file is empty",
					})
				}
			}
		}
	}

	for seq, paths := range sequenceToLedgerPaths {
		if len(paths) < 2 {
			continue
		}
		sortedPaths := append([]string{}, paths...)
		sort.Strings(sortedPaths)
		findings = append(findings, LoggingAuditFinding{
			RuleID:     loggingAuditRuleDuplicateSequence,
			Category:   loggingAuditCategoryIntegrity,
			Severity:   LoggingAuditSeverityHigh,
			LedgerPath: sortedPaths[0],
			Field:      "sequence",
			Message:    fmt.Sprintf("sequence %d is reused by ledger entries [%s]", seq, strings.Join(sortedPaths, ", ")),
		})
	}

	sort.Slice(validSequenceRecords, func(i, j int) bool {
		if validSequenceRecords[i].IngestedAt.Equal(validSequenceRecords[j].IngestedAt) {
			return validSequenceRecords[i].PathRel < validSequenceRecords[j].PathRel
		}
		return validSequenceRecords[i].IngestedAt.Before(validSequenceRecords[j].IngestedAt)
	})
	for i := 1; i < len(validSequenceRecords); i++ {
		current := validSequenceRecords[i]
		previous := validSequenceRecords[i-1]
		if current.Entry.Sequence <= previous.Entry.Sequence {
			findings = append(findings, LoggingAuditFinding{
				RuleID:     loggingAuditRuleNonMonotonicSequence,
				Category:   loggingAuditCategoryQuality,
				Severity:   LoggingAuditSeverityMedium,
				SpecID:     strings.TrimSpace(current.Entry.SpecID),
				LedgerPath: current.PathRel,
				Field:      "sequence",
				Message:    fmt.Sprintf("sequence %d is not greater than previous sequence %d by ingest time ordering", current.Entry.Sequence, previous.Entry.Sequence),
			})
		}
	}

	for _, record := range ledgerRecords {
		specID := strings.TrimSpace(record.Entry.SpecID)
		seenParents := make(map[string]struct{}, len(record.Entry.Parents))
		for _, rawParent := range record.Entry.Parents {
			parent := strings.TrimSpace(rawParent)
			if parent == "" {
				findings = append(findings, LoggingAuditFinding{
					RuleID:     loggingAuditRuleMissingRequiredField,
					Category:   loggingAuditCategoryCompleteness,
					Severity:   LoggingAuditSeverityMedium,
					SpecID:     specID,
					LedgerPath: record.PathRel,
					Field:      "parents",
					Message:    "parents contains an empty value",
				})
				continue
			}

			if parent == specID {
				findings = append(findings, LoggingAuditFinding{
					RuleID:     loggingAuditRuleSelfParent,
					Category:   loggingAuditCategoryQuality,
					Severity:   LoggingAuditSeverityMedium,
					SpecID:     specID,
					LedgerPath: record.PathRel,
					Field:      "parents",
					Message:    "parent reference points to same spec id",
				})
			}

			if _, ok := seenParents[parent]; ok {
				findings = append(findings, LoggingAuditFinding{
					RuleID:     loggingAuditRuleDuplicateParent,
					Category:   loggingAuditCategoryQuality,
					Severity:   LoggingAuditSeverityLow,
					SpecID:     specID,
					LedgerPath: record.PathRel,
					Field:      "parents",
					Message:    fmt.Sprintf("duplicate parent reference %q", parent),
				})
			} else {
				seenParents[parent] = struct{}{}
			}

			if _, ok := ledgerSpecIDs[parent]; !ok {
				findings = append(findings, LoggingAuditFinding{
					RuleID:     loggingAuditRuleMissingParent,
					Category:   loggingAuditCategoryIntegrity,
					Severity:   LoggingAuditSeverityHigh,
					SpecID:     specID,
					LedgerPath: record.PathRel,
					Field:      "parents",
					Message:    fmt.Sprintf("parent %q does not reference an existing ledger spec_id", parent),
				})
			}
		}
	}

	for specID, rec := range specByID {
		if _, ok := ledgerSpecIDs[specID]; ok {
			continue
		}
		findings = append(findings, LoggingAuditFinding{
			RuleID:   loggingAuditRuleMissingRequiredField,
			Category: loggingAuditCategoryIntegrity,
			Severity: LoggingAuditSeverityMedium,
			SpecID:   specID,
			SpecPath: rec.PathRel,
			Field:    "spec_id",
			Message:  "spec exists without corresponding ledger entry",
		})
	}

	sortLoggingAuditFindings(findings)
	summary := summarizeLoggingAuditFindings(findings)

	result := LoggingAuditResult{
		Root:          absRoot,
		LedgerDir:     ledgerDir,
		SpecsDir:      specsDir,
		SourcesDir:    sourcesDir,
		LedgerEntries: ledgerEntryCount,
		SpecFiles:     specFileCount,
		SourceFiles:   sourceFileCount,
		Findings:      findings,
		Summary:       summary,
	}

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

func collectLoggingAuditSpecRecords(root string) (int, map[string]loggingAuditSpecRecord, map[string]loggingAuditSpecRecord, []LoggingAuditFinding, error) {
	glob := filepath.Join(root, ".canon", "specs", "*.spec.md")
	paths, err := filepath.Glob(glob)
	if err != nil {
		return 0, nil, nil, nil, err
	}
	sort.Strings(paths)

	records := make(map[string]loggingAuditSpecRecord, len(paths))
	byID := make(map[string]loggingAuditSpecRecord, len(paths))
	findings := make([]LoggingAuditFinding, 0)

	for _, path := range paths {
		rel := toRelativeSlash(root, path)
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			findings = append(findings, LoggingAuditFinding{
				RuleID:   loggingAuditRuleSpecUnreadable,
				Category: loggingAuditCategoryIntegrity,
				Severity: LoggingAuditSeverityCritical,
				SpecPath: rel,
				Message:  fmt.Sprintf("failed to read spec file: %v", readErr),
			})
			continue
		}

		record := loggingAuditSpecRecord{PathAbs: path, PathRel: rel, Raw: string(b)}
		spec, parseErr := parseSpecText(string(b), rel)
		if parseErr != nil {
			record.ParseErr = parseErr
			findings = append(findings, LoggingAuditFinding{
				RuleID:   loggingAuditRuleSpecParseError,
				Category: loggingAuditCategoryCompleteness,
				Severity: LoggingAuditSeverityHigh,
				SpecPath: rel,
				Message:  parseErr.Error(),
			})
		} else {
			record.Spec = spec
			if prior, ok := byID[spec.ID]; ok {
				findings = append(findings, LoggingAuditFinding{
					RuleID:   loggingAuditRuleDuplicateSpecID,
					Category: loggingAuditCategoryIntegrity,
					Severity: LoggingAuditSeverityHigh,
					SpecID:   spec.ID,
					SpecPath: rel,
					Message:  fmt.Sprintf("duplicate spec id also present in %s", prior.PathRel),
				})
			} else {
				byID[spec.ID] = record
			}
		}
		records[rel] = record
	}

	return len(paths), records, byID, findings, nil
}

func collectLoggingAuditSourceRecords(root string) (int, map[string]loggingAuditSourceRecord, []LoggingAuditFinding, error) {
	glob := filepath.Join(root, ".canon", "sources", "*.source.md")
	paths, err := filepath.Glob(glob)
	if err != nil {
		return 0, nil, nil, err
	}
	sort.Strings(paths)

	records := make(map[string]loggingAuditSourceRecord, len(paths))
	findings := make([]LoggingAuditFinding, 0)

	for _, path := range paths {
		rel := toRelativeSlash(root, path)
		info, statErr := os.Stat(path)
		if statErr != nil {
			findings = append(findings, LoggingAuditFinding{
				RuleID:     loggingAuditRuleSourceUnreadable,
				Category:   loggingAuditCategoryIntegrity,
				Severity:   LoggingAuditSeverityCritical,
				SourcePath: rel,
				Message:    fmt.Sprintf("failed to read source metadata: %v", statErr),
			})
			continue
		}
		records[rel] = loggingAuditSourceRecord{
			PathAbs: path,
			PathRel: rel,
			Size:    info.Size(),
		}
	}

	return len(paths), records, findings, nil
}

func collectLoggingAuditLedgerRecords(root string) (int, []loggingAuditLedgerRecord, []LoggingAuditFinding, error) {
	glob := filepath.Join(root, ".canon", "ledger", "*.json")
	paths, err := filepath.Glob(glob)
	if err != nil {
		return 0, nil, nil, err
	}
	sort.Strings(paths)

	records := make([]loggingAuditLedgerRecord, 0, len(paths))
	findings := make([]LoggingAuditFinding, 0)

	for _, path := range paths {
		rel := toRelativeSlash(root, path)
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			findings = append(findings, LoggingAuditFinding{
				RuleID:     loggingAuditRuleLedgerUnreadable,
				Category:   loggingAuditCategoryIntegrity,
				Severity:   LoggingAuditSeverityCritical,
				LedgerPath: rel,
				Message:    fmt.Sprintf("failed to read ledger entry: %v", readErr),
			})
			continue
		}

		var entry LedgerEntry
		if err := json.Unmarshal(b, &entry); err != nil {
			findings = append(findings, LoggingAuditFinding{
				RuleID:     loggingAuditRuleLedgerInvalidJSON,
				Category:   loggingAuditCategoryCompleteness,
				Severity:   LoggingAuditSeverityCritical,
				LedgerPath: rel,
				Message:    fmt.Sprintf("invalid ledger JSON: %v", err),
			})
			continue
		}

		record := loggingAuditLedgerRecord{
			PathAbs: path,
			PathRel: rel,
			Entry:   entry,
		}

		findings = append(findings, validateLoggingAuditRequiredLedgerFields(record)...)

		ingestedAt := strings.TrimSpace(entry.IngestedAt)
		if ingestedAt != "" {
			t, parseErr := time.Parse(timeRFC3339, ingestedAt)
			if parseErr != nil {
				findings = append(findings, LoggingAuditFinding{
					RuleID:     loggingAuditRuleInvalidTimestamp,
					Category:   loggingAuditCategoryCompleteness,
					Severity:   LoggingAuditSeverityHigh,
					SpecID:     strings.TrimSpace(entry.SpecID),
					LedgerPath: rel,
					Field:      "ingested_at",
					Message:    fmt.Sprintf("invalid RFC3339 timestamp %q", ingestedAt),
				})
			} else {
				record.IngestedAt = t.UTC()
				record.IngestedAtValid = true
			}
		}

		records = append(records, record)
	}

	return len(paths), records, findings, nil
}

func validateLoggingAuditRequiredLedgerFields(record loggingAuditLedgerRecord) []LoggingAuditFinding {
	entry := record.Entry
	specID := strings.TrimSpace(entry.SpecID)
	findings := make([]LoggingAuditFinding, 0)

	requiredFields := map[string]string{
		"spec_id":      strings.TrimSpace(entry.SpecID),
		"title":        strings.TrimSpace(entry.Title),
		"type":         strings.TrimSpace(entry.Type),
		"domain":       strings.TrimSpace(entry.Domain),
		"ingested_at":  strings.TrimSpace(entry.IngestedAt),
		"content_hash": strings.TrimSpace(entry.ContentHash),
		"spec_path":    strings.TrimSpace(entry.SpecPath),
		"source_path":  strings.TrimSpace(entry.SourcePath),
	}

	for field, value := range requiredFields {
		if value != "" {
			continue
		}
		findings = append(findings, LoggingAuditFinding{
			RuleID:     loggingAuditRuleMissingRequiredField,
			Category:   loggingAuditCategoryCompleteness,
			Severity:   LoggingAuditSeverityHigh,
			SpecID:     specID,
			LedgerPath: record.PathRel,
			Field:      field,
			Message:    fmt.Sprintf("required field %q is empty", field),
		})
	}

	if entry.Sequence <= 0 {
		findings = append(findings, LoggingAuditFinding{
			RuleID:     loggingAuditRuleInvalidSequence,
			Category:   loggingAuditCategoryCompleteness,
			Severity:   LoggingAuditSeverityHigh,
			SpecID:     specID,
			LedgerPath: record.PathRel,
			Field:      "sequence",
			Message:    fmt.Sprintf("sequence must be greater than zero (got %d)", entry.Sequence),
		})
	}

	return findings
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
		if left.Category != right.Category {
			return left.Category < right.Category
		}
		if left.RuleID != right.RuleID {
			return left.RuleID < right.RuleID
		}
		if left.SpecID != right.SpecID {
			return left.SpecID < right.SpecID
		}
		if left.LedgerPath != right.LedgerPath {
			return left.LedgerPath < right.LedgerPath
		}
		if left.SpecPath != right.SpecPath {
			return left.SpecPath < right.SpecPath
		}
		if left.SourcePath != right.SourcePath {
			return left.SourcePath < right.SourcePath
		}
		if left.Field != right.Field {
			return left.Field < right.Field
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

func isKnownSpecType(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "feature", "technical", "resolution":
		return true
	default:
		return false
	}
}

func normalizeLoggingAuditArtifactPath(value string, expectedPrefix string, requiredSuffix string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("path is empty")
	}
	if filepath.IsAbs(trimmed) {
		return "", fmt.Errorf("path must be relative: %s", trimmed)
	}

	clean := filepath.ToSlash(filepath.Clean(trimmed))
	clean = strings.TrimPrefix(clean, "./")
	if clean == "" || clean == "." {
		return "", fmt.Errorf("path is empty")
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path escapes repository root: %s", trimmed)
	}

	prefix := filepath.ToSlash(filepath.Clean(expectedPrefix))
	if clean != prefix && !strings.HasPrefix(clean, prefix+"/") {
		return "", fmt.Errorf("path must be under %s: %s", prefix, trimmed)
	}
	if requiredSuffix != "" && !strings.HasSuffix(clean, requiredSuffix) {
		return "", fmt.Errorf("path must end with %s: %s", requiredSuffix, trimmed)
	}

	return clean, nil
}

func toRelativeSlash(root string, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	rel = filepath.ToSlash(rel)
	rel = strings.TrimPrefix(rel, "./")
	if rel == "" {
		return "."
	}
	return rel
}
