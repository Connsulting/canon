package canon

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	initDefaultMaxSpecs     = 10
	initDefaultContextKB    = 100
	initDefaultMaxFileBytes = 50 * 1024
	initDefaultCrawlMode    = "snapshot"
	initMultipassMaxAreas   = 16
	initMultipassMaxFiles   = 12
	initMultipassMaxList    = 200
	initTreeDepth           = 3
	initPreviewLines        = 20
)

type InitOptions struct {
	AIMode       string
	AIProvider   string
	CrawlMode    string
	ResponseFile string
	Interactive  bool
	AcceptAll    bool
	MaxSpecs     int
	ContextLimit int
	Include      []string
	Exclude      []string
	In           io.Reader
	Out          io.Writer
}

type InitResult struct {
	GeneratedSpecs int
	AcceptedSpecs  int
	SkippedSpecs   int
	DeferredSpecs  int
	FoundFiles     int
	IncludedFiles  int
	ExcludedFiles  int
	ContextBytes   int
	FallbackUsed   bool
}

type aiInitResponse struct {
	Model          string       `json:"model"`
	ProjectSummary string       `json:"project_summary"`
	Specs          []aiInitSpec `json:"specs"`
}

type aiInitSpec struct {
	ID             string   `json:"id"`
	Type           string   `json:"type"`
	Title          string   `json:"title"`
	Domain         string   `json:"domain"`
	DependsOn      []string `json:"depends_on"`
	TouchedDomains []string `json:"touched_domains"`
	Body           string   `json:"body"`
	ReviewHint     string   `json:"review_hint"`
}

type aiInitAreaResponse struct {
	Model              string   `json:"model"`
	Area               string   `json:"area"`
	Summary            string   `json:"summary"`
	Components         []string `json:"components"`
	UserFacingFeatures []string `json:"user_facing_features"`
	TechnicalBehaviors []string `json:"technical_behaviors"`
	RuntimeWiring      []string `json:"runtime_wiring"`
	RisksOrGaps        []string `json:"risks_or_gaps"`
	EvidenceFiles      []string `json:"evidence_files"`
	SupportOnly        bool     `json:"support_only"`
	OmissionReason     string   `json:"omission_reason"`
}

type initDraftSpec struct {
	Spec       Spec
	ReviewHint string
}

type initScanCandidate struct {
	Path      string
	Priority  int
	Size      int
	Content   string
	Truncated bool
}

type initScanEntry struct {
	Path    string
	AbsPath string
	Symlink bool
}

type initScanReport struct {
	FoundFiles      int
	IncludedFiles   int
	ExcludedFiles   int
	ContextBytes    int
	ContextLimit    int
	Context         string
	TopReadme       string
	Tree            string
	AllProjectFiles []string
}

type initCrawlArea struct {
	Name  string
	Files []string
}

type ignorePattern struct {
	Pattern  string
	Negated  bool
	DirOnly  bool
	Anchored bool
}

func isSupportedInitCrawlMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "snapshot", "agentic", "multipass":
		return true
	default:
		return false
	}
}

