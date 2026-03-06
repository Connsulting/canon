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

func TestFlakinessForCLI(root string, opts TestFlakinessOptions) (TestFlakinessResult, error) {
	return TestFlakiness(root, opts)
}

func TestFlakinessExceedsThresholdForCLI(result TestFlakinessResult) bool {
	return testFlakinessExceedsThreshold(result)
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
