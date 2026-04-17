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

type GCInput struct {
	Domain       string
	SpecIDs      []string
	MinSpecs     int
	Write        bool
	Force        bool
	AIMode       string
	AIProvider   string
	ResponseFile string
}

type GCResult struct {
	ScopeType    string
	ScopeValue   string
	TargetSpecs  []Spec
	Consolidated []Spec
	ExternalDeps []string
	Skip         bool
	SkipReason   string
}

func GC(root string, input GCInput) (GCResult, error) {
	domain := strings.TrimSpace(input.Domain)
	specIDs := normalizeList(input.SpecIDs)
	mode := strings.ToLower(strings.TrimSpace(input.AIMode))
	if mode == "" {
		mode = "auto"
	}
	if strings.TrimSpace(input.ResponseFile) != "" && mode == "auto" {
		mode = "from-response"
	}

	minSpecs := input.MinSpecs
	if minSpecs <= 0 {
		minSpecs = 5
	}

	if strings.TrimSpace(input.AIProvider) == "" {
		input.AIProvider = "codex"
	}

	if domain != "" && len(specIDs) > 0 {
		return GCResult{}, fmt.Errorf("use either --domain or --specs, not both")
	}
	if domain == "" && len(specIDs) == 0 {
		return GCResult{}, fmt.Errorf("must provide --domain or --specs")
	}

	if err := EnsureLayout(root, input.Write); err != nil {
		return GCResult{}, err
	}

	allSpecs, err := loadSpecs(root)
	if err != nil {
		return GCResult{}, err
	}

	scopeType, scopeValue := "specs", strings.Join(specIDs, ",")
	selected, err := gcSelectTargetSpecs(allSpecs, domain, specIDs)
	if err != nil {
		return GCResult{}, err
	}
	if domain != "" {
		scopeType = "domain"
		scopeValue = domain
	}
	result := GCResult{
		ScopeType:   scopeType,
		ScopeValue:  scopeValue,
		TargetSpecs: selected,
	}

	if len(result.TargetSpecs) == 0 {
		return GCResult{}, fmt.Errorf("no specs found for gc scope %s %s", scopeType, scopeValue)
	}
	if len(result.TargetSpecs) < minSpecs && !input.Force {
		result.Skip = true
		result.SkipReason = fmt.Sprintf("not enough specs for gc in %s %s: %d < %d (use --force to run)", scopeType, scopeValue, len(result.TargetSpecs), minSpecs)
		return result, nil
	}

	conflicts := detectGCScopeConflicts(result.TargetSpecs)
	if len(conflicts) > 0 {
		return GCResult{}, fmt.Errorf("unresolved semantic conflicts in gc scope: %s", strings.Join(conflicts, "; "))
	}

	activeIDs := make(map[string]struct{}, len(allSpecs))
	for _, spec := range allSpecs {
		activeIDs[spec.ID] = struct{}{}
	}
	consolidated, err := gcBuildConsolidatedSpecs(root, result.TargetSpecs, activeIDs, mode, input.AIProvider, input.ResponseFile)
	if err != nil {
		return GCResult{}, err
	}
	result.Consolidated = consolidated

	result.ExternalDeps = gcExternalDependencies(result.Consolidated)
	if !input.Write {
		return result, nil
	}

	parentHeads, err := gcCurrentLedgerParents(root)
	if err != nil {
		return GCResult{}, err
	}
	if err := gcWriteConsolidatedSpecs(root, result.Consolidated, parentHeads, nowUTC()); err != nil {
		return GCResult{}, err
	}
	if err := gcArchiveTargetSpecs(root, result.TargetSpecs); err != nil {
		return GCResult{}, err
	}
	return result, nil
}

