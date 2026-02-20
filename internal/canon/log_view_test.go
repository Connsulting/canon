package canon

import (
	"strings"
	"testing"
	"time"
)

func TestBuildLogViewPrimaryHeadScopeAndAll(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	ingestLogSpec(t, root, "spec-a", "Auth Base", "auth", "feature", "2026-02-19T10:00:00Z", nil, nil)
	ingestLogSpec(t, root, "spec-b", "Rate Limits", "api", "feature", "2026-02-19T10:10:00Z", []string{"spec-a"}, nil)
	ingestLogSpec(t, root, "spec-c", "Billing Hooks", "billing", "feature", "2026-02-19T10:20:00Z", []string{"spec-b"}, nil)
	ingestLogSpec(t, root, "spec-d", "Ops Alerts", "ops", "feature", "2026-02-19T10:30:00Z", nil, []string{"ghost-parent"})

	nodes, err := BuildLogView(root, LogOptions{
		Limit: 50,
		Graph: true,
	})
	if err != nil {
		t.Fatalf("BuildLogView default scope failed: %v", err)
	}
	gotDefault := realNodeIDs(nodes)
	wantDefault := []string{"spec-d"}
	if !equalStringSlices(gotDefault, wantDefault) {
		t.Fatalf("default scope mismatch: got %v want %v", gotDefault, wantDefault)
	}

	nodesAll, err := BuildLogView(root, LogOptions{
		Limit: 50,
		Graph: true,
		All:   true,
	})
	if err != nil {
		t.Fatalf("BuildLogView all scope failed: %v", err)
	}
	gotAll := realNodeIDs(nodesAll)
	wantAll := []string{"spec-d", "spec-c", "spec-b", "spec-a"}
	if !equalStringSlices(gotAll, wantAll) {
		t.Fatalf("--all scope mismatch: got %v want %v", gotAll, wantAll)
	}
}

func TestBuildLogViewMissingDependencyPlaceholderAndLimit(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	ingestLogSpec(t, root, "spec-a", "Auth Base", "auth", "feature", "2026-02-19T10:00:00Z", nil, nil)
	ingestLogSpec(t, root, "spec-c", "Rate Limits", "api", "feature", "2026-02-19T10:10:00Z", []string{"spec-a", "spec-missing"}, nil)

	nodes, err := BuildLogView(root, LogOptions{
		Limit: 1,
		Graph: true,
		All:   true,
	})
	if err != nil {
		t.Fatalf("BuildLogView failed: %v", err)
	}

	gotReal := realNodeIDs(nodes)
	wantReal := []string{"spec-c"}
	if !equalStringSlices(gotReal, wantReal) {
		t.Fatalf("real nodes mismatch: got %v want %v", gotReal, wantReal)
	}
	if !containsMissingNode(nodes, "spec-missing") {
		t.Fatalf("expected missing dependency placeholder for spec-missing")
	}
	if containsNode(nodes, "spec-a") {
		t.Fatalf("did not expect limited-out real dependency spec-a in graph nodes")
	}
}

func TestBuildLogViewFilters(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	ingestLogSpec(t, root, "spec-auth", "Audit Trail", "auth", "feature", "2026-02-19T10:00:00Z", nil, []string{"ghost-auth"})
	ingestLogSpec(t, root, "spec-api", "Rate Limits", "api", "feature", "2026-02-19T10:10:00Z", nil, []string{"ghost-api"})
	ingestLogSpec(t, root, "spec-api-tech", "Cache Policy", "api", "technical", "2026-02-19T10:20:00Z", nil, []string{"ghost-api-tech"})

	opts := LogOptions{
		Limit:   50,
		OneLine: true,
		All:     true,
		Domain:  "api",
		Type:    "feature",
		Grep:    "rate",
	}
	nodes, err := BuildLogView(root, opts)
	if err != nil {
		t.Fatalf("BuildLogView failed: %v", err)
	}

	got := realNodeIDs(nodes)
	want := []string{"spec-api"}
	if !equalStringSlices(got, want) {
		t.Fatalf("filter mismatch: got %v want %v", got, want)
	}

	text := RenderLogText(nodes, opts)
	if !strings.Contains(text, "spec-api Rate Limits") {
		t.Fatalf("expected oneline output for spec-api, got:\n%s", text)
	}
	if strings.Contains(text, "spec-auth") {
		t.Fatalf("unexpected filtered spec in output:\n%s", text)
	}
}

