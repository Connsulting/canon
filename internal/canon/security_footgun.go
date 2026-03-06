package canon

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const (
	securityFootgunCategoryCommandExecution = "command-execution"
	securityFootgunCategoryCryptography     = "cryptography"
	securityFootgunCategorySecrets          = "secrets-management"
	securityFootgunCategoryTransport        = "transport-security"

	securityFootgunRuleExecShellC                 = "exec-shell-c"
	securityFootgunRuleHardcodedCredentialLiteral = "hardcoded-credential-literal"
	securityFootgunRuleInsecureRandSensitive      = "insecure-rand-sensitive-context"
	securityFootgunRuleTLSInsecureSkipVerify      = "tls-insecure-skip-verify"
	securityFootgunRuleWeakHashImport             = "weak-hash-import"
)

var securityFootgunSensitiveNameTokens = []string{
	"password",
	"passwd",
	"pwd",
	"secret",
	"token",
	"credential",
	"apikey",
	"api_key",
	"clientsecret",
	"client_secret",
	"accesskey",
	"access_key",
	"privatekey",
	"private_key",
	"authkey",
	"auth_key",
	"sessionkey",
	"session_key",
}

var securityFootgunCredentialPlaceholderTokens = []string{
	"example",
	"sample",
	"dummy",
	"placeholder",
	"changeme",
	"change_me",
	"change-me",
	"test",
	"todo",
	"redacted",
	"your-",
	"your_",
}

var securityFootgunShellNames = map[string]struct{}{
	"bash":       {},
	"cmd":        {},
	"fish":       {},
	"ksh":        {},
	"powershell": {},
	"pwsh":       {},
	"sh":         {},
	"zsh":        {},
}

var securityFootgunShellFlags = map[string]struct{}{
	"-c":              {},
	"-command":        {},
	"/c":              {},
	"/command":        {},
	"-enc":            {},
	"-encodedcommand": {},
}

func SecurityFootgun(root string, opts SecurityFootgunOptions) (SecurityFootgunResult, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return SecurityFootgunResult{}, err
	}

	paths, err := collectSecurityFootgunGoFiles(absRoot)
	if err != nil {
		return SecurityFootgunResult{}, err
	}

	findings := make([]SecurityFootgunFinding, 0)
	for _, pathAbs := range paths {
		fileFindings, err := analyzeSecurityFootgunFile(absRoot, pathAbs)
		if err != nil {
			return SecurityFootgunResult{}, err
		}
		findings = append(findings, fileFindings...)
	}

	sortSecurityFootgunFindings(findings)
	result := SecurityFootgunResult{
		Root:         absRoot,
		FilesScanned: len(paths),
		Findings:     findings,
		Summary:      summarizeSecurityFootgunFindings(findings),
	}

	if strings.TrimSpace(string(opts.FailOn)) != "" {
		failOn, err := parseSecurityFootgunSeverity(string(opts.FailOn))
		if err != nil {
			return SecurityFootgunResult{}, err
		}
		if failOn != SecurityFootgunSeverityNone {
			result.FailOn = failOn
			result.ThresholdExceeded = securityFootgunExceedsThreshold(result, failOn)
		}
	}

	return result, nil
}

func collectSecurityFootgunGoFiles(root string) ([]string, error) {
	paths := make([]string, 0)
	err := filepath.WalkDir(root, func(pathAbs string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if pathAbs == root {
			return nil
		}

		rel := securityFootgunRelativePath(root, pathAbs)
		if entry.IsDir() {
			if shouldSkipDir(rel) || securityFootgunShouldSkipDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}

		if !entry.Type().IsRegular() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".go") {
			return nil
		}

		paths = append(paths, pathAbs)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(paths)
	return paths, nil
}

func securityFootgunShouldSkipDir(rel string) bool {
	trimmed := strings.TrimSpace(filepath.ToSlash(rel))
	if trimmed == "" || trimmed == "." {
		return false
	}

	parts := strings.Split(trimmed, "/")
	for _, part := range parts {
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, ".") {
			return true
		}
		switch part {
		case "testdata", "third_party", "tmp", "tmpdir":
			return true
		}
	}
	return false
}

