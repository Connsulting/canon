package canon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func LoadLedger(root string) ([]LedgerEntry, error) {
	glob := filepath.Join(root, ".canon", "ledger", "*.json")
	paths, err := filepath.Glob(glob)
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	entries := make([]LedgerEntry, 0, len(paths))
	for _, path := range paths {
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		var entry LedgerEntry
		if err := json.Unmarshal(b, &entry); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Sequence != 0 && entries[j].Sequence != 0 && entries[i].Sequence != entries[j].Sequence {
			return entries[i].Sequence > entries[j].Sequence
		}
		if entries[i].IngestedAt == entries[j].IngestedAt {
			return entries[i].SpecID > entries[j].SpecID
		}
		return entries[i].IngestedAt > entries[j].IngestedAt
	})

	return entries, nil
}

func ledgerHeads(entries []LedgerEntry) []string {
	if len(entries) == 0 {
		return nil
	}
	parented := make(map[string]struct{})
	for _, entry := range entries {
		for _, parent := range entry.Parents {
			p := strings.TrimSpace(parent)
			if p != "" {
				parented[p] = struct{}{}
			}
		}
	}

	heads := make([]string, 0)
	seen := make(map[string]struct{})
	for _, entry := range entries {
		if _, ok := parented[entry.SpecID]; ok {
			continue
		}
		if _, ok := seen[entry.SpecID]; ok {
			continue
		}
		seen[entry.SpecID] = struct{}{}
		heads = append(heads, entry.SpecID)
	}
	sort.Strings(heads)
	return heads
}
