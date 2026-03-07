package main

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"canon/internal/canon"
)

//go:embed VERSION
var embeddedVersion string

var version = resolvedVersion(embeddedVersion)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return errors.New("command is required")
	}

	switch args[0] {
	case "init":
		return cmdInit(args[1:])
	case "ingest":
		return cmdIngest(args[1:])
	case "import":
		return cmdIngest(args[1:])
	case "raw":
		return cmdRaw(args[1:])
	case "log":
		return cmdLog(args[1:])
	case "show":
		return cmdShow(args[1:])
	case "reset":
		return cmdReset(args[1:])
	case "render":
		return cmdRender(args[1:])
	case "status":
		return cmdStatus(args[1:])
	case "gc":
		return cmdGc(args[1:])
	case "index":
		return cmdIndex(args[1:])
	case "check":
		return cmdCheck(args[1:])
	case "blame":
		return cmdBlame(args[1:])
	case "deps-risk":
		return cmdDepsRisk(args[1:])
	case "schema-evolution":
		return cmdSchemaEvolution(args[1:])
	case "semantic-diff":
		return cmdSemanticDiff(args[1:])
	case "version", "-v", "--version":
		return cmdVersion(args[1:])
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		printUsage()
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	root := fs.String("root", ".", "repository root")
	aiMode := fs.String("ai", "auto", "AI mode: off, auto")
	aiProviderFlag := fs.String("ai-provider", "", "AI provider override: codex or claude")
	responseFile := fs.String("response-file", "", "precomputed AI response JSON")
	noInteractive := fs.Bool("no-interactive", false, "accept all generated specs without prompting")
	acceptAll := fs.Bool("accept-all", false, "alias for --no-interactive")
	maxSpecs := fs.Int("max-specs", 10, "maximum number of generated specs")
	contextLimit := fs.Int("context-limit", 100, "max project context size in KB")
	var include stringSliceFlag
	var exclude stringSliceFlag
	fs.Var(&include, "include", "additional glob pattern to include in scan (repeatable)")
	fs.Var(&exclude, "exclude", "additional glob pattern to exclude from scan (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	abs, err := filepath.Abs(*root)
	if err != nil {
		return err
	}

	mode := strings.ToLower(strings.TrimSpace(*aiMode))
	if mode == "" {
		mode = "auto"
	}
	if strings.TrimSpace(*responseFile) != "" && mode == "auto" {
		mode = "from-response"
	}
	if mode != "off" && mode != "auto" && mode != "from-response" {
		return fmt.Errorf("unsupported init ai mode: %s", mode)
	}
	if mode == "off" && strings.TrimSpace(*responseFile) != "" {
		return errors.New("--response-file cannot be used when --ai off")
	}

	cfg, err := canon.LoadConfig(abs)
	if err != nil {
		return err
	}
	provider := cfg.AI.Provider
	if strings.TrimSpace(*aiProviderFlag) != "" {
		provider = strings.ToLower(strings.TrimSpace(*aiProviderFlag))
	}

	if mode == "off" {
		if err := canon.EnsureLayout(abs, true); err != nil {
			return err
		}
		fmt.Printf("layout ready at %s\n", filepath.ToSlash(abs))
		return nil
	}

	interactive := !*noInteractive && !*acceptAll && isTTY(os.Stdin)
	if _, err := canon.Init(abs, canon.InitOptions{
		AIMode:       mode,
		AIProvider:   provider,
		ResponseFile: strings.TrimSpace(*responseFile),
		Interactive:  interactive,
		MaxSpecs:     *maxSpecs,
		ContextLimit: *contextLimit,
		Include:      include.Values(),
		Exclude:      exclude.Values(),
		In:           os.Stdin,
		Out:          os.Stdout,
	}); err != nil {
		return err
	}

	fmt.Printf("layout ready at %s\n", filepath.ToSlash(abs))
	return nil
}

func cmdIngest(args []string) error {
	fs := flag.NewFlagSet("ingest", flag.ContinueOnError)
	root := fs.String("root", ".", "repository root")
	file := fs.String("file", "", "path to source markdown file")
	title := fs.String("title", "", "spec title override")
	domain := fs.String("domain", "", "spec domain override")
	typeName := fs.String("type", "", "spec type override")
	specID := fs.String("id", "", "spec id override")
	created := fs.String("created", "", "created timestamp in RFC3339")
	dependsOn := fs.String("depends-on", "", "comma separated dependencies")
	touches := fs.String("touches", "", "comma separated touched domains")
	parents := fs.String("parents", "", "comma separated parent spec ids")
	aiProviderFlag := fs.String("ai-provider", "", "AI provider override: codex or claude")
	responseFile := fs.String("response-file", "", "JSON response file from headless AI run")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return errors.New("ingest accepts at most one positional file path")
	}
	if fs.NArg() == 1 {
		if strings.TrimSpace(*file) != "" {
			return errors.New("use either positional file path or --file, not both")
		}
		*file = fs.Arg(0)
	}
	if strings.TrimSpace(*file) == "" {
		return errors.New("ingest requires a file path")
	}

	abs, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	cfg, err := canon.LoadConfig(abs)
	if err != nil {
		return err
	}

	aiProvider := cfg.AI.Provider
	if strings.TrimSpace(*aiProviderFlag) != "" {
		aiProvider = strings.ToLower(strings.TrimSpace(*aiProviderFlag))
	}
	aiMode := "auto"
	if strings.TrimSpace(*responseFile) != "" {
		aiMode = "from-response"
	}

	result, err := canon.Ingest(abs, canon.IngestInput{
		IngestKind:     "file",
		File:           *file,
		Title:          *title,
		Domain:         *domain,
		Type:           *typeName,
		ID:             *specID,
		Created:        *created,
		DependsOn:      parseCSV(*dependsOn),
		TouchedDomains: parseCSV(*touches),
		Parents:        parseCSV(*parents),
		AIProvider:     aiProvider,
		ConflictMode:   aiMode,
		ResponseFile:   *responseFile,
	})
	if err != nil {
		return err
	}

	fmt.Printf("ingested %s\n", result.SpecID)
	fmt.Printf("spec: %s\n", result.SpecPath)
	fmt.Printf("ledger: %s\n", result.LedgerPath)
	if len(result.Parents) == 0 {
		fmt.Println("parents: []")
	} else {
		fmt.Printf("parents: [%s]\n", strings.Join(result.Parents, ", "))
	}
	return nil
}