func analyzeSecurityFootgunFile(root string, pathAbs string) ([]SecurityFootgunFinding, error) {
	rel := securityFootgunRelativePath(root, pathAbs)

	content, err := os.ReadFile(pathAbs)
	if err != nil {
		return nil, err
	}
	if isBinaryContent(content) {
		return nil, nil
	}

	fset := token.NewFileSet()
	parsed, parseErr := parser.ParseFile(fset, rel, content, parser.SkipObjectResolution)
	if parseErr != nil {
		return nil, nil
	}

	lines := strings.Split(string(content), "\n")
	imports := securityFootgunImportAliases(parsed)
	parents := securityFootgunParentMap(parsed)

	findings := make([]SecurityFootgunFinding, 0)
	seen := map[string]struct{}{}
	appendFinding := func(f SecurityFootgunFinding) {
		key := fmt.Sprintf("%s|%s|%s|%d|%d|%s", f.RuleID, f.File, f.Severity, f.Line, f.Column, f.Message)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		findings = append(findings, f)
	}

	for _, spec := range parsed.Imports {
		importPath, ok := securityFootgunUnquoteImportPath(spec)
		if !ok {
			continue
		}
		if importPath != "crypto/md5" && importPath != "crypto/sha1" {
			continue
		}
		line, column, snippet := securityFootgunPositionEvidence(fset, lines, spec)
		appendFinding(SecurityFootgunFinding{
			RuleID:   securityFootgunRuleWeakHashImport,
			Category: securityFootgunCategoryCryptography,
			Severity: SecurityFootgunSeverityMedium,
			File:     rel,
			Line:     line,
			Column:   column,
			Snippet:  snippet,
			Message:  fmt.Sprintf("imports weak hash package %q; prefer stronger primitives", importPath),
		})
	}

	ast.Inspect(parsed, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.CompositeLit:
			securityFootgunDetectCompositeLit(rel, fset, lines, n, appendFinding)
		case *ast.ValueSpec:
			securityFootgunDetectValueSpecCredential(rel, fset, lines, n, appendFinding)
		case *ast.AssignStmt:
			securityFootgunDetectAssignTLSInsecureSkipVerify(rel, fset, lines, n, appendFinding)
			securityFootgunDetectAssignCredential(rel, fset, lines, n, appendFinding)
		case *ast.CallExpr:
			securityFootgunDetectExecShell(rel, fset, lines, imports, n, appendFinding)
			securityFootgunDetectInsecureRand(rel, fset, lines, imports, parents, n, appendFinding)
		}
		return true
	})

	return findings, nil
}

func securityFootgunRelativePath(root string, pathAbs string) string {
	rel, err := filepath.Rel(root, pathAbs)
	if err != nil {
		return filepath.ToSlash(pathAbs)
	}
	return filepath.ToSlash(rel)
}

func securityFootgunImportAliases(file *ast.File) map[string]string {
	aliases := make(map[string]string, len(file.Imports))
	for _, spec := range file.Imports {
		pathValue, ok := securityFootgunUnquoteImportPath(spec)
		if !ok {
			continue
		}

		alias := ""
		if spec.Name != nil {
			alias = strings.TrimSpace(spec.Name.Name)
		}
		if alias == "" {
			alias = path.Base(pathValue)
		}
		if alias == "_" || alias == "." {
			continue
		}
		aliases[alias] = pathValue
	}
	return aliases
}

func securityFootgunUnquoteImportPath(spec *ast.ImportSpec) (string, bool) {
	if spec == nil || spec.Path == nil {
		return "", false
	}
	value, err := strconv.Unquote(spec.Path.Value)
	if err != nil {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	return value, true
}

func securityFootgunParentMap(root ast.Node) map[ast.Node]ast.Node {
	parents := make(map[ast.Node]ast.Node)
	stack := make([]ast.Node, 0, 128)
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			return false
		}
		if len(stack) > 0 {
			parents[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})
	return parents
}

