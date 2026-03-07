package canon

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	roadmapEntropyCategoryScopeCreep = "scope-creep"
	roadmapEntropyCategoryDrift      = "drift"

	roadmapEntropyRuleNewPrimaryDomains      = "new-primary-domains"
	roadmapEntropyRuleTouchedDomainExpansion = "touched-domain-expansion"
	roadmapEntropyRuleNonFeatureRatioRise    = "non-feature-ratio-rise"
	roadmapEntropyRuleOrphanNonFeatureSpecs  = "orphan-non-feature-specs"
)

func RoadmapEntropy(root string, opts RoadmapEntropyOptions) (RoadmapEntropyResult, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return RoadmapEntropyResult{}, err
	}
	if opts.Window <= 0 {
		return RoadmapEntropyResult{}, fmt.Errorf("window must be positive (got %d)", opts.Window)
	}
	if err := ensureRoadmapEntropyInputs(absRoot); err != nil {
		return RoadmapEntropyResult{}, err
	}

	failOn := RoadmapEntropySeverityNone
	if strings.TrimSpace(string(opts.FailOn)) != "" {
		parsed, err := parseRoadmapEntropySeverity(string(opts.FailOn))
		if err != nil {
			return RoadmapEntropyResult{}, err
		}
		failOn = parsed
	}

	specs, err := loadSpecs(absRoot)
	if err != nil {
		return RoadmapEntropyResult{}, err
	}
	entries, err := LoadLedger(absRoot)
	if err != nil {
		return RoadmapEntropyResult{}, err
	}

	orderedSpecs := buildRoadmapEntropyOrderedSpecs(specs, entries)
	baselineSpecs, recentSpecs := splitRoadmapEntropyWindows(orderedSpecs, opts.Window)
	findings := analyzeRoadmapEntropyFindings(orderedSpecs, baselineSpecs, recentSpecs, entries)
	sortRoadmapEntropyFindings(findings)

	result := RoadmapEntropyResult{
		Root:                absRoot,
		Window:              opts.Window,
		InsufficientHistory: len(orderedSpecs) < opts.Window*2,
		OrderedSpecCount:    len(orderedSpecs),
		BaselineSpecIDs:     roadmapEntropySpecIDs(baselineSpecs),
		RecentSpecIDs:       roadmapEntropySpecIDs(recentSpecs),
		Findings:            findings,
		Summary:             summarizeRoadmapEntropyFindings(findings),
	}

	if failOn != RoadmapEntropySeverityNone {
		result.FailOn = failOn
		result.ThresholdExceeded = roadmapEntropyExceedsThreshold(result, failOn)
	}

	return result, nil
}

func ensureRoadmapEntropyInputs(root string) error {
	requiredDirs := []string{
		filepath.Join(root, ".canon", "specs"),
		filepath.Join(root, ".canon", "ledger"),
	}
	for _, path := range requiredDirs {
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("required path not found: %s", filepath.ToSlash(path))
			}
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("required path is not a directory: %s", filepath.ToSlash(path))
		}
	}
	return nil
}

