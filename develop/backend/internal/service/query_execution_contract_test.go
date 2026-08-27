package service

import (
	"reflect"
	"testing"

	"github.com/addp/common/taskprovider"
)

func TestBuildQueryExecutionContractUsesDefinitionsAndDefaults(t *testing.T) {
	content := map[string]interface{}{
		"query_type": "sql",
		"query":      "SELECT * FROM members WHERE status = :status AND nickname = :nickname",
		"query_parameters": []interface{}{
			map[string]interface{}{"name": "status", "type": "string", "default": "领队", "title": "成员身份"},
			map[string]interface{}{"name": "nickname", "type": "string", "default": "PiPi"},
		},
	}
	contract, err := BuildQueryExecutionContract(content)
	if err != nil {
		t.Fatal(err)
	}
	properties := contract.InputSchema["properties"].(map[string]interface{})
	if len(properties) != 2 || contract.InputDefaults["status"] != "领队" || contract.InputDefaults["nickname"] != "PiPi" {
		t.Fatalf("contract = %#v", contract)
	}
	if contract.InputUISchema["status"].(map[string]interface{})["order"] != 0 ||
		contract.InputUISchema["nickname"].(map[string]interface{})["order"] != 1 {
		t.Fatalf("input UI schema = %#v", contract.InputUISchema)
	}
}

func TestBuildQueryExecutionContractRequiresExactDefinitions(t *testing.T) {
	tests := []map[string]interface{}{
		{
			"query_type": "cypher", "query": "MATCH (m) WHERE m.name = $name RETURN m",
			"query_parameters": []interface{}{},
		},
		{
			"query_type": "mql", "query": `{"find":"Outdoors","filter":{"name":{"$param":"name"}}}`,
			"query_parameters": []interface{}{map[string]interface{}{"name": "unused", "type": "string", "default": "x"}},
		},
	}
	for _, content := range tests {
		if _, err := BuildQueryExecutionContract(content); err == nil {
			t.Fatalf("expected invalid query parameters: %#v", content)
		}
	}
}

func TestBuildQueryExecutionContractRequiresRelationLocatorsAtRuntime(t *testing.T) {
	content := map[string]interface{}{
		"query_type":      "sql",
		"query":           "SELECT member_id FROM addp_input.members",
		"relation_inputs": []interface{}{"members"},
	}
	contract, err := BuildQueryExecutionContract(content)
	if err != nil {
		t.Fatal(err)
	}
	if err := taskprovider.ValidateExecutionContract(map[string]interface{}{
		"input_schema":    contract.InputSchema,
		"input_defaults":  contract.InputDefaults,
		"input_ui_schema": contract.InputUISchema,
		"output_schema":   contract.OutputSchema,
	}); err != nil {
		t.Fatalf("execution contract is invalid: %v", err)
	}
	required := contract.InputSchema["required"].([]interface{})
	if !reflect.DeepEqual(required, []interface{}{"input_locators", "target_locator"}) {
		t.Fatalf("required = %#v", required)
	}
	if len(contract.InputDefaults) != 0 {
		t.Fatalf("runtime locators must not have defaults: %#v", contract.InputDefaults)
	}
	if _, _, err := resolveQueryExecutionParameters(content, map[string]interface{}{}); err == nil {
		t.Fatal("expected missing runtime locators to fail")
	}
	_, effective, err := resolveQueryExecutionParameters(content, map[string]interface{}{
		"input_locators": map[string]interface{}{
			"members": "addp://engine/12/path/public/members?type=table",
		},
		"target_locator": "addp://engine/12/path/public/member_result?type=table",
	})
	if err != nil {
		t.Fatal(err)
	}
	if effective != nil {
		t.Fatalf("runtime locators must not become SQL value parameters: %#v", effective)
	}
}

func TestResolveQueryExecutionParametersMergesAndNormalizesOverrides(t *testing.T) {
	content := map[string]interface{}{
		"query_type": "sql",
		"query":      "SELECT * FROM events WHERE attempts = :attempts AND enabled = :enabled",
		"query_parameters": []interface{}{
			map[string]interface{}{"name": "attempts", "type": "integer", "default": 2},
			map[string]interface{}{"name": "enabled", "type": "boolean", "default": true},
		},
	}
	_, effective, err := resolveQueryExecutionParameters(content, map[string]interface{}{"attempts": float64(5)})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"attempts": int64(5), "enabled": true}
	if !reflect.DeepEqual(effective, want) {
		t.Fatalf("effective = %#v, want %#v", effective, want)
	}
}

func TestResolveQueryExecutionParametersKeepsUnparameterizedQueryRuntimeValuesNil(t *testing.T) {
	contract, effective, err := resolveQueryExecutionParameters(map[string]interface{}{
		"query_type": "sql",
		"query":      "SELECT 1",
	}, map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	if contract == nil || effective != nil {
		t.Fatalf("contract = %#v, effective = %#v", contract, effective)
	}
}

func TestResolveQueryExecutionParametersRejectsUnknownOverride(t *testing.T) {
	_, _, err := resolveQueryExecutionParameters(map[string]interface{}{
		"query_type": "sql",
		"query":      "SELECT * FROM events WHERE enabled = :enabled",
		"query_parameters": []interface{}{
			map[string]interface{}{"name": "enabled", "type": "boolean", "default": true},
		},
	}, map[string]interface{}{"unknown": true})
	if err == nil {
		t.Fatal("expected unknown query parameter override to fail")
	}
}