func Init(root string, options InitOptions) (InitResult, error) {
	if err := EnsureLayout(root, true); err != nil {
		return InitResult{}, err
	}

	out := options.Out
	if out == nil {
		out = io.Discard
	}
	in := options.In
	if in == nil {
		in = strings.NewReader("")
	}

	mode := strings.ToLower(strings.TrimSpace(options.AIMode))
	if mode == "" {
		mode = "auto"
	}
	if strings.TrimSpace(options.ResponseFile) != "" && mode == "auto" {
		mode = "from-response"
	}
	if mode != "off" && mode != "auto" && mode != "from-response" {
		return InitResult{}, fmt.Errorf("unsupported init ai mode: %s", mode)
	}
	if mode == "off" {
		return InitResult{}, nil
	}
	if mode == "from-response" && strings.TrimSpace(options.ResponseFile) == "" {
		return InitResult{}, fmt.Errorf("from-response mode requires --response-file")
	}

	provider := strings.ToLower(strings.TrimSpace(options.AIProvider))
	if provider == "" {
		provider = "codex"
	}
	crawlMode := strings.ToLower(strings.TrimSpace(options.CrawlMode))
	if crawlMode == "" {
		crawlMode = initDefaultCrawlMode
	}
	if !isSupportedInitCrawlMode(crawlMode) {
		return InitResult{}, fmt.Errorf("unsupported init crawl mode: %s", crawlMode)
	}
	maxSpecs := options.MaxSpecs
	if maxSpecs <= 0 {
		maxSpecs = initDefaultMaxSpecs
	}
	contextLimitKB := options.ContextLimit
	if contextLimitKB <= 0 {
		contextLimitKB = initDefaultContextKB
	}

	existing, err := loadSpecs(root)
	if err != nil {
		return InitResult{}, err
	}
	if len(existing) > 0 {
		fmt.Fprintf(out, "Canon repository already contains %d specs. Re-running init will generate new draft specs without affecting existing ones.\n", len(existing))
	}

	fmt.Fprintln(out, "Scanning project...")
	scan, err := scanProjectForInit(root, initScanOptions{
		ContextLimitBytes: contextLimitKB * 1024,
		MaxFileBytes:      initDefaultMaxFileBytes,
		Include:           options.Include,
		Exclude:           options.Exclude,
	})
	if err != nil {
		return InitResult{}, err
	}

	seedContext := scan.Context
	seedIncluded := scan.IncludedFiles
	seedBytes := scan.ContextBytes
	if crawlMode == "agentic" {
		seedContext, seedIncluded = buildInitAgenticSeed(scan, contextLimitKB*1024)
		seedBytes = len(seedContext)
	}

	result := InitResult{
		FoundFiles:    scan.FoundFiles,
		IncludedFiles: seedIncluded,
		ExcludedFiles: scan.ExcludedFiles,
		ContextBytes:  seedBytes,
	}

	fmt.Fprintf(out, "  Crawl mode: %s\n", crawlMode)
	switch crawlMode {
	case "snapshot":
		fmt.Fprintf(out, "  Found %d files (%d included in context, %d excluded)\n", scan.FoundFiles, seedIncluded, scan.ExcludedFiles)
		fmt.Fprintf(out, "  Context size: %d KB / %d KB limit\n", seedBytes/1024, contextLimitKB)
	case "agentic":
		fmt.Fprintf(out, "  Found %d files (%d included in seed inventory, %d excluded)\n", scan.FoundFiles, seedIncluded, scan.ExcludedFiles)
		fmt.Fprintf(out, "  Seed inventory size: %d KB / %d KB limit\n", seedBytes/1024, contextLimitKB)
		fmt.Fprintln(out, "  Agentic mode may inspect additional repository files directly during AI decomposition.")
	case "multipass":
		areas := buildInitCrawlAreas(scan.AllProjectFiles, initMultipassMaxAreas)
		result.IncludedFiles = len(scan.AllProjectFiles)
		result.ContextBytes = contextLimitKB * 1024
		fmt.Fprintf(out, "  Found %d files (%d available for multipass crawl after filtering)\n", scan.FoundFiles, len(scan.AllProjectFiles))
		fmt.Fprintf(out, "  Per-area evidence limit: %d KB\n", contextLimitKB)
		fmt.Fprintf(out, "  Planned crawl areas: %d\n", len(areas))
		fmt.Fprintln(out, "  Canon will analyze planned areas before final spec synthesis.")
	}

	noProjectContext := strings.TrimSpace(seedContext) == ""
	if crawlMode == "snapshot" {
		noProjectContext = noProjectContext || seedIncluded == 0
	} else {
		noProjectContext = noProjectContext || len(scan.AllProjectFiles) == 0
	}
	if noProjectContext {
		fmt.Fprintln(out, "No project files found. Skipping AI scan.")
		return result, nil
	}

	fmt.Fprintln(out, "Requesting AI decomposition...")
	fmt.Fprintf(out, "  Provider: %s\n", provider)

	var aiResponse aiInitResponse
	switch mode {
	case "from-response":
		aiResponse, err = parseAIInitResponse(root, options.ResponseFile)
		if err != nil {
			return result, err
		}
	case "auto":
		if !aiProviderRuntimeReady(provider) {
			fmt.Fprintf(out, "  Warning: AI decomposition unavailable (%s). Falling back to README bootstrap spec.\n", provider)
			aiResponse = fallbackAIInitResponse(scan)
			result.FallbackUsed = true
			break
		}
		if crawlMode == "multipass" {
			aiResponse, err = runHeadlessAIMultipassInit(provider, root, scan, existing, maxSpecs, contextLimitKB*1024, out)
		} else {
			aiResponse, err = runHeadlessAIInit(provider, root, scan, existing, maxSpecs, crawlMode, seedContext)
		}
		if err != nil {
			fmt.Fprintf(out, "  Warning: AI decomposition unavailable (%v). Falling back to README bootstrap spec.\n", err)
			aiResponse = fallbackAIInitResponse(scan)
			result.FallbackUsed = true
		}
	}

	drafts := buildInitDrafts(aiResponse, existing, maxSpecs)
	if len(drafts) == 0 {
		fmt.Fprintln(out, "Warning: AI returned no specs. Layout created only.")
		return result, nil
	}

	result.GeneratedSpecs = len(drafts)
	fmt.Fprintf(out, "  Generating specs... done (%d specs produced)\n", len(drafts))

	if options.AcceptAll {
		for _, draft := range drafts {
			if err := ingestInitDraft(root, draft.Spec); err != nil {
				return result, err
			}
			result.AcceptedSpecs++
		}
		return result, nil
	}

	if !options.Interactive {
		return result, fmt.Errorf("generated bootstrap specs require review before ingest; rerun in an interactive terminal to review each spec or pass --accept-all/--no-interactive to ingest all generated specs")
	}

	fmt.Fprintf(out, "\nStarting interactive review (%d specs to review):\n\n", len(drafts))
	scanner := bufio.NewScanner(in)
	for idx := 0; idx < len(drafts); {
		draft := drafts[idx]
		renderInitDraftPrompt(out, idx, len(drafts), draft)
		if !scanner.Scan() {
			for j := idx; j < len(drafts); j++ {
				if err := writeInitDraftFile(root, drafts[j].Spec); err != nil {
					return result, err
				}
				result.DeferredSpecs++
			}
			break
		}
		choice := strings.ToLower(strings.TrimSpace(scanner.Text()))
		if choice == "" {
			fmt.Fprintln(out, "  Choose one of: a, s, e, v, q")
			continue
		}
		switch choice[0] {
		case 'a':
			if err := ingestInitDraft(root, draft.Spec); err != nil {
				fmt.Fprintf(out, "  Failed to ingest spec: %v\n", err)
				continue
			}
			result.AcceptedSpecs++
			idx++
		case 's':
			if err := writeInitDraftFile(root, draft.Spec); err != nil {
				return result, err
			}
			result.SkippedSpecs++
			idx++
		case 'e':
			edited, editErr := editInitDraft(root, draft.Spec, out)
			if editErr != nil {
				fmt.Fprintf(out, "  Edit unavailable: %v\n", editErr)
				continue
			}
			drafts[idx].Spec = edited
		case 'v':
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "--- full spec ---")
			fmt.Fprintln(out, strings.TrimSpace(draft.Spec.Body))
			fmt.Fprintln(out, "--- end ---")
			fmt.Fprintln(out, "")
		case 'q':
			for j := idx; j < len(drafts); j++ {
				if err := writeInitDraftFile(root, drafts[j].Spec); err != nil {
					return result, err
				}
				result.DeferredSpecs++
			}
			idx = len(drafts)
		default:
			fmt.Fprintln(out, "  Choose one of: a, s, e, v, q")
		}
	}
	if err := scanner.Err(); err != nil {
		return result, err
	}

	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "Review summary: accepted=%d skipped=%d deferred=%d\n", result.AcceptedSpecs, result.SkippedSpecs, result.DeferredSpecs)
	return result, nil
}

func renderInitDraftPrompt(out io.Writer, index int, total int, draft initDraftSpec) {
	depends := renderList(draft.Spec.DependsOn)
	if depends == "[]" {
		depends = "(none)"
	}
	fmt.Fprintf(out, "[%d/%d] %s\n", index+1, total, draft.Spec.Title)
	fmt.Fprintf(out, "      Domain: %s | Type: %s | Depends on: %s\n", draft.Spec.Domain, draft.Spec.Type, depends)
	if strings.TrimSpace(draft.ReviewHint) != "" {
		fmt.Fprintf(out, "      Hint: %s\n", strings.TrimSpace(draft.ReviewHint))
	}
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "      --- preview ---")
	for _, line := range previewLines(draft.Spec.Body, initPreviewLines) {
		fmt.Fprintf(out, "      %s\n", line)
	}
	fmt.Fprintln(out, "      --- end preview ---")
	fmt.Fprintln(out, "")
	fmt.Fprint(out, "  [a]ccept  [s]kip  [e]dit  [v]iew full  [q]uit\n> ")
}

func previewLines(body string, maxLines int) []string {
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) == 0 || (len(lines) == 1 && strings.TrimSpace(lines[0]) == "") {
		return []string{"(no body content)"}
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines = append(lines, "...")
	}
	return lines
}

func ingestInitDraft(root string, spec Spec) error {
	_, err := Ingest(root, IngestInput{
		Text:          canonicalSpecText(spec),
		NoAutoParents: true,
	})
	return err
}

func editInitDraft(root string, spec Spec, out io.Writer) (Spec, error) {
	editor := strings.TrimSpace(os.Getenv("EDITOR"))
	if editor == "" {
		editor = "vi"
	}
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		return spec, fmt.Errorf("no editor configured")
	}
	if _, err := exec.LookPath(parts[0]); err != nil {
		return spec, err
	}

	tmpFile, err := os.CreateTemp("", "canon-init-edit-*.md")
	if err != nil {
		return spec, err
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.WriteString(canonicalSpecText(spec)); err != nil {
		tmpFile.Close()
		_ = os.Remove(tmpPath)
		return spec, err
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return spec, err
	}
	defer func() { _ = os.Remove(tmpPath) }()

	cmdArgs := append(parts[1:], tmpPath)
	cmd := exec.Command(parts[0], cmdArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return spec, err
	}
	b, err := os.ReadFile(tmpPath)
	if err != nil {
		return spec, err
	}
	edited, err := parseSpecText(string(b), tmpPath)
	if err != nil {
		fmt.Fprintf(out, "  Edited spec parse failed, keeping previous draft: %v\n", err)
		return spec, nil
	}
	edited.Path = ""
	edited.Consolidates = nil
	return edited, nil
}

