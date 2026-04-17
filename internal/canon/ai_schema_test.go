package canon

import (
	"encoding/json"
	"testing"
)

func TestAIIngestStrictSchemaRequiresEveryCanonicalSpecProperty(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(aiIngestJSONSchema()), &schema); err != nil {
		t.Fatalf("invalid ingest schema JSON: %v", err)
	}

	canonicalSpec := schemaObject(t, schema, "properties", "canonical_spec")
	assertRequiredIncludesAllProperties(t, canonicalSpec, "ingest canonical_spec")
}

func TestGCStrictSchemaRequiresEveryConsolidatedSpecProperty(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(gcConsolidationJSONSchema()), &schema); err != nil {
		t.Fatalf("invalid gc schema JSON: %v", err)
	}

	consolidatedSpecs := schemaObject(t, schema, "properties", "consolidated_specs")
	items, ok := consolidatedSpecs["items"].(map[string]any)
	if !ok {
		t.Fatalf("gc consolidated_specs schema is missing object items: %#v", consolidatedSpecs["items"])
	}
	assertRequiredIncludesAllProperties(t, items, "gc consolidated_specs item")
}

func assertRequiredIncludesAllProperties(t *testing.T, schema map[string]any, label string) {
	t.Helper()

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s schema is missing properties object", label)
	}
	requiredValues, ok := schema["required"].([]any)
	if !ok {
		t.Fatalf("%s schema is missing required array", label)
	}

	required := make(map[string]bool, len(requiredValues))
	for _, value := range requiredValues {
		name, ok := value.(string)
		if !ok {
			t.Fatalf("%s required entry is not a string: %#v", label, value)
		}
		required[name] = true
	}

	for name := range properties {
		if !required[name] {
			t.Fatalf("%s schema property %q is not required", label, name)
		}
	}
}

func schemaObject(t *testing.T, schema map[string]any, path ...string) map[string]any {
	t.Helper()

	current := schema
	for _, key := range path {
		next, ok := current[key].(map[string]any)
		if !ok {
			t.Fatalf("schema path %v is missing object at %q: %#v", path, key, current[key])
		}
		current = next
	}
	return current
}