func buildRoadmapEntropyOrderedSpecs(specs []Spec, entries []LedgerEntry) []Spec {
	if len(specs) == 0 {
		return []Spec{}
	}

	specByID := make(map[string]Spec, len(specs))
	for _, spec := range specs {
		specByID[spec.ID] = spec
	}

	ordered := make([]Spec, 0, len(specs))
	seen := make(map[string]struct{}, len(specs))

	ledgerOrdered := make([]LedgerEntry, 0, len(entries))
	ledgerOrdered = append(ledgerOrdered, entries...)
	sort.SliceStable(ledgerOrdered, func(i, j int) bool {
		left := ledgerOrdered[i]
		right := ledgerOrdered[j]

		if left.Sequence != 0 && right.Sequence != 0 && left.Sequence != right.Sequence {
			return left.Sequence < right.Sequence
		}
		if left.IngestedAt != right.IngestedAt {
			return left.IngestedAt < right.IngestedAt
		}
		if left.SpecID != right.SpecID {
			return left.SpecID < right.SpecID
		}
		return left.ContentHash < right.ContentHash
	})

	for _, entry := range ledgerOrdered {
		id := strings.TrimSpace(entry.SpecID)
		if id == "" {
			continue
		}
		spec, ok := specByID[id]
		if !ok {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ordered = append(ordered, spec)
	}

	remaining := make([]Spec, 0, len(specByID)-len(ordered))
	for _, spec := range specs {
		if _, exists := seen[spec.ID]; exists {
			continue
		}
		remaining = append(remaining, spec)
	}
	sortSpecsStable(remaining)
	ordered = append(ordered, remaining...)
	return ordered
}

func splitRoadmapEntropyWindows(ordered []Spec, window int) ([]Spec, []Spec) {
	if len(ordered) == 0 {
		return []Spec{}, []Spec{}
	}

	recentWindow := window
	if recentWindow > len(ordered) {
		recentWindow = len(ordered)
	}

	recentStart := len(ordered) - recentWindow
	recent := append([]Spec(nil), ordered[recentStart:]...)

	baselineEnd := recentStart
	if baselineEnd < 0 {
		baselineEnd = 0
	}
	baselineStart := baselineEnd - window
	if baselineStart < 0 {
		baselineStart = 0
	}
	baseline := append([]Spec(nil), ordered[baselineStart:baselineEnd]...)
	return baseline, recent
}

func analyzeRoadmapEntropyFindings(allSpecs []Spec, baseline []Spec, recent []Spec, entries []LedgerEntry) []RoadmapEntropyFinding {
	findings := make([]RoadmapEntropyFinding, 0, 4)
	if len(recent) == 0 || len(baseline) == 0 {
		return findings
	}

	baselinePrimary := roadmapEntropyPrimaryDomainSet(baseline)
	recentPrimary := roadmapEntropyPrimaryDomainSet(recent)
	newPrimaryDomains := roadmapEntropySetDiffSorted(recentPrimary, baselinePrimary)
	if len(newPrimaryDomains) > 0 {
		findings = append(findings, RoadmapEntropyFinding{
			RuleID:        roadmapEntropyRuleNewPrimaryDomains,
			Category:      roadmapEntropyCategoryScopeCreep,
			Severity:      roadmapEntropySeverityForNewPrimaryDomains(len(newPrimaryDomains)),
			Message:       fmt.Sprintf("recent window introduced %d new primary domain(s): %s", len(newPrimaryDomains), strings.Join(newPrimaryDomains, ", ")),
			BaselineCount: len(baselinePrimary),
			RecentCount:   len(recentPrimary),
			Domains:       newPrimaryDomains,
		})
	}

	baselineTouched := roadmapEntropyTouchedDomainSet(baseline)
	recentTouched := roadmapEntropyTouchedDomainSet(recent)
	newTouchedDomains := roadmapEntropySetDiffSorted(recentTouched, baselineTouched)
	if len(newTouchedDomains) > 0 {
		findings = append(findings, RoadmapEntropyFinding{
			RuleID:        roadmapEntropyRuleTouchedDomainExpansion,
			Category:      roadmapEntropyCategoryScopeCreep,
			Severity:      roadmapEntropySeverityForTouchedDomainExpansion(len(newTouchedDomains)),
			Message:       fmt.Sprintf("recent window touched %d additional domain(s): %s", len(newTouchedDomains), strings.Join(newTouchedDomains, ", ")),
			BaselineCount: len(baselineTouched),
			RecentCount:   len(recentTouched),
			Domains:       newTouchedDomains,
		})
	}

	baselineNonFeatureCount := roadmapEntropyNonFeatureCount(baseline)
	recentNonFeatureCount := roadmapEntropyNonFeatureCount(recent)
	baselineRatio := roadmapEntropyRatio(baselineNonFeatureCount, len(baseline))
	recentRatio := roadmapEntropyRatio(recentNonFeatureCount, len(recent))
	ratioDelta := recentRatio - baselineRatio

	if severity, ok := roadmapEntropySeverityForRatioRise(baselineRatio, recentRatio, ratioDelta, recentNonFeatureCount); ok {
		findings = append(findings, RoadmapEntropyFinding{
			RuleID:        roadmapEntropyRuleNonFeatureRatioRise,
			Category:      roadmapEntropyCategoryDrift,
			Severity:      severity,
			Message:       fmt.Sprintf("non-feature ratio rose from %.1f%% to %.1f%% (+%.1f%%)", baselineRatio*100, recentRatio*100, ratioDelta*100),
			BaselineCount: baselineNonFeatureCount,
			RecentCount:   recentNonFeatureCount,
			BaselineRatio: baselineRatio,
			RecentRatio:   recentRatio,
			RatioDelta:    ratioDelta,
		})
	}

	orphanSpecIDs, orphanResolutionCount := roadmapEntropyFindOrphanNonFeatureRecentSpecs(allSpecs, recent, entries)
	if len(orphanSpecIDs) > 0 {
		findings = append(findings, RoadmapEntropyFinding{
			RuleID:      roadmapEntropyRuleOrphanNonFeatureSpecs,
			Category:    roadmapEntropyCategoryDrift,
			Severity:    roadmapEntropySeverityForOrphanNonFeature(len(orphanSpecIDs), orphanResolutionCount),
			Message:     fmt.Sprintf("recent window contains %d orphan non-feature spec(s): %s", len(orphanSpecIDs), strings.Join(orphanSpecIDs, ", ")),
			RecentCount: len(orphanSpecIDs),
			SpecIDs:     orphanSpecIDs,
		})
	}

	return findings
}

func sortRoadmapEntropyFindings(findings []RoadmapEntropyFinding) {
	sort.Slice(findings, func(i, j int) bool {
		left := findings[i]
		right := findings[j]

		leftRank := roadmapEntropySeverityRank(left.Severity)
		rightRank := roadmapEntropySeverityRank(right.Severity)
		if leftRank != rightRank {
			return leftRank > rightRank
		}
		if left.Category != right.Category {
			return left.Category < right.Category
		}
		if left.RuleID != right.RuleID {
			return left.RuleID < right.RuleID
		}
		leftDomainKey := strings.Join(left.Domains, ",")
		rightDomainKey := strings.Join(right.Domains, ",")
		if leftDomainKey != rightDomainKey {
			return leftDomainKey < rightDomainKey
		}
		leftSpecKey := strings.Join(left.SpecIDs, ",")
		rightSpecKey := strings.Join(right.SpecIDs, ",")
		if leftSpecKey != rightSpecKey {
			return leftSpecKey < rightSpecKey
		}
		if left.Message != right.Message {
			return left.Message < right.Message
		}
		if left.BaselineCount != right.BaselineCount {
			return left.BaselineCount < right.BaselineCount
		}
		return left.RecentCount < right.RecentCount
	})
}

func summarizeRoadmapEntropyFindings(findings []RoadmapEntropyFinding) RoadmapEntropySummary {
	summary := RoadmapEntropySummary{
		TotalFindings:      len(findings),
		HighestSeverity:    RoadmapEntropySeverityNone,
		FindingsByCategory: RoadmapEntropyCategoryCounts{},
		FindingsBySeverity: RoadmapEntropySeverityCounts{},
	}

	for _, finding := range findings {
		switch finding.Category {
		case roadmapEntropyCategoryScopeCreep:
			summary.FindingsByCategory.ScopeCreep++
		case roadmapEntropyCategoryDrift:
			summary.FindingsByCategory.Drift++
		}

		switch finding.Severity {
		case RoadmapEntropySeverityLow:
			summary.FindingsBySeverity.Low++
		case RoadmapEntropySeverityMedium:
			summary.FindingsBySeverity.Medium++
		case RoadmapEntropySeverityHigh:
			summary.FindingsBySeverity.High++
		case RoadmapEntropySeverityCritical:
			summary.FindingsBySeverity.Critical++
		}

		if roadmapEntropySeverityRank(finding.Severity) > roadmapEntropySeverityRank(summary.HighestSeverity) {
			summary.HighestSeverity = finding.Severity
		}
	}

	return summary
}

func parseRoadmapEntropySeverity(value string) (RoadmapEntropySeverity, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "", string(RoadmapEntropySeverityNone):
		return RoadmapEntropySeverityNone, nil
	case string(RoadmapEntropySeverityLow):
		return RoadmapEntropySeverityLow, nil
	case string(RoadmapEntropySeverityMedium):
		return RoadmapEntropySeverityMedium, nil
	case string(RoadmapEntropySeverityHigh):
		return RoadmapEntropySeverityHigh, nil
	case string(RoadmapEntropySeverityCritical):
		return RoadmapEntropySeverityCritical, nil
	default:
		return "", fmt.Errorf("unsupported severity %q (expected one of: low, medium, high, critical)", strings.TrimSpace(value))
	}
}