func writeInitDraftFile(root string, spec Spec) error {
	dir := filepath.Join(root, "specs", "init-drafts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	base := slugify(spec.Title)
	if base == "" {
		base = slugify(spec.ID)
	}
	name := base + ".md"
	pathAbs := filepath.Join(dir, name)
	for i := 2; ; i++ {
		if _, err := os.Stat(pathAbs); os.IsNotExist(err) {
			break
		}
		name = base + "-" + strconv.Itoa(i) + ".md"
		pathAbs = filepath.Join(dir, name)
	}
	content := canonicalSpecText(spec)
	return os.WriteFile(pathAbs, []byte(content), 0o644)
}

type initScanOptions struct {
	ContextLimitBytes int
	MaxFileBytes      int64
	Include           []string
	Exclude           []string
}

func scanProjectForInit(root string, options initScanOptions) (initScanReport, error) {
	limit := options.ContextLimitBytes
	if limit <= 0 {
		limit = initDefaultContextKB * 1024
	}
	maxFile := options.MaxFileBytes
	if maxFile <= 0 {
		maxFile = initDefaultMaxFileBytes
	}

	ignorePatterns, err := loadGitignorePatterns(root)
	if err != nil {
		return initScanReport{}, err
	}

	candidates := make([]initScanCandidate, 0)
	entries := make([]initScanEntry, 0)
	allFiles := make([]string, 0)
	readmeBody := ""
	found := 0
	excluded := 0

	err = filepath.WalkDir(root, func(pathAbs string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if pathAbs == root {
			return nil
		}
		rel, relErr := filepath.Rel(root, pathAbs)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)

		if entry.IsDir() {
			if shouldSkipDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}

		found++
		entries = append(entries, initScanEntry{
			Path:    rel,
			AbsPath: pathAbs,
			Symlink: entry.Type()&fs.ModeSymlink != 0,
		})
		return nil
	})
	if err != nil {
		return initScanReport{}, err
	}

	rawPaths := make([]string, 0, len(entries))
	for _, item := range entries {
		rawPaths = append(rawPaths, item.Path)
	}
	gitIgnored, useGitIgnore := gitCheckIgnorePathSet(root, rawPaths)

	for _, item := range entries {
		rel := item.Path
		forceInclude := matchAnyGlob(rel, options.Include)
		if !forceInclude {
			if matchAnyGlob(rel, options.Exclude) {
				excluded++
				continue
			}
			if useGitIgnore {
				if gitIgnored[rel] {
					excluded++
					continue
				}
			} else if matchesIgnorePatterns(rel, false, ignorePatterns) {
				excluded++
				continue
			}
		}

		if item.Symlink {
			excluded++
			continue
		}

		allFiles = append(allFiles, rel)

		if isLikelyBinaryPath(rel) {
			excluded++
			continue
		}

		info, statErr := os.Stat(item.AbsPath)
		if statErr != nil {
			return initScanReport{}, statErr
		}
		if info.Size() > maxFile {
			excluded++
			continue
		}

		b, readErr := os.ReadFile(item.AbsPath)
		if readErr != nil {
			return initScanReport{}, readErr
		}
		if isBinaryContent(b) {
			excluded++
			continue
		}

		if strings.EqualFold(rel, "README.md") || strings.EqualFold(rel, "README") {
			readmeBody = strings.TrimSpace(string(b))
		}

		priority := initFilePriority(rel)
		content := string(b)
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}

		maxPerFile := maxBytesForPriority(priority)
		truncated := false
		if len(content) > maxPerFile {
			content = truncateText(content, maxPerFile)
			truncated = true
		}

		candidates = append(candidates, initScanCandidate{
			Path:      rel,
			Priority:  priority,
			Size:      len(content),
			Content:   content,
			Truncated: truncated,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Priority == candidates[j].Priority {
			return candidates[i].Path < candidates[j].Path
		}
		return candidates[i].Priority < candidates[j].Priority
	})
	sort.Strings(allFiles)

	tree := buildDirectoryTree(allFiles, initTreeDepth)
	context, included := buildInitContext(tree, candidates, limit)
	excluded += len(candidates) - included
	if excluded < 0 {
		excluded = 0
	}

	return initScanReport{
		FoundFiles:      found,
		IncludedFiles:   included,
		ExcludedFiles:   excluded,
		ContextBytes:    len(context),
		ContextLimit:    limit,
		Context:         context,
		TopReadme:       readmeBody,
		Tree:            tree,
		AllProjectFiles: allFiles,
	}, nil
}

func gitCheckIgnorePathSet(root string, relPaths []string) (map[string]bool, bool) {
	if len(relPaths) == 0 {
		return map[string]bool{}, false
	}
	if _, err := exec.LookPath("git"); err != nil {
		return nil, false
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		return nil, false
	}

	var input strings.Builder
	for _, rel := range relPaths {
		if strings.TrimSpace(rel) == "" {
			continue
		}
		input.WriteString(rel)
		input.WriteByte(0)
	}
	if input.Len() == 0 {
		return map[string]bool{}, true
	}

	cmd := exec.Command("git", "check-ignore", "--stdin", "-z", "--verbose", "--non-matching", "--no-index")
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(input.String())
	output, err := cmd.Output()
	if err != nil {
		return nil, false
	}

	fields := strings.Split(string(output), "\x00")
	ignored := make(map[string]bool)
	for i := 0; i+3 < len(fields); i += 4 {
		pattern := fields[i+2]
		path := fields[i+3]
		if path == "" {
			continue
		}
		if pattern == "" || strings.HasPrefix(pattern, "!") {
			continue
		}
		ignored[path] = true
	}
	return ignored, true
}

func shouldSkipDir(rel string) bool {
	clean := strings.TrimSpace(rel)
	if clean == "" {
		return false
	}
	parts := strings.Split(clean, "/")
	for _, part := range parts {
		if strings.HasPrefix(part, ".canon") {
			return true
		}
		switch part {
		case ".git", "state", "node_modules", "vendor", "dist", "build", "out", "target", "__pycache__", ".venv", "venv", ".pytest_cache", ".mypy_cache":
			return true
		}
	}
	return false
}

func isLikelyBinaryPath(rel string) bool {
	lower := strings.ToLower(rel)
	ext := strings.ToLower(filepath.Ext(lower))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".bmp", ".tiff", ".svg", ".pdf", ".zip", ".gz", ".tar", ".tgz", ".7z", ".rar", ".exe", ".dll", ".so", ".dylib", ".woff", ".woff2", ".ttf", ".otf", ".eot", ".mp3", ".mp4", ".mov", ".avi", ".wav", ".class", ".jar", ".wasm", ".pyc", ".pyo":
		return true
	}
	if strings.HasSuffix(lower, ".min.js") || strings.HasSuffix(lower, ".min.css") {
		return true
	}
	return false
}

func isBinaryContent(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	max := len(b)
	if max > 4096 {
		max = 4096
	}
	for i := 0; i < max; i++ {
		if b[i] == 0 {
			return true
		}
	}
	return false
}