func securityFootgunDetectCompositeLit(
	rel string,
	fset *token.FileSet,
	lines []string,
	lit *ast.CompositeLit,
	appendFinding func(SecurityFootgunFinding),
) {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}

		if securityFootgunExprNameEquals(kv.Key, "InsecureSkipVerify") {
			if value, ok := securityFootgunBoolLiteral(kv.Value); ok && value {
				line, column, snippet := securityFootgunPositionEvidence(fset, lines, kv.Value)
				appendFinding(SecurityFootgunFinding{
					RuleID:   securityFootgunRuleTLSInsecureSkipVerify,
					Category: securityFootgunCategoryTransport,
					Severity: SecurityFootgunSeverityHigh,
					File:     rel,
					Line:     line,
					Column:   column,
					Snippet:  snippet,
					Message:  "tls.Config sets InsecureSkipVerify to true",
				})
			}
		}

		if keyName, ok := securityFootgunExprName(kv.Key); ok && securityFootgunIsSensitiveName(keyName) {
			if value, ok := securityFootgunStringLiteral(kv.Value); ok && securityFootgunIsLikelyCredentialLiteral(value) {
				line, column, snippet := securityFootgunPositionEvidence(fset, lines, kv.Value)
				appendFinding(SecurityFootgunFinding{
					RuleID:   securityFootgunRuleHardcodedCredentialLiteral,
					Category: securityFootgunCategorySecrets,
					Severity: SecurityFootgunSeverityCritical,
					File:     rel,
					Line:     line,
					Column:   column,
					Snippet:  snippet,
					Message:  fmt.Sprintf("hardcoded credential literal assigned to %q", keyName),
				})
			}
		}
	}
}

func securityFootgunDetectAssignTLSInsecureSkipVerify(
	rel string,
	fset *token.FileSet,
	lines []string,
	assign *ast.AssignStmt,
	appendFinding func(SecurityFootgunFinding),
) {
	if len(assign.Lhs) == 0 || len(assign.Rhs) == 0 {
		return
	}

	if len(assign.Rhs) == 1 {
		value, ok := securityFootgunBoolLiteral(assign.Rhs[0])
		if !ok || !value {
			return
		}
		for _, lhs := range assign.Lhs {
			if !securityFootgunExprNameEquals(lhs, "InsecureSkipVerify") {
				continue
			}
			line, column, snippet := securityFootgunPositionEvidence(fset, lines, assign.Rhs[0])
			appendFinding(SecurityFootgunFinding{
				RuleID:   securityFootgunRuleTLSInsecureSkipVerify,
				Category: securityFootgunCategoryTransport,
				Severity: SecurityFootgunSeverityHigh,
				File:     rel,
				Line:     line,
				Column:   column,
				Snippet:  snippet,
				Message:  "tls.Config sets InsecureSkipVerify to true",
			})
		}
		return
	}

	max := len(assign.Lhs)
	if len(assign.Rhs) < max {
		max = len(assign.Rhs)
	}
	for i := 0; i < max; i++ {
		value, ok := securityFootgunBoolLiteral(assign.Rhs[i])
		if !ok || !value {
			continue
		}
		if !securityFootgunExprNameEquals(assign.Lhs[i], "InsecureSkipVerify") {
			continue
		}
		line, column, snippet := securityFootgunPositionEvidence(fset, lines, assign.Rhs[i])
		appendFinding(SecurityFootgunFinding{
			RuleID:   securityFootgunRuleTLSInsecureSkipVerify,
			Category: securityFootgunCategoryTransport,
			Severity: SecurityFootgunSeverityHigh,
			File:     rel,
			Line:     line,
			Column:   column,
			Snippet:  snippet,
			Message:  "tls.Config sets InsecureSkipVerify to true",
		})
	}
}

