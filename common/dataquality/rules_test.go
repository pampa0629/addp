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
			{"rule_key":"00000000-0000-4000-8000-000000000001","type":"not_null","enabled":true,"severity":"error","message":"required","params":{}},
			{"rule_key":"00000000-0000-4000-8000-000000000002","type":"unique","enabled":true,"severity":"warning","message":"","params":{}},
			{"rule_key":"00000000-0000-4000-8000-000000000003","type":"format","enabled":true,"severity":"error","message":"","params":{"pattern":"^[A-Z]+$"}},
			{"rule_key":"00000000-0000-4000-8000-000000000004","type":"length","enabled":true,"severity":"info","message":"","params":{"min":1,"max":20}},
			{"rule_key":"00000000-0000-4000-8000-000000000005","type":"value_range","enabled":true,"severity":"error","message":"","params":{"min":-1.5,"max":10}},
			{"rule_key":"00000000-0000-4000-8000-000000000006","type":"allowed_values","enabled":false,"severity":"warning","message":"","params":{"values":["A","B"]}}
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
		{name: "missing rule key", raw: `{"schema_version":"addp.quality.rules/v1","rules":[{"type":"not_null","enabled":true,"severity":"error","message":"","params":{}}]}`},
		{name: "non canonical rule key", raw: `{"schema_version":"addp.quality.rules/v1","rules":[{"rule_key":"00000000-0000-4000-8000-00000000000A","type":"not_null","enabled":true,"severity":"error","message":"","params":{}}]}`},
		{name: "nil rule key", raw: `{"schema_version":"addp.quality.rules/v1","rules":[{"rule_key":"00000000-0000-0000-0000-000000000000","type":"not_null","enabled":true,"severity":"error","message":"","params":{}}]}`},
		{name: "unknown rule", raw: canonicalRuleJSON("00000000-0000-4000-8000-000000000001", "custom", `{},"legacy":true`)},
		{name: "missing enabled", raw: `{"schema_version":"addp.quality.rules/v1","rules":[{"rule_key":"00000000-0000-4000-8000-000000000001","type":"not_null","severity":"error","message":"","params":{}}]}`},
		{name: "top level param", raw: canonicalRuleJSON("00000000-0000-4000-8000-000000000001", "format", `{},"pattern":"x"`)},
		{name: "decimal length", raw: canonicalRuleJSON("00000000-0000-4000-8000-000000000001", "length", `{"min":1.5}`)},
		{name: "reversed range", raw: canonicalRuleJSON("00000000-0000-4000-8000-000000000001", "value_range", `{"min":10,"max":1}`)},
		{name: "duplicate allowed values", raw: canonicalRuleJSON("00000000-0000-4000-8000-000000000001", "allowed_values", `{"values":["A","A"]}`)},
		{name: "duplicate rule keys", raw: `{"schema_version":"addp.quality.rules/v1","rules":[{"rule_key":"00000000-0000-4000-8000-000000000001","type":"not_null","enabled":true,"severity":"error","message":"","params":{}},{"rule_key":"00000000-0000-4000-8000-000000000001","type":"unique","enabled":true,"severity":"error","message":"","params":{}}]}`},
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

func canonicalRuleJSON(ruleKey, ruleType, params string) string {
	return `{"schema_version":"addp.quality.rules/v1","rules":[{"rule_key":"` + ruleKey + `","type":"` + ruleType + `","enabled":true,"severity":"error","message":"","params":` + params + `}]}`
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
