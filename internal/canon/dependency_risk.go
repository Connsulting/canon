package canon

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	dependencyRiskCategorySecurity    = "security"
	dependencyRiskCategoryMaintenance = "maintenance"

	dependencyRiskRuleReplaceLocalPath  = "replace-local-path"
	dependencyRiskRuleReplaceModulePath = "replace-module-path"
	dependencyRiskRuleMajorZeroVersion  = "version-major-zero"
	dependencyRiskRulePseudoVersion     = "version-pseudo"
	dependencyRiskRulePrereleaseVersion = "version-prerelease"
	dependencyRiskRuleMissingGoSum      = "missing-go-sum"
)

var pseudoVersionPattern = regexp.MustCompile(`(?:-|\.)\d{14}-[0-9a-fA-F]{12}(?:\+incompatible)?$`)

type goModEditJSON struct {
	Require []goModuleRequirement `json:"Require"`
	Replace []goModuleReplace     `json:"Replace"`
}

type goModuleRequirement struct {
	Path    string `json:"Path"`
	Version string `json:"Version"`
}

type goModuleReplace struct {
	Old goModuleVersion `json:"Old"`
	New goModuleVersion `json:"New"`
}

type goModuleVersion struct {
	Path    string `json:"Path"`
	Version string `json:"Version"`
}

func DependencyRisk(root string, opts DependencyRiskOptions) (DependencyRiskResult, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return DependencyRiskResult{}, err
	}

	goModPath := filepath.Join(absRoot, "go.mod")
	if _, err := os.Stat(goModPath); err != nil {
		if os.IsNotExist(err) {
			return DependencyRiskResult{}, fmt.Errorf("go.mod not found at %s", filepath.ToSlash(goModPath))
		}
		return DependencyRiskResult{}, err
	}

	goSumPath := filepath.Join(absRoot, "go.sum")
	_, goSumErr := os.Stat(goSumPath)
	goSumPresent := goSumErr == nil
	if goSumErr != nil && !os.IsNotExist(goSumErr) {
		return DependencyRiskResult{}, goSumErr
	}

	mod, err := loadGoModuleMetadata(absRoot)
	if err != nil {
		return DependencyRiskResult{}, err
	}

	findings := analyzeDependencyRiskFindings(mod, goSumPresent)
	sortDependencyRiskFindings(findings)

	result := DependencyRiskResult{
		Root:            absRoot,
		GoModPath:       goModPath,
		GoSumPath:       goSumPath,
		GoSumPresent:    goSumPresent,
		DependencyCount: len(mod.Require),
		Findings:        findings,
		Summary:         summarizeDependencyRiskFindings(findings),
	}

	if strings.TrimSpace(string(opts.FailOn)) != "" {
		failOn, err := parseDependencyRiskSeverity(string(opts.FailOn))
		if err != nil {
			return DependencyRiskResult{}, err
		}
		if failOn != DependencyRiskSeverityNone {
			result.FailOn = failOn
			result.ThresholdExceeded = dependencyRiskExceedsThreshold(result, failOn)
		}
	}

	return result, nil
}

func loadGoModuleMetadata(root string) (goModEditJSON, error) {
	cmd := exec.Command("go", "mod", "edit", "-json")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		return goModEditJSON{}, fmt.Errorf("go mod edit -json failed: %w\n%s", err, strings.TrimSpace(string(output)))
	}

	var payload goModEditJSON
	if err := json.Unmarshal(output, &payload); err != nil {
		return goModEditJSON{}, fmt.Errorf("decode go.mod metadata: %w", err)
	}

	return payload, nil
}

