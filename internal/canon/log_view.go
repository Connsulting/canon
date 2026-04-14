package canon

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type LogOptions struct {
	Limit           int
	Graph           bool
	OneLine         bool
	All             bool
	Grep            string
	Domain          string
	Type            string
	RequirementKind string
	Color           string
	IsTTY           bool
	Date            string
	ShowTags        bool
}

type LogNode struct {
	ID      string
	Entry   *LedgerEntry
	Spec    *Spec
	Missing bool
	Cycle   bool
	Deps    []string
}

func BuildLogView(root string, opts LogOptions) ([]LogNode, error) {
	entries, err := LoadLedger(root)
	if err != nil {
		return nil, err
	}
	specs, err := loadSpecs(root)
	if err != nil {
		return nil, err
	}

	entryByID := make(map[string]LedgerEntry, len(entries))
	for _, entry := range entries {
		if _, ok := entryByID[entry.SpecID]; ok {
			continue
		}
		entryByID[entry.SpecID] = entry
	}

	specByID := make(map[string]Spec, len(specs))
	for _, spec := range specs {
		specByID[spec.ID] = spec
	}
	if len(specByID) == 0 {
		return nil, nil
	}

	heads := orderedHeadsByRecency(entries)
	scoped := scopedSpecIDs(specByID, heads, opts.All)
	filteredIDs := filteredSpecIDs(specByID, scoped, opts)
	if len(filteredIDs) == 0 {
		return nil, nil
	}

	compare := func(a string, b string) bool {
		return newerID(a, b, specByID, entryByID)
	}

	realOrder := orderedRealIDs(filteredIDs, specByID, compare, opts.Graph)
	limitedReal := limitIDs(realOrder, opts.Limit)
	if len(limitedReal) == 0 {
		return nil, nil
	}

	if !opts.Graph {
		return buildLogNodes(limitedReal, nil, nil, specByID, entryByID, compare), nil
	}

	displaySet := make(map[string]struct{}, len(limitedReal))
	for _, id := range limitedReal {
		displaySet[id] = struct{}{}
	}
	for _, id := range limitedReal {
		spec := specByID[id]
		for _, dep := range spec.DependsOn {
			dep = strings.TrimSpace(dep)
			if dep == "" {
				continue
			}
			if _, ok := specByID[dep]; ok {
				continue
			}
			displaySet[dep] = struct{}{}
		}
	}

	allIDs := mapKeys(displaySet)
	outgoing := outgoingEdges(displaySet, specByID)
	cycleIDs := make(map[string]bool)
	order := topoOrder(allIDs, outgoing, compare, cycleIDs)

	return buildLogNodes(order, outgoing, cycleIDs, specByID, entryByID, compare), nil
}

func RenderLogText(nodes []LogNode, opts LogOptions) string {
	if len(nodes) == 0 {
		return ""
	}

	lines := make([]string, 0, len(nodes)*2)
	if opts.Graph {
		lines = append(lines, renderGraphLines(nodes, opts)...)
		text := strings.TrimRight(strings.Join(lines, "\n"), "\n")
		if text == "" {
			return ""
		}
		return text + "\n"
	}

	for _, node := range nodes {
		if opts.OneLine {
			lines = append(lines, renderOneLine(node, opts))
			continue
		}

		lines = append(lines, renderDetailedBlock(node, opts)...)
		lines = append(lines, "")
	}

	text := strings.TrimRight(strings.Join(lines, "\n"), "\n")
	if text == "" {
		return ""
	}
	return text + "\n"
}

func renderGraphLines(nodes []LogNode, opts LogOptions) []string {
	columns := make([]string, 0)
	lines := make([]string, 0, len(nodes))

	for _, node := range nodes {
		curr := append([]string{}, columns...)
		idx := indexOf(curr, node.ID)
		if idx == -1 {
			curr = append([]string{node.ID}, curr...)
			idx = 0
		}

		prefix := graphPrefix(curr, idx, opts)
		lines = append(lines, prefix+" "+renderOneLine(node, opts))

		deps := normalizeList(node.Deps)
		next := append([]string{}, curr...)
		switch len(deps) {
		case 0:
			next = removeAt(next, idx)
		case 1:
			dep := deps[0]
			otherIdx := indexOf(next, dep)
			if otherIdx != -1 && otherIdx != idx {
				next = removeAt(next, idx)
			} else {
				next[idx] = dep
			}
		default:
			next[idx] = deps[0]
			insertAt := idx + 1
			for _, dep := range deps[1:] {
				if indexOf(next, dep) != -1 {
					continue
				}
				next = append(next[:insertAt], append([]string{dep}, next[insertAt:]...)...)
				insertAt++
			}
		}

		next = dedupeColumns(next)
		if connector := graphConnector(curr, next, idx, opts); connector != "" {
			lines = append(lines, connector)
		}
		columns = next
	}

	return lines
}