func cmdRaw(args []string) error {
	fs := flag.NewFlagSet("raw", flag.ContinueOnError)
	root := fs.String("root", ".", "repository root")
	text := fs.String("text", "", "raw voice note or freeform text")
	title := fs.String("title", "", "spec title override")
	domain := fs.String("domain", "", "spec domain override")
	typeName := fs.String("type", "", "spec type override")
	specID := fs.String("id", "", "spec id override")
	created := fs.String("created", "", "created timestamp in RFC3339")
	dependsOn := fs.String("depends-on", "", "comma separated dependencies")
	touches := fs.String("touches", "", "comma separated touched domains")
	parents := fs.String("parents", "", "comma separated parent spec ids")
	aiProviderFlag := fs.String("ai-provider", "", "AI provider override: codex or claude")
	responseFile := fs.String("response-file", "", "JSON response file from headless AI run")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if strings.TrimSpace(*text) == "" && fs.NArg() > 0 {
		*text = strings.Join(fs.Args(), " ")
	}
	if strings.TrimSpace(*text) == "" {
		interactiveText, err := collectRawInputInteractive(os.Stdin, os.Stdout)
		if err != nil {
			return err
		}
		*text = interactiveText
	}

	abs, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	cfg, err := canon.LoadConfig(abs)
	if err != nil {
		return err
	}

	aiProvider := cfg.AI.Provider
	if strings.TrimSpace(*aiProviderFlag) != "" {
		aiProvider = strings.ToLower(strings.TrimSpace(*aiProviderFlag))
	}
	aiMode := "auto"
	if strings.TrimSpace(*responseFile) != "" {
		aiMode = "from-response"
	}

	result, err := canon.Ingest(abs, canon.IngestInput{
		IngestKind:     "raw",
		Text:           *text,
		Title:          *title,
		Domain:         *domain,
		Type:           *typeName,
		ID:             *specID,
		Created:        *created,
		DependsOn:      parseCSV(*dependsOn),
		TouchedDomains: parseCSV(*touches),
		Parents:        parseCSV(*parents),
		AIProvider:     aiProvider,
		ConflictMode:   aiMode,
		ResponseFile:   *responseFile,
	})
	if err != nil {
		return err
	}

	fmt.Printf("ingested %s\n", result.SpecID)
	fmt.Printf("spec: %s\n", result.SpecPath)
	fmt.Printf("ledger: %s\n", result.LedgerPath)
	if len(result.Parents) == 0 {
		fmt.Println("parents: []")
	} else {
		fmt.Printf("parents: [%s]\n", strings.Join(result.Parents, ", "))
	}
	return nil
}

func cmdLog(args []string) error {
	fs := flag.NewFlagSet("log", flag.ContinueOnError)
	root := fs.String("root", ".", "repository root")
	limit := fs.Int("n", 50, "max entries")
	graph := fs.Bool("graph", false, "render dependency graph")
	oneline := fs.Bool("oneline", false, "compact one line output")
	all := fs.Bool("all", true, "include all disconnected heads")
	grep := fs.String("grep", "", "case-insensitive title substring filter")
	domain := fs.String("domain", "", "exact domain filter")
	typeName := fs.String("type", "", "exact type filter")
	color := fs.String("color", "auto", "colorize output: auto, always, never")
	date := fs.String("date", "relative", "timestamp format: absolute or relative")
	showTags := fs.Bool("show-tags", false, "include qualified [type/domain] tags")
	if err := fs.Parse(args); err != nil {
		return err
	}
	abs, err := filepath.Abs(*root)
	if err != nil {
		return err
	}

	useLegacy := !*graph &&
		!*oneline &&
		strings.TrimSpace(*grep) == "" &&
		strings.TrimSpace(*domain) == "" &&
		strings.TrimSpace(*typeName) == "" &&
		strings.EqualFold(strings.TrimSpace(*color), "auto") &&
		strings.EqualFold(strings.TrimSpace(*date), "absolute")

	if useLegacy {
		entries, err := canon.LoadLedger(abs)
		if err != nil {
			return err
		}
		if *limit < 0 {
			entries = nil
		} else if *limit < len(entries) {
			entries = entries[:*limit]
		}
		for _, entry := range entries {
			fmt.Printf("Spec: %s\n", entry.SpecID)
			if strings.TrimSpace(entry.Title) != "" {
				fmt.Printf("Title: %s\n", entry.Title)
			}
			if strings.TrimSpace(entry.Type) != "" {
				fmt.Printf("Type: %s\n", entry.Type)
			}
			if strings.TrimSpace(entry.Domain) != "" {
				fmt.Printf("Domain: %s\n", entry.Domain)
			}
			fmt.Printf("Date: %s\n", entry.IngestedAt)
			if len(entry.Parents) == 0 {
				fmt.Println("Parents: []")
			} else {
				fmt.Printf("Parents: [%s]\n", strings.Join(entry.Parents, ", "))
			}
			fmt.Printf("Hash: %s\n", entry.ContentHash)
			if strings.TrimSpace(entry.SourcePath) != "" {
				fmt.Printf("Source: %s\n", entry.SourcePath)
			}
			fmt.Printf("SpecPath: %s\n", entry.SpecPath)
			fmt.Println()
		}
		return nil
	}

	opts := canon.LogOptions{
		Limit:    *limit,
		Graph:    *graph,
		OneLine:  *oneline,
		All:      *all,
		Grep:     *grep,
		Domain:   *domain,
		Type:     *typeName,
		Color:    *color,
		IsTTY:    isTTY(os.Stdout),
		Date:     *date,
		ShowTags: *showTags,
	}
	nodes, err := canon.BuildLogViewForCLI(abs, opts)
	if err != nil {
		return err
	}
	text := canon.RenderLogTextForCLI(nodes, opts)
	if text != "" {
		fmt.Print(text)
	}
	return nil
}

