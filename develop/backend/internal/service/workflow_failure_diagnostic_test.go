package service

import "testing"

func TestWorkflowConflictRequiresFailedTypedRuntimeEvidence(t *testing.T) {
	for _, tc := range []struct{ taskType, suffix, status, code string }{
		{"workflow", "execution_failed", "failed", "EXECUTION_FAILED"},
		{"workflow", "execution_failed", "success", "TARGET_ALREADY_EXISTS"},
		{"workflow", "execution_failed", "running", "TARGET_ALREADY_EXISTS"},
		{"workflow", "execution_timeout", "failed", "TARGET_ALREADY_EXISTS"},
		{"query", "execution_failed", "failed", "TARGET_ALREADY_EXISTS"},
	} {
		details := executionFailureDetails(tc.taskType, tc.suffix, "target already exists: private-path", map[string]interface{}{
			"runtime_status": map[string]interface{}{"status": tc.status, "error_code": tc.code},
		}, nil)
		if details["diagnostic"] != nil || details["code"] != "develop."+tc.taskType+"."+tc.suffix {
			t.Fatalf("fabricated conflict for %+v: %+v", tc, details)
		}
	}
	if executionFailureDetails("workflow", "execution_failed", "target already exists", nil, nil)["diagnostic"] != nil {
		t.Fatal("missing runtime evidence fabricated a conflict")
	}
}

func TestWorkflowParameterDiagnosticRequiresMatchingFrozenDefinition(t *testing.T) {
	definition := map[string]interface{}{"tasks": []interface{}{map[string]interface{}{"id": "source", "operator": "load"}, map[string]interface{}{"id": "private", "operator": "batch_buffer"}}}
	raw := map[string]interface{}{"runtime_status": map[string]interface{}{"status": "failed", "error_code": "INVALID_PARAMS", "diagnostic": map[string]interface{}{
		"schema_version": "addp.execution-failure/v3", "stage": "execution", "reason": "operator_field_conflict", "operator": map[string]interface{}{
			"node_index": 2, "operator_id": "batch_buffer", "parameter": "input_gdf", "field": "distance"}}}}
	result := executionFailureDetails("workflow", "execution_failed", "password=secret", raw, definition)
	if result["code"] != "develop.workflow.invalid_params" || result["diagnostic"] == nil {
		t.Fatalf("lost validated diagnostic %+v", result)
	}
	for _, frozen := range []map[string]interface{}{nil, {"tasks": []interface{}{}}, {"tasks": []interface{}{map[string]interface{}{"operator": "batch_buffer"}, map[string]interface{}{"operator": "load"}}}} {
		details := executionFailureDetails("workflow", "execution_failed", "distance conflicts", raw, frozen)
		if details["diagnostic"] != nil || details["code"] != "develop.workflow.execution_failed" {
			t.Fatal("unbound Runtime input trusted")
		}
	}
}
