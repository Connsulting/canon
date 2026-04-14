package canon

import (
	"fmt"
	"sort"
	"strings"
)

var requiredProductRequirementSections = []string{
	"Problem statement",
	"Proposed solution",
	"Success criteria",
	"Scope boundaries",
	"Testability notes",
}

func ProductRequirementReadinessGaps(spec Spec) []CheckReadinessGap {
	if !strings.EqualFold(strings.TrimSpace(spec.RequirementKind), "product") {
		return nil
	}

	gaps := make([]CheckReadinessGap, 0)
	addGap := func(field string, message string) {
		gaps = append(gaps, CheckReadinessGap{
			SpecID:  spec.ID,
			Title:   spec.Title,
			Field:   field,
			Message: message,
		})
	}

	if strings.TrimSpace(spec.SourceIssue) == "" {
		addGap("source_issue", "source_issue is required for product requirements")
	}
	if !strings.EqualFold(strings.TrimSpace(spec.ApprovalState), "approved") {
		addGap("approval_state", "approval_state must be approved for product requirements")
	}

	sections := markdownSectionSet(spec.Body)
	for _, section := range requiredProductRequirementSections {
		if _, ok := sections[canonicalSectionName(section)]; !ok {
			addGap("section:"+section, "missing required section: "+section)
		}
	}

	return gaps
}

func collectReadinessGaps(specs []Spec) []CheckReadinessGap {
	gaps := make([]CheckReadinessGap, 0)
	for _, spec := range specs {
		gaps = append(gaps, ProductRequirementReadinessGaps(spec)...)
	}
	sort.Slice(gaps, func(i, j int) bool {
		if gaps[i].SpecID == gaps[j].SpecID {
			return gaps[i].Field < gaps[j].Field
		}
		return gaps[i].SpecID < gaps[j].SpecID
	})
	return gaps
}

func markdownSectionSet(body string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		trimmed = strings.TrimLeft(trimmed, "#")
		trimmed = strings.TrimSpace(strings.TrimRight(trimmed, "#"))
		name := canonicalSectionName(trimmed)
		if name == "" {
			continue
		}
		out[name] = struct{}{}
	}
	return out
}

func canonicalSectionName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func formatReadinessGapMessages(gaps []CheckReadinessGap) string {
	parts := make([]string, 0, len(gaps))
	for _, gap := range gaps {
		if strings.TrimSpace(gap.Message) != "" {
			if strings.TrimSpace(gap.SpecID) != "" {
				parts = append(parts, fmt.Sprintf("%s: %s", gap.SpecID, gap.Message))
			} else {
				parts = append(parts, gap.Message)
			}
		}
	}
	return strings.Join(parts, "; ")
}
