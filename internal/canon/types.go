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

type SemanticDiffOptions struct {
	DiffFile     string
	AIMode       string
	AIProvider   string
	ResponseFile string
}

type SemanticDiffInput struct {
	Root         string                   `json:"root"`
	DiffSource   string                   `json:"diff_source"`
	DiffText     string                   `json:"-"`
	ChangedFiles []SemanticDiffFileChange `json:"changed_files"`
}

type SemanticDiffImpact string

const (
	SemanticDiffImpactNone     SemanticDiffImpact = "none"
	SemanticDiffImpactLow      SemanticDiffImpact = "low"
	SemanticDiffImpactMedium   SemanticDiffImpact = "medium"
	SemanticDiffImpactHigh     SemanticDiffImpact = "high"
	SemanticDiffImpactCritical SemanticDiffImpact = "critical"
)

type SemanticDiffFileChange struct {
	File         string `json:"file"`
	Status       string `json:"status"`
	AddedLines   int    `json:"added_lines"`
	DeletedLines int    `json:"deleted_lines"`
	HunkCount    int    `json:"hunk_count"`
}

type SemanticDiffEvidenceKind string

const (
	SemanticDiffEvidenceKindFile SemanticDiffEvidenceKind = "file"
	SemanticDiffEvidenceKindHunk SemanticDiffEvidenceKind = "hunk"
)

type SemanticDiffEvidence struct {
	File     string                   `json:"file"`
	Kind     SemanticDiffEvidenceKind `json:"kind"`
	OldStart int                      `json:"old_start,omitempty"`
	OldLines int                      `json:"old_lines,omitempty"`
	NewStart int                      `json:"new_start,omitempty"`
	NewLines int                      `json:"new_lines,omitempty"`
}

type SemanticDiffExplanation struct {
	ID        string                 `json:"id"`
	Category  string                 `json:"category"`
	Impact    SemanticDiffImpact     `json:"impact"`
	Summary   string                 `json:"summary"`
	Rationale string                 `json:"rationale"`
	Evidence  []SemanticDiffEvidence `json:"evidence,omitempty"`
}

type SemanticDiffImpactCounts struct {
	Low      int `json:"low"`
	Medium   int `json:"medium"`
	High     int `json:"high"`
	Critical int `json:"critical"`
}

type SemanticDiffCategoryCount struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
}

type SemanticDiffSummary struct {
	AIModel           string                      `json:"ai_model,omitempty"`
	AISummary         string                      `json:"ai_summary,omitempty"`
	TotalExplanations int                         `json:"total_explanations"`
	HighestImpact     SemanticDiffImpact          `json:"highest_impact"`
	ImpactCounts      SemanticDiffImpactCounts    `json:"impact_counts"`
	CategoryCounts    []SemanticDiffCategoryCount `json:"category_counts"`
}

type SemanticDiffResult struct {
	Root              string                    `json:"root"`
	DiffSource        string                    `json:"diff_source"`
	DiffBytes         int                       `json:"diff_bytes"`
	ChangedFileCount  int                       `json:"changed_file_count"`
	TotalAddedLines   int                       `json:"total_added_lines"`
	TotalDeletedLines int                       `json:"total_deleted_lines"`
	TotalHunks        int                       `json:"total_hunks"`
	ChangedFiles      []SemanticDiffFileChange  `json:"changed_files"`
	Explanations      []SemanticDiffExplanation `json:"explanations"`
	Summary           SemanticDiffSummary       `json:"summary"`
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
