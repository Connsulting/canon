package canon

func LoadSpecsForCLI(root string) ([]Spec, error) {
	return loadSpecs(root)
}

func BuildIndexYAMLForCLI(specs []Spec) string {
	index := buildIndex(specs)
	return serializeIndexYAML(index)
}

func WriteTextIfChangedForCLI(path string, content string) (bool, error) {
	return writeTextIfChanged(path, content)
}

func BuildLogViewForCLI(root string, opts LogOptions) ([]LogNode, error) {
	return BuildLogView(root, opts)
}

func RenderLogTextForCLI(nodes []LogNode, opts LogOptions) string {
	return RenderLogText(nodes, opts)
}

func BlameForCLI(root string, input BlameInput) (BlameResult, error) {
	return Blame(root, input)
}

func CheckForCLI(root string, opts CheckOptions) (CheckResult, error) {
	return Check(root, opts)
}

func DependencyRiskForCLI(root string, opts DependencyRiskOptions) (DependencyRiskResult, error) {
	return DependencyRisk(root, opts)
}

func ParseDependencyRiskSeverityForCLI(value string) (DependencyRiskSeverity, error) {
	return parseDependencyRiskSeverity(value)
}

func DependencyRiskExceedsThresholdForCLI(result DependencyRiskResult, threshold DependencyRiskSeverity) bool {
	return dependencyRiskExceedsThreshold(result, threshold)
}

func PrivacyCheckForCLI(root string, opts PrivacyCheckOptions) (PrivacyCheckResult, error) {
	return PrivacyCheck(root, opts)
}

func ParsePrivacyCheckSeverityForCLI(value string) (PrivacyCheckSeverity, error) {
	return parsePrivacyCheckSeverity(value)
}

func PrivacyCheckExceedsThresholdForCLI(result PrivacyCheckResult, threshold PrivacyCheckSeverity) bool {
	return privacyCheckExceedsThreshold(result, threshold)
}

func LoggingAuditForCLI(root string, opts LoggingAuditOptions) (LoggingAuditResult, error) {
	return LoggingAudit(root, opts)
}

func ParseLoggingAuditSeverityForCLI(value string) (LoggingAuditSeverity, error) {
	return parseLoggingAuditSeverity(value)
}

func LoggingAuditExceedsThresholdForCLI(result LoggingAuditResult, threshold LoggingAuditSeverity) bool {
	return loggingAuditExceedsThreshold(result, threshold)
}

func RoadmapEntropyForCLI(root string, opts RoadmapEntropyOptions) (RoadmapEntropyResult, error) {
	return RoadmapEntropy(root, opts)
}

func ParseRoadmapEntropySeverityForCLI(value string) (RoadmapEntropySeverity, error) {
	return parseRoadmapEntropySeverity(value)
}

func RoadmapEntropyExceedsThresholdForCLI(result RoadmapEntropyResult, threshold RoadmapEntropySeverity) bool {
	return roadmapEntropyExceedsThreshold(result, threshold)
}