func cmdShow(args []string) error {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	root := fs.String("root", ".", "repository root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("show requires <spec-id>")
	}
	specID := fs.Arg(0)
	abs, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	relPath, text, err := canon.ShowSpec(abs, specID)
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", relPath)
	fmt.Print(text)
	return nil
}

func cmdReset(args []string) error {
	fs := flag.NewFlagSet("reset", flag.ContinueOnError)
	root := fs.String("root", ".", "repository root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("reset requires <spec-id>")
	}

	abs, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	result, err := canon.Reset(abs, canon.ResetInput{
		RefSpecID: fs.Arg(0),
	})
	if err != nil {
		return err
	}

	fmt.Printf("reset to %s\n", result.KeptSpecID)
	fmt.Printf("deleted ledger entries: %d\n", result.LedgerDeleted)
	fmt.Printf("deleted spec files: %d\n", result.SpecDeleted)
	fmt.Printf("deleted source files: %d\n", result.SourceDeleted)
	return nil
}

func cmdRender(args []string) error {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	root := fs.String("root", ".", "repository root")
	write := fs.Bool("write", false, "write generated artifacts")
	aiMode := fs.String("ai", "auto", "AI render mode: off, auto, from-response")
	aiProviderFlag := fs.String("ai-provider", "", "AI provider override: codex or claude")
	responseFile := fs.String("response-file", "", "JSON response file from headless AI render run")
	if err := fs.Parse(args); err != nil {
		return err
	}
	abs, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	cfg, err := canon.LoadConfig(abs)
	if err != nil {
		return err
	}

	mode := strings.ToLower(strings.TrimSpace(*aiMode))
	if mode == "" {
		mode = "auto"
	}
	if strings.TrimSpace(*responseFile) != "" && mode == "auto" {
		mode = "from-response"
	}

	aiProvider := cfg.AI.Provider
	if strings.TrimSpace(*aiProviderFlag) != "" {
		aiProvider = strings.ToLower(strings.TrimSpace(*aiProviderFlag))
	}

	result, err := canon.Render(abs, canon.RenderOptions{
		Write:        *write,
		AIMode:       mode,
		AIProvider:   aiProvider,
		ResponseFile: strings.TrimSpace(*responseFile),
	})
	if err != nil {
		return err
	}
	if *write {
		fmt.Printf(
			"render complete, files changed: %d (updated: %d, removed stale: %d)\n",
			result.FilesWritten,
			result.FilesUpdated,
			result.FilesRemoved,
		)
		if result.AIUsed {
			fmt.Println("ai render: applied")
		} else if result.AIFallback {
			fmt.Println("ai render: fallback to deterministic output")
			if strings.TrimSpace(result.AIFallbackReason) != "" {
				fmt.Printf("ai render fallback reason: %s\n", result.AIFallbackReason)
			}
		}
	} else {
		fmt.Println("dry run complete; use --write to persist state")
	}
	return nil
}

