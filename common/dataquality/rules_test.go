package dataquality

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseRulesV1(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"schema_version":"addp.quality.rules/v1",
		"rules":[
			{"type":"not_null","enabled":true,"severity":"error","message":"required","params":{}},
			{"type":"unique","enabled":true,"severity":"warning","message":"","params":{}},
			{"type":"format","enabled":true,"severity":"error","message":"","params":{"pattern":"^[A-Z]+$"}},
			{"type":"length","enabled":true,"severity":"info","message":"","params":{"min":1,"max":20}},
			{"type":"value_range","enabled":true,"severity":"error","message":"","params":{"min":-1.5,"max":10}},
			{"type":"allowed_values","enabled":false,"severity":"warning","message":"","params":{"values":["A","B"]}}
		]
	}`)

	document, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Rules) != 6 || len(document.EnabledRules()) != 5 {
		t.Fatalf("unexpected rules: %#v", document.Rules)
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"schema_version":"addp.quality.rules/v1"`) {
		t.Fatalf("encoded document = %s", encoded)
	}
}

func TestParseRulesRejectsInvalidContracts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
	}{
		{name: "legacy document", raw: `{"rules":[]}`},
		{name: "unknown document field", raw: `{"schema_version":"addp.quality.rules/v1","rules":[],"legacy":true}`},
		{name: "unknown rule", raw: `{"schema_version":"addp.quality.rules/v1","rules":[{"type":"custom","enabled":true,"severity":"error","message":"","params":{}}]}`},
		{name: "missing enabled", raw: `{"schema_version":"addp.quality.rules/v1","rules":[{"type":"not_null","severity":"error","message":"","params":{}}]}`},
		{name: "top level param", raw: `{"schema_version":"addp.quality.rules/v1","rules":[{"type":"format","enabled":true,"severity":"error","message":"","params":{},"pattern":"x"}]}`},
		{name: "decimal length", raw: `{"schema_version":"addp.quality.rules/v1","rules":[{"type":"length","enabled":true,"severity":"error","message":"","params":{"min":1.5}}]}`},
		{name: "reversed range", raw: `{"schema_version":"addp.quality.rules/v1","rules":[{"type":"value_range","enabled":true,"severity":"error","message":"","params":{"min":10,"max":1}}]}`},
		{name: "duplicate allowed values", raw: `{"schema_version":"addp.quality.rules/v1","rules":[{"type":"allowed_values","enabled":true,"severity":"error","message":"","params":{"values":["A","A"]}}]}`},
		{name: "trailing value", raw: `{"schema_version":"addp.quality.rules/v1","rules":[]} {}`},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse([]byte(tt.raw)); err == nil {
				t.Fatalf("Parse(%s) error = nil", tt.raw)
			}
		})
	}
}

func TestEmptyDocumentIsValid(t *testing.T) {
	t.Parallel()

	document := EmptyDocument()
	if err := document.Validate(); err != nil {
		t.Fatal(err)
	}
	result, err := ToMap(document)
	if err != nil {
		t.Fatal(err)
	}
	if result["schema_version"] != RulesSchemaVersion {
		t.Fatalf("schema_version = %#v", result["schema_version"])
	}
}