func roadmapEntropyExceedsThreshold(result RoadmapEntropyResult, threshold RoadmapEntropySeverity) bool {
	thresholdRank := roadmapEntropySeverityRank(threshold)
	if thresholdRank <= roadmapEntropySeverityRank(RoadmapEntropySeverityNone) {
		return false
	}
	return roadmapEntropySeverityRank(result.Summary.HighestSeverity) >= thresholdRank
}

func roadmapEntropySeverityRank(severity RoadmapEntropySeverity) int {
	switch severity {
	case RoadmapEntropySeverityNone:
		return 0
	case RoadmapEntropySeverityLow:
		return 1
	case RoadmapEntropySeverityMedium:
		return 2
	case RoadmapEntropySeverityHigh:
		return 3
	case RoadmapEntropySeverityCritical:
		return 4
	default:
		return -1
	}
}

func roadmapEntropySeverityForNewPrimaryDomains(count int) RoadmapEntropySeverity {
	switch {
	case count >= 3:
		return RoadmapEntropySeverityCritical
	case count == 2:
		return RoadmapEntropySeverityHigh
	default:
		return RoadmapEntropySeverityMedium
	}
}

func roadmapEntropySeverityForTouchedDomainExpansion(count int) RoadmapEntropySeverity {
	switch {
	case count >= 3:
		return RoadmapEntropySeverityHigh
	case count == 2:
		return RoadmapEntropySeverityMedium
	default:
		return RoadmapEntropySeverityLow
	}
}

