package canon

type Status struct {
	Root                    string   `json:"root"`
	Healthy                 bool     `json:"healthy"`
	LayoutOK                bool     `json:"layout_ok"`
	LayoutRepairRequired    bool     `json:"layout_repair_required"`
	LayoutRepairCommand     string   `json:"layout_repair_command,omitempty"`
	MissingLayoutPaths      []string `json:"missing_layout_paths"`
	TotalSpecs              int      `json:"total_specs"`
	FeatureSpecs            int      `json:"feature_specs"`
	TechnicalSpecs          int      `json:"technical_specs"`
	ResolutionSpecs         int      `json:"resolution_specs"`
	Domains                 int      `json:"domains"`
	CrossDomainInteractions int      `json:"cross_domain_interactions"`
	LedgerEntries           int      `json:"ledger_entries"`
	LedgerHeads             int      `json:"ledger_heads"`
}

func GetStatus(root string) (Status, error) {
	missing, err := MissingLayoutPaths(root)
	st := Status{
		Root:               root,
		LayoutOK:           len(missing) == 0 && err == nil,
		MissingLayoutPaths: missing,
	}
	st.Healthy = st.LayoutOK
	if !st.LayoutOK {
		st.LayoutRepairRequired = true
		st.LayoutRepairCommand = "canon init --ai off"
		if err != nil {
			return st, err
		}
		return st, layoutMissingError(missing)
	}
	specs, err := loadSpecs(root)
	if err != nil {
		return st, err
	}
	index := buildIndex(specs)
	entries, err := LoadLedger(root)
	if err != nil {
		return st, err
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
	return st, nil
}