func graphPrefix(columns []string, active int, opts LogOptions) string {
	if len(columns) == 0 {
		return colorize(opts, "90", "*")
	}
	symbols := make([]string, len(columns))
	for i := range columns {
		if i == active {
			symbols[i] = colorize(opts, "90", "*")
			continue
		}
		symbols[i] = colorize(opts, "90", "|")
	}
	return strings.Join(symbols, " ")
}

func graphConnector(curr []string, next []string, active int, opts LogOptions) string {
	if equalColumns(curr, next) {
		return ""
	}
	if len(next) == 0 {
		return ""
	}
	if len(curr) == len(next) && len(curr) <= 1 {
		return ""
	}

	maxCols := len(curr)
	if len(next) > maxCols {
		maxCols = len(next)
	}
	if maxCols == 0 {
		return ""
	}

	out := make([]string, maxCols)
	for i := range out {
		out[i] = " "
	}

	if len(next) == len(curr) {
		changed := false
		for i := range curr {
			if i == active {
				continue
			}
			if curr[i] != next[i] {
				changed = true
				break
			}
		}
		if !changed {
			out[active] = colorize(opts, "90", "|")
			return strings.TrimRight(strings.Join(out, ""), " ")
		}
	}

	for i := range next {
		out[i] = colorize(opts, "90", "|")
	}

	switch {
	case len(next) > len(curr):
		for i := len(curr); i < len(next); i++ {
			out[i] = colorize(opts, "90", "\\")
		}
		if active >= 0 && active < len(out) {
			out[active] = colorize(opts, "90", "|")
		}
	case len(next) < len(curr):
		for i := len(next); i < len(curr); i++ {
			out[i] = colorize(opts, "90", "/")
		}
		if active >= 0 && active < len(next) {
			out[active] = colorize(opts, "90", "|")
		}
	}

	return strings.TrimRight(strings.Join(out, " "), " ")
}

func renderOneLine(node LogNode, opts LogOptions) string {
	if node.Missing {
		line := colorize(opts, "31", "[missing] "+node.ID)
		if node.Cycle {
			line += " " + colorize(opts, "31", "[cycle]")
		}
		return line
	}

	title := node.ID
	typ := "unknown"
	domain := "unknown"
	when := ""
	if node.Spec != nil {
		if strings.TrimSpace(node.Spec.Title) != "" {
			title = node.Spec.Title
		}
		if strings.TrimSpace(node.Spec.Type) != "" {
			typ = node.Spec.Type
		}
		if strings.TrimSpace(node.Spec.Domain) != "" {
			domain = node.Spec.Domain
		}
		when = strings.TrimSpace(node.Spec.Created)
	}
	if node.Entry != nil && strings.TrimSpace(node.Entry.IngestedAt) != "" {
		when = strings.TrimSpace(node.Entry.IngestedAt)
	}
	when = formatLogTime(when, opts)

	line := fmt.Sprintf(
		"%s %s",
		colorize(opts, "32", node.ID),
		colorize(opts, "33", title),
	)
	if opts.ShowTags {
		tags := []string{
			colorize(opts, "36", typ),
			colorize(opts, "34", domain),
		}
		if node.Spec != nil && strings.TrimSpace(node.Spec.RequirementKind) != "" {
			tags = append(tags, colorize(opts, "35", strings.TrimSpace(node.Spec.RequirementKind)))
		}
		line += fmt.Sprintf(" [%s]", strings.Join(tags, "/"))
	}
	if when != "" {
		line += " " + colorize(opts, "35", when)
	}
	if node.Cycle {
		line += " " + colorize(opts, "31", "[cycle]")
	}
	return line
}