func analyzeDependencyRiskFindings(mod goModEditJSON, goSumPresent bool) []DependencyRiskFinding {
	findings := make([]DependencyRiskFinding, 0)

	for _, item := range mod.Replace {
		oldPath := strings.TrimSpace(item.Old.Path)
		newPath := strings.TrimSpace(item.New.Path)
		if oldPath == "" || newPath == "" {
			continue
		}

		if isLocalModuleReplacePath(newPath) {
			replacement := newPath
			if strings.TrimSpace(item.New.Version) != "" {
				replacement = newPath + "@" + strings.TrimSpace(item.New.Version)
			}
			findings = append(findings, DependencyRiskFinding{
				RuleID:   dependencyRiskRuleReplaceLocalPath,
				Category: dependencyRiskCategorySecurity,
				Severity: DependencyRiskSeverityHigh,
				Module:   oldPath,
				Version:  strings.TrimSpace(item.Old.Version),
				Replace:  replacement,
				Message:  fmt.Sprintf("module %s is replaced by local path %s", oldPath, replacement),
			})
			continue
		}

		if !strings.EqualFold(oldPath, newPath) {
			replacement := newPath
			if strings.TrimSpace(item.New.Version) != "" {
				replacement = newPath + "@" + strings.TrimSpace(item.New.Version)
			}
			findings = append(findings, DependencyRiskFinding{
				RuleID:   dependencyRiskRuleReplaceModulePath,
				Category: dependencyRiskCategorySecurity,
				Severity: DependencyRiskSeverityMedium,
				Module:   oldPath,
				Version:  strings.TrimSpace(item.Old.Version),
				Replace:  replacement,
				Message:  fmt.Sprintf("module %s is replaced by alternate module path %s", oldPath, replacement),
			})
		}
	}

	for _, item := range mod.Require {
		modulePath := strings.TrimSpace(item.Path)
		version := strings.TrimSpace(item.Version)
		if modulePath == "" || version == "" {
			continue
		}

		if strings.HasPrefix(version, "v0.") {
			findings = append(findings, DependencyRiskFinding{
				RuleID:   dependencyRiskRuleMajorZeroVersion,
				Category: dependencyRiskCategoryMaintenance,
				Severity: DependencyRiskSeverityMedium,
				Module:   modulePath,
				Version:  version,
				Message:  fmt.Sprintf("module %s uses major-zero version %s", modulePath, version),
			})
		}

		if isPseudoVersion(version) {
			findings = append(findings, DependencyRiskFinding{
				RuleID:   dependencyRiskRulePseudoVersion,
				Category: dependencyRiskCategoryMaintenance,
				Severity: DependencyRiskSeverityMedium,
				Module:   modulePath,
				Version:  version,
				Message:  fmt.Sprintf("module %s uses pseudo-version %s", modulePath, version),
			})
		}

		if isPrereleaseVersion(version) {
			findings = append(findings, DependencyRiskFinding{
				RuleID:   dependencyRiskRulePrereleaseVersion,
				Category: dependencyRiskCategoryMaintenance,
				Severity: DependencyRiskSeverityLow,
				Module:   modulePath,
				Version:  version,
				Message:  fmt.Sprintf("module %s uses pre-release version %s", modulePath, version),
			})
		}
	}

	if len(mod.Require) > 0 && !goSumPresent {
		findings = append(findings, DependencyRiskFinding{
			RuleID:   dependencyRiskRuleMissingGoSum,
			Category: dependencyRiskCategorySecurity,
			Severity: DependencyRiskSeverityHigh,
			Message:  "go.sum is missing while go.mod declares required modules",
		})
	}

	return findings
}

func sortDependencyRiskFindings(findings []DependencyRiskFinding) {
	sort.Slice(findings, func(i, j int) bool {
		left := findings[i]
		right := findings[j]

		leftRank := dependencyRiskSeverityRank(left.Severity)
		rightRank := dependencyRiskSeverityRank(right.Severity)
		if leftRank != rightRank {
			return leftRank > rightRank
		}
		if left.Category != right.Category {
			return left.Category < right.Category
		}
		if left.Module != right.Module {
			return left.Module < right.Module
		}
		if left.RuleID != right.RuleID {
			return left.RuleID < right.RuleID
		}
		if left.Version != right.Version {
			return left.Version < right.Version
		}
		return left.Replace < right.Replace
	})
}

