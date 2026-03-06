package canon

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
)

const (
	roadmapEntropyDefaultWindow = 8

	roadmapEntropyCategoryScopeCreep = "scope_creep"
	roadmapEntropyCategoryDrift      = "drift"

	roadmapEntropyRuleNewDomains             = "new-domains"
	roadmapEntropyRuleTouchedDomainExpansion = "touched-domain-expansion"
	roadmapEntropyRuleNonFeatureRatioRise    = "non-feature-ratio-rise"
	roadmapEntropyRuleOrphanNonFeatureSpecs  = "orphan-non-feature-specs"
)

type roadmapEntropyRelationshipIndex struct {
	hasIncomingDependsOn map[string]bool
	hasOutgoingDependsOn map[string]bool
	hasIncomingParents   map[string]bool
	hasOutgoingParents   map[string]bool
}

type roadmapEntropyWindowMetrics struct {
	Summary                 RoadmapEntropyWindowSummary
	DomainSet               map[string]struct{}
	TouchedDomainSet        map[string]struct{}
	OrphanNonFeatureSpecIDs []string
	OrphanNonFeatureDomains []string
	HasOrphanResolution     bool
}

func RoadmapEntropy(root string, opts RoadmapEntropyOptions) (RoadmapEntropyResult, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return RoadmapEntropyResult{}, err
	}

	window := opts.Window
	if window == 0 {
		window = roadmapEntropyDefaultWindow
	}
	if window <= 0 {
		return RoadmapEntropyResult{}, fmt.Errorf("window must be greater than zero (got %d)", window)
	}

	specs, err := loadSpecs(absRoot)
	if err != nil {
		return RoadmapEntropyResult{}, err
	}

	entries, err := LoadLedger(absRoot)
	if err != nil {
		return RoadmapEntropyResult{}, err
	}

	orderedSpecs := orderRoadmapEntropySpecs(specs, entries)
	maxAnalyzed := window * 2
	if maxAnalyzed > len(orderedSpecs) {
		maxAnalyzed = len(orderedSpecs)
	}
	analyzedSpecs := orderedSpecs[:maxAnalyzed]

	recentEnd := window
	if recentEnd > len(analyzedSpecs) {
		recentEnd = len(analyzedSpecs)
	}
	baselineEnd := recentEnd + window
	if baselineEnd > len(analyzedSpecs) {
		baselineEnd = len(analyzedSpecs)
	}

	recentSpecs := analyzedSpecs[:recentEnd]
	baselineSpecs := analyzedSpecs[recentEnd:baselineEnd]

	relationships := buildRoadmapEntropyRelationshipIndex(specs, entries)
	recentMetrics := summarizeRoadmapEntropyWindow(recentSpecs, relationships)
	baselineMetrics := summarizeRoadmapEntropyWindow(baselineSpecs, relationships)

	findings := analyzeRoadmapEntropyFindings(recentMetrics, baselineMetrics)
	sortRoadmapEntropyFindings(findings)

	result := RoadmapEntropyResult{
		Root:           absRoot,
		Window:         window,
		LedgerEntries:  len(entries),
		SpecsAnalyzed:  len(analyzedSpecs),
		RecentWindow:   recentMetrics.Summary,
		BaselineWindow: baselineMetrics.Summary,
		Findings:       findings,
		Summary:        summarizeRoadmapEntropyFindings(findings),
	}

	if strings.TrimSpace(string(opts.FailOn)) != "" {
		failOn, err := parseRoadmapEntropySeverity(string(opts.FailOn))
		if err != nil {
			return RoadmapEntropyResult{}, err
		}
		if failOn != RoadmapEntropySeverityNone {
			result.FailOn = failOn
			result.ThresholdExceeded = roadmapEntropyExceedsThreshold(result, failOn)
		}
	}

	return result, nil
}

