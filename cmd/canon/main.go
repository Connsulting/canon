package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"canon/internal/canon"
)

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
	case "render":
		return cmdRender(args[1:])
	case "status":
		return cmdStatus(args[1:])
	case "index":
		return cmdIndex(args[1:])
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	abs, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	if err := canon.EnsureLayout(abs, true); err != nil {
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	abs, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	entries, err := canon.LoadLedger(abs)
	if err != nil {
		return err
	}
	if *limit < len(entries) {
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

func printUsage() {
	fmt.Println("usage: canon <command> [options]")
	fmt.Println()
	fmt.Println("commands:")
	fmt.Println("  init    create required repository layout")
	fmt.Println("  ingest  ingest a source file with AI metadata and conflict checks")
	fmt.Println("  import  alias for ingest")
	fmt.Println("  raw     synthesize a spec from freeform text or voice note content")
	fmt.Println("  log     show spec ledger newest first")
	fmt.Println("  show    show a canonical spec by id")
	fmt.Println("  index   build deterministic index")
	fmt.Println("  render  render expected state from canonical specs")
	fmt.Println("  status  show repository summary")
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
