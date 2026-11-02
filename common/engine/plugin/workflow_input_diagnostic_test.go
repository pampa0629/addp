package plugin

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestWorkflowInputDiagnosticMatchesCanonicalLanguageContract(t *testing.T) {
	source, err := os.ReadFile("../../../common-python/addp_common/workflow_runtime/errors.py")
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`\('([^']+)', '([^']+)', '([^']*)'\): '([^']*)'`)
	tuples := pattern.FindAllStringSubmatch(string(source), -1)
	if len(tuples) != 47 {
		t.Fatalf("rules=%d", len(tuples))
	}
	valid := map[string]bool{}
	reasons := map[string]bool{}
	for _, r := range tuples {
		valid[strings.Join([]string{r[1], r[2], r[4], r[3]}, "|")] = true
		reasons[r[2]] = true
	}
	for _, op := range []string{"union", "clip", "difference", "intersection", "batch_buffer", "distance_to", "check_line_proximity", "extract_polygon_rings", "trim_line_endpoints", "extract_line_nodes", "foreign"} {
		for reason := range reasons {
			for _, parameter := range []string{"", "input_gdf", "gdf_b", "mask_gdf", "reference_gdf", "distances", "resolution", "tolerance", "max_distance", "private"} {
				for _, field := range []string{"", "distance", "source_id", "part_no", "ring_no", "ring_id", "is_hole", "node_id", "degree", "source_part_no", "end", "private"} {
					context := &WorkflowOperatorFailure{NodeIndex: 2, OperatorID: op, Parameter: parameter, Field: field}
					want := valid[strings.Join([]string{op, reason, parameter, field}, "|")]
					if context.Valid(reason) != want {
						t.Fatalf("contract drift: %+v %s", context, reason)
					}
				}
			}
		}
	}
}

func TestHTTPWorkflowInputDiagnosticRejectsUnknownAndUnprovenFacts(t *testing.T) {
	source := `{"status":"failed","error_code":"INVALID_PARAMS","diagnostic":{"schema_version":"addp.execution-failure/v3","stage":"execution","reason":"operator_field_conflict","operator":{"node_index":2,"operator_id":"batch_buffer","parameter":"input_gdf","field":"distance"}}}`
	var raw map[string]interface{}
	json.Unmarshal([]byte(source), &raw)
	got := workflowExecutionStatusFromRaw(raw)
	if got.Diagnostic == nil || got.Diagnostic.Operator.Field != "distance" {
		t.Fatalf("lost diagnostic %+v", got)
	}
	for _, change := range []func(map[string]interface{}){
		func(v map[string]interface{}) { v["status"] = "success" }, func(v map[string]interface{}) { v["error_code"] = "EXECUTION_FAILED" },
		func(v map[string]interface{}) { v["diagnostic"].(map[string]interface{})["message"] = "private" },
		func(v map[string]interface{}) { v["diagnostic"].(map[string]interface{})["stage"] = "validation" },
		func(v map[string]interface{}) {
			v["diagnostic"].(map[string]interface{})["operator"].(map[string]interface{})["node_index"] = 1.5
		},
		func(v map[string]interface{}) {
			v["diagnostic"].(map[string]interface{})["operator"].(map[string]interface{})["node_id"] = "private"
		},
		func(v map[string]interface{}) {
			v["diagnostic"].(map[string]interface{})["operator"].(map[string]interface{})["field"] = "password"
		},
	} {
		json.Unmarshal([]byte(source), &raw)
		change(raw)
		if workflowExecutionStatusFromRaw(raw).Diagnostic != nil {
			t.Fatal("accepted unproven diagnostic")
		}
	}
}

func TestDifferenceHTTPDiagnosticKeepsOnlyThreeClosedAdmissions(t *testing.T) {
	for reason, parameter := range map[string]string{"operator_input_invalid": "input_gdf", "operator_mask_invalid": "gdf_b", "operator_crs_invalid": ""} {
		context := map[string]interface{}{"node_index": 2, "operator_id": "difference"}
		if parameter != "" {
			context["parameter"] = parameter
		}
		raw := map[string]interface{}{"status": "failed", "error_code": "INVALID_PARAMS", "diagnostic": map[string]interface{}{"schema_version": "addp.execution-failure/v3", "stage": "execution", "reason": reason, "operator": context}}
		if got := workflowExecutionStatusFromRaw(raw); got.Diagnostic == nil {
			t.Fatal("lost difference diagnostic")
		}
		context["field"] = "PRIVATE"
		if got := workflowExecutionStatusFromRaw(raw); got.Diagnostic != nil {
			t.Fatal("trusted arbitrary difference field")
		}
	}
}

func TestClipHTTPDiagnosticKeepsOnlyThreeClosedAdmissions(t *testing.T) {
	for reason, parameter := range map[string]string{"operator_input_invalid": "input_gdf", "operator_mask_invalid": "mask_gdf", "operator_crs_invalid": ""} {
		context := map[string]interface{}{"node_index": 2, "operator_id": "clip"}
		if parameter != "" {
			context["parameter"] = parameter
		}
		raw := map[string]interface{}{"status": "failed", "error_code": "INVALID_PARAMS", "diagnostic": map[string]interface{}{"schema_version": "addp.execution-failure/v3", "stage": "execution", "reason": reason, "operator": context}}
		if got := workflowExecutionStatusFromRaw(raw); got.Diagnostic == nil {
			t.Fatal("lost clip diagnostic")
		}
		context["field"] = "PRIVATE"
		if got := workflowExecutionStatusFromRaw(raw); got.Diagnostic != nil {
			t.Fatal("trusted arbitrary clip field")
		}
	}
}

func TestUnionHTTPDiagnosticKeepsOnlySixClosedAdmissions(t *testing.T) {
	for reason, parameter := range map[string]string{"operator_input_invalid": "input_gdf", "operator_reference_invalid": "gdf_b", "operator_crs_invalid": "", "operator_overlay_fields_conflict": "", "operator_input_overlap": "input_gdf", "operator_reference_overlap": "gdf_b"} {
		context := map[string]interface{}{"node_index": 2, "operator_id": "union"}
		if parameter != "" {
			context["parameter"] = parameter
		}
		raw := map[string]interface{}{"status": "failed", "error_code": "INVALID_PARAMS", "diagnostic": map[string]interface{}{"schema_version": "addp.execution-failure/v3", "stage": "execution", "reason": reason, "operator": context}}
		if got := workflowExecutionStatusFromRaw(raw); got.Diagnostic == nil {
			t.Fatal("lost union diagnostic")
		}
		context["field"] = "PRIVATE"
		if got := workflowExecutionStatusFromRaw(raw); got.Diagnostic != nil {
			t.Fatal("trusted arbitrary union field")
		}
	}
}