func orderRoadmapEntropySpecs(specs []Spec, entries []LedgerEntry) []Spec {
	if len(specs) == 0 {
		return nil
	}

	specByID := make(map[string]Spec, len(specs))
	for _, spec := range specs {
		specByID[strings.TrimSpace(spec.ID)] = spec
	}

	ordered := make([]Spec, 0, len(specs))
	seen := make(map[string]struct{}, len(specs))

	for _, entry := range entries {
		specID := strings.TrimSpace(entry.SpecID)
		if specID == "" {
			continue
		}
		if _, exists := seen[specID]; exists {
			continue
		}
		spec, ok := specByID[specID]
		if !ok {
			continue
		}
		seen[specID] = struct{}{}
		ordered = append(ordered, spec)
	}

	remaining := make([]Spec, 0, len(specs)-len(ordered))
	for _, spec := range specs {
		specID := strings.TrimSpace(spec.ID)
		if _, exists := seen[specID]; exists {
			continue
		}
		remaining = append(remaining, spec)
	}
	sortSpecsStable(remaining)
	for i := len(remaining) - 1; i >= 0; i-- {
		ordered = append(ordered, remaining[i])
	}

	return ordered
}

func buildRoadmapEntropyRelationshipIndex(specs []Spec, entries []LedgerEntry) roadmapEntropyRelationshipIndex {
	relationships := roadmapEntropyRelationshipIndex{
		hasIncomingDependsOn: make(map[string]bool),
		hasOutgoingDependsOn: make(map[string]bool),
		hasIncomingParents:   make(map[string]bool),
		hasOutgoingParents:   make(map[string]bool),
	}

	for _, spec := range specs {
		specID := strings.TrimSpace(spec.ID)
		if specID == "" {
			continue
		}

		dependsOn := normalizeList(spec.DependsOn)
		if len(dependsOn) > 0 {
			relationships.hasOutgoingDependsOn[specID] = true
		}
		for _, dep := range dependsOn {
			parentID := strings.TrimSpace(dep)
			if parentID == "" {
				continue
			}
			relationships.hasIncomingDependsOn[parentID] = true
		}
	}

	for _, entry := range entries {
		specID := strings.TrimSpace(entry.SpecID)
		hasParent := false
		seenParents := make(map[string]struct{})
		for _, rawParent := range entry.Parents {
			parentID := strings.TrimSpace(rawParent)
			if parentID == "" {
				continue
			}
			if _, exists := seenParents[parentID]; exists {
				continue
			}
			seenParents[parentID] = struct{}{}
			hasParent = true
			relationships.hasIncomingParents[parentID] = true
		}
		if specID != "" && hasParent {
			relationships.hasOutgoingParents[specID] = true
		}
	}

	return relationships
}

func summarizeRoadmapEntropyWindow(specs []Spec, relationships roadmapEntropyRelationshipIndex) roadmapEntropyWindowMetrics {
	metrics := roadmapEntropyWindowMetrics{
		Summary: RoadmapEntropyWindowSummary{
			Specs: len(specs),
		},
		DomainSet:               make(map[string]struct{}),
		TouchedDomainSet:        make(map[string]struct{}),
		OrphanNonFeatureSpecIDs: make([]string, 0),
		OrphanNonFeatureDomains: make([]string, 0),
	}

	orphanDomains := make(map[string]struct{})

	for _, spec := range specs {
		specID := strings.TrimSpace(spec.ID)
		domain := strings.TrimSpace(spec.Domain)
		specType := strings.ToLower(strings.TrimSpace(spec.Type))

		if domain != "" {
			metrics.DomainSet[domain] = struct{}{}
			metrics.TouchedDomainSet[domain] = struct{}{}
		}
		for _, touched := range spec.TouchedDomains {
			touchedDomain := strings.TrimSpace(touched)
			if touchedDomain == "" {
				continue
			}
			metrics.TouchedDomainSet[touchedDomain] = struct{}{}
		}

		switch specType {
		case "feature":
			metrics.Summary.FeatureSpecs++
		case "technical":
			metrics.Summary.TechnicalSpecs++
		case "resolution":
			metrics.Summary.ResolutionSpecs++
		}

		if !isRoadmapEntropyNonFeatureSpecType(specType) {
			continue
		}

		metrics.Summary.NonFeatureSpecs++
		if !roadmapEntropyIsOrphanNonFeatureSpec(specID, relationships) {
			continue
		}

		metrics.OrphanNonFeatureSpecIDs = append(metrics.OrphanNonFeatureSpecIDs, specID)
		if domain != "" {
			orphanDomains[domain] = struct{}{}
		}
		if specType == "resolution" {
			metrics.HasOrphanResolution = true
		}
	}

	sort.Strings(metrics.OrphanNonFeatureSpecIDs)
	metrics.OrphanNonFeatureDomains = roadmapEntropySortedSetKeys(orphanDomains)

	metrics.Summary.OrphanNonFeatureSpecs = len(metrics.OrphanNonFeatureSpecIDs)
	metrics.Summary.UniqueDomains = len(metrics.DomainSet)
	metrics.Summary.UniqueTouchedDomains = len(metrics.TouchedDomainSet)
	metrics.Summary.NonFeatureRatio = roadmapEntropyRoundedFraction(metrics.Summary.NonFeatureSpecs, metrics.Summary.Specs)

	return metrics
}