func initFilePriority(rel string) int {
	lower := strings.ToLower(rel)
	base := path.Base(lower)
	dir := path.Dir(lower)

	if lower == "readme.md" || lower == "readme" {
		return 0
	}
	if dir == "docs" || strings.HasPrefix(dir, "docs/") || base == "architecture.md" || base == "contributing.md" {
		return 5
	}

	manifests := map[string]struct{}{
		"package.json":     {},
		"go.mod":           {},
		"cargo.toml":       {},
		"pyproject.toml":   {},
		"requirements.txt": {},
		"pom.xml":          {},
		"build.gradle":     {},
		"build.gradle.kts": {},
	}
	if _, ok := manifests[base]; ok {
		return 10
	}

	if strings.HasPrefix(base, ".env") || base == "docker-compose.yml" || base == "docker-compose.yaml" || strings.HasPrefix(lower, ".github/workflows/") || strings.HasPrefix(lower, ".gitlab-ci") {
		return 20
	}
	if strings.HasPrefix(lower, "cmd/") || strings.Contains(base, "main.") || strings.Contains(base, "index.") || strings.Contains(base, "app.") {
		return 30
	}
	if strings.Contains(lower, "migration") || strings.Contains(lower, "schema") || strings.Contains(lower, "db/") || strings.Contains(lower, "database/") {
		return 40
	}
	if strings.Contains(lower, "route") || strings.Contains(lower, "router") || strings.Contains(lower, "api") {
		return 45
	}
	if strings.HasPrefix(lower, ".canon/") || strings.HasPrefix(lower, "state/") {
		return 85
	}
	return 70
}

func maxBytesForPriority(priority int) int {
	switch {
	case priority <= 20:
		return 12000
	case priority <= 45:
		return 8000
	default:
		return 3500
	}
}

func truncateText(text string, maxBytes int) string {
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text
	}
	marker := "\n[...truncated...]\n"
	limit := maxBytes - len(marker)
	if limit <= 0 {
		return marker
	}
	truncated := text[:limit]
	lastNewline := strings.LastIndex(truncated, "\n")
	if lastNewline > 0 {
		truncated = truncated[:lastNewline]
	}
	return truncated + marker
}