func summarizeDependencyRiskFindings(findings []DependencyRiskFinding) DependencyRiskSummary {
	summary := DependencyRiskSummary{
		TotalFindings:      len(findings),
		HighestSeverity:    DependencyRiskSeverityNone,
		FindingsBySeverity: DependencyRiskSeverityCounts{},
	}

	for _, finding := range findings {
		switch finding.Category {
		case dependencyRiskCategorySecurity:
			summary.SecurityFindings++
		case dependencyRiskCategoryMaintenance:
			summary.MaintenanceFindings++
		}

		switch finding.Severity {
		case DependencyRiskSeverityLow:
			summary.FindingsBySeverity.Low++
		case DependencyRiskSeverityMedium:
			summary.FindingsBySeverity.Medium++
		case DependencyRiskSeverityHigh:
			summary.FindingsBySeverity.High++
		case DependencyRiskSeverityCritical:
			summary.FindingsBySeverity.Critical++
		}

		if dependencyRiskSeverityRank(finding.Severity) > dependencyRiskSeverityRank(summary.HighestSeverity) {
			summary.HighestSeverity = finding.Severity
		}
	}

	return summary
}

func parseDependencyRiskSeverity(value string) (DependencyRiskSeverity, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "", string(DependencyRiskSeverityNone):
		return DependencyRiskSeverityNone, nil
	case string(DependencyRiskSeverityLow):
		return DependencyRiskSeverityLow, nil
	case string(DependencyRiskSeverityMedium):
		return DependencyRiskSeverityMedium, nil
	case string(DependencyRiskSeverityHigh):
		return DependencyRiskSeverityHigh, nil
	case string(DependencyRiskSeverityCritical):
		return DependencyRiskSeverityCritical, nil
	default:
		return "", fmt.Errorf("unsupported severity %q (expected one of: low, medium, high, critical)", strings.TrimSpace(value))
	}
}

func dependencyRiskExceedsThreshold(result DependencyRiskResult, threshold DependencyRiskSeverity) bool {
	thresholdRank := dependencyRiskSeverityRank(threshold)
	if thresholdRank <= dependencyRiskSeverityRank(DependencyRiskSeverityNone) {
		return false
	}
	return dependencyRiskSeverityRank(result.Summary.HighestSeverity) >= thresholdRank
}

func dependencyRiskSeverityRank(severity DependencyRiskSeverity) int {
	switch severity {
	case DependencyRiskSeverityNone:
		return 0
	case DependencyRiskSeverityLow:
		return 1
	case DependencyRiskSeverityMedium:
		return 2
	case DependencyRiskSeverityHigh:
		return 3
	case DependencyRiskSeverityCritical:
		return 4
	default:
		return -1
	}
}

func isPseudoVersion(version string) bool {
	return pseudoVersionPattern.MatchString(strings.TrimSpace(version))
}

func isPrereleaseVersion(version string) bool {
	trimmed := strings.TrimSpace(version)
	if trimmed == "" || isPseudoVersion(trimmed) {
		return false
	}
	return strings.Contains(trimmed, "-")
}

func isLocalModuleReplacePath(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, "./") ||
		strings.HasPrefix(trimmed, "../") ||
		strings.HasPrefix(trimmed, ".\\") ||
		strings.HasPrefix(trimmed, "..\\") ||
		strings.HasPrefix(trimmed, "/") ||
		strings.HasPrefix(trimmed, "\\") ||
		strings.HasPrefix(trimmed, "~/") {
		return true
	}
	if len(trimmed) >= 2 && trimmed[1] == ':' && isASCIIAlpha(trimmed[0]) {
		return true
	}
	return false
}

func isASCIIAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