func cmdGc(args []string) error {
	fs := flag.NewFlagSet("gc", flag.ContinueOnError)
	root := fs.String("root", ".", "repository root")
	domain := fs.String("domain", "", "consolidate all specs in one domain")
	specIDs := fs.String("specs", "", "comma separated spec ids to consolidate")
	write := fs.Bool("write", false, "execute consolidation")
	minSpecs := fs.Int("min-specs", 5, "minimum number of specs before consolidation")
	force := fs.Bool("force", false, "run consolidation below minimum spec count")
	aiProviderFlag := fs.String("ai-provider", "", "AI provider override: codex or claude")
	responseFile := fs.String("response-file", "", "JSON response file from headless AI run")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("gc does not accept positional arguments")
	}

	abs, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	cfg, err := canon.LoadConfig(abs)
	if err != nil {
		return err
	}
	provider := cfg.AI.Provider
	if strings.TrimSpace(*aiProviderFlag) != "" {
		provider = strings.ToLower(strings.TrimSpace(*aiProviderFlag))
	}

	ids := parseCSV(*specIDs)
	mode := "auto"
	if strings.TrimSpace(*responseFile) != "" {
		mode = "from-response"
	}

	result, err := canon.GC(abs, canon.GCInput{
		Domain:       strings.TrimSpace(*domain),
		SpecIDs:      ids,
		MinSpecs:     *minSpecs,
		Force:        *force,
		Write:        *write,
		AIMode:       mode,
		AIProvider:   provider,
		ResponseFile: strings.TrimSpace(*responseFile),
	})
	if err != nil {
		return err
	}

	if result.Skip {
		fmt.Println(result.SkipReason)
		return nil
	}

	switch result.ScopeType {
	case "domain":
		fmt.Printf("gc plan for domain: %s\n", result.ScopeValue)
	case "specs":
		fmt.Printf("gc plan for specs: %s\n", result.ScopeValue)
	}
	fmt.Printf("  specs to consolidate: %d\n", len(result.TargetSpecs))
	fmt.Printf("  estimated result: %d consolidated specs\n", len(result.Consolidated))
	if len(result.ExternalDeps) > 0 {
		fmt.Printf("  external dependencies preserved: [%s]\n", strings.Join(result.ExternalDeps, ", "))
	}

	for i, spec := range result.Consolidated {
		preview := strings.TrimSpace(spec.Body)
		previewLimit := 180
		if len(preview) > previewLimit {
			preview = preview[:previewLimit] + "..."
		}
		fmt.Printf("\n  consolidated spec %d/%d:\n", i+1, len(result.Consolidated))
		fmt.Printf("    title: %q\n", spec.Title)
		fmt.Printf("    consolidates: [%s]\n", strings.Join(spec.Consolidates, ", "))
		fmt.Printf("    %s\n", preview)
	}

	if !*write {
		fmt.Println("dry run complete; use --write to execute")
		return nil
	}

	reduction := 0
	if len(result.TargetSpecs) > 0 {
		reduction = int((1.0 - float64(len(result.Consolidated))/float64(len(result.TargetSpecs))) * 100)
	}
	fmt.Printf("gc complete for %s: %s\n", result.ScopeType, result.ScopeValue)
	fmt.Printf("  archived: %d specs\n", len(result.TargetSpecs))
	fmt.Printf("  created: %d consolidated specs\n", len(result.Consolidated))
	fmt.Printf("  reduction: %d -> %d specs (%d%%)\n", len(result.TargetSpecs), len(result.Consolidated), reduction)
	fmt.Println("run 'canon render --write' to update state")
	return nil
}

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	root := fs.String("root", ".", "repository root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	abs, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	status, err := canon.GetStatus(abs)
	if err != nil {
		return err
	}

	fmt.Printf("total specs: %d\n", status.TotalSpecs)
	fmt.Printf("feature specs: %d\n", status.FeatureSpecs)
	fmt.Printf("technical specs: %d\n", status.TechnicalSpecs)
	fmt.Printf("resolution specs: %d\n", status.ResolutionSpecs)
	fmt.Printf("domains: %d\n", status.Domains)
	fmt.Printf("cross-domain interactions: %d\n", status.CrossDomainInteractions)
	fmt.Printf("ledger entries: %d\n", status.LedgerEntries)
	fmt.Printf("ledger heads: %d\n", status.LedgerHeads)
	return nil
}

func cmdDepsRisk(args []string) error {
	fs := flag.NewFlagSet("deps-risk", flag.ContinueOnError)
	root := fs.String("root", ".", "repository root")
	jsonOut := fs.Bool("json", false, "output machine-readable JSON")
	failOnFlag := fs.String("fail-on", "", "fail when highest severity meets/exceeds: low, medium, high, critical")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("deps-risk does not accept positional arguments")
	}

	failOn := canon.DependencyRiskSeverity("")
	if strings.TrimSpace(*failOnFlag) != "" {
		parsed, err := canon.ParseDependencyRiskSeverityForCLI(*failOnFlag)
		if err != nil || parsed == canon.DependencyRiskSeverityNone {
			return fmt.Errorf("invalid --fail-on severity %q (expected one of: low, medium, high, critical)", strings.TrimSpace(*failOnFlag))
		}
		failOn = parsed
	}

	abs, err := filepath.Abs(*root)
	if err != nil {
		return err
	}

	result, err := canon.DependencyRiskForCLI(abs, canon.DependencyRiskOptions{
		FailOn: failOn,
	})
	if err != nil {
		return err
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return err
		}
	} else {
		fmt.Print(renderDependencyRiskText(result))
	}

	if failOn != "" && canon.DependencyRiskExceedsThresholdForCLI(result, failOn) {
		return fmt.Errorf("dependency risk threshold failed: highest=%s fail-on=%s", result.Summary.HighestSeverity, failOn)
	}
	return nil
}

func cmdSchemaEvolution(args []string) error {
	fs := flag.NewFlagSet("schema-evolution", flag.ContinueOnError)
	root := fs.String("root", ".", "repository root")
	jsonOut := fs.Bool("json", false, "output machine-readable JSON")
	failOnFlag := fs.String("fail-on", "", "fail when highest severity meets/exceeds: low, medium, high, critical")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("schema-evolution does not accept positional arguments")
	}

	failOn := canon.SchemaEvolutionSeverity("")
	if strings.TrimSpace(*failOnFlag) != "" {
		parsed, err := canon.ParseSchemaEvolutionSeverityForCLI(*failOnFlag)
		if err != nil || parsed == canon.SchemaEvolutionSeverityNone {
			return fmt.Errorf("invalid --fail-on severity %q (expected one of: low, medium, high, critical)", strings.TrimSpace(*failOnFlag))
		}
		failOn = parsed
	}

	abs, err := filepath.Abs(*root)
	if err != nil {
		return err
	}

	result, err := canon.SchemaEvolutionForCLI(abs, canon.SchemaEvolutionOptions{
		FailOn: failOn,
	})
	if err != nil {
		return err
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return err
		}
	} else {
		fmt.Print(renderSchemaEvolutionText(result))
	}

	if failOn != "" && canon.SchemaEvolutionExceedsThresholdForCLI(result, failOn) {
		return fmt.Errorf("schema evolution threshold failed: highest=%s fail-on=%s", result.Summary.HighestSeverity, failOn)
	}
	return nil
}

