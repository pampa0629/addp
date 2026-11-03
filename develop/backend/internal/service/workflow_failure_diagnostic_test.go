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

func TestVoronoiDiagnosticBindsOnlyExactFrozenOperatorAndInput(t *testing.T) {
	for reason, parameter := range map[string]string{
		"operator_input_invalid": "input_gdf", "operator_mask_invalid": "mask_gdf",
		"operator_crs_invalid": "",
	} {
		context := map[string]interface{}{"node_index": 1, "operator_id": "voronoi"}
		if parameter != "" {
			context["parameter"] = parameter
		}
		diagnostic := map[string]interface{}{"schema_version": "addp.execution-failure/v3", "stage": "execution", "reason": reason, "operator": context}
		raw := map[string]interface{}{"runtime_status": map[string]interface{}{"status": "failed", "error_code": "INVALID_PARAMS", "diagnostic": diagnostic}}
		frozen := map[string]interface{}{"tasks": []interface{}{map[string]interface{}{"id": "PRIVATE_NODE", "operator": "voronoi", "depends_on": []string{"load"}}, map[string]interface{}{"operator": "load"}}}
		got := executionFailureDetails("workflow", "execution_failed", "PRIVATE source geometry", raw, frozen)
		if got["diagnostic"] == nil || got["code"] != "develop.workflow.invalid_params" {
			t.Fatalf("lost voronoi context %s: %+v", reason, got)
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

func TestBufferDiagnosticsBindClosedScalarArrayDomainsAndFrozenDefinitionPosition(t *testing.T) {
	for _, tc := range []struct{ operator, reason, parameter, field string }{
		{"buffer", "operator_input_invalid", "input_gdf", ""},
		{"buffer", "operator_crs_invalid", "", ""},
		{"buffer", "operator_number_invalid", "distance", ""},
		{"buffer", "operator_positive_integer_required", "resolution", ""},
		{"batch_buffer", "operator_input_invalid", "input_gdf", ""},
		{"batch_buffer", "operator_crs_invalid", "", ""},
		{"batch_buffer", "operator_field_conflict", "input_gdf", "distance"},
		{"batch_buffer", "operator_number_array_invalid", "distances", ""},
		{"batch_buffer", "operator_positive_integer_required", "resolution", ""},
	} {
		t.Run(tc.operator+"/"+tc.reason, func(t *testing.T) {
			context := map[string]interface{}{"node_index": 1, "operator_id": tc.operator}
			if tc.parameter != "" {
				context["parameter"] = tc.parameter
			}
			if tc.field != "" {
				context["field"] = tc.field
			}
			diagnostic := map[string]interface{}{"schema_version": "addp.execution-failure/v3", "stage": "execution", "reason": tc.reason, "operator": context}
			raw := map[string]interface{}{"runtime_status": map[string]interface{}{"status": "failed", "error_code": "INVALID_PARAMS", "diagnostic": diagnostic}}
			frozen := map[string]interface{}{"tasks": []interface{}{
				map[string]interface{}{"id": "PRIVATE_BUFFER", "operator": tc.operator, "depends_on": []string{"load"}},
				map[string]interface{}{"id": "load", "operator": "load"},
			}}
			got := executionFailureDetails("workflow", "execution_failed", "PRIVATE distance=-3", raw, frozen)
			if got["diagnostic"] == nil || got["code"] != "develop.workflow.invalid_params" {
				t.Fatalf("lost buffer admission: %+v", got)
			}
			wrongParameter := "distances"
			if tc.parameter == wrongParameter {
				wrongParameter = "distance"
			}
			invalid := []map[string]interface{}{
				{"node_index": 2}, {"node_index": 0}, {"node_index": true}, {"node_index": 1.5},
				{"operator_id": "unknown_buffer"}, {"parameter": wrongParameter}, {"parameter": ""},
				{"field": "PRIVATE"}, {"field": ""}, {"node_id": "PRIVATE_BUFFER"},
			}
			if tc.field == "" {
				invalid = append(invalid, map[string]interface{}{"field": "distance"})
			}
			for _, patch := range invalid {
				forged := map[string]interface{}{}
				for key, value := range context {
					forged[key] = value
				}
				for key, value := range patch {
					forged[key] = value
				}
				diagnostic["operator"] = forged
				got := executionFailureDetails("workflow", "execution_failed", "PRIVATE", raw, frozen)
				if got["diagnostic"] != nil || got["code"] != "develop.workflow.execution_failed" {
					t.Fatalf("trusted forged buffer context %+v: %+v", patch, got)
				}
			}
		})
	}
}

func TestBufferDiagnosticsRejectForeignDistanceReasonsEvenWithMatchingFrozenOperator(t *testing.T) {
	for _, tc := range []struct{ operator, reason, parameter string }{
		{"buffer", "operator_positive_number_required", "distance"},
		{"buffer", "operator_number_array_invalid", "distance"},
		{"batch_buffer", "operator_number_invalid", "distances"},
		{"batch_buffer", "operator_positive_number_required", "distances"},
		{"unknown_buffer", "operator_input_invalid", "input_gdf"},
	} {
		context := map[string]interface{}{"node_index": 1, "operator_id": tc.operator, "parameter": tc.parameter}
		diagnostic := map[string]interface{}{"schema_version": "addp.execution-failure/v3", "stage": "execution", "reason": tc.reason, "operator": context}
		raw := map[string]interface{}{"runtime_status": map[string]interface{}{"status": "failed", "error_code": "INVALID_PARAMS", "diagnostic": diagnostic}}
		frozen := map[string]interface{}{"tasks": []interface{}{map[string]interface{}{"operator": tc.operator}}}
		got := executionFailureDetails("workflow", "execution_failed", "PRIVATE distance=-3", raw, frozen)
		if got["diagnostic"] != nil || got["code"] != "develop.workflow.execution_failed" {
			t.Fatalf("trusted foreign distance reason %+v: %+v", tc, got)
		}
	}
}

func TestSimplifyDiagnosticsRequireClosedTupleAndFrozenDefinition(t *testing.T) {
	for reason, parameter := range map[string]string{
		"operator_input_invalid": "input_gdf", "operator_crs_invalid": "",
		"operator_nonnegative_number_required": "tolerance", "operator_boolean_required": "preserve_topology",
	} {
		t.Run(reason, func(t *testing.T) {
			context := map[string]interface{}{"node_index": 1, "operator_id": "simplify"}
			if parameter != "" {
				context["parameter"] = parameter
			}
			diagnostic := map[string]interface{}{"schema_version": "addp.execution-failure/v3", "stage": "execution", "reason": reason, "operator": context}
			raw := map[string]interface{}{"runtime_status": map[string]interface{}{"status": "failed", "error_code": "INVALID_PARAMS", "diagnostic": diagnostic}}
			frozen := map[string]interface{}{"tasks": []interface{}{
				map[string]interface{}{"id": "PRIVATE", "operator": "simplify", "depends_on": []string{"load"}},
				map[string]interface{}{"id": "load", "operator": "load"},
			}}
			if got := executionFailureDetails("workflow", "execution_failed", "PRIVATE", raw, frozen); got["diagnostic"] == nil || got["code"] != "develop.workflow.invalid_params" {
				t.Fatalf("lost simplify input fact: %+v", got)
			}
			for _, patch := range []map[string]interface{}{
				{"node_index": 2}, {"node_index": 0}, {"node_index": true}, {"node_index": 1.5},
				{"operator_id": "unknown_simplify"}, {"parameter": "distance"}, {"parameter": ""},
				{"field": "tolerance"}, {"field": ""}, {"node_id": "PRIVATE"},
			} {
				forged := map[string]interface{}{}
				for key, value := range context {
					forged[key] = value
				}
				for key, value := range patch {
					forged[key] = value
				}
				diagnostic["operator"] = forged
				if got := executionFailureDetails("workflow", "execution_failed", "PRIVATE", raw, frozen); got["diagnostic"] != nil || got["code"] != "develop.workflow.execution_failed" {
					t.Fatalf("trusted forged simplify context %+v: %+v", patch, got)
				}
			}
			diagnostic["operator"] = context
			for _, foreignReason := range []string{"operator_positive_number_required", "operator_number_invalid", "operator_number_array_invalid"} {
				diagnostic["reason"] = foreignReason
				if got := executionFailureDetails("workflow", "execution_failed", "PRIVATE", raw, frozen); got["diagnostic"] != nil {
					t.Fatalf("trusted foreign simplify reason %s: %+v", foreignReason, got)
				}
			}
		})
	}
}

func TestEnclosingConstructionDiagnosticsRequireClosedTupleAndFrozenDefinition(t *testing.T) {
	for _, operator := range []string{"envelope", "representative_point", "centroid", "convex_hull", "minimum_rotated_rectangle"} {
		for reason, parameter := range map[string]string{"operator_input_invalid": "input_gdf", "operator_crs_invalid": ""} {
			t.Run(operator+"/"+reason, func(t *testing.T) {
				context := map[string]interface{}{"node_index": 1, "operator_id": operator}
				if parameter != "" {
					context["parameter"] = parameter
				}
				diagnostic := map[string]interface{}{"schema_version": "addp.execution-failure/v3", "stage": "execution", "reason": reason, "operator": context}
				raw := map[string]interface{}{"runtime_status": map[string]interface{}{"status": "failed", "error_code": "INVALID_PARAMS", "diagnostic": diagnostic}}
				frozen := map[string]interface{}{"tasks": []interface{}{
					map[string]interface{}{"id": "PRIVATE", "operator": operator, "depends_on": []string{"load"}},
					map[string]interface{}{"id": "load", "operator": "load"},
				}}
				if got := executionFailureDetails("workflow", "execution_failed", "PRIVATE", raw, frozen); got["diagnostic"] == nil || got["code"] != "develop.workflow.invalid_params" {
					t.Fatalf("lost enclosing source admission: %+v", got)
				}
				for _, patch := range []map[string]interface{}{
					{"node_index": 2}, {"node_index": 0}, {"node_index": true}, {"node_index": 1.5},
					{"operator_id": "foreign"}, {"parameter": "distance"}, {"parameter": "reference_gdf"}, {"parameter": ""},
					{"field": "geometry"}, {"field": ""}, {"node_id": "PRIVATE"},
				} {
					forged := map[string]interface{}{}
					for key, value := range context {
						forged[key] = value
					}
					for key, value := range patch {
						forged[key] = value
					}
					diagnostic["operator"] = forged
					if got := executionFailureDetails("workflow", "execution_failed", "PRIVATE", raw, frozen); got["diagnostic"] != nil || got["code"] != "develop.workflow.execution_failed" {
						t.Fatalf("trusted forged enclosing context %+v: %+v", patch, got)
					}
				}
				diagnostic["operator"] = context
				for _, foreignReason := range []string{"operator_reference_invalid", "operator_number_invalid", "operator_nonnegative_number_required", "operator_boolean_required"} {
					diagnostic["reason"] = foreignReason
					if got := executionFailureDetails("workflow", "execution_failed", "PRIVATE", raw, frozen); got["diagnostic"] != nil {
						t.Fatalf("trusted foreign enclosing reason %s: %+v", foreignReason, got)
					}
				}
			})
		}
	}
}

func TestMeasurementDiagnosticsRequireClosedFieldsAndFrozenDefinition(t *testing.T) {
	rows := []struct{ operator, reason, parameter, field string }{
		{"get_area", "operator_input_invalid", "input_gdf", ""},
		{"get_area", "operator_crs_invalid", "", ""},
		{"get_area", "operator_geometry_field_conflict", "input_gdf", "area"},
		{"get_length", "operator_input_invalid", "input_gdf", ""},
		{"get_length", "operator_crs_invalid", "", ""},
		{"get_length", "operator_geometry_field_conflict", "input_gdf", "length"},
		{"split_by_area", "operator_input_invalid", "input_gdf", ""},
		{"split_by_area", "operator_crs_invalid", "", ""},
		{"split_by_area", "operator_geometry_field_conflict", "input_gdf", "area"},
		{"split_by_area", "operator_nonnegative_number_required", "threshold", ""},
	}
	for _, row := range rows {
		context := map[string]interface{}{"node_index": 1, "operator_id": row.operator}
		if row.parameter != "" {
			context["parameter"] = row.parameter
		}
		if row.field != "" {
			context["field"] = row.field
		}
		diagnostic := map[string]interface{}{"schema_version": "addp.execution-failure/v3", "stage": "execution", "reason": row.reason, "operator": context}
		raw := map[string]interface{}{"runtime_status": map[string]interface{}{"status": "failed", "error_code": "INVALID_PARAMS", "diagnostic": diagnostic}}
		frozen := map[string]interface{}{"tasks": []interface{}{map[string]interface{}{"id": "PRIVATE", "operator": row.operator, "depends_on": []string{"load"}}, map[string]interface{}{"id": "load", "operator": "load"}}}
		if got := executionFailureDetails("workflow", "execution_failed", "PRIVATE", raw, frozen); got["diagnostic"] == nil || got["code"] != "develop.workflow.invalid_params" {
			t.Fatalf("lost measurement: %+v", got)
		}
		for _, patch := range []map[string]interface{}{{"field": "PRIVATE"}, {"parameter": "reference_gdf"}, {"node_index": 2}, {"node_index": true}, {"node_id": "PRIVATE"}, {"operator_id": "unknown_measurement"}} {
			forged := map[string]interface{}{}
			for k, v := range context {
				forged[k] = v
			}
			for k, v := range patch {
				forged[k] = v
			}
			diagnostic["operator"] = forged
			if got := executionFailureDetails("workflow", "execution_failed", "PRIVATE", raw, frozen); got["diagnostic"] != nil {
				t.Fatalf("trusted forged measurement %+v", got)
			}
		}
	}
}