func roadmapEntropySeverityForRatioRise(baselineRatio float64, recentRatio float64, delta float64, recentNonFeatureCount int) (RoadmapEntropySeverity, bool) {
	if recentRatio <= baselineRatio || recentNonFeatureCount == 0 {
		return RoadmapEntropySeverityNone, false
	}
	switch {
	case recentRatio >= 0.90 && delta >= 0.50:
		return RoadmapEntropySeverityCritical, true
	case recentRatio >= 0.75 || delta >= 0.40:
		return RoadmapEntropySeverityHigh, true
	case recentRatio >= 0.60 || delta >= 0.25:
		return RoadmapEntropySeverityMedium, true
	case recentRatio >= 0.40 || delta >= 0.10:
		return RoadmapEntropySeverityLow, true
	default:
		return RoadmapEntropySeverityNone, false
	}
}

func roadmapEntropySeverityForOrphanNonFeature(orphanCount int, orphanResolutionCount int) RoadmapEntropySeverity {
	switch {
	case orphanCount >= 2 && orphanResolutionCount > 0:
		return RoadmapEntropySeverityCritical
	case orphanCount >= 3:
		return RoadmapEntropySeverityHigh
	case orphanCount == 2:
		return RoadmapEntropySeverityMedium
	default:
		return RoadmapEntropySeverityLow
	}
}