func cmdSemanticDiff(args []string) error {
	fs := flag.NewFlagSet("semantic-diff", flag.ContinueOnError)
	root := fs.String("root", ".", "repository root")
	diffFile := fs.String("diff-file", "", "path to unified diff file (defaults to git diff)")
	baseRef := fs.String("base-ref", "", "base git ref for comparison (requires --head-ref)")
	headRef := fs.String("head-ref", "", "head git ref for comparison (requires --base-ref)")
	jsonOut := fs.Bool("json", false, "output machine-readable JSON")
	aiMode := fs.String("ai", "auto", "AI semantic-diff mode: auto, from-response")
	aiProviderFlag := fs.String("ai-provider", "", "AI provider override: codex or claude")
	responseFile := fs.String("response-file", "", "JSON response file from headless AI semantic-diff run")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("semantic-diff does not accept positional arguments")
	}

	mode := strings.ToLower(strings.TrimSpace(*aiMode))
	if mode == "" {
		mode = "auto"
	}
	if strings.TrimSpace(*responseFile) != "" && mode == "auto" {
		mode = "from-response"
	}
	if mode != "auto" && mode != "from-response" {
		return fmt.Errorf("invalid --ai mode %q (expected one of: auto, from-response)", strings.TrimSpace(*aiMode))
	}
	if (strings.TrimSpace(*baseRef) == "") != (strings.TrimSpace(*headRef) == "") {
		return errors.New("--base-ref and --head-ref must be provided together")
	}
	if strings.TrimSpace(*diffFile) != "" && (strings.TrimSpace(*baseRef) != "" || strings.TrimSpace(*headRef) != "") {
		return errors.New("--diff-file cannot be used with --base-ref/--head-ref")
	}

	abs, err := filepath.Abs(*root)
	if err != nil {
		return err
	}

	cfg, err := canon.LoadConfig(abs)
	if err != nil {
		return err
	}
	provider := cfg.AI.Provider
	if strings.TrimSpace(*aiProviderFlag) != "" {
		provider = strings.ToLower(strings.TrimSpace(*aiProviderFlag))
	}

	result, err := canon.SemanticDiffForCLI(abs, canon.SemanticDiffOptions{
		DiffFile:     strings.TrimSpace(*diffFile),
		BaseRef:      strings.TrimSpace(*baseRef),
		HeadRef:      strings.TrimSpace(*headRef),
		AIMode:       mode,
		AIProvider:   provider,
		ResponseFile: strings.TrimSpace(*responseFile),
	})
	if err != nil {
		return err
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return err
		}
	} else {
		fmt.Print(renderSemanticDiffText(result))
	}
	return nil
}

func renderDependencyRiskText(result canon.DependencyRiskResult) string {
	var b strings.Builder

	fmt.Fprintf(&b, "dependency risk scan: %s\n", filepath.ToSlash(result.Root))
	fmt.Fprintf(&b, "go.mod: %s\n", filepath.ToSlash(result.GoModPath))
	if result.GoSumPresent {
		fmt.Fprintf(&b, "go.sum: %s (present)\n", filepath.ToSlash(result.GoSumPath))
	} else {
		fmt.Fprintf(&b, "go.sum: %s (missing)\n", filepath.ToSlash(result.GoSumPath))
	}
	fmt.Fprintf(&b, "dependencies: %d\n", result.DependencyCount)
	fmt.Fprintf(&b, "findings: %d\n", result.Summary.TotalFindings)
	fmt.Fprintf(&b, "highest severity: %s\n", result.Summary.HighestSeverity)
	fmt.Fprintf(
		&b,
		"severity counts: low=%d medium=%d high=%d critical=%d\n",
		result.Summary.FindingsBySeverity.Low,
		result.Summary.FindingsBySeverity.Medium,
		result.Summary.FindingsBySeverity.High,
		result.Summary.FindingsBySeverity.Critical,
	)
	if result.FailOn != "" {
		fmt.Fprintf(&b, "fail-on: %s (exceeded=%t)\n", result.FailOn, result.ThresholdExceeded)
	}

	if len(result.Findings) == 0 {
		b.WriteString("no dependency risk findings detected\n")
		return b.String()
	}

	b.WriteString("findings detail:\n")
	for _, finding := range result.Findings {
		details := make([]string, 0, 3)
		if finding.Module != "" {
			details = append(details, "module="+finding.Module)
		}
		if finding.Version != "" {
			details = append(details, "version="+finding.Version)
		}
		if finding.Replace != "" {
			details = append(details, "replace="+finding.Replace)
		}

		fmt.Fprintf(&b, "  - [%s] [%s] %s", strings.ToUpper(string(finding.Severity)), finding.Category, finding.RuleID)
		if len(details) > 0 {
			fmt.Fprintf(&b, " (%s)", strings.Join(details, ", "))
		}
		fmt.Fprintf(&b, ": %s\n", finding.Message)
	}
	return b.String()
}

