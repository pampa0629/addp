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

func TestPolygonRingDiagnosticBindsFrozenDefinitionAndRejectsForgedContext(t *testing.T) {
	for _, reason := range []string{"operator_field_conflict", "operator_geometry_invalid"} {
		operator := map[string]interface{}{"node_index": 2, "operator_id": "extract_polygon_rings", "parameter": "input_gdf"}
		if reason == "operator_field_conflict" {
			operator["field"] = "ring_id"
		}
		diagnostic := map[string]interface{}{"schema_version": "addp.execution-failure/v3", "stage": "execution", "reason": reason, "operator": operator}
		raw := map[string]interface{}{"runtime_status": map[string]interface{}{"status": "failed", "error_code": "INVALID_PARAMS", "diagnostic": diagnostic}}
		definition := map[string]interface{}{"tasks": []interface{}{map[string]interface{}{"operator": "load"}, map[string]interface{}{"operator": "extract_polygon_rings"}}}
		result := executionFailureDetails("workflow", "execution_failed", "private GEOS password", raw, definition)
		if result["code"] != "develop.workflow.invalid_params" || result["diagnostic"] == nil {
			t.Fatalf("lost ring binding: %+v", result)
		}
		for _, invalid := range []map[string]interface{}{
			{"node_index": 1, "operator_id": "extract_polygon_rings", "parameter": "input_gdf", "field": "ring_id"},
			{"node_index": 2, "operator_id": "extract_polygon_rings", "parameter": "input_gdf", "field": "private"},
			{"node_index": 2, "operator_id": "extract_polygon_rings", "parameter": "reference_gdf", "field": "ring_id"},
			{"node_index": 2, "operator_id": "distance_to", "parameter": "input_gdf", "field": "ring_id"},
		} {
			diagnostic["operator"] = invalid
			rejected := executionFailureDetails("workflow", "execution_failed", "private GEOS", raw, definition)
			if rejected["diagnostic"] != nil || rejected["code"] != "develop.workflow.execution_failed" {
				t.Fatal("trusted forged ring context")
			}
		}
	}
}

func TestLineTrimDiagnosticMatchesDefinitionPositionAndRejectsForgedParameter(t *testing.T) {
	for reason, parameter := range map[string]string{
		"operator_distance_limit_invalid": "max_distance", "operator_input_invalid": "input_gdf",
		"operator_reference_invalid": "reference_gdf", "operator_crs_invalid": "",
		"operator_source_topology_invalid": "input_gdf", "operator_trim_conflict": "",
	} {
		context := map[string]interface{}{"node_index": 1, "operator_id": "trim_line_endpoints"}
		if parameter != "" {
			context["parameter"] = parameter
		}
		diagnostic := map[string]interface{}{"schema_version": "addp.execution-failure/v3", "stage": "execution", "reason": reason, "operator": context}
		raw := map[string]interface{}{"runtime_status": map[string]interface{}{"status": "failed", "error_code": "INVALID_PARAMS", "diagnostic": diagnostic}}
		frozen := map[string]interface{}{"tasks": []interface{}{map[string]interface{}{"id": "PRIVATE", "operator": "trim_line_endpoints", "depends_on": []string{"load"}}, map[string]interface{}{"operator": "load"}}}
		got := executionFailureDetails("workflow", "execution_failed", "PRIVATE GEOS /source.gpkg", raw, frozen)
		if got["diagnostic"] == nil || got["code"] != "develop.workflow.invalid_params" {
			t.Fatalf("lost trim context: %+v", got)
		}
		context["node_index"] = 2
		if executionFailureDetails("workflow", "execution_failed", "PRIVATE", raw, frozen)["diagnostic"] != nil {
			t.Fatal("trusted scheduling position instead of definition position")
		}
		context["node_index"] = 1
		context["parameter"] = "tolerance"
		if executionFailureDetails("workflow", "execution_failed", "PRIVATE", raw, frozen)["diagnostic"] != nil {
			t.Fatal("trusted foreign parameter")
		}
	}
}