func gcSelectTargetSpecs(all []Spec, domain string, specIDs []string) ([]Spec, error) {
	if len(specIDs) > 0 {
		requested := make(map[string]struct{}, len(specIDs))
		for _, specID := range specIDs {
			requested[specID] = struct{}{}
		}
		out := make([]Spec, 0, len(specIDs))
		seen := make(map[string]struct{})
		for _, spec := range all {
			if _, ok := requested[spec.ID]; ok {
				out = append(out, spec)
				seen[spec.ID] = struct{}{}
			}
		}
		missing := make([]string, 0)
		for _, specID := range specIDs {
			if _, ok := seen[specID]; !ok {
				missing = append(missing, specID)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			return nil, fmt.Errorf("unknown spec ids for --specs: %s", strings.Join(missing, ", "))
		}
		sortSpecsStable(out)
		return out, nil
	}

	target := strings.ToLower(domain)
	out := make([]Spec, 0)
	for _, spec := range all {
		if strings.ToLower(strings.TrimSpace(spec.Domain)) == target {
			out = append(out, spec)
		}
	}
	sortSpecsStable(out)
	return out, nil
}

func detectGCScopeConflicts(specs []Spec) []string {
	conflicts := make([]string, 0)
	seen := make(map[string]struct{})

	for i, candidate := range specs {
		others := make([]Spec, 0, len(specs)-1)
		for j, other := range specs {
			if i == j {
				continue
			}
			others = append(others, other)
		}
		for _, conflict := range detectSpecConflicts(others, candidate) {
			left := strings.TrimSpace(conflict.ExistingSpecID)
			right := strings.TrimSpace(conflict.CandidateSpec)
			if left > right {
				left, right = right, left
			}
			key := left + "|" + right + "|" + conflict.StatementKey
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			conflicts = append(conflicts, fmt.Sprintf("%s conflicts with %s on %s", left, right, conflict.StatementKey))
		}
	}

	sort.Strings(conflicts)
	return conflicts
}

func gcBuildConsolidatedSpecs(root string, targetSpecs []Spec, activeIDs map[string]struct{}, mode string, aiProvider string, responseFile string) ([]Spec, error) {
	draftSpecs, err := gcRunAIConsolidation(root, targetSpecs, mode, aiProvider, responseFile)
	if err != nil {
		return nil, err
	}

	targetByID := make(map[string]Spec, len(targetSpecs))
	for _, spec := range targetSpecs {
		targetByID[spec.ID] = spec
	}
	targetSet := make(map[string]struct{}, len(targetSpecs))
	for id := range targetByID {
		targetSet[id] = struct{}{}
	}
	seenSpecIDs := make(map[string]struct{}, len(targetByID))
	covered := make(map[string]struct{}, len(targetByID))
	now := nowUTC()

	out := make([]Spec, 0, len(draftSpecs))
	for i, draft := range draftSpecs {
		consolidates := normalizeList(draft.Consolidates)
		if len(consolidates) == 0 {
			return nil, fmt.Errorf("consolidated spec %d is missing consolidates", i+1)
		}

		for _, sourceID := range consolidates {
			if _, ok := targetSet[sourceID]; !ok {
				return nil, fmt.Errorf("consolidated spec %d references unknown source spec %s", i+1, sourceID)
			}
			if _, ok := covered[sourceID]; ok {
				return nil, fmt.Errorf("source spec %s is consolidated multiple times", sourceID)
			}
			covered[sourceID] = struct{}{}
		}

		depSet := make(map[string]struct{})
		for _, sourceID := range consolidates {
			sourceSpec := targetByID[sourceID]
			for _, dep := range sourceSpec.DependsOn {
				if _, inScope := targetSet[dep]; inScope {
					continue
				}
				if dep != "" {
					depSet[dep] = struct{}{}
				}
			}
		}
		for _, dep := range draft.DependsOn {
			if _, inScope := targetSet[dep]; inScope {
				continue
			}
			if dep != "" {
				depSet[dep] = struct{}{}
			}
		}
		depList := make([]string, 0, len(depSet))
		for dep := range depSet {
			depList = append(depList, dep)
		}

		title := strings.TrimSpace(draft.Title)
		domain := strings.TrimSpace(draft.Domain)
		if domain == "" {
			domain = strings.TrimSpace(targetByID[consolidates[0]].Domain)
		}
		body := strings.TrimSpace(draft.Body)
		if body == "" {
			body = "No consolidated body content."
		}
		specID := strings.TrimSpace(draft.ID)
		if specID == "" {
			specID = generatedSpecID(now.Add(time.Duration(i)*time.Second), title)
		}
		if _, exists := activeIDs[specID]; exists {
			specID = generatedSpecID(now.Add(time.Duration(i)*time.Second), title)
		}
		if _, exists := seenSpecIDs[specID]; exists {
			specID = generatedSpecID(now.Add(time.Duration(i)*time.Second), title)
		}
		for {
			if _, activeExists := activeIDs[specID]; activeExists {
				now = now.Add(time.Nanosecond)
				specID = generatedSpecID(now, title)
				continue
			}
			if _, seenExists := seenSpecIDs[specID]; seenExists {
				now = now.Add(time.Nanosecond)
				specID = generatedSpecID(now, title)
				continue
			}
			break
		}
		seenSpecIDs[specID] = struct{}{}
		activeIDs[specID] = struct{}{}

		spec := Spec{
			ID:              specID,
			Type:            strings.TrimSpace(draft.Type),
			Title:           title,
			Domain:          domain,
			Created:         strings.TrimSpace(draft.Created),
			RequirementKind: strings.TrimSpace(draft.RequirementKind),
			SourceIssue:     strings.TrimSpace(draft.SourceIssue),
			ApprovalState:   strings.TrimSpace(draft.ApprovalState),
			DependsOn:       depList,
			TouchedDomains:  normalizeList(draft.TouchedDomains),
			Consolidates:    consolidates,
			Body:            body,
		}
		if spec.Created == "" {
			spec.Created = now.Format(timeRFC3339)
		}
		spec, err = normalizeSpecDefaults(spec)
		if err != nil {
			return nil, fmt.Errorf("consolidated spec %q invalid: %w", specID, err)
		}
		out = append(out, spec)
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("ai returned no consolidated specs")
	}
	if len(covered) != len(targetSpecs) {
		missing := make([]string, 0)
		for id := range targetSet {
			if _, ok := covered[id]; !ok {
				missing = append(missing, id)
			}
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("ai response did not consolidate all target specs: missing %s", strings.Join(missing, ", "))
	}

	sortSpecsStable(out)
	return out, nil
}

func gcCurrentLedgerParents(root string) ([]string, error) {
	entries, err := LoadLedger(root)
	if err != nil {
		return nil, err
	}
	return ledgerHeads(entries), nil
}

func gcWriteConsolidatedSpecs(root string, specs []Spec, parents []string, now time.Time) error {
	for i, spec := range specs {
		specRelPath := filepath.ToSlash(filepath.Join(".canon", "specs", specFileName(spec.ID)))
		specAbsPath := filepath.Join(root, specRelPath)
		if _, err := writeTextIfChanged(specAbsPath, canonicalSpecText(spec)); err != nil {
			return err
		}

		sourceText := strings.TrimSpace(spec.Body) + "\n"
		sourceRelPath := filepath.ToSlash(filepath.Join(".canon", "sources", spec.ID+".source.md"))
		sourceAbsPath := filepath.Join(root, sourceRelPath)
		if _, err := writeTextIfChanged(sourceAbsPath, sourceText); err != nil {
			return err
		}

		entry := LedgerEntry{
			SpecID:      spec.ID,
			Title:       spec.Title,
			Type:        spec.Type,
			Domain:      spec.Domain,
			Parents:     parents,
			Sequence:    now.Add(time.Duration(i) * time.Nanosecond).UnixNano(),
			IngestedAt:  now.Format(timeRFC3339),
			ContentHash: checksum(canonicalSpecText(spec)),
			SpecPath:    specRelPath,
			SourcePath:  sourceRelPath,
			Operation:   "consolidation",
		}
		ledgerBytes, err := json.MarshalIndent(entry, "", "  ")
		if err != nil {
			return err
		}
		ledgerName := ledgerFileName(entry.IngestedAt, spec.ID)
		ledgerAbsPath := filepath.Join(root, ".canon", "ledger", ledgerName)
		ledgerText := string(ledgerBytes) + "\n"
		if _, err := writeTextIfChanged(ledgerAbsPath, ledgerText); err != nil {
			return err
		}
	}
	return nil
}

func gcArchiveTargetSpecs(root string, specs []Spec) error {
	for _, spec := range specs {
		specFrom := filepath.Clean(spec.Path)
		if !filepath.IsAbs(specFrom) {
			specFrom = filepath.Join(root, specFrom)
		}
		specTo := filepath.Clean(filepath.Join(root, ".canon", "archive", "specs", specFileName(spec.ID)))
		if err := os.MkdirAll(filepath.Dir(specTo), 0o755); err != nil {
			return err
		}
		if err := os.Rename(specFrom, specTo); err != nil {
			return err
		}

		sourceRel := filepath.Clean(filepath.Join(".canon", "sources", spec.ID+".source.md"))
		sourceFrom, err := pathWithinRoot(root, filepath.ToSlash(sourceRel))
		if err != nil {
			return err
		}
		sourceTo := filepath.Clean(filepath.Join(root, ".canon", "archive", "sources", spec.ID+".source.md"))
		if err := os.MkdirAll(filepath.Dir(sourceTo), 0o755); err != nil {
			return err
		}
		if err := os.Rename(sourceFrom, sourceTo); err != nil {
			return err
		}
	}
	return nil
}

func gcExternalDependencies(consolidated []Spec) []string {
	out := make([]string, 0)
	seen := make(map[string]struct{})
	for _, spec := range consolidated {
		for _, dep := range spec.DependsOn {
			if _, ok := seen[dep]; !ok {
				seen[dep] = struct{}{}
				out = append(out, dep)
			}
		}
	}
	sort.Strings(out)
	return out
}