func analyzeRoadmapEntropyFindings(recent roadmapEntropyWindowMetrics, baseline roadmapEntropyWindowMetrics) []RoadmapEntropyFinding {
	findings := make([]RoadmapEntropyFinding, 0, 4)

	newDomains := roadmapEntropySortedSetDifference(recent.DomainSet, baseline.DomainSet)
	if len(newDomains) > 0 {
		findings = append(findings, RoadmapEntropyFinding{
			RuleID:   roadmapEntropyRuleNewDomains,
			Category: roadmapEntropyCategoryScopeCreep,
			Severity: roadmapEntropySeverityForNewDomains(len(newDomains)),
			Domains:  newDomains,
			Message:  fmt.Sprintf("recent window introduced %d new primary domain(s) vs baseline: %s", len(newDomains), strings.Join(newDomains, ", ")),
		})
	}

	touchedExpansion := roadmapEntropySortedSetDifference(recent.TouchedDomainSet, baseline.TouchedDomainSet)
	if len(touchedExpansion) > 0 {
		findings = append(findings, RoadmapEntropyFinding{
			RuleID:   roadmapEntropyRuleTouchedDomainExpansion,
			Category: roadmapEntropyCategoryScopeCreep,
			Severity: roadmapEntropySeverityForTouchedExpansion(len(touchedExpansion)),
			Domains:  touchedExpansion,
			Message:  fmt.Sprintf("recent window touched %d domain(s) absent from baseline: %s", len(touchedExpansion), strings.Join(touchedExpansion, ", ")),
		})
	}

	recentRatio := roadmapEntropyFraction(recent.Summary.NonFeatureSpecs, recent.Summary.Specs)
	baselineRatio := roadmapEntropyFraction(baseline.Summary.NonFeatureSpecs, baseline.Summary.Specs)
	ratioDelta := recentRatio - baselineRatio
	if ratioDelta > 0 {
		findings = append(findings, RoadmapEntropyFinding{
			RuleID:   roadmapEntropyRuleNonFeatureRatioRise,
			Category: roadmapEntropyCategoryDrift,
			Severity: roadmapEntropySeverityForNonFeatureRatioRise(ratioDelta, recentRatio),
			Message: fmt.Sprintf(
				"non-feature ratio increased from %.2f to %.2f (delta=%.2f)",
				baselineRatio,
				recentRatio,
				ratioDelta,
			),
		})
	}

	orphanGrowth := recent.Summary.OrphanNonFeatureSpecs - baseline.Summary.OrphanNonFeatureSpecs
	if recent.Summary.OrphanNonFeatureSpecs > 0 && (orphanGrowth > 0 || recent.HasOrphanResolution) {
		findings = append(findings, RoadmapEntropyFinding{
			RuleID:   roadmapEntropyRuleOrphanNonFeatureSpecs,
			Category: roadmapEntropyCategoryDrift,
			Severity: roadmapEntropySeverityForOrphanNonFeatureSpecs(orphanGrowth, recent.HasOrphanResolution),
			Domains:  append([]string(nil), recent.OrphanNonFeatureDomains...),
			SpecIDs:  append([]string(nil), recent.OrphanNonFeatureSpecIDs...),
			Message: fmt.Sprintf(
				"recent window has %d orphan technical/resolution spec(s) (baseline=%d, delta=%+d, orphan_resolution=%t)",
				recent.Summary.OrphanNonFeatureSpecs,
				baseline.Summary.OrphanNonFeatureSpecs,
				orphanGrowth,
				recent.HasOrphanResolution,
			),
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

		leftDomains := strings.Join(left.Domains, ",")
		rightDomains := strings.Join(right.Domains, ",")
		if leftDomains != rightDomains {
			return leftDomains < rightDomains
		}

		leftSpecs := strings.Join(left.SpecIDs, ",")
		rightSpecs := strings.Join(right.SpecIDs, ",")
		if leftSpecs != rightSpecs {
			return leftSpecs < rightSpecs
		}

		return left.Message < right.Message
	})
}

func summarizeRoadmapEntropyFindings(findings []RoadmapEntropyFinding) RoadmapEntropySummary {
	summary := RoadmapEntropySummary{
		TotalFindings:      len(findings),
		HighestSeverity:    RoadmapEntropySeverityNone,
		FindingsBySeverity: RoadmapEntropySeverityCounts{},
	}

	for _, finding := range findings {
		switch finding.Category {
		case roadmapEntropyCategoryScopeCreep:
			summary.ScopeCreepFindings++
		case roadmapEntropyCategoryDrift:
			summary.DriftFindings++
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

func roadmapEntropySeverityForNewDomains(newDomainCount int) RoadmapEntropySeverity {
	switch {
	case newDomainCount >= 4:
		return RoadmapEntropySeverityCritical
	case newDomainCount >= 2:
		return RoadmapEntropySeverityHigh
	default:
		return RoadmapEntropySeverityMedium
	}
}

func roadmapEntropySeverityForTouchedExpansion(expansionCount int) RoadmapEntropySeverity {
	switch {
	case expansionCount >= 4:
		return RoadmapEntropySeverityHigh
	case expansionCount >= 2:
		return RoadmapEntropySeverityMedium
	default:
		return RoadmapEntropySeverityLow
	}
}

func roadmapEntropySeverityForNonFeatureRatioRise(delta float64, recentRatio float64) RoadmapEntropySeverity {
	switch {
	case delta >= 0.75 || recentRatio >= 0.90:
		return RoadmapEntropySeverityCritical
	case delta >= 0.35 || recentRatio >= 0.65:
		return RoadmapEntropySeverityHigh
	case delta >= 0.15 || recentRatio >= 0.45:
		return RoadmapEntropySeverityMedium
	default:
		return RoadmapEntropySeverityLow
	}
}

func roadmapEntropySeverityForOrphanNonFeatureSpecs(growth int, hasOrphanResolution bool) RoadmapEntropySeverity {
	switch {
	case growth >= 4:
		return RoadmapEntropySeverityCritical
	case growth >= 3:
		if hasOrphanResolution {
			return RoadmapEntropySeverityCritical
		}
		return RoadmapEntropySeverityHigh
	case growth == 1:
		return RoadmapEntropySeverityMedium
	case growth >= 2:
		return RoadmapEntropySeverityMedium
	case hasOrphanResolution:
		return RoadmapEntropySeverityHigh
	default:
		return RoadmapEntropySeverityLow
	}
}

func roadmapEntropyIsOrphanNonFeatureSpec(specID string, relationships roadmapEntropyRelationshipIndex) bool {
	specID = strings.TrimSpace(specID)
	if specID == "" {
		return false
	}
	if relationships.hasOutgoingDependsOn[specID] || relationships.hasIncomingDependsOn[specID] {
		return false
	}
	if relationships.hasOutgoingParents[specID] || relationships.hasIncomingParents[specID] {
		return false
	}
	return true
}

func isRoadmapEntropyNonFeatureSpecType(specType string) bool {
	switch strings.ToLower(strings.TrimSpace(specType)) {
	case "technical", "resolution":
		return true
	default:
		return false
	}
}

func roadmapEntropySortedSetDifference(left map[string]struct{}, right map[string]struct{}) []string {
	if len(left) == 0 {
		return nil
	}
	diff := make([]string, 0, len(left))
	for key := range left {
		if _, exists := right[key]; exists {
			continue
		}
		diff = append(diff, key)
	}
	sort.Strings(diff)
	return diff
}

func roadmapEntropySortedSetKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func roadmapEntropyFraction(part int, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(part) / float64(total)
}

func roadmapEntropyRoundedFraction(part int, total int) float64 {
	ratio := roadmapEntropyFraction(part, total)
	return math.Round(ratio*10000) / 10000
}