func buildDirectoryTree(files []string, depth int) string {
	if len(files) == 0 {
		return "(empty)\n"
	}
	seen := make(map[string]struct{})
	lines := make([]string, 0)
	for _, rel := range files {
		parts := strings.Split(rel, "/")
		if len(parts) > depth {
			parts = parts[:depth]
		}
		if len(parts) == 0 {
			continue
		}
		for i := range parts {
			prefix := strings.Join(parts[:i+1], "/")
			if _, ok := seen[prefix]; ok {
				continue
			}
			seen[prefix] = struct{}{}
			indent := strings.Repeat("  ", i)
			line := indent + "- " + parts[i]
			if i == len(parts)-1 && len(strings.Split(rel, "/")) > depth {
				line += "/..."
			}
			lines = append(lines, line)
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

func buildInitAgenticSeed(scan initScanReport, limit int) (string, int) {
	if limit <= 0 {
		limit = initDefaultContextKB * 1024
	}

	var seed strings.Builder
	appendChunk := func(chunk string) bool {
		if chunk == "" {
			return true
		}
		if seed.Len()+len(chunk) <= limit {
			seed.WriteString(chunk)
			return true
		}
		remaining := limit - seed.Len()
		if remaining <= 256 {
			return false
		}
		seed.WriteString(truncateText(chunk, remaining))
		return false
	}

	if !appendChunk("# Canon Init Agentic Seed\n\n") {
		return seed.String(), 0
	}
	if !appendChunk("This seed inventory is a starting point only. Inspect additional repository files directly as needed.\n\n") {
		return seed.String(), 0
	}

	summary := fmt.Sprintf("- Available files after filtering: %d\n- Excluded during local scan: %d\n- README present: %t\n\n", len(scan.AllProjectFiles), scan.ExcludedFiles, strings.TrimSpace(scan.TopReadme) != "")
	if !appendChunk(summary) {
		return seed.String(), 0
	}

	if !appendChunk("## Directory Tree (depth-limited)\n") {
		return seed.String(), 0
	}
	if !appendChunk(scan.Tree) {
		return seed.String(), 0
	}
	if !appendChunk("\n") {
		return seed.String(), 0
	}

	if !appendChunk("## High-Signal Paths\n") {
		return seed.String(), 0
	}
	count := 0
	for _, rel := range selectInitInventoryPaths(scan.AllProjectFiles, 120) {
		if !appendChunk("- " + rel + "\n") {
			break
		}
		count++
	}

	readme := strings.TrimSpace(scan.TopReadme)
	if readme != "" {
		appendChunk("\n## README Excerpt\n```\n")
		appendChunk(truncateText(readme, 6000))
		appendChunk("\n```\n")
	}

	return seed.String(), count
}

func selectInitInventoryPaths(files []string, max int) []string {
	if max <= 0 {
		max = 120
	}
	type candidate struct {
		path     string
		priority int
	}
	ranked := make([]candidate, 0, len(files))
	for _, rel := range files {
		ranked = append(ranked, candidate{
			path:     rel,
			priority: initFilePriority(rel),
		})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].priority == ranked[j].priority {
			return ranked[i].path < ranked[j].path
		}
		return ranked[i].priority < ranked[j].priority
	})
	if len(ranked) > max {
		ranked = ranked[:max]
	}
	out := make([]string, 0, len(ranked))
	for _, item := range ranked {
		out = append(out, item.path)
	}
	return out
}

func buildInitCrawlAreas(files []string, maxAreas int) []initCrawlArea {
	groups := make(map[string][]string)
	for _, rel := range files {
		clean := strings.Trim(strings.TrimSpace(filepath.ToSlash(rel)), "/")
		if clean == "" {
			continue
		}
		area := initCrawlAreaName(clean)
		groups[area] = append(groups[area], clean)
	}

	areas := make([]initCrawlArea, 0, len(groups))
	for name, areaFiles := range groups {
		sort.Strings(areaFiles)
		areas = append(areas, initCrawlArea{Name: name, Files: areaFiles})
	}
	sort.Slice(areas, func(i, j int) bool {
		leftPriority := initCrawlAreaPriority(areas[i])
		rightPriority := initCrawlAreaPriority(areas[j])
		if leftPriority == rightPriority {
			if len(areas[i].Files) == len(areas[j].Files) {
				return areas[i].Name < areas[j].Name
			}
			return len(areas[i].Files) > len(areas[j].Files)
		}
		return leftPriority < rightPriority
	})

	if maxAreas <= 0 || len(areas) <= maxAreas {
		return areas
	}
	if maxAreas == 1 {
		combined := combineInitCrawlAreas("other", areas)
		return []initCrawlArea{combined}
	}
	kept := append([]initCrawlArea{}, areas[:maxAreas-1]...)
	kept = append(kept, combineInitCrawlAreas("other", areas[maxAreas-1:]))
	return kept
}

func initCrawlAreaName(rel string) string {
	parts := strings.Split(rel, "/")
	if len(parts) <= 1 {
		return "root"
	}
	return parts[0]
}

func initCrawlAreaPriority(area initCrawlArea) int {
	score := 100
	switch area.Name {
	case "root":
		score = 0
	case "docs":
		score = 5
	case ".github", ".gitlab":
		score = 15
	case "cmd", "app", "src", "internal", "pkg", "lib", "api", "server", "client", "frontend", "backend", "amplify", "cdk", "infra", "infrastructure":
		score = 20
	case "test", "tests", "e2e", "__tests__":
		score = 50
	case "scripts", "tools":
		score = 60
	default:
		score = 70
	}
	for _, rel := range area.Files {
		if priority := initFilePriority(rel); priority < score {
			score = priority
		}
	}
	return score
}

func combineInitCrawlAreas(name string, areas []initCrawlArea) initCrawlArea {
	combined := initCrawlArea{Name: name}
	for _, area := range areas {
		combined.Files = append(combined.Files, area.Files...)
	}
	sort.Strings(combined.Files)
	return combined
}

func buildInitAreaEvidencePack(root string, area initCrawlArea, limit int) (string, []string, error) {
	if limit <= 0 {
		limit = initDefaultContextKB * 1024
	}

	var pack strings.Builder
	appendChunk := func(chunk string) bool {
		if chunk == "" {
			return true
		}
		if pack.Len()+len(chunk) <= limit {
			pack.WriteString(chunk)
			return true
		}
		remaining := limit - pack.Len()
		if remaining <= 256 {
			return false
		}
		pack.WriteString(truncateText(chunk, remaining))
		return false
	}

	appendChunk("# Canon Init Multipass Area Evidence\n\n")
	appendChunk("Area: " + area.Name + "\n")
	appendChunk(fmt.Sprintf("Filtered file count: %d\n\n", len(area.Files)))
	appendChunk("## Area File Inventory\n")
	for i, rel := range area.Files {
		if i >= initMultipassMaxList {
			appendChunk(fmt.Sprintf("- ... %d more files\n", len(area.Files)-i))
			break
		}
		if !appendChunk("- " + rel + "\n") {
			return pack.String(), nil, nil
		}
	}
	appendChunk("\n## Evidence File Excerpts\n")

	selected := make([]string, 0, initMultipassMaxFiles)
	for _, rel := range rankedInitAreaFiles(area.Files) {
		if len(selected) >= initMultipassMaxFiles {
			break
		}
		content, truncated, ok, err := readInitEvidenceFile(root, rel)
		if err != nil {
			return "", nil, err
		}
		if !ok {
			continue
		}
		section := strings.Builder{}
		section.WriteString("\n### ")
		section.WriteString(rel)
		section.WriteString("\n```")
		section.WriteString("\n")
		section.WriteString(content)
		if !strings.HasSuffix(content, "\n") {
			section.WriteString("\n")
		}
		section.WriteString("```\n")
		if truncated {
			section.WriteString("(excerpt truncated)\n")
		}
		if !appendChunk(section.String()) {
			break
		}
		selected = append(selected, rel)
	}

	return pack.String(), selected, nil
}

func rankedInitAreaFiles(files []string) []string {
	ranked := append([]string{}, files...)
	sort.Slice(ranked, func(i, j int) bool {
		leftPriority := initFilePriority(ranked[i])
		rightPriority := initFilePriority(ranked[j])
		if leftPriority == rightPriority {
			return ranked[i] < ranked[j]
		}
		return leftPriority < rightPriority
	})
	return ranked
}

func readInitEvidenceFile(root string, rel string) (string, bool, bool, error) {
	if isLikelyBinaryPath(rel) {
		return "", false, false, nil
	}
	pathAbs := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Stat(pathAbs)
	if err != nil {
		return "", false, false, err
	}
	if info.IsDir() || info.Size() > initDefaultMaxFileBytes {
		return "", false, false, nil
	}
	b, err := os.ReadFile(pathAbs)
	if err != nil {
		return "", false, false, err
	}
	if isBinaryContent(b) {
		return "", false, false, nil
	}
	content := string(b)
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	maxBytes := maxBytesForPriority(initFilePriority(rel))
	truncated := false
	if len(content) > maxBytes {
		content = truncateText(content, maxBytes)
		truncated = true
	}
	return content, truncated, true, nil
}

func buildInitContext(tree string, candidates []initScanCandidate, limit int) (string, int) {
	if limit <= 0 {
		limit = initDefaultContextKB * 1024
	}
	head := strings.Builder{}
	head.WriteString("# Canon Init Project Scan\n\n")
	head.WriteString("## Directory Tree (depth-limited)\n")
	head.WriteString(tree)
	head.WriteString("\n")

	context := head.String()
	if len(context) >= limit {
		return context[:limit], 0
	}

	included := 0
	for _, candidate := range candidates {
		section := strings.Builder{}
		section.WriteString("## File: ")
		section.WriteString(candidate.Path)
		section.WriteString("\n")
		section.WriteString("```\n")
		section.WriteString(candidate.Content)
		if !strings.HasSuffix(candidate.Content, "\n") {
			section.WriteString("\n")
		}
		section.WriteString("```\n\n")
		chunk := section.String()
		if len(context)+len(chunk) > limit {
			remaining := limit - len(context)
			if remaining > 256 {
				chunk = truncateText(chunk, remaining)
				context += chunk
				included++
			}
			break
		}
		context += chunk
		included++
	}

	return context, included
}

func fallbackAIInitResponse(scan initScanReport) aiInitResponse {
	body := strings.TrimSpace(scan.TopReadme)
	if body == "" {
		body = "Project context detected, but AI decomposition was unavailable during init."
	}
	return aiInitResponse{
		Model:          "fallback",
		ProjectSummary: "AI decomposition unavailable. Generated a README bootstrap spec.",
		Specs: []aiInitSpec{
			{
				Type:           "technical",
				Title:          "Project Bootstrap",
				Domain:         "general",
				DependsOn:      []string{},
				TouchedDomains: []string{"general"},
				Body:           body,
				ReviewHint:     "Fallback draft created from README content.",
			},
		},
	}
}

func buildInitDrafts(response aiInitResponse, existing []Spec, maxSpecs int) []initDraftSpec {
	if maxSpecs <= 0 {
		maxSpecs = initDefaultMaxSpecs
	}
	existingIDs := make(map[string]struct{}, len(existing))
	for _, spec := range existing {
		existingIDs[spec.ID] = struct{}{}
	}

	drafts := make([]initDraftSpec, 0, len(response.Specs))
	usedIDs := make(map[string]struct{})
	seedTimestamp := nowUTC().Format(timeRFC3339)
	createdAt := seedTimestamp
	for _, item := range response.Specs {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = inferTitle(item.Body)
		}
		domain := strings.TrimSpace(item.Domain)
		if domain == "" {
			domain = "general"
		}
		typeName := strings.TrimSpace(item.Type)
		if typeName == "" {
			typeName = "feature"
		}
		body := strings.TrimSpace(item.Body)
		if body == "" {
			body = "(No body content)"
		}

		specID := strings.TrimSpace(item.ID)
		if specID == "" || idExists(specID, existingIDs, usedIDs) {
			specID = uniqueGeneratedInitID(seedTimestamp, title, existingIDs, usedIDs)
		}
		usedIDs[specID] = struct{}{}

		spec := Spec{
			ID:             specID,
			Type:           typeName,
			Title:          title,
			Domain:         domain,
			Created:        createdAt,
			DependsOn:      normalizeList(item.DependsOn),
			TouchedDomains: mustInclude(item.TouchedDomains, domain),
			Body:           body,
		}
		drafts = append(drafts, initDraftSpec{Spec: spec, ReviewHint: strings.TrimSpace(item.ReviewHint)})
	}

	drafts = orderInitDraftsByDependencies(drafts)
	if len(drafts) > maxSpecs {
		drafts = drafts[:maxSpecs]
	}
	return drafts
}

func idExists(id string, existing map[string]struct{}, used map[string]struct{}) bool {
	if _, ok := existing[id]; ok {
		return true
	}
	if _, ok := used[id]; ok {
		return true
	}
	return false
}

