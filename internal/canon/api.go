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

func SecurityFootgunForCLI(root string, opts SecurityFootgunOptions) (SecurityFootgunResult, error) {
	return SecurityFootgun(root, opts)
}

func ParseSecurityFootgunSeverityForCLI(value string) (SecurityFootgunSeverity, error) {
	return parseSecurityFootgunSeverity(value)
}

func SecurityFootgunExceedsThresholdForCLI(result SecurityFootgunResult, threshold SecurityFootgunSeverity) bool {
	return securityFootgunExceedsThreshold(result, threshold)
}
