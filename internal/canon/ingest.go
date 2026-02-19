package canon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func Ingest(root string, input IngestInput) (IngestResult, error) {
	if err := EnsureLayout(root, true); err != nil {
		return IngestResult{}, err
	}

	rawText, err := readIngestText(input)
	if err != nil {
		return IngestResult{}, err
	}

	existing, err := loadSpecs(root)
	if err != nil {
		return IngestResult{}, err
	}

	mode := strings.ToLower(strings.TrimSpace(input.ConflictMode))
	if mode == "" {
		mode = "off"
	}

	now := nowUTC()
	spec, conflicts, err := prepareCandidateSpec(root, existing, rawText, input, mode, now)
	if err != nil {
		return IngestResult{}, err
	}

	for _, ex := range existing {
		if ex.ID == spec.ID {
			return IngestResult{}, fmt.Errorf("spec id already exists: %s", spec.ID)
		}
	}

	if len(conflicts) > 0 {
		reportPath, err := writeConflictReport(root, spec, conflicts)
		if err != nil {
			return IngestResult{}, err
		}
		return IngestResult{}, fmt.Errorf("merge conflict detected for %s, report: %s", spec.ID, reportPath)
	}
	if mode == "auto" || mode == "from-response" {
		if err := validateCanonicalBodyCoverage(rawText, spec.Body); err != nil {
			return IngestResult{}, err
		}
	}

	specText := canonicalSpecText(spec)
	specRelPath := filepath.ToSlash(filepath.Join(".canon", "specs", specFileName(spec.ID)))
	specAbsPath := filepath.Join(root, specRelPath)
	if _, err := writeTextIfChanged(specAbsPath, specText); err != nil {
		return IngestResult{}, err
	}
	sourceText := strings.TrimRight(rawText, "\n") + "\n"
	sourceRelPath := filepath.ToSlash(filepath.Join(".canon", "sources", spec.ID+".source.md"))
	sourceAbsPath := filepath.Join(root, sourceRelPath)
	if _, err := writeTextIfChanged(sourceAbsPath, sourceText); err != nil {
		return IngestResult{}, err
	}

	ledgerEntries, err := LoadLedger(root)
	if err != nil {
		return IngestResult{}, err
	}

	parents := normalizeList(input.Parents)
	if len(parents) == 0 {
		heads := ledgerHeads(ledgerEntries)
		if len(heads) == 0 {
			parents = []string{}
		} else {
			parents = []string{heads[len(heads)-1]}
		}
	}

	entry := LedgerEntry{
		SpecID:      spec.ID,
		Title:       spec.Title,
		Type:        spec.Type,
		Domain:      spec.Domain,
		Parents:     parents,
		Sequence:    now.UnixNano(),
		IngestedAt:  now.Format(timeRFC3339),
		ContentHash: checksum(specText),
		SpecPath:    specRelPath,
		SourcePath:  sourceRelPath,
	}

	entryBytes, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return IngestResult{}, err
	}
	entryBytes = append(entryBytes, '\n')

	ledgerName := ledgerFileName(entry.IngestedAt, spec.ID)
	ledgerRelPath := filepath.ToSlash(filepath.Join(".canon", "ledger", ledgerName))
	ledgerAbsPath := filepath.Join(root, ledgerRelPath)
	if _, err := writeTextIfChanged(ledgerAbsPath, string(entryBytes)); err != nil {
		return IngestResult{}, err
	}

	return IngestResult{
		SpecID:     spec.ID,
		SpecPath:   specRelPath,
		LedgerPath: ledgerRelPath,
		Parents:    parents,
	}, nil
}

func prepareCandidateSpec(root string, existing []Spec, rawText string, input IngestInput, mode string, now time.Time) (Spec, []specConflict, error) {
	switch mode {
	case "off":
		_, body, hasFrontmatter, parseErr := splitOptionalFrontmatter(rawText)
		if parseErr != nil {
			return Spec{}, nil, parseErr
		}
		spec, err := buildSpecFromInput(input, rawText, body, hasFrontmatter, now)
		if err != nil {
			return Spec{}, nil, err
		}
		// Deterministic fallback mode for local testing.
		return spec, detectSpecConflicts(existing, spec), nil
	case "from-response", "auto":
		return prepareCandidateSpecFromAI(root, existing, rawText, input, mode, now)
	default:
		return Spec{}, nil, fmt.Errorf("unsupported ingest mode: %s", mode)
	}
}