func roadmapEntropySpecIDs(specs []Spec) []string {
	ids := make([]string, 0, len(specs))
	for _, spec := range specs {
		id := strings.TrimSpace(spec.ID)
		if id == "" {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

func roadmapEntropyPrimaryDomainSet(specs []Spec) map[string]struct{} {
	out := make(map[string]struct{})
	for _, spec := range specs {
		domain := roadmapEntropyNormalizeDomain(spec.Domain)
		if domain == "" {
			continue
		}
		out[domain] = struct{}{}
	}
	return out
}

func roadmapEntropyTouchedDomainSet(specs []Spec) map[string]struct{} {
	out := make(map[string]struct{})
	for _, spec := range specs {
		for _, domain := range spec.TouchedDomains {
			normalized := roadmapEntropyNormalizeDomain(domain)
			if normalized == "" {
				continue
			}
			out[normalized] = struct{}{}
		}
		primary := roadmapEntropyNormalizeDomain(spec.Domain)
		if primary != "" {
			out[primary] = struct{}{}
		}
	}
	return out
}

func roadmapEntropyNormalizeDomain(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func roadmapEntropySetDiffSorted(left map[string]struct{}, right map[string]struct{}) []string {
	out := make([]string, 0)
	for value := range left {
		if _, exists := right[value]; exists {
			continue
		}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func roadmapEntropyNonFeatureCount(specs []Spec) int {
	count := 0
	for _, spec := range specs {
		if roadmapEntropyIsNonFeature(spec.Type) {
			count++
		}
	}
	return count
}

func roadmapEntropyIsNonFeature(specType string) bool {
	switch strings.ToLower(strings.TrimSpace(specType)) {
	case "technical", "resolution":
		return true
	default:
		return false
	}
}

func roadmapEntropyIsResolution(specType string) bool {
	return strings.EqualFold(strings.TrimSpace(specType), "resolution")
}

func roadmapEntropyRatio(numerator int, denominator int) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func roadmapEntropyFindOrphanNonFeatureRecentSpecs(allSpecs []Spec, recentSpecs []Spec, entries []LedgerEntry) ([]string, int) {
	inboundDepends := make(map[string]int)
	for _, spec := range allSpecs {
		for _, dep := range spec.DependsOn {
			normalized := strings.TrimSpace(dep)
			if normalized == "" {
				continue
			}
			inboundDepends[normalized]++
		}
	}

	ledgerParents := make(map[string]bool)
	ledgerChildren := make(map[string]int)
	for _, entry := range entries {
		specID := strings.TrimSpace(entry.SpecID)
		for _, parent := range entry.Parents {
			normalizedParent := strings.TrimSpace(parent)
			if normalizedParent == "" {
				continue
			}
			ledgerChildren[normalizedParent]++
			if specID != "" {
				ledgerParents[specID] = true
			}
		}
	}

	orphanIDs := make([]string, 0)
	orphanResolutionCount := 0
	for _, spec := range recentSpecs {
		if !roadmapEntropyIsNonFeature(spec.Type) {
			continue
		}
		hasDependsOn := len(spec.DependsOn) > 0
		hasInboundDepends := inboundDepends[spec.ID] > 0
		hasLedgerParents := ledgerParents[spec.ID]
		hasLedgerChildren := ledgerChildren[spec.ID] > 0
		if hasDependsOn || hasInboundDepends || hasLedgerParents || hasLedgerChildren {
			continue
		}
		orphanIDs = append(orphanIDs, spec.ID)
		if roadmapEntropyIsResolution(spec.Type) {
			orphanResolutionCount++
		}
	}

	sort.Strings(orphanIDs)
	return orphanIDs, orphanResolutionCount
}
