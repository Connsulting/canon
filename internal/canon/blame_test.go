package canon

import (
	"fmt"
	"testing"
)

func TestNarrowSpecsForBlamePrefersDomainAndKeywordMatches(t *testing.T) {
	specs := make([]Spec, 0, 40)
	for i := 0; i < 36; i++ {
		specs = append(specs, Spec{
			ID:      fmt.Sprintf("noise-%02d", i),
			Title:   "Noise",
			Type:    "feature",
			Domain:  "ops",
			Created: fmt.Sprintf("2026-02-01T00:%02d:00Z", i%60),
			Body:    "Background maintenance should run daily.",
		})
	}

	specs = append(specs,
		Spec{
			ID:             "f4e5d6c",
			Title:          "User Registration Flow",
			Type:           "feature",
			Domain:         "auth",
			TouchedDomains: []string{"auth"},
			Created:        "2026-01-10T09:00:00Z",
			Body: "New users must verify their email address before account activation.\n" +
				"Unverified accounts must be restricted to read-only access.",
		},
		Spec{
			ID:             "7890abc",
			Title:          "Billing and Subscription Gating",
			Type:           "feature",
			Domain:         "billing",
			TouchedDomains: []string{"billing", "auth"},
			Created:        "2026-02-01T14:00:00Z",
			Body: "Paid features must only be accessible to users with active subscriptions.\n" +
				"Subscription activation requires a fully verified account.",
		},
		Spec{
			ID:             "aabbccd",
			Title:          "Billing Reporting",
			Type:           "feature",
			Domain:         "billing",
			TouchedDomains: []string{"billing"},
			Created:        "2026-02-01T14:10:00Z",
			Body:           "Billing reports should be generated nightly.",
		},
		Spec{
			ID:             "ddeeff0",
			Title:          "Email Delivery",
			Type:           "feature",
			Domain:         "notifications",
			TouchedDomains: []string{"notifications", "auth"},
			Created:        "2026-02-01T14:20:00Z",
			Body:           "Email notifications should include unsubscribe links.",
		},
	)

	index := buildIndex(specs)
	narrowed := narrowSpecsForBlame(
		specs,
		index,
		"users must verify their email before accessing paid features",
		12,
	)

	if len(narrowed) == 0 {
		t.Fatalf("expected narrowed candidate set")
	}
	if len(narrowed) >= len(specs) {
		t.Fatalf("expected narrowing to reduce candidate set: narrowed=%d total=%d", len(narrowed), len(specs))
	}

	ids := map[string]struct{}{}
	for _, spec := range narrowed {
		ids[spec.ID] = struct{}{}
	}
	if _, ok := ids["f4e5d6c"]; !ok {
		t.Fatalf("expected auth match to be retained")
	}
	if _, ok := ids["7890abc"]; !ok {
		t.Fatalf("expected billing gating match to be retained")
	}
}

func TestFindBlameCitationResolvesMarkdownSectionAndLines(t *testing.T) {
	lines := []string{
		"---",
		"id: spec-123",
		"---",
		"# API Rules",
		"",
		"## Pagination",
		"",
		"- All list endpoints must return paginated responses.",
		"- Default page size must be 25 items.",
	}

	citation, ok := findBlameCitation(lines, aiBlameCitation{
		Text: "Default page size must be 25 items.",
	})
	if !ok {
		t.Fatalf("expected citation match")
	}
	if citation.Section != "Pagination" {
		t.Fatalf("expected nearest section Pagination, got %q", citation.Section)
	}
	if citation.StartLine != 9 || citation.EndLine != 9 {
		t.Fatalf("expected line 9, got %d-%d", citation.StartLine, citation.EndLine)
	}
	if citation.Text != "Default page size must be 25 items." {
		t.Fatalf("unexpected citation text: %q", citation.Text)
	}
}

func TestFindBlameCitationRejectsUnresolvedText(t *testing.T) {
	lines := []string{
		"# API Rules",
		"Default page size must be 25 items.",
	}

	if _, ok := findBlameCitation(lines, aiBlameCitation{Text: "Responses are translated."}); ok {
		t.Fatalf("expected unresolved citation text to be rejected")
	}
}
