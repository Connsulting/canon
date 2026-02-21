package canon

import (
	"fmt"
	"os"
	"path/filepath"
)

var requiredLayout = []string{
	".canon/specs",
	".canon/ledger",
	".canon/sources",
	".canon/conflict-reports",
	".canon/archive/specs",
	".canon/archive/sources",
	"state/interactions",
}

func EnsureLayout(root string, createMissing bool) error {
	missing := make([]string, 0)
	for _, rel := range requiredLayout {
		abs := filepath.Join(root, rel)
		st, err := os.Stat(abs)
		if err == nil {
			if !st.IsDir() {
				return fmt.Errorf("required path is not a directory: %s", rel)
			}
			continue
		}
		if !os.IsNotExist(err) {
			return err
		}
		if createMissing {
			if mkErr := os.MkdirAll(abs, 0o755); mkErr != nil {
				return mkErr
			}
		} else {
			missing = append(missing, rel)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("required repository directories are missing: %v", missing)
	}

	return nil
}