func uniqueGeneratedInitID(seedTimestamp string, title string, existing map[string]struct{}, used map[string]struct{}) string {
	timestamp := strings.TrimSpace(seedTimestamp)
	if timestamp == "" {
		timestamp = nowUTC().Format(timeRFC3339)
	}
	for i := 0; i < 1000; i++ {
		seedTitle := title
		if i > 0 {
			seedTitle = fmt.Sprintf("%s|%d", title, i)
		}
		sum := sha256.Sum256([]byte(timestamp + "|" + strings.ToLower(strings.TrimSpace(seedTitle))))
		candidate := hex.EncodeToString(sum[:])[:7]
		if !idExists(candidate, existing, used) {
			return candidate
		}
	}
	return generatedSpecID(nowUTC(), title)
}

func orderInitDraftsByDependencies(drafts []initDraftSpec) []initDraftSpec {
	if len(drafts) <= 1 {
		return drafts
	}
	idToIndex := make(map[string]int, len(drafts))
	for i, draft := range drafts {
		idToIndex[draft.Spec.ID] = i
	}

	indegree := make([]int, len(drafts))
	adj := make([][]int, len(drafts))
	for i, draft := range drafts {
		seen := map[int]struct{}{}
		for _, dep := range draft.Spec.DependsOn {
			j, ok := idToIndex[dep]
			if !ok {
				continue
			}
			if _, exists := seen[j]; exists {
				continue
			}
			seen[j] = struct{}{}
			adj[j] = append(adj[j], i)
			indegree[i]++
		}
	}

	queue := make([]int, 0)
	for i := range drafts {
		if indegree[i] == 0 {
			queue = append(queue, i)
		}
	}
	sort.Ints(queue)

	out := make([]initDraftSpec, 0, len(drafts))
	for len(queue) > 0 {
		i := queue[0]
		queue = queue[1:]
		out = append(out, drafts[i])
		for _, to := range adj[i] {
			indegree[to]--
			if indegree[to] == 0 {
				queue = append(queue, to)
				sort.Ints(queue)
			}
		}
	}
	if len(out) == len(drafts) {
		return out
	}
	for i, degree := range indegree {
		if degree > 0 {
			out = append(out, drafts[i])
		}
	}
	return out
}

func runHeadlessAIInit(provider string, root string, scan initScanReport, existing []Spec, maxSpecs int, crawlMode string, seedContext string) (aiInitResponse, error) {
	prompt := buildAIInitPrompt(provider, crawlMode, scan, existing, maxSpecs, seedContext)
	output, err := runHeadlessJSONPrompt(provider, root, aiInitJSONSchema(), prompt)
	if err != nil {
		return aiInitResponse{}, err
	}
	return decodeAIInitResponse(output)
}

func runHeadlessAIMultipassInit(provider string, root string, scan initScanReport, existing []Spec, maxSpecs int, evidenceLimit int, out io.Writer) (aiInitResponse, error) {
	areas := buildInitCrawlAreas(scan.AllProjectFiles, initMultipassMaxAreas)
	if len(areas) == 0 {
		return aiInitResponse{}, fmt.Errorf("multipass crawl found no areas")
	}

	if out == nil {
		out = io.Discard
	}
	fmt.Fprintf(out, "  Planning managed crawl: %d areas\n", len(areas))

	analyses := make([]aiInitAreaResponse, 0, len(areas))
	for i, area := range areas {
		evidence, selected, err := buildInitAreaEvidencePack(root, area, evidenceLimit)
		if err != nil {
			return aiInitResponse{}, fmt.Errorf("build evidence for area %s: %w", area.Name, err)
		}
		fmt.Fprintf(out, "  Analyzing area %d/%d: %s (%d files, %d evidence paths)\n", i+1, len(areas), area.Name, len(area.Files), len(selected))

		prompt := buildAIInitAreaPrompt(provider, area, selected, evidence)
		output, err := runHeadlessJSONPrompt(provider, root, aiInitAreaJSONSchema(), prompt)
		if err != nil {
			return aiInitResponse{}, fmt.Errorf("analyze area %s: %w", area.Name, err)
		}
		analysis, err := decodeAIInitAreaResponse(output)
		if err != nil {
			return aiInitResponse{}, fmt.Errorf("decode analysis for area %s: %w", area.Name, err)
		}
		if strings.TrimSpace(analysis.Area) == "" {
			analysis.Area = area.Name
		}
		if len(analysis.EvidenceFiles) == 0 {
			analysis.EvidenceFiles = selected
		}
		analyses = append(analyses, analysis)
	}

	fmt.Fprintln(out, "  Synthesizing specs from area analyses...")
	prompt := buildAIInitMultipassSynthesisPrompt(provider, scan, existing, maxSpecs, areas, analyses)
	output, err := runHeadlessJSONPrompt(provider, root, aiInitJSONSchema(), prompt)
	if err != nil {
		return aiInitResponse{}, err
	}
	return decodeAIInitResponse(output)
}

func runHeadlessJSONPrompt(provider string, root string, schema string, prompt string) ([]byte, error) {
	switch provider {
	case "codex":
		schemaFile, err := os.CreateTemp("", "canon-init-schema-*.json")
		if err != nil {
			return nil, err
		}
		schemaPath := schemaFile.Name()
		defer func() {
			schemaFile.Close()
			_ = os.Remove(schemaPath)
		}()
		if _, err := schemaFile.WriteString(schema); err != nil {
			return nil, err
		}
		if err := schemaFile.Close(); err != nil {
			return nil, err
		}

		responseFile, err := os.CreateTemp("", "canon-init-response-*.json")
		if err != nil {
			return nil, err
		}
		responsePath := responseFile.Name()
		responseFile.Close()
		defer func() { _ = os.Remove(responsePath) }()

		cmd := exec.Command(
			"codex",
			"exec",
			"-C",
			root,
			"--skip-git-repo-check",
			"--output-schema",
			schemaPath,
			"-o",
			responsePath,
			"-",
		)
		cmd.Stdin = strings.NewReader(prompt)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("codex exec failed: %w\n%s", err, strings.TrimSpace(string(output)))
		}
		b, err := os.ReadFile(responsePath)
		if err != nil {
			return nil, err
		}
		return b, nil

	case "claude":
		cmd := exec.Command(
			"claude",
			"--print",
			"--output-format",
			"json",
			"--json-schema",
			schema,
			prompt,
		)
		cmd.Dir = root
		output, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("claude --print failed: %w\n%s", err, strings.TrimSpace(string(output)))
		}
		return output, nil
	default:
		return nil, fmt.Errorf("unsupported ai provider: %s", provider)
	}
}

func buildAIInitPrompt(provider string, crawlMode string, scan initScanReport, existing []Spec, maxSpecs int, seedContext string) string {
	if crawlMode == "agentic" {
		return buildAIInitAgenticPrompt(provider, scan, existing, maxSpecs, seedContext)
	}
	return buildAIInitSnapshotPrompt(provider, existing, maxSpecs, seedContext)
}

