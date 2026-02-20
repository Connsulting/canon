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
