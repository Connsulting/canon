package canon

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

var metricsCoverageScopeTokens = []string{
	"metric",
	"metrics",
	"telemetry",
	"stats",
	"prometheus",
	"meter",
	"instrument",
}

var metricsCoverageEmitterVerbs = []string{
	"inc",
	"increment",
	"add",
	"observe",
	"record",
	"emit",
	"count",
	"timing",
	"duration",
}

func MetricsCoverage(root string, opts MetricsCoverageOptions) (MetricsCoverageResult, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return MetricsCoverageResult{}, err
	}

	rootInfo, err := os.Stat(absRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return MetricsCoverageResult{}, fmt.Errorf("root path not found: %s", filepath.ToSlash(absRoot))
		}
		return MetricsCoverageResult{}, err
	}
	if !rootInfo.IsDir() {
		return MetricsCoverageResult{}, fmt.Errorf("root path is not a directory: %s", filepath.ToSlash(absRoot))
	}

	if opts.FailUnder != nil {
		if *opts.FailUnder < 0 || *opts.FailUnder > 100 {
			return MetricsCoverageResult{}, fmt.Errorf("fail-under must be between 0 and 100")
		}
	}

	goFiles, err := collectMetricsCoverageGoFiles(absRoot)
	if err != nil {
		return MetricsCoverageResult{}, err
	}

	findings := make([]MetricsCoverageFinding, 0)
	for _, path := range goFiles {
		fileSet := token.NewFileSet()
		node, err := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return MetricsCoverageResult{}, fmt.Errorf("parse %s: %w", filepath.ToSlash(path), err)
		}

		relPath, err := filepath.Rel(absRoot, path)
		if err != nil {
			relPath = path
		}
		relPath = filepath.ToSlash(relPath)

		for _, decl := range node.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !isMetricsCoverageTargetHandler(relPath, fn) {
				continue
			}

			line := fileSet.Position(fn.Pos()).Line
			matches := detectMetricsEmitterCalls(fn)
			findings = append(findings, MetricsCoverageFinding{
				Handler:      fn.Name.Name,
				File:         relPath,
				Line:         line,
				Instrumented: len(matches) > 0,
				MatchedCalls: matches,
			})
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		left := findings[i]
		right := findings[j]
		if left.File != right.File {
			return left.File < right.File
		}
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		return left.Handler < right.Handler
	})

	missingTargets := make([]string, 0)
	instrumentedCount := 0
	for _, finding := range findings {
		if finding.Instrumented {
			instrumentedCount++
			continue
		}
		missingTargets = append(missingTargets, fmt.Sprintf("%s (%s:%d)", finding.Handler, finding.File, finding.Line))
	}

	summary := MetricsCoverageSummary{
		TargetCount:       len(findings),
		InstrumentedCount: instrumentedCount,
		MissingCount:      len(findings) - instrumentedCount,
		CoveragePercent:   100.0,
	}
	if summary.TargetCount > 0 {
		coverage := (float64(summary.InstrumentedCount) / float64(summary.TargetCount)) * 100
		summary.CoveragePercent = roundMetricsCoveragePercent(coverage)
	}

	result := MetricsCoverageResult{
		Root:           absRoot,
		Findings:       findings,
		MissingTargets: missingTargets,
		Summary:        summary,
	}

	if opts.FailUnder != nil {
		failUnder := *opts.FailUnder
		result.FailUnder = &failUnder
		result.ThresholdExceeded = metricsCoverageExceedsThreshold(result, failUnder)
	}

	return result, nil
}

