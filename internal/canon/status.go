package canon

type Status struct {
	TotalSpecs              int
	FeatureSpecs            int
	TechnicalSpecs          int
	ResolutionSpecs         int
	Domains                 int
	CrossDomainInteractions int
	LedgerEntries           int
	LedgerHeads             int
}

func GetStatus(root string) (Status, error) {
	if err := EnsureLayout(root, false); err != nil {
		return Status{}, err
	}
	specs, err := loadSpecs(root)
	if err != nil {
		return Status{}, err
	}
	index := buildIndex(specs)
	entries, err := LoadLedger(root)
	if err != nil {
		return Status{}, err
	}

	st := Status{}
	for _, spec := range specs {
		st.TotalSpecs++
		switch spec.Type {
		case "feature":
			st.FeatureSpecs++
		case "technical":
			st.TechnicalSpecs++
		case "resolution":
			st.ResolutionSpecs++
		}
	}
	st.Domains = len(index.Domains)
	st.CrossDomainInteractions = len(index.CrossDomainEdges)
	st.LedgerEntries = len(entries)
	st.LedgerHeads = len(ledgerHeads(entries))
	return st, nil
}