func buildAIInitSnapshotPrompt(provider string, existing []Spec, maxSpecs int, seedContext string) string {
	lines := []string{
		"# Canon Init AI Decomposition",
		"",
		"Provider: " + provider,
		"Crawl mode: snapshot",
		"",
		"Task:",
		"1. Analyze the scanned project context.",
		"2. Produce between 2 and " + strconv.Itoa(maxSpecs) + " specs that capture current project behavior.",
		"3. Use feature specs for user-facing behavior and technical specs for architecture or infrastructure concerns.",
		"4. Use depends_on and touched_domains when appropriate.",
		"5. Describe current behavior only. Do not propose future work.",
		"6. Prefer command or subsystem scoped specs over one broad catch-all summary spec.",
		"7. For multi-command CLIs, ensure major command families are represented by at least one specific spec.",
		"8. If the repository has high-signal domain folders (for example commands, skills, hooks, agents, api, migrations, docs), ensure each major folder is represented by at least one spec.",
		"9. If environment variables or runtime configuration files are present, include at least one spec covering configuration and runtime wiring behavior.",
		"10. For each spec body, include multiple concrete behavior statements (target 4 to 7) when source evidence supports it.",
		"11. Include concrete current-state behavior details in each body, not only high-level summaries.",
		"12. Return JSON only following the schema.",
		"",
		"Schema:",
		"{",
		`  "model": "string",`,
		`  "project_summary": "string",`,
		`  "specs": [`,
		`    {`,
		`      "id": "7-char-hex",`,
		`      "type": "feature|technical",`,
		`      "title": "string",`,
		`      "domain": "string",`,
		`      "depends_on": ["spec-id"],`,
		`      "touched_domains": ["domain"],`,
		`      "body": "markdown",`,
		`      "review_hint": "string"`,
		`    }`,
		`  ]`,
		"}",
		"",
		"## Project Context",
		"",
		seedContext,
		"",
	}
	return appendExistingInitSpecs(lines, existing)
}

func buildAIInitAgenticPrompt(provider string, scan initScanReport, existing []Spec, maxSpecs int, seedContext string) string {
	lines := []string{
		"# Canon Init AI Decomposition",
		"",
		"Provider: " + provider,
		"Crawl mode: agentic",
		"",
		"Task:",
		"1. Start from the seed inventory below, then inspect the repository directly using local tools as needed.",
		"2. Determine the key components, user-facing features, workflows, infrastructure, and runtime wiring of the current project.",
		"3. Read additional files beyond the seed inventory whenever the initial inventory is insufficient.",
		"4. Prioritize entrypoints, manifests, docs, handlers, routes, tests, and infrastructure definitions while crawling.",
		"5. Use feature specs for user-facing behavior and technical specs for architecture or infrastructure concerns.",
		"6. Use depends_on and touched_domains when appropriate.",
		"7. Describe current behavior only. Do not propose future work.",
		"8. Do not modify repository files while analyzing the project.",
		"9. Before finalizing, ensure major top-level areas from the inventory are either represented by at least one spec or intentionally omitted as non-product/support-only.",
		"10. For each spec body, include multiple concrete behavior statements (target 4 to 7) when source evidence supports it.",
		"11. Include concrete current-state behavior details in each body, not only high-level summaries.",
		"12. Produce between 2 and " + strconv.Itoa(maxSpecs) + " specs that capture current project behavior.",
		"13. Return JSON only following the schema.",
		"",
		"Repository scan summary:",
		fmt.Sprintf("- Available files after filtering: %d", len(scan.AllProjectFiles)),
		fmt.Sprintf("- Excluded during local scan: %d", scan.ExcludedFiles),
		"",
		"Schema:",
		"{",
		`  "model": "string",`,
		`  "project_summary": "string",`,
		`  "specs": [`,
		`    {`,
		`      "id": "7-char-hex",`,
		`      "type": "feature|technical",`,
		`      "title": "string",`,
		`      "domain": "string",`,
		`      "depends_on": ["spec-id"],`,
		`      "touched_domains": ["domain"],`,
		`      "body": "markdown",`,
		`      "review_hint": "string"`,
		`    }`,
		`  ]`,
		"}",
		"",
		"## Seed Inventory",
		"",
		seedContext,
		"",
	}
	return appendExistingInitSpecs(lines, existing)
}

func buildAIInitAreaPrompt(provider string, area initCrawlArea, selected []string, evidence string) string {
	lines := []string{
		"# Canon Init Area Analysis",
		"",
		"Provider: " + provider,
		"Crawl mode: multipass",
		"Area: " + area.Name,
		"",
		"Task:",
		"1. Analyze only the current behavior evidenced by this area pack.",
		"2. Identify important components, user-facing features, technical behaviors, runtime wiring, and risks or gaps.",
		"3. Use evidence_files to list concrete paths that support your findings.",
		"4. Set support_only=true when this area is only tooling, generated artifacts, or otherwise should not become a dedicated Canon spec.",
		"5. Do not modify repository files.",
		"6. Return JSON only following the schema.",
		"",
		"Schema:",
		"{",
		`  "model": "string",`,
		`  "area": "string",`,
		`  "summary": "string",`,
		`  "components": ["string"],`,
		`  "user_facing_features": ["string"],`,
		`  "technical_behaviors": ["string"],`,
		`  "runtime_wiring": ["string"],`,
		`  "risks_or_gaps": ["string"],`,
		`  "evidence_files": ["string"],`,
		`  "support_only": false,`,
		`  "omission_reason": "string"`,
		"}",
		"",
		"Selected evidence paths:",
	}
	if len(selected) == 0 {
		lines = append(lines, "- (none)")
	} else {
		for _, rel := range selected {
			lines = append(lines, "- "+rel)
		}
	}
	lines = append(lines,
		"",
		"## Area Evidence Pack",
		"",
		evidence,
		"",
	)
	return strings.Join(lines, "\n")
}

func buildAIInitMultipassSynthesisPrompt(provider string, scan initScanReport, existing []Spec, maxSpecs int, areas []initCrawlArea, analyses []aiInitAreaResponse) string {
	lines := []string{
		"# Canon Init AI Decomposition",
		"",
		"Provider: " + provider,
		"Crawl mode: multipass",
		"",
		"Task:",
		"1. Synthesize final Canon specs from Canon-managed area analyses.",
		"2. Produce between 2 and " + strconv.Itoa(maxSpecs) + " specs that capture current project behavior.",
		"3. Use feature specs for user-facing behavior and technical specs for architecture or infrastructure concerns.",
		"4. Use depends_on and touched_domains when appropriate.",
		"5. Describe current behavior only. Do not propose future work.",
		"6. Ensure major analyzed areas are represented by at least one spec or intentionally omitted as support-only.",
		"7. Do not invent behavior that is not supported by the area summaries and evidence paths.",
		"8. Include concrete current-state behavior details in each body, not only high-level summaries.",
		"9. Return JSON only following the schema.",
		"",
		"Repository scan summary:",
		fmt.Sprintf("- Available files after filtering: %d", len(scan.AllProjectFiles)),
		fmt.Sprintf("- Excluded during local scan: %d", scan.ExcludedFiles),
		fmt.Sprintf("- Planned crawl areas: %d", len(areas)),
		"",
		"Schema:",
		"{",
		`  "model": "string",`,
		`  "project_summary": "string",`,
		`  "specs": [`,
		`    {`,
		`      "id": "7-char-hex",`,
		`      "type": "feature|technical",`,
		`      "title": "string",`,
		`      "domain": "string",`,
		`      "depends_on": ["spec-id"],`,
		`      "touched_domains": ["domain"],`,
		`      "body": "markdown",`,
		`      "review_hint": "string"`,
		`    }`,
		`  ]`,
		"}",
		"",
		"## Area Coverage",
	}
	for _, area := range areas {
		lines = append(lines, fmt.Sprintf("- %s: %d filtered files", area.Name, len(area.Files)))
	}
	lines = append(lines, "", "## Area Analyses", "")
	for _, analysis := range analyses {
		lines = append(lines,
			"### "+analysis.Area,
			"",
			"Summary: "+strings.TrimSpace(analysis.Summary),
			"Support only: "+strconv.FormatBool(analysis.SupportOnly),
		)
		if strings.TrimSpace(analysis.OmissionReason) != "" {
			lines = append(lines, "Omission reason: "+strings.TrimSpace(analysis.OmissionReason))
		}
		lines = append(lines,
			"",
			"Components:",
			renderInitPromptList(analysis.Components),
			"",
			"User-facing features:",
			renderInitPromptList(analysis.UserFacingFeatures),
			"",
			"Technical behaviors:",
			renderInitPromptList(analysis.TechnicalBehaviors),
			"",
			"Runtime wiring:",
			renderInitPromptList(analysis.RuntimeWiring),
			"",
			"Risks or gaps:",
			renderInitPromptList(analysis.RisksOrGaps),
			"",
			"Evidence files:",
			renderInitPromptList(analysis.EvidenceFiles),
			"",
		)
	}
	return appendExistingInitSpecs(lines, existing)
}