func collectMetricsCoverageGoFiles(root string) ([]string, error) {
	files := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if d.IsDir() {
			switch d.Name() {
			case ".git", ".canon", "vendor", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}

		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func isMetricsCoverageTargetHandler(relPath string, fn *ast.FuncDecl) bool {
	if fn == nil || fn.Name == nil || fn.Recv != nil || fn.Type == nil || fn.Type.Params == nil || fn.Type.Results == nil {
		return false
	}
	if !strings.HasPrefix(relPath, "cmd/") {
		return false
	}

	name := fn.Name.Name
	if !strings.HasPrefix(name, "cmd") || len(name) <= 3 {
		return false
	}
	firstSuffixRune := []rune(name[3:])[0]
	if !unicode.IsUpper(firstSuffixRune) {
		return false
	}

	if len(fn.Type.Params.List) != 1 || len(fn.Type.Results.List) != 1 {
		return false
	}
	if !isMetricsCoverageStringSliceType(fn.Type.Params.List[0].Type) {
		return false
	}
	return isMetricsCoverageErrorType(fn.Type.Results.List[0].Type)
}

func isMetricsCoverageStringSliceType(expr ast.Expr) bool {
	arrayType, ok := expr.(*ast.ArrayType)
	if !ok || arrayType.Len != nil {
		return false
	}
	elt, ok := arrayType.Elt.(*ast.Ident)
	return ok && elt.Name == "string"
}

func isMetricsCoverageErrorType(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "error"
}

func detectMetricsEmitterCalls(fn *ast.FuncDecl) []string {
	if fn == nil || fn.Body == nil {
		return nil
	}

	matches := make([]string, 0)
	seen := make(map[string]struct{})
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if !isMetricsEmitterCall(call.Fun) {
			return true
		}
		segments := metricsCalleeSegments(call.Fun)
		if len(segments) == 0 {
			return true
		}
		callee := strings.Join(segments, ".")
		if _, exists := seen[callee]; exists {
			return true
		}
		seen[callee] = struct{}{}
		matches = append(matches, callee)
		return true
	})

	sort.Strings(matches)
	return matches
}

func isMetricsEmitterCall(fun ast.Expr) bool {
	segments := metricsCalleeSegments(fun)
	if len(segments) == 0 {
		return false
	}

	normalized := make([]string, 0, len(segments))
	for _, segment := range segments {
		trimmed := strings.ToLower(strings.TrimSpace(segment))
		if trimmed != "" {
			normalized = append(normalized, trimmed)
		}
	}
	if len(normalized) == 0 {
		return false
	}

	full := strings.Join(normalized, ".")
	last := normalized[len(normalized)-1]
	if metricsCoverageContainsScopeToken(full) && metricsCoverageContainsEmitterVerb(last) {
		return true
	}
	if strings.Contains(last, "metric") && metricsCoverageContainsEmitterVerb(last) {
		return true
	}
	return false
}

func metricsCoverageContainsScopeToken(value string) bool {
	for _, token := range metricsCoverageScopeTokens {
		if strings.Contains(value, token) {
			return true
		}
	}
	return false
}

func metricsCoverageContainsEmitterVerb(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return false
	}

	if normalized == "set" || strings.HasSuffix(normalized, "setmetric") {
		return true
	}

	for _, verb := range metricsCoverageEmitterVerbs {
		if normalized == verb || strings.HasPrefix(normalized, verb) || strings.HasSuffix(normalized, verb) {
			return true
		}
		if strings.Contains(normalized, verb+"metric") {
			return true
		}
	}
	return false
}

func metricsCalleeSegments(expr ast.Expr) []string {
	switch node := expr.(type) {
	case *ast.Ident:
		return []string{node.Name}
	case *ast.SelectorExpr:
		base := metricsCalleeSegments(node.X)
		return append(base, node.Sel.Name)
	case *ast.CallExpr:
		return metricsCalleeSegments(node.Fun)
	case *ast.IndexExpr:
		return metricsCalleeSegments(node.X)
	case *ast.IndexListExpr:
		return metricsCalleeSegments(node.X)
	case *ast.ParenExpr:
		return metricsCalleeSegments(node.X)
	default:
		return nil
	}
}

func roundMetricsCoveragePercent(value float64) float64 {
	return math.Round(value*100) / 100
}

func parseMetricsCoverageFailUnder(value string) (float64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, fmt.Errorf("threshold value is required")
	}
	percent, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || math.IsNaN(percent) || math.IsInf(percent, 0) {
		return 0, fmt.Errorf("threshold must be a number between 0 and 100")
	}
	if percent < 0 || percent > 100 {
		return 0, fmt.Errorf("threshold must be a number between 0 and 100")
	}
	return roundMetricsCoveragePercent(percent), nil
}

func metricsCoverageExceedsThreshold(result MetricsCoverageResult, failUnder float64) bool {
	return result.Summary.CoveragePercent < failUnder
}
