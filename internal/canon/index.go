package canon

import (
	"sort"
	"strings"
)

func buildIndex(specs []Spec) Index {
	ordered := make([]Spec, len(specs))
	copy(ordered, specs)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].ID < ordered[j].ID
	})

	specMap := make(map[string]Spec, len(ordered))
	domains := make(map[string][]string)
	dependsOn := make(map[string]map[string]struct{})
	dependedOnBy := make(map[string]map[string]struct{})
	cross := make([]CrossDomainEdge, 0)

	for _, spec := range ordered {
		specMap[spec.ID] = spec
		touched := mustInclude(spec.TouchedDomains, spec.Domain)
		for _, domain := range touched {
			domains[domain] = append(domains[domain], spec.ID)
		}

		if _, ok := dependedOnBy[spec.ID]; !ok {
			dependedOnBy[spec.ID] = make(map[string]struct{})
		}

		depSet := make(map[string]struct{})
		for _, dep := range spec.DependsOn {
			if strings.TrimSpace(dep) == "" {
				continue
			}
			depSet[dep] = struct{}{}
			if _, ok := dependedOnBy[dep]; !ok {
				dependedOnBy[dep] = make(map[string]struct{})
			}
			dependedOnBy[dep][spec.ID] = struct{}{}
		}
		dependsOn[spec.ID] = depSet

		if len(touched) > 1 {
			cross = append(cross, CrossDomainEdge{Spec: spec.ID, Domains: touched})
		}
	}

	for domain, ids := range domains {
		sort.Strings(ids)
		domains[domain] = ids
	}

	sort.Slice(cross, func(i, j int) bool {
		if cross[i].Spec == cross[j].Spec {
			return strings.Join(cross[i].Domains, ",") < strings.Join(cross[j].Domains, ",")
		}
		return cross[i].Spec < cross[j].Spec
	})

	return Index{
		Specs:            specMap,
		Domains:          domains,
		CrossDomainEdges: cross,
		DependsOn:        dependsOn,
		DependedOnBy:     dependedOnBy,
	}
}

func serializeIndexYAML(index Index) string {
	lines := []string{"specs:"}
	specIDs := make([]string, 0, len(index.Specs))
	for id := range index.Specs {
		specIDs = append(specIDs, id)
	}
	sort.Strings(specIDs)

	for _, id := range specIDs {
		spec := index.Specs[id]
		touches := mustInclude(spec.TouchedDomains, spec.Domain)
		deps := mapKeysSorted(index.DependsOn[id])
		dependents := mapKeysSorted(index.DependedOnBy[id])

		lines = append(lines,
			"  "+id+":",
			"    title: "+yamlScalar(spec.Title),
			"    type: "+yamlScalar(spec.Type),
			"    domain: "+yamlScalar(spec.Domain),
			"    touches: "+renderList(touches),
			"    depends_on: "+renderList(deps),
			"    depended_on_by: "+renderList(dependents),
		)
	}

	lines = append(lines, "domains:")
	domainNames := make([]string, 0, len(index.Domains))
	for domain := range index.Domains {
		domainNames = append(domainNames, domain)
	}
	sort.Strings(domainNames)
	for _, domain := range domainNames {
		lines = append(lines,
			"  "+domain+":",
			"    specs: "+renderList(index.Domains[domain]),
		)
	}

	lines = append(lines, "cross_domain_edges:")
	if len(index.CrossDomainEdges) == 0 {
		lines = append(lines, "  []")
	} else {
		for _, edge := range index.CrossDomainEdges {
			lines = append(lines,
				"  - spec: "+yamlScalar(edge.Spec),
				"    domains: "+renderList(edge.Domains),
			)
		}
	}

	return strings.Join(lines, "\n") + "\n"
}

func mapKeysSorted(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
