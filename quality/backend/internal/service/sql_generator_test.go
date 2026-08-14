package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/addp/common/dataquality"
)

func parseTestRule(t *testing.T, raw string) dataquality.Rule {
	t.Helper()
	var rule map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &rule); err != nil {
		t.Fatal(err)
	}
	rule["rule_key"] = "00000000-0000-4000-8000-000000000001"
	encoded, err := json.Marshal(rule)
	if err != nil {
		t.Fatal(err)
	}
	document, err := dataquality.Parse([]byte(`{"schema_version":"addp.quality.rules/v1","rules":[` + string(encoded) + `]}`))
	if err != nil {
		t.Fatal(err)
	}
	return document.Rules[0]
}

func TestSQLGeneratorQuotesIdentifiersAndBindsValues(t *testing.T) {
	t.Parallel()

	rule := parseTestRule(t, `{"type":"format","enabled":true,"severity":"error","message":"","params":{"pattern":"x' OR 1=1 --"}}`)
	compiled, err := NewSQLGenerator().GenerateCheckSQL(`tenant";DROP TABLE users;--`, `users";--`, `email"`, rule)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(compiled.SQL, "x' OR") {
		t.Fatalf("compiled SQL contains untrusted value text: %s", compiled.SQL)
	}
	if !strings.Contains(compiled.SQL, `"tenant"";DROP TABLE users;--"`) || !strings.Contains(compiled.SQL, `"users"";--"`) || !strings.Contains(compiled.SQL, `"email"""`) {
		t.Fatalf("compiled SQL does not quote identifiers: %s", compiled.SQL)
	}
	if len(compiled.Args) != 1 || compiled.Args[0] != "x' OR 1=1 --" {
		t.Fatalf("compiled args = %#v", compiled.Args)
	}
}

func TestSQLGeneratorUsesV1ParameterNames(t *testing.T) {
	t.Parallel()

	legacy, err := json.Marshal(map[string]interface{}{
		"rule_key": "00000000-0000-4000-8000-000000000001", "type": "length", "enabled": true, "severity": "error", "message": "", "params": map[string]interface{}{"min_length": 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dataquality.Parse([]byte(`{"schema_version":"addp.quality.rules/v1","rules":[` + string(legacy) + `]}`)); err == nil {
		t.Fatal("legacy min_length parameter should be rejected")
	}
}

func TestSQLGeneratorUniqueUsesSingleAggregateQuery(t *testing.T) {
	t.Parallel()

	rule := parseTestRule(t, `{"type":"unique","enabled":true,"severity":"error","message":"","params":{}}`)
	compiled, err := NewSQLGenerator().GenerateCheckSQL("public", "users", "email", rule)
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Args) != 0 || !strings.Contains(compiled.SQL, "duplicate_count") || !strings.Contains(compiled.SQL, `"public"."users"`) {
		t.Fatalf("unexpected unique SQL: %#v", compiled)
	}
}