func securityFootgunDetectValueSpecCredential(
	rel string,
	fset *token.FileSet,
	lines []string,
	spec *ast.ValueSpec,
	appendFinding func(SecurityFootgunFinding),
) {
	if len(spec.Names) == 0 || len(spec.Values) == 0 {
		return
	}

	if len(spec.Values) == 1 {
		value := spec.Values[0]
		for _, name := range spec.Names {
			target := strings.TrimSpace(name.Name)
			if !securityFootgunIsSensitiveName(target) {
				continue
			}
			if literal, ok := securityFootgunStringLiteral(value); ok && securityFootgunIsLikelyCredentialLiteral(literal) {
				line, column, snippet := securityFootgunPositionEvidence(fset, lines, value)
				appendFinding(SecurityFootgunFinding{
					RuleID:   securityFootgunRuleHardcodedCredentialLiteral,
					Category: securityFootgunCategorySecrets,
					Severity: SecurityFootgunSeverityCritical,
					File:     rel,
					Line:     line,
					Column:   column,
					Snippet:  snippet,
					Message:  fmt.Sprintf("hardcoded credential literal assigned to %q", target),
				})
			}
		}
		return
	}

	max := len(spec.Names)
	if len(spec.Values) < max {
		max = len(spec.Values)
	}
	for i := 0; i < max; i++ {
		target := strings.TrimSpace(spec.Names[i].Name)
		if !securityFootgunIsSensitiveName(target) {
			continue
		}
		if literal, ok := securityFootgunStringLiteral(spec.Values[i]); ok && securityFootgunIsLikelyCredentialLiteral(literal) {
			line, column, snippet := securityFootgunPositionEvidence(fset, lines, spec.Values[i])
			appendFinding(SecurityFootgunFinding{
				RuleID:   securityFootgunRuleHardcodedCredentialLiteral,
				Category: securityFootgunCategorySecrets,
				Severity: SecurityFootgunSeverityCritical,
				File:     rel,
				Line:     line,
				Column:   column,
				Snippet:  snippet,
				Message:  fmt.Sprintf("hardcoded credential literal assigned to %q", target),
			})
		}
	}
}

func securityFootgunDetectAssignCredential(
	rel string,
	fset *token.FileSet,
	lines []string,
	assign *ast.AssignStmt,
	appendFinding func(SecurityFootgunFinding),
) {
	if len(assign.Lhs) == 0 || len(assign.Rhs) == 0 {
		return
	}

	if len(assign.Rhs) == 1 {
		value := assign.Rhs[0]
		for _, lhs := range assign.Lhs {
			targetName, ok := securityFootgunSensitiveTargetName(lhs)
			if !ok {
				continue
			}
			if literal, ok := securityFootgunStringLiteral(value); ok && securityFootgunIsLikelyCredentialLiteral(literal) {
				line, column, snippet := securityFootgunPositionEvidence(fset, lines, value)
				appendFinding(SecurityFootgunFinding{
					RuleID:   securityFootgunRuleHardcodedCredentialLiteral,
					Category: securityFootgunCategorySecrets,
					Severity: SecurityFootgunSeverityCritical,
					File:     rel,
					Line:     line,
					Column:   column,
					Snippet:  snippet,
					Message:  fmt.Sprintf("hardcoded credential literal assigned to %q", targetName),
				})
			}
		}
		return
	}

	max := len(assign.Lhs)
	if len(assign.Rhs) < max {
		max = len(assign.Rhs)
	}
	for i := 0; i < max; i++ {
		targetName, ok := securityFootgunSensitiveTargetName(assign.Lhs[i])
		if !ok {
			continue
		}
		if literal, ok := securityFootgunStringLiteral(assign.Rhs[i]); ok && securityFootgunIsLikelyCredentialLiteral(literal) {
			line, column, snippet := securityFootgunPositionEvidence(fset, lines, assign.Rhs[i])
			appendFinding(SecurityFootgunFinding{
				RuleID:   securityFootgunRuleHardcodedCredentialLiteral,
				Category: securityFootgunCategorySecrets,
				Severity: SecurityFootgunSeverityCritical,
				File:     rel,
				Line:     line,
				Column:   column,
				Snippet:  snippet,
				Message:  fmt.Sprintf("hardcoded credential literal assigned to %q", targetName),
			})
		}
	}
}

