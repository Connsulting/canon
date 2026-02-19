package canon

import "fmt"

func computeBlastRadius(index Index, targetSpecID string, depth int) (map[string]struct{}, error) {
	if _, ok := index.Specs[targetSpecID]; !ok {
		return nil, fmt.Errorf("unknown spec id: %s", targetSpecID)
	}
	if depth < 0 {
		depth = 0
	}

	radius := map[string]struct{}{targetSpecID: {}}

	for _, ids := range index.Domains {
		contains := false
		for _, id := range ids {
			if id == targetSpecID {
				contains = true
				break
			}
		}
		if !contains {
			continue
		}
		for _, id := range ids {
			radius[id] = struct{}{}
		}
	}

	type nodeDepth struct {
		id    string
		depth int
	}
	queue := []nodeDepth{{id: targetSpecID, depth: 0}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.depth >= depth {
			continue
		}
		neighbors := make([]string, 0)
		for id := range index.DependsOn[cur.id] {
			neighbors = append(neighbors, id)
		}
		for id := range index.DependedOnBy[cur.id] {
			neighbors = append(neighbors, id)
		}
		for _, neighbor := range neighbors {
			if _, ok := radius[neighbor]; ok {
				continue
			}
			radius[neighbor] = struct{}{}
			queue = append(queue, nodeDepth{id: neighbor, depth: cur.depth + 1})
		}
	}

	changed := true
	for changed {
		changed = false
		for _, spec := range index.Specs {
			if spec.Type != "resolution" {
				continue
			}
			if _, ok := radius[spec.ID]; ok {
				continue
			}
			for _, dep := range spec.DependsOn {
				if _, ok := radius[dep]; ok {
					radius[spec.ID] = struct{}{}
					changed = true
					break
				}
			}
		}
	}

	return radius, nil
}