func renderDetailedBlock(node LogNode, opts LogOptions) []string {
	if node.Missing {
		line := "Spec: " + colorize(opts, "31", "[missing] "+node.ID)
		if node.Cycle {
			line += " " + colorize(opts, "31", "[cycle]")
		}
		return []string{line}
	}

	lines := []string{
		"Spec: " + colorize(opts, "32", node.ID),
	}
	if node.Spec != nil {
		if strings.TrimSpace(node.Spec.Title) != "" {
			lines = append(lines, "Title: "+colorize(opts, "33", node.Spec.Title))
		}
		if opts.ShowTags && strings.TrimSpace(node.Spec.Type) != "" {
			lines = append(lines, "Type: "+colorize(opts, "36", node.Spec.Type))
		}
		if opts.ShowTags && strings.TrimSpace(node.Spec.Domain) != "" {
			lines = append(lines, "Domain: "+colorize(opts, "34", node.Spec.Domain))
		}
		if opts.ShowTags && strings.TrimSpace(node.Spec.RequirementKind) != "" {
			lines = append(lines, "RequirementKind: "+colorize(opts, "35", node.Spec.RequirementKind))
		}
	}
	if node.Entry != nil && strings.TrimSpace(node.Entry.IngestedAt) != "" {
		lines = append(lines, "Date: "+colorize(opts, "35", formatLogTime(node.Entry.IngestedAt, opts)))
	} else if node.Spec != nil && strings.TrimSpace(node.Spec.Created) != "" {
		lines = append(lines, "Date: "+colorize(opts, "35", formatLogTime(node.Spec.Created, opts)))
	}

	if node.Entry != nil {
		if len(node.Entry.Parents) == 0 {
			lines = append(lines, "Parents: []")
		} else {
			lines = append(lines, "Parents: ["+strings.Join(node.Entry.Parents, ", ")+"]")
		}
		if strings.TrimSpace(node.Entry.ContentHash) != "" {
			lines = append(lines, "Hash: "+node.Entry.ContentHash)
		}
		if strings.TrimSpace(node.Entry.SourcePath) != "" {
			lines = append(lines, "Source: "+node.Entry.SourcePath)
		}
		if strings.TrimSpace(node.Entry.SpecPath) != "" {
			lines = append(lines, "SpecPath: "+node.Entry.SpecPath)
		}
	}

	if node.Cycle {
		lines = append(lines, "Cycle: "+colorize(opts, "31", "true"))
	}
	return lines
}

