package canon

type Spec struct {
	ID             string
	Type           string
	Title          string
	Domain         string
	Created        string
	DependsOn      []string
	TouchedDomains []string
	Consolidates   []string
	Path           string
	Body           string
}

type LedgerEntry struct {
	SpecID      string   `json:"spec_id"`
	Title       string   `json:"title"`
	Type        string   `json:"type"`
	Domain      string   `json:"domain"`
	Operation   string   `json:"operation,omitempty"`
	Parents     []string `json:"parents"`
	Sequence    int64    `json:"sequence"`
	IngestedAt  string   `json:"ingested_at"`
	ContentHash string   `json:"content_hash"`
	SpecPath    string   `json:"spec_path"`
	SourcePath  string   `json:"source_path"`
}

type IngestInput struct {
	IngestKind     string
	File           string
	Text           string
	ID             string
	Type           string
	Title          string
	Domain         string
	Created        string
	DependsOn      []string
	TouchedDomains []string
	Parents        []string
	NoAutoParents  bool
	ConflictMode   string
	ResponseFile   string
	AIProvider     string
}

type IngestResult struct {
	SpecID     string
	SpecPath   string
	LedgerPath string
	Parents    []string
}

type ResetInput struct {
	RefSpecID string
}

type ResetResult struct {
	KeptSpecID    string
	LedgerDeleted int
	SpecDeleted   int
	SourceDeleted int
}

type RenderOptions struct {
	Write        bool
	AIMode       string
	AIProvider   string
	ResponseFile string
}

type RenderResult struct {
	FilesRemoved     int
	FilesUpdated     int
	FilesWritten     int
	DomainChecksums  map[string]string
	AIUsed           bool
	AIFallback       bool
	AIFallbackReason string
}

type BlameInput struct {
	Query        string
	Domain       string
	AIProvider   string
	ResponseFile string
}

type BlameResult struct {
	Query   string      `json:"query"`
	Found   bool        `json:"found"`
	Results []BlameSpec `json:"results,omitempty"`
}

type BlameSpec struct {
	SpecID        string   `json:"spec_id"`
	Title         string   `json:"title"`
	Domain        string   `json:"domain"`
	Confidence    string   `json:"confidence"`
	Created       string   `json:"created"`
	RelevantLines []string `json:"relevant_lines"`
}
type CheckOptions struct {
	Domain       string
	SpecID       string
	Write        bool
	AIMode       string
	AIProvider   string
	ResponseFile string
}

type CheckConflict struct {
	SpecA          string   `json:"spec_a"`
	TitleA         string   `json:"title_a"`
	SpecB          string   `json:"spec_b"`
	TitleB         string   `json:"title_b"`
	Domain         string   `json:"domain"`
	StatementKey   string   `json:"statement_key"`
	LineA          string   `json:"line_a"`
	LineB          string   `json:"line_b"`
	OverlapDomains []string `json:"-"`
}

type CheckResult struct {
	Passed         bool            `json:"passed"`
	TotalSpecs     int             `json:"total_specs"`
	TotalConflicts int             `json:"total_conflicts"`
	Conflicts      []CheckConflict `json:"conflicts"`
	ReportPaths    []string        `json:"report_paths,omitempty"`
}
type Index struct {
	Specs            map[string]Spec
	Domains          map[string][]string
	CrossDomainEdges []CrossDomainEdge
	DependsOn        map[string]map[string]struct{}
	DependedOnBy     map[string]map[string]struct{}
}

type CrossDomainEdge struct {
	Spec    string
	Domains []string
}
