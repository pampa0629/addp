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
			map[string]interface{}{"name": "status", "type": "string", "default": "领队"},
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
		"query_type": "sql",
		"query":      "SELECT m.member_id FROM members AS m JOIN activities AS a ON a.member_id = m.member_id",
		"query_parameters": []interface{}{
			map[string]interface{}{"name": "members", "type": "relation", "default": map[string]interface{}{"locator": "addp://engine/12/path/public/members?type=table"}},
			map[string]interface{}{"name": "activities", "type": "relation"},
			map[string]interface{}{"name": "status", "type": "string", "default": "active"},
		},
	}
	content["query"] = content["query"].(string) + " WHERE m.status = :status"
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
	if !reflect.DeepEqual(required, []interface{}{"activities", "target_locator"}) {
		t.Fatalf("required = %#v", required)
	}
	if contract.InputDefaults["status"] != "active" || len(contract.InputDefaults) != 2 {
		t.Fatalf("query defaults = %#v", contract.InputDefaults)
	}
	membersUI, ok := contract.InputUISchema["members"].(map[string]interface{})
	if !ok || membersUI["control"] != "resource_tree_picker" || membersUI["order"] != 0 {
		t.Fatalf("members UI schema = %#v", contract.InputUISchema["members"])
	}
	if contract.InputUISchema["activities"].(map[string]interface{})["order"] != 1 ||
		contract.InputUISchema["status"].(map[string]interface{})["order"] != 2 ||
		contract.InputUISchema["target_locator"].(map[string]interface{})["order"] != 3 {
		t.Fatalf("target_locator UI schema = %#v", contract.InputUISchema["target_locator"])
	}
	if _, _, _, err := resolveQueryOrchestrationParameters(content, map[string]interface{}{}); err == nil {
		t.Fatal("expected missing runtime locators to fail")
	}
	_, effective, _, err := resolveQueryOrchestrationParameters(content, map[string]interface{}{
		"activities":     map[string]interface{}{"locator": "addp://engine/12/path/public/activities?type=table"},
		"target_locator": "addp://engine/12/path/public/member_result?type=table",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(effective, map[string]interface{}{"status": "active"}) {
		t.Fatalf("runtime locators must not become SQL value parameters: %#v", effective)
	}
	previewContract, previewValues, previewInputs, err := resolveQueryPreviewParameters(content, map[string]interface{}{
		"activities": map[string]interface{}{"locator": "addp://engine/12/path/public/activities?type=table"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := previewContract.InputSchema["properties"].(map[string]interface{})["target_locator"]; exists {
		t.Fatalf("preview contract must not contain result target: %#v", previewContract.InputSchema)
	}
	if previewValues["status"] != "active" || len(previewInputs) != 3 {
		t.Fatalf("preview effective inputs = %#v", previewInputs)
	}
}

func TestBuildQueryExecutionContractTreatsMissingDefaultsUniformly(t *testing.T) {
	content := map[string]interface{}{
		"query_type": "sql",
		"query":      "SELECT :empty_text, :zero_count, :disabled, :required_text",
		"query_parameters": []interface{}{
			map[string]interface{}{"name": "empty_text", "type": "string", "default": ""},
			map[string]interface{}{"name": "zero_count", "type": "integer", "default": 0},
			map[string]interface{}{"name": "disabled", "type": "boolean", "default": false},
			map[string]interface{}{"name": "required_text", "type": "string"},
		},
	}
	contract, err := BuildQueryExecutionContract(content)
	if err != nil {
		t.Fatal(err)
	}
	if required := contract.InputSchema["required"]; !reflect.DeepEqual(required, []interface{}{"required_text"}) {
		t.Fatalf("required = %#v", required)
	}
	if value, exists := contract.InputDefaults["empty_text"]; !exists || value != "" {
		t.Fatalf("empty string default = %#v, exists = %v", value, exists)
	}
	if value, exists := contract.InputDefaults["zero_count"]; !exists || value != int64(0) {
		t.Fatalf("zero default = %#v, exists = %v", value, exists)
	}
	if value, exists := contract.InputDefaults["disabled"]; !exists || value != false {
		t.Fatalf("false default = %#v, exists = %v", value, exists)
	}
	if _, _, _, err := resolveQueryPreviewParameters(content, nil); err == nil {
		t.Fatal("expected missing value parameter without default to fail")
	}
	_, runtimeValues, _, err := resolveQueryPreviewParameters(content, map[string]interface{}{"required_text": "ready"})
	if err != nil || runtimeValues["required_text"] != "ready" {
		t.Fatalf("runtime values = %#v, err = %v", runtimeValues, err)
	}
}

func TestBuildQueryExecutionContractRejectsRemovedTitleField(t *testing.T) {
	_, err := BuildQueryExecutionContract(map[string]interface{}{
		"query_type": "sql",
		"query":      "SELECT :status",
		"query_parameters": []interface{}{
			map[string]interface{}{"name": "status", "type": "string", "title": "状态"},
		},
	})
	if err == nil {
		t.Fatal("expected removed title field to be rejected")
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
	_, effective, _, err := resolveQueryPreviewParameters(content, map[string]interface{}{"attempts": float64(5)})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"attempts": int64(5), "enabled": true}
	if !reflect.DeepEqual(effective, want) {
		t.Fatalf("effective = %#v, want %#v", effective, want)
	}
}

func TestResolveQueryExecutionParametersKeepsUnparameterizedQueryRuntimeValuesNil(t *testing.T) {
	contract, effective, _, err := resolveQueryPreviewParameters(map[string]interface{}{
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
	_, _, _, err := resolveQueryPreviewParameters(map[string]interface{}{
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