func orderedHeadsByRecency(entries []LedgerEntry) []string {
	heads := ledgerHeads(entries)
	if len(heads) == 0 {
		return nil
	}

	headSet := make(map[string]struct{}, len(heads))
	for _, id := range heads {
		headSet[id] = struct{}{}
	}

	out := make([]string, 0, len(heads))
	seen := make(map[string]struct{}, len(heads))
	for _, entry := range entries {
		if _, ok := headSet[entry.SpecID]; !ok {
			continue
		}
		if _, ok := seen[entry.SpecID]; ok {
			continue
		}
		seen[entry.SpecID] = struct{}{}
		out = append(out, entry.SpecID)
	}
	for _, id := range heads {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func scopedSpecIDs(specByID map[string]Spec, heads []string, all bool) map[string]struct{} {
	if len(specByID) == 0 {
		return nil
	}
	starts := make([]string, 0)
	if len(heads) > 0 {
		if all {
			starts = append(starts, heads...)
		} else {
			starts = append(starts, heads[0])
		}
	}

	scope := make(map[string]struct{})
	if len(starts) > 0 {
		stack := append([]string{}, starts...)
		for len(stack) > 0 {
			last := len(stack) - 1
			id := stack[last]
			stack = stack[:last]
			if _, seen := scope[id]; seen {
				continue
			}
			spec, ok := specByID[id]
			if !ok {
				continue
			}
			scope[id] = struct{}{}
			for _, dep := range spec.DependsOn {
				dep = strings.TrimSpace(dep)
				if dep == "" {
					continue
				}
				stack = append(stack, dep)
			}
		}
	}

	if len(scope) > 0 {
		return scope
	}

	for id := range specByID {
		scope[id] = struct{}{}
	}
	return scope
}

func filteredSpecIDs(specByID map[string]Spec, scope map[string]struct{}, opts LogOptions) []string {
	out := make([]string, 0, len(scope))
	grep := strings.ToLower(strings.TrimSpace(opts.Grep))
	domain := strings.TrimSpace(opts.Domain)
	typ := strings.TrimSpace(opts.Type)
	requirementKind := strings.TrimSpace(opts.RequirementKind)

	for id := range scope {
		spec, ok := specByID[id]
		if !ok {
			continue
		}
		if domain != "" && !strings.EqualFold(spec.Domain, domain) {
			continue
		}
		if typ != "" && !strings.EqualFold(spec.Type, typ) {
			continue
		}
		if requirementKind != "" && !strings.EqualFold(spec.RequirementKind, requirementKind) {
			continue
		}
		if grep != "" && !strings.Contains(strings.ToLower(spec.Title), grep) {
			continue
		}
		out = append(out, id)
	}
	return out
}

func orderedRealIDs(ids []string, specByID map[string]Spec, compare func(string, string) bool, graph bool) []string {
	ordered := append([]string{}, ids...)
	if len(ordered) <= 1 {
		return ordered
	}

	if !graph {
		sort.Slice(ordered, func(i, j int) bool {
			return compare(ordered[i], ordered[j])
		})
		return ordered
	}

	set := make(map[string]struct{}, len(ordered))
	for _, id := range ordered {
		set[id] = struct{}{}
	}
	outgoing := outgoingEdges(set, specByID)
	cycleIDs := make(map[string]bool)
	return topoOrder(ordered, outgoing, compare, cycleIDs)
}

func outgoingEdges(displaySet map[string]struct{}, specByID map[string]Spec) map[string][]string {
	out := make(map[string][]string, len(displaySet))
	for id := range displaySet {
		spec, ok := specByID[id]
		if !ok {
			continue
		}
		deps := make([]string, 0, len(spec.DependsOn))
		for _, dep := range spec.DependsOn {
			dep = strings.TrimSpace(dep)
			if dep == "" {
				continue
			}
			if _, ok := displaySet[dep]; !ok {
				continue
			}
			deps = append(deps, dep)
		}
		out[id] = deps
	}
	return out
}

func topoOrder(ids []string, outgoing map[string][]string, compare func(string, string) bool, cycleIDs map[string]bool) []string {
	indegree := make(map[string]int, len(ids))
	for _, id := range ids {
		indegree[id] = 0
	}
	for _, src := range ids {
		for _, dep := range outgoing[src] {
			if _, ok := indegree[dep]; !ok {
				continue
			}
			indegree[dep]++
		}
	}

	available := make([]string, 0)
	for _, id := range ids {
		if indegree[id] == 0 {
			available = append(available, id)
		}
	}

	result := make([]string, 0, len(ids))
	for len(available) > 0 {
		sort.Slice(available, func(i, j int) bool {
			return compare(available[i], available[j])
		})
		id := available[0]
		available = available[1:]
		result = append(result, id)

		for _, dep := range outgoing[id] {
			if _, ok := indegree[dep]; !ok {
				continue
			}
			indegree[dep]--
			if indegree[dep] == 0 {
				available = append(available, dep)
			}
		}
		delete(indegree, id)
	}

	if len(indegree) == 0 {
		return result
	}

	remaining := mapKeys(indegree)
	sort.Slice(remaining, func(i, j int) bool {
		return compare(remaining[i], remaining[j])
	})
	for _, id := range remaining {
		cycleIDs[id] = true
		result = append(result, id)
	}
	return result
}

func buildLogNodes(order []string, outgoing map[string][]string, cycleIDs map[string]bool, specByID map[string]Spec, entryByID map[string]LedgerEntry, compare func(string, string) bool) []LogNode {
	nodes := make([]LogNode, 0, len(order))
	for _, id := range order {
		node := LogNode{
			ID:      id,
			Missing: true,
		}
		if spec, ok := specByID[id]; ok {
			specCopy := spec
			node.Spec = &specCopy
			node.Missing = false
		}
		if entry, ok := entryByID[id]; ok {
			entryCopy := entry
			node.Entry = &entryCopy
		}
		if cycleIDs != nil && cycleIDs[id] {
			node.Cycle = true
		}
		if outgoing != nil {
			deps := append([]string{}, outgoing[id]...)
			sort.Slice(deps, func(i, j int) bool {
				return compare(deps[i], deps[j])
			})
			node.Deps = deps
		}
		nodes = append(nodes, node)
	}
	return nodes
}

func shouldColor(opts LogOptions) bool {
	mode := strings.ToLower(strings.TrimSpace(opts.Color))
	switch mode {
	case "always":
		return true
	case "never":
		return false
	default:
		return opts.IsTTY
	}
}

func colorize(opts LogOptions, code string, value string) string {
	if value == "" || !shouldColor(opts) {
		return value
	}
	return "\033[" + code + "m" + value + "\033[0m"
}

func formatLogTime(value string, opts LogOptions) string {
	text := strings.TrimSpace(value)
	if text == "" {
		return text
	}
	if !strings.EqualFold(strings.TrimSpace(opts.Date), "relative") {
		return text
	}
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return text
	}
	return relativeTime(parsed.UTC(), nowUTC())
}

func relativeTime(target time.Time, reference time.Time) string {
	diff := reference.Sub(target)
	suffix := "ago"
	if diff < 0 {
		diff = -diff
		suffix = "from now"
	}

	switch {
	case diff < 30*time.Second:
		if suffix == "ago" {
			return "just now"
		}
		return "in moments"
	case diff < 90*time.Second:
		return "1 minute " + suffix
	case diff < 60*time.Minute:
		return fmt.Sprintf("%d minutes %s", int(diff.Minutes()+0.5), suffix)
	case diff < 90*time.Minute:
		return "1 hour " + suffix
	case diff < 24*time.Hour:
		return fmt.Sprintf("%d hours %s", int(diff.Hours()+0.5), suffix)
	case diff < 48*time.Hour:
		return "1 day " + suffix
	case diff < 14*24*time.Hour:
		return fmt.Sprintf("%d days %s", int(diff.Hours()/24+0.5), suffix)
	case diff < 60*24*time.Hour:
		return fmt.Sprintf("%d weeks %s", int(diff.Hours()/(24*7)+0.5), suffix)
	case diff < 365*24*time.Hour:
		return fmt.Sprintf("%d months %s", int(diff.Hours()/(24*30)+0.5), suffix)
	default:
		return fmt.Sprintf("%d years %s", int(diff.Hours()/(24*365)+0.5), suffix)
	}
}

func limitIDs(ids []string, limit int) []string {
	if limit < 0 {
		limit = 0
	}
	if limit >= len(ids) {
		return ids
	}
	return ids[:limit]
}

func newerID(a string, b string, specByID map[string]Spec, entryByID map[string]LedgerEntry) bool {
	aEntry, aHasEntry := entryByID[a]
	bEntry, bHasEntry := entryByID[b]
	if aHasEntry && bHasEntry {
		if aEntry.Sequence != 0 && bEntry.Sequence != 0 && aEntry.Sequence != bEntry.Sequence {
			return aEntry.Sequence > bEntry.Sequence
		}
		if aEntry.IngestedAt != bEntry.IngestedAt {
			return aEntry.IngestedAt > bEntry.IngestedAt
		}
		if aEntry.SpecID != bEntry.SpecID {
			return aEntry.SpecID > bEntry.SpecID
		}
	}
	if aHasEntry != bHasEntry {
		return aHasEntry
	}

	aSpec, aHasSpec := specByID[a]
	bSpec, bHasSpec := specByID[b]
	if aHasSpec && bHasSpec {
		if aSpec.Created != bSpec.Created {
			return aSpec.Created > bSpec.Created
		}
	}
	if aHasSpec != bHasSpec {
		return aHasSpec
	}
	return a > b
}

func mapKeys[V any](m map[string]V) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}

func indexOf(values []string, value string) int {
	for i, current := range values {
		if current == value {
			return i
		}
	}
	return -1
}

func removeAt(values []string, idx int) []string {
	if idx < 0 || idx >= len(values) {
		return values
	}
	return append(values[:idx], values[idx+1:]...)
}

func dedupeColumns(values []string) []string {
	if len(values) <= 1 {
		return values
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func equalColumns(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