func TestIntersectionDiagnosticBindsOnlyExactFrozenOperatorAndInput(t *testing.T) {
	for reason, parameter := range map[string]string{
		"operator_input_invalid": "input_gdf", "operator_reference_invalid": "gdf_b",
		"operator_crs_invalid": "", "operator_overlay_fields_conflict": "",
	} {
		context := map[string]interface{}{"node_index": 1, "operator_id": "intersection"}
		if parameter != "" {
			context["parameter"] = parameter
		}
		diagnostic := map[string]interface{}{"schema_version": "addp.execution-failure/v3", "stage": "execution", "reason": reason, "operator": context}
		raw := map[string]interface{}{"runtime_status": map[string]interface{}{"status": "failed", "error_code": "INVALID_PARAMS", "diagnostic": diagnostic}}
		frozen := map[string]interface{}{"tasks": []interface{}{map[string]interface{}{"id": "PRIVATE_NODE", "operator": "intersection", "depends_on": []string{"load"}}, map[string]interface{}{"operator": "load"}}}
		got := executionFailureDetails("workflow", "execution_failed", "PRIVATE source geometry", raw, frozen)
		if got["diagnostic"] == nil || got["code"] != "develop.workflow.invalid_params" {
			t.Fatalf("lost intersection context %s: %+v", reason, got)
		}
		context["node_index"] = 2
		if executionFailureDetails("workflow", "execution_failed", "PRIVATE", raw, frozen)["diagnostic"] != nil {
			t.Fatal("trusted scheduling order")
		}
		context["node_index"] = 1
		context["parameter"] = "reference_gdf"
		if executionFailureDetails("workflow", "execution_failed", "PRIVATE", raw, frozen)["diagnostic"] != nil {
			t.Fatal("trusted foreign source parameter")
		}
	}
}

func TestLineNodeDiagnosticRequiresExactFrozenOperatorPosition(t *testing.T) {
	for _, field := range []string{"", "node_id", "degree", "source_id", "source_part_no", "end"} {
		reasons := []string{"operator_field_conflict"}
		if field == "" {
			reasons = []string{"operator_input_invalid", "operator_projected_crs_required", "operator_endpoint_network_invalid"}
		}
		for _, reason := range reasons {
			context := map[string]interface{}{"node_index": 1, "operator_id": "extract_line_nodes"}
			if reason != "operator_projected_crs_required" {
				context["parameter"] = "input_gdf"
			}
			if field != "" {
				context["field"] = field
			}
			diagnostic := map[string]interface{}{"schema_version": "addp.execution-failure/v3", "stage": "execution", "reason": reason, "operator": context}
			raw := map[string]interface{}{"runtime_status": map[string]interface{}{"status": "failed", "error_code": "INVALID_PARAMS", "diagnostic": diagnostic}}
			frozen := map[string]interface{}{"tasks": []interface{}{map[string]interface{}{"id": "PRIVATE_NODE", "operator": "extract_line_nodes", "depends_on": []string{"load"}}, map[string]interface{}{"operator": "load"}}}
			got := executionFailureDetails("workflow", "execution_failed", "PRIVATE GEOS /source/path", raw, frozen)
			if got["diagnostic"] == nil || got["code"] != "develop.workflow.invalid_params" {
				t.Fatalf("lost nodes %s/%s: %+v", reason, field, got)
			}
			context["node_index"] = 2
			if executionFailureDetails("workflow", "execution_failed", "PRIVATE", raw, frozen)["diagnostic"] != nil {
				t.Fatal("trusted scheduling index")
			}
			context["node_index"] = 1
			context["parameter"] = "id_field"
			if executionFailureDetails("workflow", "execution_failed", "PRIVATE", raw, frozen)["diagnostic"] != nil {
				t.Fatal("trusted foreign parameter")
			}
		}
	}
}