func TestRenderLogTextGraphIncludesEdges(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	ingestLogSpec(t, root, "spec-a", "Auth Base", "auth", "feature", "2026-02-19T10:00:00Z", nil, nil)
	ingestLogSpec(t, root, "spec-b", "Rate Limits", "api", "feature", "2026-02-19T10:10:00Z", []string{"spec-a"}, nil)
	ingestLogSpec(t, root, "spec-c", "Billing Hooks", "billing", "feature", "2026-02-19T10:20:00Z", []string{"spec-a"}, nil)
	ingestLogSpec(t, root, "spec-d", "Merge Point", "billing", "feature", "2026-02-19T10:30:00Z", []string{"spec-b", "spec-c"}, nil)

	opts := LogOptions{
		Limit:   50,
		Graph:   true,
		OneLine: true,
		All:     true,
	}
	nodes, err := BuildLogView(root, opts)
	if err != nil {
		t.Fatalf("BuildLogView failed: %v", err)
	}
	text := RenderLogText(nodes, opts)

	if !strings.Contains(text, "* spec-d") {
		t.Fatalf("expected graph node for spec-d, got:\n%s", text)
	}
	if !strings.Contains(text, "| * spec-") && !strings.Contains(text, "* | spec-") {
		t.Fatalf("expected branch-style graph columns, got:\n%s", text)
	}
	if !strings.Contains(text, "\\") {
		t.Fatalf("expected split connector in graph output, got:\n%s", text)
	}
	if !strings.Contains(text, "/") {
		t.Fatalf("expected merge connector in graph output, got:\n%s", text)
	}
	if strings.Contains(text, "|->") {
		t.Fatalf("did not expect separate edge lines in graph output, got:\n%s", text)
	}
}

func TestBuildLogViewMarksCycleNodes(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root, true); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	ingestLogSpec(t, root, "spec-a", "A", "auth", "feature", "2026-02-19T10:00:00Z", []string{"spec-b"}, nil)
	ingestLogSpec(t, root, "spec-b", "B", "auth", "feature", "2026-02-19T10:10:00Z", []string{"spec-a"}, nil)

	nodes, err := BuildLogView(root, LogOptions{
		Limit: 50,
		Graph: true,
		All:   true,
	})
	if err != nil {
		t.Fatalf("BuildLogView failed: %v", err)
	}

	cycleCount := 0
	for _, node := range nodes {
		if node.Missing {
			continue
		}
		if node.Cycle {
			cycleCount++
		}
	}
	if cycleCount != 2 {
		t.Fatalf("expected both real nodes to be marked as cycle participants, got %d", cycleCount)
	}
}

func TestRenderLogTextColorAlways(t *testing.T) {
	nodes := []LogNode{
		{
			ID: "spec-a",
			Spec: &Spec{
				ID:      "spec-a",
				Title:   "Auth Base",
				Type:    "feature",
				Domain:  "auth",
				Created: "2026-02-19T10:00:00Z",
			},
		},
	}
	text := RenderLogText(nodes, LogOptions{
		OneLine: true,
		Color:   "always",
	})
	if !strings.Contains(text, "\u001b[32m") {
		t.Fatalf("expected ANSI color sequence in output, got:\n%s", text)
	}
}

func TestRelativeTimeFormatting(t *testing.T) {
	reference := time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC)
	target := reference.Add(-6 * 7 * 24 * time.Hour)
	got := relativeTime(target, reference)
	if got != "6 weeks ago" {
		t.Fatalf("expected 6 weeks ago, got %s", got)
	}
}

func ingestLogSpec(t *testing.T, root string, id string, title string, domain string, typ string, created string, dependsOn []string, parents []string) {
	t.Helper()
	if _, err := Ingest(root, IngestInput{
		Text:      "Body for " + id,
		ID:        id,
		Type:      typ,
		Title:     title,
		Domain:    domain,
		Created:   created,
		DependsOn: dependsOn,
		Parents:   parents,
	}); err != nil {
		t.Fatalf("Ingest %s failed: %v", id, err)
	}
}

func realNodeIDs(nodes []LogNode) []string {
	out := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if node.Missing {
			continue
		}
		out = append(out, node.ID)
	}
	return out
}

func containsMissingNode(nodes []LogNode, id string) bool {
	for _, node := range nodes {
		if node.ID == id && node.Missing {
			return true
		}
	}
	return false
}

func containsNode(nodes []LogNode, id string) bool {
	for _, node := range nodes {
		if node.ID == id {
			return true
		}
	}
	return false
}

func equalStringSlices(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