func renderInitPromptList(values []string) string {
	if len(values) == 0 {
		return "- (none)"
	}
	lines := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		lines = append(lines, "- "+trimmed)
	}
	if len(lines) == 0 {
		return "- (none)"
	}
	return strings.Join(lines, "\n")
}

func appendExistingInitSpecs(lines []string, existing []Spec) string {
	lines = append(lines,
		"## Existing Canonical Specs",
		"",
	)
	ordered := make([]Spec, len(existing))
	copy(ordered, existing)
	sortSpecsStable(ordered)
	for _, spec := range ordered {
		lines = append(lines,
			"### "+spec.ID,
			"",
			canonicalSpecText(spec),
			"",
		)
	}
	return strings.Join(lines, "\n")
}

func aiInitJSONSchema() string {
	return `{
  "type": "object",
  "required": ["model", "project_summary", "specs"],
  "additionalProperties": false,
  "properties": {
    "model": {"type": "string"},
    "project_summary": {"type": "string"},
    "specs": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["id", "type", "title", "domain", "depends_on", "touched_domains", "body", "review_hint"],
        "additionalProperties": false,
        "properties": {
          "id": {"type": "string"},
          "type": {"type": "string", "enum": ["feature", "technical"]},
          "title": {"type": "string"},
          "domain": {"type": "string"},
          "depends_on": {
            "type": "array",
            "items": {"type": "string"}
          },
          "touched_domains": {
            "type": "array",
            "items": {"type": "string"}
          },
          "body": {"type": "string"},
          "review_hint": {"type": "string"}
        }
      }
    }
  }
}`
}

func aiInitAreaJSONSchema() string {
	return `{
  "type": "object",
  "required": ["model", "area", "summary", "components", "user_facing_features", "technical_behaviors", "runtime_wiring", "risks_or_gaps", "evidence_files", "support_only", "omission_reason"],
  "additionalProperties": false,
  "properties": {
    "model": {"type": "string"},
    "area": {"type": "string"},
    "summary": {"type": "string"},
    "components": {
      "type": "array",
      "items": {"type": "string"}
    },
    "user_facing_features": {
      "type": "array",
      "items": {"type": "string"}
    },
    "technical_behaviors": {
      "type": "array",
      "items": {"type": "string"}
    },
    "runtime_wiring": {
      "type": "array",
      "items": {"type": "string"}
    },
    "risks_or_gaps": {
      "type": "array",
      "items": {"type": "string"}
    },
    "evidence_files": {
      "type": "array",
      "items": {"type": "string"}
    },
    "support_only": {"type": "boolean"},
    "omission_reason": {"type": "string"}
  }
}`
}

func parseAIInitResponse(root string, responseFile string) (aiInitResponse, error) {
	b, err := readAIResponseFile(root, responseFile)
	if err != nil {
		if errors.Is(err, errAIResponseFilePathRequired) {
			return aiInitResponse{}, fmt.Errorf("from-response mode requires --response-file")
		}
		return aiInitResponse{}, err
	}
	return decodeAIInitResponse(b)
}

func decodeAIInitResponse(b []byte) (aiInitResponse, error) {
	return decodeAIResponseJSON[aiInitResponse](b, "invalid AI init response JSON")
}

func decodeAIInitAreaResponse(b []byte) (aiInitAreaResponse, error) {
	return decodeAIResponseJSON[aiInitAreaResponse](b, "invalid AI init area response JSON")
}

func loadGitignorePatterns(root string) ([]ignorePattern, error) {
	path := filepath.Join(root, ".gitignore")
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	lines := strings.Split(string(b), "\n")
	patterns := make([]ignorePattern, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		negated := false
		if strings.HasPrefix(line, "!") {
			negated = true
			line = strings.TrimSpace(strings.TrimPrefix(line, "!"))
		}
		if line == "" {
			continue
		}
		line = filepath.ToSlash(line)
		dirOnly := strings.HasSuffix(line, "/")
		line = strings.TrimSuffix(line, "/")
		anchored := strings.HasPrefix(line, "/")
		line = strings.TrimPrefix(line, "/")
		patterns = append(patterns, ignorePattern{
			Pattern:  line,
			Negated:  negated,
			DirOnly:  dirOnly,
			Anchored: anchored,
		})
	}
	return patterns, nil
}

func matchesIgnorePatterns(rel string, isDir bool, patterns []ignorePattern) bool {
	matched := false
	for _, pattern := range patterns {
		if pattern.DirOnly && !isDir {
			if !strings.HasPrefix(rel, pattern.Pattern+"/") {
				continue
			}
		} else if !matchIgnorePattern(rel, pattern) {
			continue
		}
		if pattern.Negated {
			matched = false
		} else {
			matched = true
		}
	}
	return matched
}

func matchIgnorePattern(rel string, pattern ignorePattern) bool {
	candidate := strings.TrimSpace(rel)
	if candidate == "" {
		return false
	}
	p := strings.TrimSpace(pattern.Pattern)
	if p == "" {
		return false
	}
	if pattern.Anchored {
		ok, _ := path.Match(p, candidate)
		if ok {
			return true
		}
		if strings.HasPrefix(candidate, p+"/") {
			return true
		}
		return false
	}
	if !strings.Contains(p, "/") {
		base := path.Base(candidate)
		ok, _ := path.Match(p, base)
		if ok {
			return true
		}
		if strings.HasPrefix(candidate, p+"/") {
			return true
		}
		return false
	}
	ok, _ := path.Match(p, candidate)
	if ok {
		return true
	}
	if strings.HasPrefix(candidate, p+"/") {
		return true
	}
	return false
}

func matchAnyGlob(rel string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	for _, raw := range patterns {
		pattern := strings.TrimSpace(filepath.ToSlash(raw))
		if pattern == "" {
			continue
		}
		if matched, _ := path.Match(pattern, rel); matched {
			return true
		}
		if !strings.Contains(pattern, "/") {
			if matched, _ := path.Match(pattern, path.Base(rel)); matched {
				return true
			}
		}
	}
	return false
}
