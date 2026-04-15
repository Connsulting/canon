package canon

import (
	"encoding/json"
	"testing"
)

func TestAIIngestJSONSchemaRequiresCanonicalSpecProperties(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(aiIngestJSONSchema()), &schema); err != nil {
		t.Fatalf("ingest schema is invalid JSON: %v", err)
	}

	canonicalSpec, ok := objectProperty(t, schema, "canonical_spec")
	if !ok {
		t.Fatalf("missing canonical_spec schema")
	}
	properties, ok := canonicalSpec["properties"].(map[string]any)
	if !ok {
		t.Fatalf("canonical_spec properties are missing or malformed")
	}
	requiredValues, ok := canonicalSpec["required"].([]any)
	if !ok {
		t.Fatalf("canonical_spec required list is missing or malformed")
	}

	required := make(map[string]bool, len(requiredValues))
	for _, value := range requiredValues {
		name, ok := value.(string)
		if !ok {
			t.Fatalf("canonical_spec required entry is not a string: %#v", value)
		}
		required[name] = true
	}

	for name := range properties {
		if !required[name] {
			t.Fatalf("canonical_spec property %q must be required for strict provider schemas", name)
		}
	}
	for _, name := range []string{"requirement_kind", "source_issue", "approval_state"} {
		if !required[name] {
			t.Fatalf("canonical_spec required list is missing %q", name)
		}
	}
}

func objectProperty(t *testing.T, schema map[string]any, name string) (map[string]any, bool) {
	t.Helper()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return nil, false
	}
	property, ok := properties[name].(map[string]any)
	return property, ok
}
