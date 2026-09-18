package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/addp/ontology/docs"
)

// Route coverage alone cannot detect a generator that silently lost imported
// semantic types because the development Go workspace omitted this module.
func TestSwaggerContainsConcreteNativeDefinition(t *testing.T) {
	var document struct {
		Definitions map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"definitions"`
	}
	if err := json.Unmarshal([]byte(docs.SwaggerInfo.ReadDoc()), &document); err != nil {
		t.Fatal(err)
	}
	for suffix, fields := range map[string][]string{
		".CreateRequest":   {"revision", "definition"},
		".DefinitionInput": {"classes", "properties", "relations", "rules"},
		".Class":           {"id", "name", "parents"},
		".Rule":            {"id", "class_id", "expression", "basis", "inputs"},
		".RebuildRequest":  {"version", "failed_generation", "activation_version"},
	} {
		found := false
		for name, definition := range document.Definitions {
			if !strings.HasSuffix(name, suffix) {
				continue
			}
			found = true
			for _, field := range fields {
				if _, ok := definition.Properties[field]; !ok {
					t.Fatalf("Swagger lost %s.%s", name, field)
				}
			}
		}
		if !found {
			t.Fatalf("Swagger lost %s", suffix)
		}
	}
}