func securityFootgunSensitiveTargetName(expr ast.Expr) (string, bool) {
	switch n := expr.(type) {
	case *ast.Ident:
		name := strings.TrimSpace(n.Name)
		if name == "" || !securityFootgunIsSensitiveName(name) {
			return "", false
		}
		return name, true
	case *ast.SelectorExpr:
		name := strings.TrimSpace(n.Sel.Name)
		if name == "" || !securityFootgunIsSensitiveName(name) {
			return "", false
		}
		return name, true
	case *ast.IndexExpr:
		if key, ok := securityFootgunStringLiteral(n.Index); ok && securityFootgunIsSensitiveName(key) {
			return key, true
		}
	}
	return "", false
}

func securityFootgunDetectExecShell(
	rel string,
	fset *token.FileSet,
	lines []string,
	imports map[string]string,
	call *ast.CallExpr,
	appendFinding func(SecurityFootgunFinding),
) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return
	}
	if imports[pkgIdent.Name] != "os/exec" {
		return
	}

	argStart := -1
	switch sel.Sel.Name {
	case "Command":
		argStart = 0
	case "CommandContext":
		argStart = 1
	default:
		return
	}

	if len(call.Args) <= argStart+1 {
		return
	}

	shellArg, ok := securityFootgunStringLiteral(call.Args[argStart])
	if !ok {
		return
	}
	flagArg, ok := securityFootgunStringLiteral(call.Args[argStart+1])
	if !ok {
		return
	}

	shellNormalized := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(shellArg, "\\", "/")))
	shellName := strings.TrimSuffix(path.Base(shellNormalized), ".exe")
	flagNormalized := strings.ToLower(strings.TrimSpace(flagArg))

	if _, ok := securityFootgunShellNames[shellName]; !ok {
		return
	}
	if _, ok := securityFootgunShellFlags[flagNormalized]; !ok {
		return
	}

	line, column, snippet := securityFootgunPositionEvidence(fset, lines, call)
	appendFinding(SecurityFootgunFinding{
		RuleID:   securityFootgunRuleExecShellC,
		Category: securityFootgunCategoryCommandExecution,
		Severity: SecurityFootgunSeverityHigh,
		File:     rel,
		Line:     line,
		Column:   column,
		Snippet:  snippet,
		Message:  fmt.Sprintf("os/exec %s invokes shell %q with %q", sel.Sel.Name, shellArg, flagArg),
	})
}

func securityFootgunDetectInsecureRand(
	rel string,
	fset *token.FileSet,
	lines []string,
	imports map[string]string,
	parents map[ast.Node]ast.Node,
	call *ast.CallExpr,
	appendFinding func(SecurityFootgunFinding),
) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return
	}
	importPath := imports[pkgIdent.Name]
	if importPath != "math/rand" && importPath != "math/rand/v2" {
		return
	}

	sensitive, reason := securityFootgunRandSensitiveContext(call, parents)
	if !sensitive {
		return
	}

	line, column, snippet := securityFootgunPositionEvidence(fset, lines, call)
	appendFinding(SecurityFootgunFinding{
		RuleID:   securityFootgunRuleInsecureRandSensitive,
		Category: securityFootgunCategoryCryptography,
		Severity: SecurityFootgunSeverityHigh,
		File:     rel,
		Line:     line,
		Column:   column,
		Snippet:  snippet,
		Message:  fmt.Sprintf("%s used in security-sensitive context (%s); prefer crypto/rand", importPath, reason),
	})
}

