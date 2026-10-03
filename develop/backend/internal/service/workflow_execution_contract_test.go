package service

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	commonModels "github.com/addp/common/models"
	"github.com/addp/common/taskprovider"
	"github.com/addp/develop/backend/internal/models"
)

func TestWorkflowExecutionContractExposesOnlySavedUnconnectedPublicParameters(t *testing.T) {
	workflow := workflowExecutionContractFixture()
	contract, err := buildWorkflowExecutionContract(workflow, workflowExecutionOperatorsFixture())
	if err != nil {
		t.Fatalf("buildWorkflowExecutionContract() error = %v", err)
	}

	properties := contract.InputSchema["properties"].(map[string]interface{})
	if !reflect.DeepEqual(sortedMapKeys(properties), []string{"buffer_2", "load_1", "save_3"}) {
		t.Fatalf("input task properties = %#v", properties)
	}
	bufferProperties := properties["buffer_2"].(map[string]interface{})["properties"].(map[string]interface{})
	if _, exists := bufferProperties["input"]; exists {
		t.Fatalf("internally connected input must not be executable: %#v", bufferProperties)
	}
	if _, exists := bufferProperties["distance"]; !exists {
		t.Fatalf("saved public distance must be executable: %#v", bufferProperties)
	}
	if got := contract.InputDefaults["buffer_2"].(map[string]interface{})["distance"]; got != float64(100) {
		t.Fatalf("distance default = %#v, want 100", got)
	}
	if _, exists := contract.OutputSchema["properties"].(map[string]interface{})["save_3"]; !exists {
		t.Fatalf("save task stable output missing: %#v", contract.OutputSchema)
	}
	assertWorkflowExecutionUIOrder(t, contract.InputUISchema, []string{"load_1", "buffer_2", "save_3"})
	assertWorkflowExecutionFieldOrder(t, contract.InputUISchema, "buffer_2", []string{"distance", "quadrant_segments"})
	assertWorkflowExecutionFieldOrder(t, contract.InputUISchema, "save_3", []string{"target_resource"})
}

func TestWorkflowExecutionContractOrdersInputsByStableDAGOrder(t *testing.T) {
	workflow := map[string]interface{}{
		"tasks": []interface{}{
			map[string]interface{}{
				"id": "buffer_b", "operator": "buffer", "depends_on": []interface{}{"load_b"},
				"params": map[string]interface{}{"input": map[string]interface{}{"$ref": "load_b.default"}, "distance": float64(200), "quadrant_segments": float64(8)},
			},
			map[string]interface{}{
				"id": "buffer_a", "operator": "buffer", "depends_on": []interface{}{"load_a"},
				"params": map[string]interface{}{"input": map[string]interface{}{"$ref": "load_a.default"}, "distance": float64(100), "quadrant_segments": float64(4)},
			},
			map[string]interface{}{
				"id": "load_b", "operator": "load", "depends_on": []interface{}{},
				"params": map[string]interface{}{"locator": "addp://engine/7/path/public/b?type=table", "geom_column": "geom"},
			},
			map[string]interface{}{
				"id": "load_a", "operator": "load", "depends_on": []interface{}{},
				"params": map[string]interface{}{"locator": "addp://engine/7/path/public/a?type=table", "geom_column": "geom"},
			},
		},
	}

	contract, err := buildWorkflowExecutionContract(workflow, workflowExecutionOperatorsFixture())
	if err != nil {
		t.Fatalf("buildWorkflowExecutionContract() error = %v", err)
	}

	assertWorkflowExecutionUIOrder(t, contract.InputUISchema, []string{"load_b", "load_a", "buffer_b", "buffer_a"})
}

func TestApplyWorkflowExecutionOverridesChangesOnlyExecutionSnapshot(t *testing.T) {
	original := workflowExecutionContractFixture()
	resolved, err := cloneWorkflowDefinition(original)
	if err != nil {
		t.Fatal(err)
	}
	overrides := map[string]interface{}{
		"load_1": map[string]interface{}{
			"source_resource": map[string]interface{}{
				"locator":         "addp://engine/8/path/public/new_roads?type=table",
				"geometry_column": "shape",
			},
		},
		"buffer_2": map[string]interface{}{"distance": float64(250)},
		"save_3": map[string]interface{}{
			"target_resource": map[string]interface{}{
				"parent_locator": "addp://engine/8/path/public?type=schema",
				"name":           "roads_buffered_once",
				"mode":           "append",
			},
		},
	}
	contract, err := buildWorkflowExecutionContract(original, workflowExecutionOperatorsFixture())
	if err != nil {
		t.Fatal(err)
	}
	if err := taskprovider.ValidateExecutionParameters(contract.InputSchema, overrides, taskprovider.ParameterValidationOptions{}); err != nil {
		t.Fatalf("ValidateExecutionParameters() error = %v", err)
	}
	if err := applyWorkflowExecutionOverrides(resolved, overrides, workflowExecutionOperatorsFixture()); err != nil {
		t.Fatalf("applyWorkflowExecutionOverrides() error = %v", err)
	}

	originalTasks, _ := workflowTasksFromInterface(original["tasks"])
	resolvedTasks, _ := workflowTasksFromInterface(resolved["tasks"])
	if got := originalTasks[1]["params"].(map[string]interface{})["distance"]; got != float64(100) {
		t.Fatalf("persisted workflow was mutated: distance = %#v", got)
	}
	loadParams := resolvedTasks[0]["params"].(map[string]interface{})
	if loadParams["locator"] != "addp://engine/8/path/public/new_roads?type=table" || loadParams["geom_column"] != "shape" {
		t.Fatalf("resolved load params = %#v", loadParams)
	}
	if got := resolvedTasks[1]["params"].(map[string]interface{})["distance"]; got != float64(250) {
		t.Fatalf("resolved distance = %#v, want 250", got)
	}
	saveParams := resolvedTasks[2]["params"].(map[string]interface{})
	if saveParams["target_type"] != "table" || saveParams["target_name"] != "roads_buffered_once" || saveParams["mode"] != "append" {
		t.Fatalf("resolved save params = %#v", saveParams)
	}
}

