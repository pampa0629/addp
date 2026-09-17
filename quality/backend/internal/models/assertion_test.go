package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAssertionRejectsInvalidExpressions(t *testing.T) {
	cases := []string{
		`{"op":"number","value":1}`,
		`{"op":"number","value":"NaN"}`,
		`{"op":"number","value":"1e999999999"}`,
		`{"op":"eq","args":[{"op":"number","value":"01"},{"op":"number","value":"1"}]}`,
		`{"op":"sql","value":"SELECT 1"}`,
		`{"op":"field","field":"key"}`,
		`{"op":"eq","args":[]}`,
		`{"op":"eq","args":[{"op":"field","relation":"absent","field":"key"},{"op":"value","value":1}]}`,
		`{"op":"exists","relation":"detail","where":{"op":"exists","relation":"detail"}}`,
		`{"op":"exists","relation":"detail; DROP TABLE x"}`,
		`{"op":"exists","relation":"detail","where":{"op":"value","value":1}}`,
		`{"op":"not","args":[{"op":"value","value":1}]}`,
		`{"op":"eq","args":[{"op":"value","value":1},{"op":"value","value":"1"}]}`,
		`{"op":"value","value":[]}`,
		`{"op":"value","value":{}}`,
		`{"op":"value"}`,
		`{"op":"value","value":true,"field":"unused"}`,
		`{"op":"value","value":true,"sql":"injection"}`,
		`{"op":"count_distinct","relation":"detail"}`,
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			if _, _, err := ParseAssertionConstraint(json.RawMessage(`{"assertion":` + raw + `}`)); err == nil {
				t.Fatal("accepted invalid assertion")
			}
		})
	}
	deep := `{"op":"value","value":true}`
	for i := 0; i < 12; i++ {
		deep = `{"op":"not","args":[` + deep + `]}`
	}
	if _, _, err := ParseAssertionConstraint(json.RawMessage(`{"assertion":` + deep + `}`)); err == nil {
		t.Fatal("unbounded depth")
	}
	wide := `{"op":"and","args":[` + strings.TrimSuffix(strings.Repeat(`{"op":"value","value":true},`, 33), ",") + `]}`
	if _, _, err := ParseAssertionConstraint(json.RawMessage(`{"assertion":` + wide + `}`)); err == nil {
		t.Fatal("unbounded arguments")
	}
}

func TestAssertionInputsNestedScopesAndFrozenNumber(t *testing.T) {
	raw := json.RawMessage(`{"assertion":{"op":"exists","relation":"source","where":{"op":"exists","relation":"fact","where":{"op":"and","args":[{"op":"eq","args":[{"op":"field","relation":"source","field":"key"},{"op":"field","relation":"fact","field":"key"}]},{"op":"eq","args":[{"op":"field","field":"actual"},{"op":"number","value":"9007199254740993"}]}]}}}}`)
	_, inputs, err := ParseAssertionConstraint(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 3 || !inputs[""]["actual"] || !inputs["source"]["key"] || !inputs["fact"]["key"] {
		t.Fatalf("inputs = %#v", inputs)
	}
	resolved, err := ResolveRuleParams("relational_assertion", raw, CheckBindings{Table: "people", Fields: map[string]string{"actual": "metric_value"}, Relations: map[string]RelationBinding{"source": {Table: "source", Fields: map[string]string{"key": "person_id"}}, "fact": {Table: "fact", Fields: map[string]string{"key": "person_id"}}}})
	if err != nil || !strings.Contains(string(resolved), "9007199254740993") {
		t.Fatalf("frozen number changed: %s %v", resolved, err)
	}
	if _, err := ResolveRuleParams("relational_assertion", raw, CheckBindings{Table: "people", Column: "forbidden"}); err == nil {
		t.Fatal("accepted unrelated binding")
	}
}
