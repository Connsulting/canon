package canon

const layoutRepairCommand = "canon init --ai off"

type Status struct {
	Root                    string       `json:"root"`
	Healthy                 bool         `json:"healthy"`
	Layout                  LayoutReport `json:"layout"`
	LayoutOK                bool         `json:"layout_ok"`
	LayoutRepairRequired    bool         `json:"layout_repair_required"`
	LayoutRepairCommand     string       `json:"layout_repair_command,omitempty"`
	MissingLayoutPaths      []string     `json:"missing_layout_paths"`
	TotalSpecs              int          `json:"total_specs"`
	FeatureSpecs            int          `json:"feature_specs"`
	TechnicalSpecs          int          `json:"technical_specs"`
	ResolutionSpecs         int          `json:"resolution_specs"`
	Domains                 int          `json:"domains"`
	CrossDomainInteractions int          `json:"cross_domain_interactions"`
	LedgerEntries           int          `json:"ledger_entries"`
	LedgerHeads             int          `json:"ledger_heads"`
}

func GetStatus(root string) (Status, error) {
	layout := CheckLayout(root)
	st := Status{
		Root:                 root,
		Layout:               layout,
		LayoutOK:             layout.Health == LayoutHealthy,
		LayoutRepairRequired: layout.Health != LayoutHealthy,
		MissingLayoutPaths:   layout.MissingLayoutPaths(),
	}
	if st.LayoutRepairRequired {
		st.LayoutRepairCommand = layoutRepairCommand
	}
	if layout.Health == LayoutInvalid {
		return st, LayoutError{Report: layout}
	}

	specs, err := loadSpecs(root)
	if err != nil {
		return st, invalidRepositoryDataError(".canon/specs", err)
	}
	index := buildIndex(specs)
	entries, err := LoadLedger(root)
	if err != nil {
		return st, invalidRepositoryDataError(".canon/ledger", err)
	}

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
	st.Healthy = st.LayoutOK
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
