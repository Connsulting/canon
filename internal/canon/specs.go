package canon

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func loadSpecs(root string) ([]Spec, error) {
	glob := filepath.Join(root, ".canon", "specs", "*.spec.md")
	paths, err := filepath.Glob(glob)
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	seen := make(map[string]string)
	specs := make([]Spec, 0, len(paths))
	for _, path := range paths {
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		spec, parseErr := parseSpecText(string(b), path)
		if parseErr != nil {
			return nil, parseErr
		}
		if prior, ok := seen[spec.ID]; ok {
			return nil, fmt.Errorf("duplicate spec id %s in %s and %s", spec.ID, prior, path)
		}
		seen[spec.ID] = path
		spec.Path = path
		specs = append(specs, spec)
	}
	return specs, nil
}

func loadSpecByID(root string, specID string) (Spec, error) {
	specs, err := loadSpecs(root)
	if err != nil {
		return Spec{}, err
	}
	for _, spec := range specs {
		if spec.ID == specID {
			return spec, nil
		}
	}
	return Spec{}, fmt.Errorf("spec not found: %s", specID)
}