func renderSchemaEvolutionText(result canon.SchemaEvolutionResult) string {
	var b strings.Builder

	fmt.Fprintf(&b, "schema evolution scan: %s\n", filepath.ToSlash(result.Root))
	fmt.Fprintf(&b, "migration files: %d\n", result.MigrationFileCount)
	fmt.Fprintf(&b, "statements scanned: %d\n", result.StatementCount)
	fmt.Fprintf(&b, "findings: %d\n", result.Summary.TotalFindings)
	fmt.Fprintf(&b, "highest severity: %s\n", result.Summary.HighestSeverity)
	fmt.Fprintf(
		&b,
		"severity counts: low=%d medium=%d high=%d critical=%d\n",
		result.Summary.FindingsBySeverity.Low,
		result.Summary.FindingsBySeverity.Medium,
		result.Summary.FindingsBySeverity.High,
		result.Summary.FindingsBySeverity.Critical,
	)
	if result.FailOn != "" {
		fmt.Fprintf(&b, "fail-on: %s (exceeded=%t)\n", result.FailOn, result.ThresholdExceeded)
	}

	if len(result.Findings) == 0 {
		b.WriteString("no schema evolution risk findings detected\n")
		return b.String()
	}

	b.WriteString("findings detail:\n")
	for _, finding := range result.Findings {
		fmt.Fprintf(
			&b,
			"  - [%s] %s (file=%s line=%d): %s\n",
			strings.ToUpper(string(finding.Severity)),
			finding.RuleID,
			finding.File,
			finding.Line,
			finding.Message,
		)
		if finding.Statement != "" {
			fmt.Fprintf(&b, "      statement: %s\n", finding.Statement)
		}
	}
	return b.String()
}

func renderSemanticDiffText(result canon.SemanticDiffResult) string {
	var b strings.Builder

	fmt.Fprintf(&b, "semantic diff scan: %s\n", filepath.ToSlash(result.Root))
	fmt.Fprintf(&b, "diff source: %s\n", filepath.ToSlash(result.DiffSource))
	fmt.Fprintf(&b, "diff bytes: %d\n", result.DiffBytes)
	fmt.Fprintf(&b, "changed files: %d\n", result.ChangedFileCount)
	fmt.Fprintf(&b, "line delta: +%d -%d (hunks=%d)\n", result.TotalAddedLines, result.TotalDeletedLines, result.TotalHunks)
	if strings.TrimSpace(result.Summary.AIModel) != "" {
		fmt.Fprintf(&b, "ai model: %s\n", result.Summary.AIModel)
	}
	if strings.TrimSpace(result.Summary.AISummary) != "" {
		fmt.Fprintf(&b, "ai summary: %s\n", result.Summary.AISummary)
	}

	if len(result.ChangedFiles) > 0 {
		b.WriteString("changed file detail:\n")
		for _, item := range result.ChangedFiles {
			fmt.Fprintf(
				&b,
				"  - %s (status=%s, +%d, -%d, hunks=%d)\n",
				item.File,
				item.Status,
				item.AddedLines,
				item.DeletedLines,
				item.HunkCount,
			)
		}
	}

	fmt.Fprintf(&b, "explanations: %d\n", result.Summary.TotalExplanations)
	fmt.Fprintf(&b, "highest impact: %s\n", result.Summary.HighestImpact)
	fmt.Fprintf(
		&b,
		"impact counts: low=%d medium=%d high=%d critical=%d\n",
		result.Summary.ImpactCounts.Low,
		result.Summary.ImpactCounts.Medium,
		result.Summary.ImpactCounts.High,
		result.Summary.ImpactCounts.Critical,
	)
	if len(result.Summary.CategoryCounts) > 0 {
		parts := make([]string, 0, len(result.Summary.CategoryCounts))
		for _, item := range result.Summary.CategoryCounts {
			parts = append(parts, fmt.Sprintf("%s=%d", item.Category, item.Count))
		}
		fmt.Fprintf(&b, "category counts: %s\n", strings.Join(parts, " "))
	}

	if len(result.Explanations) == 0 {
		b.WriteString("no semantic explanations generated\n")
		return b.String()
	}

	b.WriteString("explanations detail:\n")
	for _, explanation := range result.Explanations {
		fmt.Fprintf(
			&b,
			"  - [%s] [%s] %s: %s\n",
			strings.ToUpper(string(explanation.Impact)),
			explanation.Category,
			explanation.ID,
			explanation.Summary,
		)
		if explanation.Rationale != "" && explanation.Rationale != explanation.Summary {
			fmt.Fprintf(&b, "      rationale: %s\n", explanation.Rationale)
		}
		for _, evidence := range explanation.Evidence {
			if evidence.Kind == canon.SemanticDiffEvidenceKindHunk {
				fmt.Fprintf(
					&b,
					"      evidence: %s (hunk old=%d,%d new=%d,%d)\n",
					evidence.File,
					evidence.OldStart,
					evidence.OldLines,
					evidence.NewStart,
					evidence.NewLines,
				)
				continue
			}
			fmt.Fprintf(&b, "      evidence: %s (file)\n", evidence.File)
		}
	}
	return b.String()
}

func cmdIndex(args []string) error {
	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	root := fs.String("root", ".", "repository root")
	write := fs.Bool("write", false, "write .canon/index.yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	abs, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	if err := canon.EnsureLayout(abs, *write); err != nil {
		return err
	}
	specs, err := canon.LoadSpecsForCLI(abs)
	if err != nil {
		return err
	}
	indexText := canon.BuildIndexYAMLForCLI(specs)
	if *write {
		path := filepath.Join(abs, ".canon", "index.yaml")
		changed, err := canon.WriteTextIfChangedForCLI(path, indexText)
		if err != nil {
			return err
		}
		if changed {
			fmt.Println("index written (changed)")
		} else {
			fmt.Println("index written (unchanged)")
		}
		return nil
	}
	fmt.Print(indexText)
	return nil
}