func readIngestText(input IngestInput) (string, error) {
	if strings.TrimSpace(input.File) == "" && strings.TrimSpace(input.Text) == "" {
		return "", fmt.Errorf("ingest requires --file or --text")
	}
	if strings.TrimSpace(input.File) != "" && strings.TrimSpace(input.Text) != "" {
		return "", fmt.Errorf("ingest accepts either --file or --text, not both")
	}
	if strings.TrimSpace(input.File) != "" {
		b, err := os.ReadFile(input.File)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	return input.Text, nil
}

func buildSpecFromInput(input IngestInput, rawText string, body string, hasFrontmatter bool, now time.Time) (Spec, error) {
	if hasFrontmatter {
		spec, err := parseSpecText(rawText, "ingest-input")
		if err != nil {
			return Spec{}, err
		}
		if strings.TrimSpace(input.ID) != "" {
			spec.ID = strings.TrimSpace(input.ID)
		}
		if strings.TrimSpace(input.Type) != "" {
			spec.Type = strings.TrimSpace(input.Type)
		}
		if strings.TrimSpace(input.Title) != "" {
			spec.Title = strings.TrimSpace(input.Title)
		}
		if strings.TrimSpace(input.Domain) != "" {
			spec.Domain = strings.TrimSpace(input.Domain)
		}
		if len(input.DependsOn) > 0 {
			spec.DependsOn = normalizeList(input.DependsOn)
		}
		if len(input.TouchedDomains) > 0 {
			spec.TouchedDomains = mustInclude(normalizeList(input.TouchedDomains), spec.Domain)
		}
		if strings.TrimSpace(input.Created) != "" {
			created, err := parseRFC3339OrNow(input.Created)
			if err != nil {
				return Spec{}, err
			}
			spec.Created = created
		}
		return normalizeSpecDefaults(spec)
	}

	payload := strings.TrimSpace(body)
	if payload == "" {
		payload = strings.TrimSpace(rawText)
	}
	if payload == "" {
		return Spec{}, fmt.Errorf("empty spec payload")
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = inferTitle(payload)
	}
	domain := strings.TrimSpace(input.Domain)
	if domain == "" {
		domain = "general"
	}
	typ := strings.TrimSpace(input.Type)
	if typ == "" {
		typ = "feature"
	}
	created, err := parseRFC3339OrNow(input.Created)
	if err != nil {
		return Spec{}, err
	}

	specID := strings.TrimSpace(input.ID)
	if specID == "" {
		specID = generatedSpecID(now, title)
	}

	spec := Spec{
		ID:             specID,
		Type:           typ,
		Title:          title,
		Domain:         domain,
		Created:        created,
		DependsOn:      normalizeList(input.DependsOn),
		TouchedDomains: mustInclude(normalizeList(input.TouchedDomains), domain),
		Body:           payload,
	}
	return normalizeSpecDefaults(spec)
}

func normalizeSpecDefaults(spec Spec) (Spec, error) {
	spec.ID = strings.TrimSpace(spec.ID)
	if spec.ID == "" {
		return Spec{}, fmt.Errorf("spec id is required")
	}
	spec.Type = strings.TrimSpace(spec.Type)
	if spec.Type == "" {
		spec.Type = "feature"
	}
	spec.Title = strings.TrimSpace(spec.Title)
	if spec.Title == "" {
		spec.Title = spec.ID
	}
	spec.Domain = strings.TrimSpace(spec.Domain)
	if spec.Domain == "" {
		spec.Domain = "general"
	}
	created, err := parseRFC3339OrNow(spec.Created)
	if err != nil {
		return Spec{}, err
	}
	spec.Created = created
	spec.DependsOn = normalizeList(spec.DependsOn)
	spec.TouchedDomains = mustInclude(spec.TouchedDomains, spec.Domain)
	spec.Body = strings.TrimSpace(spec.Body)
	if spec.Body == "" {
		spec.Body = "(No body content)"
	}
	return spec, nil
}

func generatedSpecID(now time.Time, title string) string {
	return "spec-" + now.Format("20060102150405") + "-" + slugify(title)
}

func ledgerFileName(ingestedAt string, specID string) string {
	t := strings.ReplaceAll(ingestedAt, ":", "")
	t = strings.ReplaceAll(t, "-", "")
	t = strings.ReplaceAll(t, "Z", "Z")
	t = strings.ReplaceAll(t, "+", "")
	t = strings.ReplaceAll(t, ".", "")
	if len(t) > 24 {
		t = t[:24]
	}
	if t == "" {
		t = nowUTC().Format("20060102T150405Z")
	}
	return t + "-" + slugify(specID) + ".json"
}
