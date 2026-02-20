package canon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ledgerRecord struct {
	Entry LedgerEntry
	Path  string
}

func LoadLedger(root string) ([]LedgerEntry, error) {
	records, err := loadLedgerRecords(root)
	if err != nil {
		return nil, err
	}
	entries := make([]LedgerEntry, 0, len(records))
	for _, record := range records {
		entries = append(entries, record.Entry)
	}
	return entries, nil
}

func loadLedgerRecords(root string) ([]ledgerRecord, error) {
	glob := filepath.Join(root, ".canon", "ledger", "*.json")
	paths, err := filepath.Glob(glob)
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	records := make([]ledgerRecord, 0, len(paths))
	for _, path := range paths {
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		var entry LedgerEntry
		if err := json.Unmarshal(b, &entry); err != nil {
			return nil, err
		}
		records = append(records, ledgerRecord{Entry: entry, Path: path})
	}

	sort.Slice(records, func(i, j int) bool {
		a := records[i].Entry
		b := records[j].Entry
		if a.Sequence != 0 && b.Sequence != 0 && a.Sequence != b.Sequence {
			return a.Sequence > b.Sequence
		}
		if a.IngestedAt == b.IngestedAt {
			return a.SpecID > b.SpecID
		}
		return a.IngestedAt > b.IngestedAt
	})

	return records, nil
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