func cmdBlame(args []string) error {
	fs := flag.NewFlagSet("blame", flag.ContinueOnError)
	root := fs.String("root", ".", "repository root")
	domain := fs.String("domain", "", "restrict search to this domain")
	jsonOutput := fs.Bool("json", false, "output machine-readable JSON")
	aiProviderFlag := fs.String("ai-provider", "", "AI provider override: codex or claude")
	responseFile := fs.String("response-file", "", "JSON response file from headless AI blame run")
	if err := fs.Parse(args); err != nil {
		return err
	}
	query := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if query == "" {
		return errors.New("blame requires a behavior description")
	}

	abs, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	cfg, err := canon.LoadConfig(abs)
	if err != nil {
		return err
	}

	aiProvider := cfg.AI.Provider
	if strings.TrimSpace(*aiProviderFlag) != "" {
		aiProvider = strings.ToLower(strings.TrimSpace(*aiProviderFlag))
	}

	result, err := canon.BlameForCLI(abs, canon.BlameInput{
		Query:        query,
		Domain:       strings.TrimSpace(*domain),
		AIProvider:   aiProvider,
		ResponseFile: strings.TrimSpace(*responseFile),
	})
	if err != nil {
		return err
	}

	if *jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}

	printBlameResult(result, strings.TrimSpace(*domain))
	return nil
}

func printBlameResult(result canon.BlameResult, domain string) {
	if !result.Found || len(result.Results) == 0 {
		fmt.Println("blame: no matching specs found")
		fmt.Println()
		fmt.Println("  The described behavior is not covered by any canonical spec.")
		fmt.Println("  This may be an implementation decision not yet captured in specs,")
		fmt.Println("  or a specification gap.")
		fmt.Println()
		fmt.Println("  Consider authoring a spec with:")
		suggestedDomain := strings.TrimSpace(domain)
		if suggestedDomain == "" {
			suggestedDomain = "general"
		}
		fmt.Printf("    canon raw --text %q --domain %s\n", result.Query, suggestedDomain)
		return
	}

	if len(result.Results) == 1 {
		fmt.Println("blame: 1 spec found")
	} else {
		fmt.Printf("blame: %d specs found\n", len(result.Results))
	}
	fmt.Println()
	for i, item := range result.Results {
		fmt.Printf("  spec: %s\n", item.SpecID)
		fmt.Printf("  title: %q\n", item.Title)
		fmt.Printf("  domain: %s\n", item.Domain)
		fmt.Printf("  confidence: %s\n", item.Confidence)
		fmt.Printf("  created: %s\n", item.Created)
		fmt.Println()
		fmt.Println("  relevant lines:")
		if len(item.RelevantLines) == 0 {
			fmt.Println("  > (no excerpt provided)")
		} else {
			for _, line := range item.RelevantLines {
				fmt.Printf("  > %s\n", line)
			}
		}
		if i != len(result.Results)-1 {
			fmt.Println()
		}
	}
}

func cmdCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	root := fs.String("root", ".", "repository root")
	domain := fs.String("domain", "", "restrict check scope to one domain")
	specID := fs.String("spec", "", "check one spec id against others in scope")
	aiMode := fs.String("ai", "auto", "AI check mode: auto, from-response")
	aiProviderFlag := fs.String("ai-provider", "", "AI provider override: codex or claude")
	responseFile := fs.String("response-file", "", "JSON response file from headless AI check run")
	jsonOut := fs.Bool("json", false, "output JSON")
	write := fs.Bool("write", false, "write conflict reports")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("check does not accept positional arguments")
	}

	abs, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	cfg, err := canon.LoadConfig(abs)
	if err != nil {
		return err
	}
	provider := cfg.AI.Provider
	if strings.TrimSpace(*aiProviderFlag) != "" {
		provider = strings.ToLower(strings.TrimSpace(*aiProviderFlag))
	}
	mode := strings.ToLower(strings.TrimSpace(*aiMode))
	if mode == "" {
		mode = "auto"
	}
	if strings.TrimSpace(*responseFile) != "" && mode == "auto" {
		mode = "from-response"
	}

	result, err := canon.CheckForCLI(abs, canon.CheckOptions{
		Domain:       strings.TrimSpace(*domain),
		SpecID:       strings.TrimSpace(*specID),
		Write:        *write,
		AIMode:       mode,
		AIProvider:   provider,
		ResponseFile: strings.TrimSpace(*responseFile),
	})
	if err != nil {
		return err
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return err
		}
	} else {
		text := renderCheckText(result)
		if text != "" {
			fmt.Print(text)
		}
	}

	if !result.Passed {
		return fmt.Errorf("check failed: %d conflicts across %d specs", result.TotalConflicts, result.TotalSpecs)
	}
	return nil
}

func cmdVersion(args []string) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	short := fs.Bool("short", false, "print version only")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("version does not accept positional arguments")
	}
	if *short {
		fmt.Println(version)
		return nil
	}
	fmt.Printf("canon %s\n", version)
	return nil
}