func TestWorkflowExecutionContractRejectsUnknownOverride(t *testing.T) {
	contract, err := buildWorkflowExecutionContract(workflowExecutionContractFixture(), workflowExecutionOperatorsFixture())
	if err != nil {
		t.Fatal(err)
	}
	err = taskprovider.ValidateExecutionParameters(contract.InputSchema, map[string]interface{}{
		"buffer_2": map[string]interface{}{"input": "forbidden"},
	}, taskprovider.ParameterValidationOptions{})
	if err == nil || !strings.Contains(err.Error(), "parameters.buffer_2.input") {
		t.Fatalf("error = %v, want internal input rejection", err)
	}
}

func workflowExecutionContractFixture() map[string]interface{} {
	return map[string]interface{}{
		"tasks": []interface{}{
			map[string]interface{}{
				"id": "load_1", "operator": "load", "depends_on": []interface{}{},
				"params": map[string]interface{}{
					"locator": "addp://engine/7/path/public/roads?type=table", "geom_column": "geom",
				},
			},
			map[string]interface{}{
				"id": "buffer_2", "operator": "buffer", "depends_on": []interface{}{"load_1"},
				"params": map[string]interface{}{
					"input": map[string]interface{}{"$ref": "load_1.default"}, "distance": float64(100), "quadrant_segments": float64(8),
				},
			},
			map[string]interface{}{
				"id": "save_3", "operator": "save", "depends_on": []interface{}{"buffer_2"},
				"params": map[string]interface{}{
					"input":                 map[string]interface{}{"$ref": "buffer_2.default"},
					"target_parent_locator": "addp://engine/7/path/public?type=schema",
					"target_name":           "roads_buffered", "target_type": "table", "mode": "replace",
				},
			},
		},
	}
}

func workflowExecutionOperatorsFixture() []PublicOperatorDescriptor {
	resourcePicker := func(name, displayName string, binding map[string]interface{}) commonModels.ParameterDescriptor {
		return commonModels.ParameterDescriptor{
			Name: name, DisplayName: displayName, Type: "ui", ParamType: "ui", UIType: "resource_tree_picker",
			UIConfig: map[string]interface{}{"resource_binding": binding},
		}
	}
	return []PublicOperatorDescriptor{
		{
			OperatorDescriptor: commonModels.OperatorDescriptor{ID: "load", Name: "load", DisplayName: "加载"},
			PublicParameters: []commonModels.ParameterDescriptor{
				resourcePicker("source_resource", "数据源", map[string]interface{}{
					"mode": "existing", "locator_param": "locator", "geometry_column_param": "geom_column",
				}),
				{Name: "locator", Type: "string", ParamType: "resource"},
				{Name: "geom_column", Type: "string", ParamType: "resource"},
			},
		},
		{
			OperatorDescriptor: commonModels.OperatorDescriptor{ID: "buffer", Name: "buffer", DisplayName: "缓冲区"},
			PublicParameters: []commonModels.ParameterDescriptor{
				{Name: "input", Type: "object", ParamType: "input"},
				{Name: "distance", DisplayName: "距离", Type: "number", Required: true, Min: floatPointer(0)},
				{Name: "quadrant_segments", DisplayName: "象限线段数", Type: "integer", Required: true, Min: floatPointer(1)},
			},
		},
		{
			OperatorDescriptor: commonModels.OperatorDescriptor{ID: "save", Name: "save", DisplayName: "保存"},
			PublicParameters: []commonModels.ParameterDescriptor{
				{Name: "input", Type: "object", ParamType: "input"},
				resourcePicker("target_resource", "保存目标", map[string]interface{}{
					"mode": "target", "parent_locator_param": "target_parent_locator", "name_param": "target_name",
					"type_param": "target_type", "type_values": map[string]interface{}{"schema": "table"},
					"default_params": map[string]interface{}{"mode": "replace"},
				}),
				{Name: "target_parent_locator", Type: "string", ParamType: "resource"},
				{Name: "target_name", Type: "string", ParamType: "resource"},
				{Name: "target_type", Type: "string", ParamType: "resource"},
				{Name: "mode", Type: "string", Enum: []string{"replace", "append"}},
			},
		},
	}
}

