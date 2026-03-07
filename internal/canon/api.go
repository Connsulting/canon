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

func SchemaEvolutionForCLI(root string, opts SchemaEvolutionOptions) (SchemaEvolutionResult, error) {
	return SchemaEvolution(root, opts)
}

func ParseSchemaEvolutionSeverityForCLI(value string) (SchemaEvolutionSeverity, error) {
	return parseSchemaEvolutionSeverity(value)
}

func SchemaEvolutionExceedsThresholdForCLI(result SchemaEvolutionResult, threshold SchemaEvolutionSeverity) bool {
	return schemaEvolutionExceedsThreshold(result, threshold)
}

func SemanticDiffForCLI(root string, opts SemanticDiffOptions) (SemanticDiffResult, error) {
	return SemanticDiff(root, opts)
}
