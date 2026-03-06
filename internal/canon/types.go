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

type PrivacyCheckStatus string

const (
	PrivacyCheckStatusSupported    PrivacyCheckStatus = "supported"
	PrivacyCheckStatusContradicted PrivacyCheckStatus = "contradicted"
	PrivacyCheckStatusUnverifiable PrivacyCheckStatus = "unverifiable"
)

type PrivacyCheckSeverity string

const (
	PrivacyCheckSeverityNone     PrivacyCheckSeverity = "none"
	PrivacyCheckSeverityLow      PrivacyCheckSeverity = "low"
	PrivacyCheckSeverityMedium   PrivacyCheckSeverity = "medium"
	PrivacyCheckSeverityHigh     PrivacyCheckSeverity = "high"
	PrivacyCheckSeverityCritical PrivacyCheckSeverity = "critical"
)

type PrivacyCheckOptions struct {
	PolicyFile        string
	CodePaths         []string
	ContextLimitBytes int
	MaxFileBytes      int64
	AIMode            string
	AIProvider        string
	ResponseFile      string
	FailOn            PrivacyCheckSeverity
}

type PrivacyCheckFinding struct {
	ClaimID          string               `json:"claim_id,omitempty"`
	Claim            string               `json:"claim"`
	Status           PrivacyCheckStatus   `json:"status"`
	Severity         PrivacyCheckSeverity `json:"severity"`
	Reason           string               `json:"reason"`
	EvidencePaths    []string             `json:"evidence_paths,omitempty"`
	EvidenceSnippets []string             `json:"evidence_snippets,omitempty"`
}

type PrivacyCheckStatusCounts struct {
	Supported    int `json:"supported"`
	Contradicted int `json:"contradicted"`
	Unverifiable int `json:"unverifiable"`
}

type PrivacyCheckSeverityCounts struct {
	Low      int `json:"low"`
	Medium   int `json:"medium"`
	High     int `json:"high"`
	Critical int `json:"critical"`
}

type PrivacyCheckSummary struct {
	TotalFindings      int                        `json:"total_findings"`
	SupportedClaims    int                        `json:"supported_claims"`
	ContradictedClaims int                        `json:"contradicted_claims"`
	UnverifiableClaims int                        `json:"unverifiable_claims"`
	HighestSeverity    PrivacyCheckSeverity       `json:"highest_severity"`
	FindingsByStatus   PrivacyCheckStatusCounts   `json:"findings_by_status"`
	FindingsBySeverity PrivacyCheckSeverityCounts `json:"findings_by_severity"`
}

type PrivacyCheckContextSummary struct {
	FoundFiles      int `json:"found_files"`
	IncludedFiles   int `json:"included_files"`
	ExcludedFiles   int `json:"excluded_files"`
	ContextBytes    int `json:"context_bytes"`
	ContextLimit    int `json:"context_limit"`
	MaxFileBytes    int `json:"max_file_bytes"`
	TruncatedToFit  bool `json:"truncated_to_fit"`
	PolicyBytesUsed int `json:"policy_bytes_used"`
}

type PrivacyCheckResult struct {
	Root              string                     `json:"root"`
	PolicyFile        string                     `json:"policy_file"`
	CodePaths         []string                   `json:"code_paths"`
	Context           PrivacyCheckContextSummary `json:"context"`
	Findings          []PrivacyCheckFinding      `json:"findings"`
	Summary           PrivacyCheckSummary        `json:"summary"`
	FailOn            PrivacyCheckSeverity       `json:"fail_on,omitempty"`
	ThresholdExceeded bool                       `json:"threshold_exceeded"`
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