func securityFootgunRandSensitiveContext(node ast.Node, parents map[ast.Node]ast.Node) (bool, string) {
	for current := node; current != nil; current = parents[current] {
		switch n := current.(type) {
		case *ast.AssignStmt:
			if name, ok := securityFootgunSensitiveNameFromExprs(n.Lhs); ok {
				return true, fmt.Sprintf("assigned to %q", name)
			}
		case *ast.ValueSpec:
			if name, ok := securityFootgunSensitiveNameFromIdents(n.Names); ok {
				return true, fmt.Sprintf("assigned to %q", name)
			}
		case *ast.CallExpr:
			if name, ok := securityFootgunCallTargetName(n.Fun); ok && securityFootgunIsSensitiveName(name) {
				return true, fmt.Sprintf("passed into %q", name)
			}
		case *ast.FuncDecl:
			if n.Name != nil {
				name := strings.TrimSpace(n.Name.Name)
				if securityFootgunIsSensitiveName(name) {
					return true, fmt.Sprintf("inside function %q", name)
				}
			}
			return false, ""
		case *ast.File:
			return false, ""
		}
	}
	return false, ""
}

func securityFootgunCallTargetName(expr ast.Expr) (string, bool) {
	switch n := expr.(type) {
	case *ast.Ident:
		name := strings.TrimSpace(n.Name)
		if name == "" {
			return "", false
		}
		return name, true
	case *ast.SelectorExpr:
		name := strings.TrimSpace(n.Sel.Name)
		if name == "" {
			return "", false
		}
		return name, true
	default:
		return "", false
	}
}

func securityFootgunSensitiveNameFromExprs(exprs []ast.Expr) (string, bool) {
	for _, expr := range exprs {
		if name, ok := securityFootgunSensitiveTargetName(expr); ok {
			return name, true
		}
	}
	return "", false
}

func securityFootgunSensitiveNameFromIdents(idents []*ast.Ident) (string, bool) {
	for _, ident := range idents {
		if ident == nil {
			continue
		}
		name := strings.TrimSpace(ident.Name)
		if securityFootgunIsSensitiveName(name) {
			return name, true
		}
	}
	return "", false
}

func securityFootgunExprNameEquals(expr ast.Expr, want string) bool {
	name, ok := securityFootgunExprName(expr)
	if !ok {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(want))
}

func securityFootgunExprName(expr ast.Expr) (string, bool) {
	switch n := expr.(type) {
	case *ast.Ident:
		name := strings.TrimSpace(n.Name)
		if name == "" {
			return "", false
		}
		return name, true
	case *ast.SelectorExpr:
		name := strings.TrimSpace(n.Sel.Name)
		if name == "" {
			return "", false
		}
		return name, true
	case *ast.BasicLit:
		if n.Kind != token.STRING {
			return "", false
		}
		value, err := strconv.Unquote(n.Value)
		if err != nil {
			return "", false
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return "", false
		}
		return value, true
	default:
		return "", false
	}
}

func securityFootgunBoolLiteral(expr ast.Expr) (bool, bool) {
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return false, false
	}
	switch ident.Name {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return false, false
	}
}

