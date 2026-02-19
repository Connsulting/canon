package canon

import (
	"fmt"
	"os"
	"path/filepath"
)

func ShowSpec(root string, specID string) (string, string, error) {
	spec, err := loadSpecByID(root, specID)
	if err != nil {
		return "", "", err
	}
	path := spec.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	rel, _ := filepath.Rel(root, path)
	if rel == "" {
		rel = path
	}
	return filepath.ToSlash(rel), string(b), nil
}

func RequireSpec(root string, specID string) (Spec, error) {
	spec, err := loadSpecByID(root, specID)
	if err != nil {
		return Spec{}, fmt.Errorf("unknown spec id: %s", specID)
	}
	return spec, nil
}
