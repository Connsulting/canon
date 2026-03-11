package canon

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseSpecTextRoundTripsCanonicalQuotedMetadata(t *testing.T) {
	spec := Spec{
		ID:             "spec-quoted",
		Type:           "feature",
		Title:          `Client says, "ship C:\drop": review`,
		Domain:         "canon-cli",
		Created:        "2026-03-11T12:00:00Z",
		DependsOn:      []string{`dep,one`, `path\branch`, `say "hi"`},
		TouchedDomains: []string{"canon-cli"},
		Consolidates:   []string{`legacy,entry`, `folder\child`},
		Body:           "Quoted frontmatter should survive reloads.",
	}

	parsed, err := parseSpecText(canonicalSpecText(spec), "inline")
	if err != nil {
		t.Fatalf("parseSpecText failed: %v", err)
	}

	want := spec
	want.DependsOn = normalizeList(spec.DependsOn)
	want.TouchedDomains = normalizeList(spec.TouchedDomains)
	want.Consolidates = normalizeList(spec.Consolidates)
	want.Path = "inline"

	if !reflect.DeepEqual(parsed, want) {
		t.Fatalf("unexpected parsed spec:\nwant: %#v\ngot:  %#v", want, parsed)
	}
}

func TestParseSpecTextRoundTripsCanonicalBareBooleanLikeStrings(t *testing.T) {
	spec := Spec{
		ID:             "True",
		Type:           "feature",
		Title:          "False",
		Domain:         "canon-cli",
		Created:        "2026-03-11T12:05:00Z",
		DependsOn:      []string{"3fd7c21"},
		TouchedDomains: []string{"canon-cli"},
		Body:           "Boolean-like metadata should remain string data.",
	}

	parsed, err := parseSpecText(canonicalSpecText(spec), "inline")
	if err != nil {
		t.Fatalf("parseSpecText failed: %v", err)
	}

	want := spec
	want.DependsOn = normalizeList(spec.DependsOn)
	want.TouchedDomains = normalizeList(spec.TouchedDomains)
	want.Path = "inline"

	if !reflect.DeepEqual(parsed, want) {
		t.Fatalf("unexpected parsed spec:\nwant: %#v\ngot:  %#v", want, parsed)
	}
}

func TestParseSpecTextRejectsMalformedQuotedFrontmatter(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		wantError string
	}{
		{
			name: "unterminated quoted scalar",
			text: `---
id: spec-bad
domain: canon-cli
title: "missing
---
Body.`,
			wantError: "invalid frontmatter value for title",
		},
		{
			name: "invalid quoted escape",
			text: `---
id: spec-bad
domain: canon-cli
title: "bad\q"
---
Body.`,
			wantError: "invalid frontmatter value for title",
		},
		{
			name: "unterminated list",
			text: `---
id: spec-bad
domain: canon-cli
depends_on: ["dep,one"
---
Body.`,
			wantError: "invalid frontmatter value for depends_on",
		},
		{
			name: "trailing comma in list",
			text: `---
id: spec-bad
domain: canon-cli
depends_on: ["dep,one", ]
---
Body.`,
			wantError: "invalid frontmatter value for depends_on",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseSpecText(tc.text, "inline")
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestParseSpecTextDefaultsAndValidation(t *testing.T) {
	t.Run("defaults optional fields", func(t *testing.T) {
		spec, err := parseSpecText(`---
id: spec-defaults
domain: canon-cli
---
Body.`, "inline")
		if err != nil {
			t.Fatalf("parseSpecText failed: %v", err)
		}
		if spec.Type != "feature" {
			t.Fatalf("expected default type feature, got %q", spec.Type)
		}
		if spec.Title != "spec-defaults" {
			t.Fatalf("expected default title spec id, got %q", spec.Title)
		}
		if spec.Created == "" {
			t.Fatalf("expected created timestamp to be populated")
		}
		if !reflect.DeepEqual(spec.TouchedDomains, []string{"canon-cli"}) {
			t.Fatalf("expected touched domains to include domain, got %v", spec.TouchedDomains)
		}
		if spec.Body != "Body." {
			t.Fatalf("unexpected body: %q", spec.Body)
		}
	})

	t.Run("missing required domain", func(t *testing.T) {
		_, err := parseSpecText(`---
id: spec-missing-domain
---
Body.`, "inline")
		if err == nil {
			t.Fatalf("expected missing domain error")
		}
		if !strings.Contains(err.Error(), "missing required field `domain`") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
