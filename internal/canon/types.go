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

type DependencyRiskSeverity string

const (
	DependencyRiskSeverityNone     DependencyRiskSeverity = "none"
	DependencyRiskSeverityLow      DependencyRiskSeverity = "low"
	DependencyRiskSeverityMedium   DependencyRiskSeverity = "medium"
	DependencyRiskSeverityHigh     DependencyRiskSeverity = "high"
	DependencyRiskSeverityCritical DependencyRiskSeverity = "critical"
)

type DependencyRiskOptions struct {
	FailOn DependencyRiskSeverity
}

type DependencyRiskFinding struct {
	RuleID   string                 `json:"rule_id"`
	Category string                 `json:"category"`
	Severity DependencyRiskSeverity `json:"severity"`
	Module   string                 `json:"module,omitempty"`
	Version  string                 `json:"version,omitempty"`
	Replace  string                 `json:"replace,omitempty"`
	Message  string                 `json:"message"`
}

type DependencyRiskSeverityCounts struct {
	Low      int `json:"low"`
	Medium   int `json:"medium"`
	High     int `json:"high"`
	Critical int `json:"critical"`
}

type DependencyRiskSummary struct {
	TotalFindings       int                          `json:"total_findings"`
	SecurityFindings    int                          `json:"security_findings"`
	MaintenanceFindings int                          `json:"maintenance_findings"`
	HighestSeverity     DependencyRiskSeverity       `json:"highest_severity"`
	FindingsBySeverity  DependencyRiskSeverityCounts `json:"findings_by_severity"`
}

type DependencyRiskResult struct {
	Root              string                  `json:"root"`
	GoModPath         string                  `json:"go_mod_path"`
	GoSumPath         string                  `json:"go_sum_path"`
	GoSumPresent      bool                    `json:"go_sum_present"`
	DependencyCount   int                     `json:"dependency_count"`
	Findings          []DependencyRiskFinding `json:"findings"`
	Summary           DependencyRiskSummary   `json:"summary"`
	FailOn            DependencyRiskSeverity  `json:"fail_on,omitempty"`
	ThresholdExceeded bool                    `json:"threshold_exceeded"`
}

type SchemaEvolutionSeverity string

const (
	SchemaEvolutionSeverityNone     SchemaEvolutionSeverity = "none"
	SchemaEvolutionSeverityLow      SchemaEvolutionSeverity = "low"
	SchemaEvolutionSeverityMedium   SchemaEvolutionSeverity = "medium"
	SchemaEvolutionSeverityHigh     SchemaEvolutionSeverity = "high"
	SchemaEvolutionSeverityCritical SchemaEvolutionSeverity = "critical"
)

type SchemaEvolutionOptions struct {
	FailOn SchemaEvolutionSeverity
}

type SchemaEvolutionFinding struct {
	RuleID    string                  `json:"rule_id"`
	Severity  SchemaEvolutionSeverity `json:"severity"`
	File      string                  `json:"file"`
	Line      int                     `json:"line"`
	Statement string                  `json:"statement"`
	Message   string                  `json:"message"`
}

type SchemaEvolutionSeverityCounts struct {
	Low      int `json:"low"`
	Medium   int `json:"medium"`
	High     int `json:"high"`
	Critical int `json:"critical"`
}

type SchemaEvolutionSummary struct {
	TotalFindings      int                           `json:"total_findings"`
	HighestSeverity    SchemaEvolutionSeverity       `json:"highest_severity"`
	FindingsBySeverity SchemaEvolutionSeverityCounts `json:"findings_by_severity"`
}

type SchemaEvolutionResult struct {
	Root               string                   `json:"root"`
	MigrationFileCount int                      `json:"migration_file_count"`
	StatementCount     int                      `json:"statement_count"`
	Findings           []SchemaEvolutionFinding `json:"findings"`
	Summary            SchemaEvolutionSummary   `json:"summary"`
	FailOn             SchemaEvolutionSeverity  `json:"fail_on,omitempty"`
	ThresholdExceeded  bool                     `json:"threshold_exceeded"`
}

type RoadmapEntropySeverity string

const (
	RoadmapEntropySeverityNone     RoadmapEntropySeverity = "none"
	RoadmapEntropySeverityLow      RoadmapEntropySeverity = "low"
	RoadmapEntropySeverityMedium   RoadmapEntropySeverity = "medium"
	RoadmapEntropySeverityHigh     RoadmapEntropySeverity = "high"
	RoadmapEntropySeverityCritical RoadmapEntropySeverity = "critical"
)

type RoadmapEntropyOptions struct {
	Window int
	FailOn RoadmapEntropySeverity
}

type RoadmapEntropyFinding struct {
	RuleID        string                 `json:"rule_id"`
	Category      string                 `json:"category"`
	Severity      RoadmapEntropySeverity `json:"severity"`
	Message       string                 `json:"message"`
	BaselineCount int                    `json:"baseline_count,omitempty"`
	RecentCount   int                    `json:"recent_count,omitempty"`
	BaselineRatio float64                `json:"baseline_ratio,omitempty"`
	RecentRatio   float64                `json:"recent_ratio,omitempty"`
	RatioDelta    float64                `json:"ratio_delta,omitempty"`
	Domains       []string               `json:"domains,omitempty"`
	SpecIDs       []string               `json:"spec_ids,omitempty"`
}

type RoadmapEntropyCategoryCounts struct {
	ScopeCreep int `json:"scope_creep"`
	Drift      int `json:"drift"`
}

type RoadmapEntropySeverityCounts struct {
	Low      int `json:"low"`
	Medium   int `json:"medium"`
	High     int `json:"high"`
	Critical int `json:"critical"`
}

type RoadmapEntropySummary struct {
	TotalFindings      int                          `json:"total_findings"`
	HighestSeverity    RoadmapEntropySeverity       `json:"highest_severity"`
	FindingsByCategory RoadmapEntropyCategoryCounts `json:"findings_by_category"`
	FindingsBySeverity RoadmapEntropySeverityCounts `json:"findings_by_severity"`
}

type RoadmapEntropyResult struct {
	Root                string                  `json:"root"`
	Window              int                     `json:"window"`
	InsufficientHistory bool                    `json:"insufficient_history"`
	OrderedSpecCount    int                     `json:"ordered_spec_count"`
	BaselineSpecIDs     []string                `json:"baseline_spec_ids"`
	RecentSpecIDs       []string                `json:"recent_spec_ids"`
	Findings            []RoadmapEntropyFinding `json:"findings"`
	Summary             RoadmapEntropySummary   `json:"summary"`
	FailOn              RoadmapEntropySeverity  `json:"fail_on,omitempty"`
	ThresholdExceeded   bool                    `json:"threshold_exceeded"`
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
