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
	pattern := regexp.MustCompile(`\('([^']+)', '([^']+)'\): \('([^']*)', '([^']*)'\)`)
	tuples := pattern.FindAllStringSubmatch(string(source), -1)
	if len(tuples) != 11 {
		t.Fatalf("rules=%d", len(tuples))
	}
	valid := map[string]bool{}
	reasons := map[string]bool{}
	for _, r := range tuples {
		valid[strings.Join(r[1:], "|")] = true
		reasons[r[2]] = true
	}
	for _, op := range []string{"batch_buffer", "distance_to", "check_line_proximity", "foreign"} {
		for reason := range reasons {
			for _, parameter := range []string{"", "input_gdf", "reference_gdf", "distances", "resolution", "tolerance", "private"} {
				for _, field := range []string{"", "distance", "private"} {
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