func TestDifferenceDiagnosticBindsOnlyExactFrozenOperatorAndInput(t *testing.T) {
	for reason, parameter := range map[string]string{
		"operator_input_invalid": "input_gdf", "operator_mask_invalid": "gdf_b",
		"operator_crs_invalid": "",
	} {
		context := map[string]interface{}{"node_index": 1, "operator_id": "difference"}
		if parameter != "" {
			context["parameter"] = parameter
		}
		diagnostic := map[string]interface{}{"schema_version": "addp.execution-failure/v3", "stage": "execution", "reason": reason, "operator": context}
		raw := map[string]interface{}{"runtime_status": map[string]interface{}{"status": "failed", "error_code": "INVALID_PARAMS", "diagnostic": diagnostic}}
		frozen := map[string]interface{}{"tasks": []interface{}{map[string]interface{}{"id": "PRIVATE_NODE", "operator": "difference", "depends_on": []string{"load"}}, map[string]interface{}{"operator": "load"}}}
		got := executionFailureDetails("workflow", "execution_failed", "PRIVATE source geometry", raw, frozen)
		if got["diagnostic"] == nil || got["code"] != "develop.workflow.invalid_params" {
			t.Fatalf("lost difference context %s: %+v", reason, got)
		}
		context["node_index"] = 2
		if executionFailureDetails("workflow", "execution_failed", "PRIVATE", raw, frozen)["diagnostic"] != nil {
			t.Fatal("trusted scheduling order")
		}
		context["node_index"] = 1
		context["parameter"] = "reference_gdf"
		if executionFailureDetails("workflow", "execution_failed", "PRIVATE", raw, frozen)["diagnostic"] != nil {
			t.Fatal("trusted foreign source parameter")
		}
	}
}

func TestClipDiagnosticBindsOnlyExactFrozenOperatorAndInput(t *testing.T) {
	for reason, parameter := range map[string]string{
		"operator_input_invalid": "input_gdf", "operator_mask_invalid": "mask_gdf",
		"operator_crs_invalid": "",
	} {
		context := map[string]interface{}{"node_index": 1, "operator_id": "clip"}
		if parameter != "" {
			context["parameter"] = parameter
		}
		diagnostic := map[string]interface{}{"schema_version": "addp.execution-failure/v3", "stage": "execution", "reason": reason, "operator": context}
		raw := map[string]interface{}{"runtime_status": map[string]interface{}{"status": "failed", "error_code": "INVALID_PARAMS", "diagnostic": diagnostic}}
		frozen := map[string]interface{}{"tasks": []interface{}{map[string]interface{}{"id": "PRIVATE_NODE", "operator": "clip", "depends_on": []string{"load"}}, map[string]interface{}{"operator": "load"}}}
		got := executionFailureDetails("workflow", "execution_failed", "PRIVATE source geometry", raw, frozen)
		if got["diagnostic"] == nil || got["code"] != "develop.workflow.invalid_params" {
			t.Fatalf("lost difference context %s: %+v", reason, got)
		}
		context["node_index"] = 2
		if executionFailureDetails("workflow", "execution_failed", "PRIVATE", raw, frozen)["diagnostic"] != nil {
			t.Fatal("trusted scheduling order")
		}
		context["node_index"] = 1
		context["parameter"] = "reference_gdf"
		if executionFailureDetails("workflow", "execution_failed", "PRIVATE", raw, frozen)["diagnostic"] != nil {
			t.Fatal("trusted foreign source parameter")
		}
	}
}

func TestUnionDiagnosticBindsOnlyExactFrozenOperatorAndInput(t *testing.T) {
	for reason, parameter := range map[string]string{"operator_input_invalid": "input_gdf", "operator_reference_invalid": "gdf_b", "operator_crs_invalid": "", "operator_overlay_fields_conflict": "", "operator_input_overlap": "input_gdf", "operator_reference_overlap": "gdf_b"} {
		context := map[string]interface{}{"node_index": 1, "operator_id": "union"}
		if parameter != "" {
			context["parameter"] = parameter
		}
		diagnostic := map[string]interface{}{"schema_version": "addp.execution-failure/v3", "stage": "execution", "reason": reason, "operator": context}
		raw := map[string]interface{}{"runtime_status": map[string]interface{}{"status": "failed", "error_code": "INVALID_PARAMS", "diagnostic": diagnostic}}
		frozen := map[string]interface{}{"tasks": []interface{}{map[string]interface{}{"id": "PRIVATE_NODE", "operator": "union", "depends_on": []string{"load"}}, map[string]interface{}{"operator": "load"}}}
		got := executionFailureDetails("workflow", "execution_failed", "PRIVATE source geometry", raw, frozen)
		if got["diagnostic"] == nil || got["code"] != "develop.workflow.invalid_params" {
			t.Fatalf("lost difference context %s: %+v", reason, got)
		}
		context["node_index"] = 2
		if executionFailureDetails("workflow", "execution_failed", "PRIVATE", raw, frozen)["diagnostic"] != nil {
			t.Fatal("trusted scheduling order")
		}
		context["node_index"] = 1
		context["parameter"] = "reference_gdf"
		if executionFailureDetails("workflow", "execution_failed", "PRIVATE", raw, frozen)["diagnostic"] != nil {
			t.Fatal("trusted foreign source parameter")
		}
	}
}

