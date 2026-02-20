package canon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func Reset(root string, input ResetInput) (ResetResult, error) {
	if err := EnsureLayout(root, false); err != nil {
		return ResetResult{}, err
	}

	refSpecID := strings.TrimSpace(input.RefSpecID)
	if refSpecID == "" {
		return ResetResult{}, fmt.Errorf("reset requires <spec-id>")
	}

	records, err := loadLedgerRecords(root)
	if err != nil {
		return ResetResult{}, err
	}
	if len(records) == 0 {
		return ResetResult{}, fmt.Errorf("cannot reset: ledger is empty")
	}

	cutoff := -1
	for i, record := range records {
		if strings.TrimSpace(record.Entry.SpecID) == refSpecID {
			cutoff = i
			break
		}
	}
	if cutoff == -1 {
		return ResetResult{}, fmt.Errorf("unknown spec id: %s", refSpecID)
	}

	result := ResetResult{KeptSpecID: refSpecID}
	if cutoff == 0 {
		return result, nil
	}

	ledgerSeen := make(map[string]struct{})
	specSeen := make(map[string]struct{})
	sourceSeen := make(map[string]struct{})

	for _, record := range records[:cutoff] {
		ledgerPath, err := pathWithinRoot(root, record.Path)
		if err != nil {
			return ResetResult{}, err
		}
		removed, err := removeFileBestEffort(ledgerPath, ledgerSeen)
		if err != nil {
			return ResetResult{}, err
		}
		if removed {
			result.LedgerDeleted++
		}

		specPath, err := resetSpecPath(root, record.Entry)
		if err != nil {
			return ResetResult{}, err
		}
		removed, err = removeFileBestEffort(specPath, specSeen)
		if err != nil {
			return ResetResult{}, err
		}
		if removed {
			result.SpecDeleted++
		}

		sourcePath, err := resetSourcePath(root, record.Entry)
		if err != nil {
			return ResetResult{}, err
		}
		removed, err = removeFileBestEffort(sourcePath, sourceSeen)
		if err != nil {
			return ResetResult{}, err
		}
		if removed {
			result.SourceDeleted++
		}
	}

	return result, nil
}

func resetSpecPath(root string, entry LedgerEntry) (string, error) {
	if strings.TrimSpace(entry.SpecPath) != "" {
		return pathWithinRoot(root, entry.SpecPath)
	}
	rel := filepath.ToSlash(filepath.Join(".canon", "specs", specFileName(entry.SpecID)))
	return pathWithinRoot(root, rel)
}

func resetSourcePath(root string, entry LedgerEntry) (string, error) {
	if strings.TrimSpace(entry.SourcePath) != "" {
		return pathWithinRoot(root, entry.SourcePath)
	}
	rel := filepath.ToSlash(filepath.Join(".canon", "sources", strings.TrimSpace(entry.SpecID)+".source.md"))
	return pathWithinRoot(root, rel)
}

func pathWithinRoot(root string, pathValue string) (string, error) {
	cleanRoot := filepath.Clean(root)
	trimmed := strings.TrimSpace(pathValue)
	if trimmed == "" {
		return "", fmt.Errorf("empty path")
	}

	var candidate string
	if filepath.IsAbs(trimmed) {
		candidate = filepath.Clean(trimmed)
	} else {
		candidate = filepath.Clean(filepath.Join(cleanRoot, filepath.FromSlash(trimmed)))
	}

	prefix := cleanRoot + string(os.PathSeparator)
	if candidate != cleanRoot && !strings.HasPrefix(candidate, prefix) {
		return "", fmt.Errorf("refusing to delete path outside repository root: %s", pathValue)
	}
	return candidate, nil
}

func removeFileBestEffort(path string, seen map[string]struct{}) (bool, error) {
	if path == "" {
		return false, nil
	}
	if _, ok := seen[path]; ok {
		return false, nil
	}
	seen[path] = struct{}{}

	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