func securityFootgunStringLiteral(expr ast.Expr) (string, bool) {
	basic, ok := expr.(*ast.BasicLit)
	if !ok || basic.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(basic.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

func securityFootgunIsSensitiveName(name string) bool {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	collapsed := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, trimmed)

	for _, token := range securityFootgunSensitiveNameTokens {
		tokenLower := strings.ToLower(token)
		tokenCollapsed := strings.ReplaceAll(tokenLower, "_", "")
		if strings.Contains(lower, tokenLower) || strings.Contains(collapsed, tokenCollapsed) {
			return true
		}
	}
	return false
}

func securityFootgunIsLikelyCredentialLiteral(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	if len([]rune(trimmed)) < 8 {
		return false
	}
	if strings.HasPrefix(trimmed, "${") || strings.HasPrefix(trimmed, "{{") {
		return false
	}

	lower := strings.ToLower(trimmed)
	for _, token := range securityFootgunCredentialPlaceholderTokens {
		if strings.Contains(lower, token) {
			return false
		}
	}

	hasLower := false
	hasUpper := false
	hasDigit := false
	hasStrongSymbol := false
	for _, r := range trimmed {
		switch {
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsSpace(r):
			return false
		case r != '-' && r != '_':
			hasStrongSymbol = true
		}
	}
	if !hasDigit && !hasStrongSymbol && !(hasLower && hasUpper) {
		return false
	}

	allSame := true
	var prior rune
	for i, r := range trimmed {
		if i == 0 {
			prior = r
			continue
		}
		if r != prior {
			allSame = false
			break
		}
	}
	if allSame {
		return false
	}

	return true
}

func securityFootgunPositionEvidence(fset *token.FileSet, lines []string, node ast.Node) (int, int, string) {
	if node == nil {
		return 0, 0, ""
	}
	pos := fset.Position(node.Pos())
	line := pos.Line
	column := pos.Column
	if line <= 0 || line > len(lines) {
		return line, column, ""
	}
	return line, column, strings.TrimSpace(lines[line-1])
}

func sortSecurityFootgunFindings(findings []SecurityFootgunFinding) {
	sort.Slice(findings, func(i, j int) bool {
		left := findings[i]
		right := findings[j]

		leftRank := securityFootgunSeverityRank(left.Severity)
		rightRank := securityFootgunSeverityRank(right.Severity)
		if leftRank != rightRank {
			return leftRank > rightRank
		}
		if left.Category != right.Category {
			return left.Category < right.Category
		}
		if left.RuleID != right.RuleID {
			return left.RuleID < right.RuleID
		}
		if left.File != right.File {
			return left.File < right.File
		}
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		if left.Column != right.Column {
			return left.Column < right.Column
		}
		return left.Message < right.Message
	})
}

func summarizeSecurityFootgunFindings(findings []SecurityFootgunFinding) SecurityFootgunSummary {
	summary := SecurityFootgunSummary{
		TotalFindings:      len(findings),
		HighestSeverity:    SecurityFootgunSeverityNone,
		FindingsBySeverity: SecurityFootgunSeverityCounts{},
	}

	for _, finding := range findings {
		switch finding.Severity {
		case SecurityFootgunSeverityLow:
			summary.FindingsBySeverity.Low++
		case SecurityFootgunSeverityMedium:
			summary.FindingsBySeverity.Medium++
		case SecurityFootgunSeverityHigh:
			summary.FindingsBySeverity.High++
		case SecurityFootgunSeverityCritical:
			summary.FindingsBySeverity.Critical++
		}
		if securityFootgunSeverityRank(finding.Severity) > securityFootgunSeverityRank(summary.HighestSeverity) {
			summary.HighestSeverity = finding.Severity
		}
	}

	return summary
}

func parseSecurityFootgunSeverity(value string) (SecurityFootgunSeverity, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "", string(SecurityFootgunSeverityNone):
		return SecurityFootgunSeverityNone, nil
	case string(SecurityFootgunSeverityLow):
		return SecurityFootgunSeverityLow, nil
	case string(SecurityFootgunSeverityMedium):
		return SecurityFootgunSeverityMedium, nil
	case string(SecurityFootgunSeverityHigh):
		return SecurityFootgunSeverityHigh, nil
	case string(SecurityFootgunSeverityCritical):
		return SecurityFootgunSeverityCritical, nil
	default:
		return "", fmt.Errorf("unsupported severity %q (expected one of: low, medium, high, critical)", strings.TrimSpace(value))
	}
}

func securityFootgunExceedsThreshold(result SecurityFootgunResult, threshold SecurityFootgunSeverity) bool {
	thresholdRank := securityFootgunSeverityRank(threshold)
	if thresholdRank <= securityFootgunSeverityRank(SecurityFootgunSeverityNone) {
		return false
	}
	return securityFootgunSeverityRank(result.Summary.HighestSeverity) >= thresholdRank
}

func securityFootgunSeverityRank(severity SecurityFootgunSeverity) int {
	switch severity {
	case SecurityFootgunSeverityNone:
		return 0
	case SecurityFootgunSeverityLow:
		return 1
	case SecurityFootgunSeverityMedium:
		return 2
	case SecurityFootgunSeverityHigh:
		return 3
	case SecurityFootgunSeverityCritical:
		return 4
	default:
		return -1
	}
}
