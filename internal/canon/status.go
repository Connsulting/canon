package canon

type Status struct {
	Layout                  LayoutReport
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
	layout := CheckLayout(root)
	if layout.Health == LayoutInvalid {
		return Status{}, LayoutError{Report: layout}
	}
	specs, err := loadSpecs(root)
	if err != nil {
		return Status{}, invalidRepositoryDataError(".canon/specs", err)
	}
	index := buildIndex(specs)
	entries, err := LoadLedger(root)
	if err != nil {
		return Status{}, invalidRepositoryDataError(".canon/ledger", err)
	}

	st := Status{Layout: layout}
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

func invalidRepositoryDataError(path string, err error) error {
	return LayoutError{Report: LayoutReport{
		Health: LayoutInvalid,
		Problems: []LayoutProblem{
			{Path: path, Kind: LayoutProblemInvalidData, Err: err},
		},
	}}
}