func sortedMapKeys(values map[string]interface{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func assertWorkflowExecutionUIOrder(t *testing.T, uiSchema map[string]interface{}, want []string) {
	t.Helper()
	ordered := make([]string, len(want))
	for name, raw := range uiSchema {
		ui := raw.(map[string]interface{})
		order, ok := ui["order"].(int)
		if !ok || order < 0 || order >= len(want) {
			t.Fatalf("input_ui_schema.%s.order = %#v, want an integer in [0, %d)", name, ui["order"], len(want))
		}
		ordered[order] = name
	}
	if !reflect.DeepEqual(ordered, want) {
		t.Fatalf("input UI order = %#v, want %#v", ordered, want)
	}
}

func assertWorkflowExecutionFieldOrder(t *testing.T, uiSchema map[string]interface{}, group string, want []string) {
	t.Helper()
	fields := uiSchema[group].(map[string]interface{})["fields"].(map[string]interface{})
	ordered := make([]string, len(want))
	for name, raw := range fields {
		ui := raw.(map[string]interface{})
		order, ok := ui["order"].(int)
		if !ok || order < 0 || order >= len(want) {
			t.Fatalf("input_ui_schema.%s.fields.%s.order = %#v, want an integer in [0, %d)", group, name, ui["order"], len(want))
		}
		ordered[order] = name
	}
	if !reflect.DeepEqual(ordered, want) {
		t.Fatalf("input UI field order = %#v, want %#v", ordered, want)
	}
}

func floatPointer(value float64) *float64 {
	return &value
}

func TestRasterExecutionContractPublishesOnlySavedResource(t *testing.T) {
	operators := publicWorkflowOperators("geopython_workflow", []commonModels.OperatorDescriptor{
		{ID: "raster_load", Name: "raster_load", Parameters: []commonModels.ParameterDescriptor{{Name: "access_plan", Type: "object"}, {Name: "source_crs", Type: "string"}}},
		{ID: "raster_save", Name: "raster_save", Parameters: []commonModels.ParameterDescriptor{{Name: "input_raster", Type: "raster", ParamType: "input"}, {Name: "access_plan", Type: "object"}, {Name: "profile", Type: "string"}}},
	})
	workflow := map[string]interface{}{"tasks": []interface{}{
		map[string]interface{}{"id": "load", "operator": "raster_load", "depends_on": []interface{}{}, "params": map[string]interface{}{"locator": "addp://engine/1/path/input.tif?type=file&item_id=8"}},
		map[string]interface{}{"id": "save", "operator": "raster_save", "depends_on": []interface{}{"load"}, "params": map[string]interface{}{"input_raster": map[string]interface{}{"$ref": "load"}, "target_parent_locator": "addp://engine/2/path/business?type=bucket", "target_name": "result.tif", "write_mode": "create", "profile": "cog"}},
	}}
	contract, err := buildWorkflowExecutionContract(workflow, operators)
	if err != nil {
		t.Fatal(err)
	}
	outputs := contract.OutputSchema["properties"].(map[string]interface{})
	if !reflect.DeepEqual(sortedMapKeys(outputs), []string{"save"}) {
		t.Fatalf("outputs = %#v", outputs)
	}
	inputs := contract.InputSchema["properties"].(map[string]interface{})
	save := inputs["save"].(map[string]interface{})["properties"].(map[string]interface{})
	for _, private := range []string{"input_raster", "access_plan"} {
		if _, exists := save[private]; exists {
			t.Fatalf("private parameter %s exposed: %#v", private, save)
		}
	}
	if _, exists := save["target_resource"]; !exists {
		t.Fatalf("target picker missing: %#v", save)
	}
	stable := workflowExecutionOutputs([]WorkflowProducedTarget{{
		TaskID: "save", Locator: "addp://engine/2/path/business/result.tif?type=object", Type: "object", WriteMode: "create",
	}})
	if err := taskprovider.ValidateExecutionParameters(contract.OutputSchema, stable, taskprovider.ParameterValidationOptions{}); err != nil {
		t.Fatalf("stable output violates its published schema: %v", err)
	}
	facts := developLineageFacts(&models.DevTask{DevType: "workflow", Content: models.DevTaskContent{"workflow_definition": workflow}}, stable)
	if facts == nil || len(facts.Inputs) != 1 || len(facts.Outputs) != 1 || facts.Outputs[0].WriteMode != "create" {
		t.Fatalf("raster lineage missing source or target: %#v", facts)
	}
}