func printUsage() {
	fmt.Println("usage: canon <command> [options]")
	fmt.Println()
	fmt.Println("commands:")
	fmt.Println("  init    create layout and optionally bootstrap canonical specs from project context")
	fmt.Println("  ingest  ingest a source file with AI metadata and conflict checks")
	fmt.Println("  import  alias for ingest")
	fmt.Println("  raw     synthesize a spec from freeform text or voice note content")
	fmt.Println("  log     show spec ledger; supports --graph and Git-style filters")
	fmt.Println("  check   scan canonical specs for semantic conflicts")
	fmt.Println("  show    show a canonical spec by id")
	fmt.Println("  reset   reset canon history to a specific spec id")
	fmt.Println("  index   build deterministic index")
	fmt.Println("  render  render expected state from canonical specs")
	fmt.Println("  gc      consolidate and archive specs with optional AI pass")
	fmt.Println("  blame   trace a behavior back to its canonical spec requirements")
	fmt.Println("  deps-risk scan Go dependencies for offline security/maintenance risks")
	fmt.Println("  schema-evolution scan SQL migrations for potentially breaking schema changes")
	fmt.Println("  semantic-diff explain semantic behavior changes from repository diffs")
	fmt.Println("  status  show repository summary")
	fmt.Println("  version print CLI version")
	fmt.Println()
	fmt.Println("init options:")
	fmt.Println("  --root <path>          repository root (default: \".\")")
	fmt.Println("  --ai <mode>            AI mode: off, auto (default: \"auto\")")
	fmt.Println("  --ai-provider <name>   AI provider: codex, claude (default: from .canonconfig)")
	fmt.Println("  --response-file <path> precomputed AI response JSON")
	fmt.Println("  --no-interactive       accept all generated specs without review")
	fmt.Println("  --accept-all           alias for --no-interactive")
	fmt.Println("  --max-specs <n>        maximum specs to generate (default: 10)")
	fmt.Println("  --context-limit <kb>   max project context size in KB (default: 100)")
	fmt.Println("  --include <glob>       additional glob pattern to include (repeatable)")
	fmt.Println("  --exclude <glob>       additional glob pattern to exclude (repeatable)")
	fmt.Println()
	fmt.Println("semantic-diff options:")
	fmt.Println("  --root <path>          repository root (default: \".\")")
	fmt.Println("  --diff-file <path>     read unified diff from file instead of git diff")
	fmt.Println("  --base-ref <ref>       compare git refs: base (requires --head-ref)")
	fmt.Println("  --head-ref <ref>       compare git refs: head (requires --base-ref)")
	fmt.Println("  --json                 output machine-readable JSON")
	fmt.Println("  --ai <mode>            AI mode: auto, from-response (default: \"auto\")")
	fmt.Println("  --ai-provider <name>   AI provider: codex, claude (default: from .canonconfig)")
	fmt.Println("  --response-file <path> precomputed AI response JSON for deterministic replay")
}

func parseCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		v := strings.TrimSpace(part)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

type stringSliceFlag struct {
	values []string
}

func (f *stringSliceFlag) String() string {
	if len(f.values) == 0 {
		return ""
	}
	return strings.Join(f.values, ",")
}

func (f *stringSliceFlag) Set(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, ",")
	for _, part := range parts {
		v := strings.TrimSpace(part)
		if v == "" {
			continue
		}
		f.values = append(f.values, v)
	}
	return nil
}

func (f *stringSliceFlag) Values() []string {
	out := make([]string, len(f.values))
	copy(out, f.values)
	return out
}

func isTTY(file *os.File) bool {
	if file == nil {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func collectRawInputInteractive(reader io.Reader, writer io.Writer) (string, error) {
	fmt.Fprintln(writer, "Enter your voice note or rough spec text. End input with a line that contains only .done")
	scanner := bufio.NewScanner(reader)
	lines := make([]string, 0)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == ".done" {
			break
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	text := strings.TrimSpace(strings.Join(lines, "\n"))
	if text == "" {
		return "", errors.New("raw requires non-empty text input")
	}
	return text, nil
}

func renderCheckText(result canon.CheckResult) string {
	lines := make([]string, 0)
	if result.TotalConflicts == 0 {
		lines = append(lines, fmt.Sprintf("check passed: 0 conflicts across %d specs", result.TotalSpecs))
		return strings.Join(lines, "\n") + "\n"
	}

	type pairLine struct {
		specA   string
		titleA  string
		specB   string
		titleB  string
		domains []string
	}
	pairs := map[string]pairLine{}
	grouped := map[string][]canon.CheckConflict{}
	order := make([]string, 0)
	for _, conflict := range result.Conflicts {
		key := conflict.SpecA + "|" + conflict.SpecB
		if _, ok := grouped[key]; !ok {
			order = append(order, key)
			pairs[key] = pairLine{
				specA:   conflict.SpecA,
				titleA:  conflict.TitleA,
				specB:   conflict.SpecB,
				titleB:  conflict.TitleB,
				domains: conflict.OverlapDomains,
			}
		}
		grouped[key] = append(grouped[key], conflict)
	}

	for _, key := range order {
		pair := pairs[key]
		lines = append(lines, fmt.Sprintf("conflict: %s <-> %s", pair.specA, pair.specB))
		lines = append(lines, fmt.Sprintf("  specs: %q <-> %q", pair.titleA, pair.titleB))
		if len(pair.domains) == 0 {
			lines = append(lines, "  domains: []")
		} else {
			lines = append(lines, fmt.Sprintf("  domains: [%s]", strings.Join(pair.domains, ", ")))
		}
		for _, conflict := range grouped[key] {
			lines = append(lines, fmt.Sprintf("  key: %s", conflict.StatementKey))
			lines = append(lines, fmt.Sprintf("  %s: %q", conflict.SpecA, conflict.LineA))
			lines = append(lines, fmt.Sprintf("  %s: %q", conflict.SpecB, conflict.LineB))
			lines = append(lines, "")
		}
	}

	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	lines = append(lines, fmt.Sprintf("check failed: %d conflicts across %d specs", result.TotalConflicts, result.TotalSpecs))
	return strings.Join(lines, "\n") + "\n"
}

func resolvedVersion(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "v0.0.0"
	}
	return trimmed
}