func TestDissolveDiagnosticBindsOnlyExactFrozenOperatorAndInput(t *testing.T) {
	for reason, parameter := range map[string]string{"operator_input_invalid": "input_gdf", "operator_crs_invalid": "", "operator_group_fields_invalid": "group_fields", "operator_dissolve_fields_conflict": ""} {
		context := map[string]interface{}{"node_index": 1, "operator_id": "dissolve"}
		if parameter != "" {
			context["parameter"] = parameter
		}
		diagnostic := map[string]interface{}{"schema_version": "addp.execution-failure/v3", "stage": "execution", "reason": reason, "operator": context}
		raw := map[string]interface{}{"runtime_status": map[string]interface{}{"status": "failed", "error_code": "INVALID_PARAMS", "diagnostic": diagnostic}}
		frozen := map[string]interface{}{"tasks": []interface{}{map[string]interface{}{"id": "PRIVATE_NODE", "operator": "dissolve", "depends_on": []string{"load"}}, map[string]interface{}{"operator": "load"}}}
		got := executionFailureDetails("workflow", "execution_failed", "PRIVATE source geometry", raw, frozen)
		if got["diagnostic"] == nil || got["code"] != "develop.workflow.invalid_params" {
			t.Fatalf("lost difference context %s: %+v", reason, got)
		}
		context["node_index"] = 2
		if executionFailureDetails("workflow", "execution_failed", "PRIVATE", raw, frozen)["diagnostic"] != nil {
			t.Fatal("trusted scheduling order")
		}
		context["node_index"] = 1
		context["parameter"] = "reference_gdf"
		if executionFailureDetails("workflow", "execution_failed", "PRIVATE", raw, frozen)["diagnostic"] != nil {
			t.Fatal("trusted foreign source parameter")
		}
	}
}

func TestRelationFilterDiagnosticBindsExactFrozenOperatorAndNoBusinessFields(t *testing.T) {
	for _, operator := range []string{"contains", "intersects"} {
		for reason, parameter := range map[string]string{"operator_input_invalid": "input_gdf", "operator_reference_invalid": "gdf_b", "operator_crs_invalid": ""} {
			context := map[string]interface{}{"node_index": 1, "operator_id": operator}
			if parameter != "" {
				context["parameter"] = parameter
			}
			diagnostic := map[string]interface{}{"schema_version": "addp.execution-failure/v3", "stage": "execution", "reason": reason, "operator": context}
			raw := map[string]interface{}{"runtime_status": map[string]interface{}{"status": "failed", "error_code": "INVALID_PARAMS", "diagnostic": diagnostic}}
			frozen := map[string]interface{}{"tasks": []interface{}{map[string]interface{}{"id": "PRIVATE_NODE", "operator": operator}, map[string]interface{}{"operator": "load"}}}
			got := executionFailureDetails("workflow", "execution_failed", "PRIVATE source data", raw, frozen)
			if got["diagnostic"] == nil || got["code"] != "develop.workflow.invalid_params" {
				t.Fatalf("lost relation admission %s/%s: %+v", operator, reason, got)
			}
			context["node_index"] = 2
			if executionFailureDetails("workflow", "execution_failed", "PRIVATE", raw, frozen)["diagnostic"] != nil {
				t.Fatal("trusted scheduling order")
			}
			context["node_index"] = 1
			context["field"] = "source_id"
			if executionFailureDetails("workflow", "execution_failed", "PRIVATE", raw, frozen)["diagnostic"] != nil {
				t.Fatal("trusted business fields")
			}
			delete(context, "field")
			context["parameter"] = "reference_gdf"
			if executionFailureDetails("workflow", "execution_failed", "PRIVATE", raw, frozen)["diagnostic"] != nil {
				t.Fatal("trusted unrelated parameter")
			}
		}
	}
}
