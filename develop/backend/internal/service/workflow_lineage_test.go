package service

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/models"
)

func lineageWorkflowNode(id, operator string, params map[string]interface{}, dependencies ...string) map[string]interface{} {
	return map[string]interface{}{"id": id, "operator": operator, "params": params, "depends_on": dependencies}
}

func lineageWorkflowTask(nodes ...map[string]interface{}) *models.DevTask {
	tasks := make([]interface{}, len(nodes))
	for i, node := range nodes {
		tasks[i] = node
	}
	return &models.DevTask{DevType: "workflow", Content: models.DevTaskContent{"workflow_definition": map[string]interface{}{"tasks": tasks}}}
}

// Expand the public port contract into resource edges, as a consumer of the
// facts would. Counts alone cannot catch a source attached to the wrong branch.
func assertWorkflowLineageEdges(t *testing.T, facts *commonExecution.LineageFacts, want ...string) {
	t.Helper()
	if facts == nil || facts.SchemaVersion != commonExecution.LineageFactsSchemaVersion || len(facts.Operations) == 0 {
		t.Fatalf("missing explicit lineage operations: %#v", facts)
	}
	var edges []string
	for _, operation := range facts.Operations {
		if operation.Kind != "derive" {
			t.Fatalf("unexpected operation: %#v", operation)
		}
		for _, input := range facts.Inputs {
			if !slices.Contains(operation.InputPorts, input.Port) {
				continue
			}
			for _, output := range facts.Outputs {
				if slices.Contains(operation.OutputPorts, output.Port) {
					edges = append(edges, input.Locator+" -> "+output.Locator)
				}
			}
		}
	}
	slices.Sort(edges)
	slices.Sort(want)
	if !reflect.DeepEqual(edges, want) {
		t.Fatalf("resource edges = %v, want %v", edges, want)
	}
}

func TestWorkflowLineageSeparatesIndependentConversionsAndOrderingDependencies(t *testing.T) {
	task := lineageWorkflowTask(
		lineageWorkflowNode("three_ds", "3ds_to_glb", map[string]interface{}{"locator": "source.3ds"}, "dae"),
		lineageWorkflowNode("unused", "load", map[string]interface{}{"locator": "unused-source"}),
		lineageWorkflowNode("dae", "dae_to_glb", map[string]interface{}{
			"locator": "source.dae", "target_parent_locator": "output-parent",
			"options": map[string]interface{}{"locator": "ordinary-json-value"},
		}),
	)
	outputs := workflowExecutionOutputs([]WorkflowProducedTarget{
		{TaskID: "three_ds", Locator: "3ds.glb", WriteMode: "create"},
		{TaskID: "dae", Locator: "dae.glb", WriteMode: "replace"},
	})
	facts := developLineageFacts(task, outputs)
	assertWorkflowLineageEdges(t, facts, "source.dae -> dae.glb", "source.3ds -> 3ds.glb")
	if len(facts.Operations) != 2 || facts.Operations[0].Operator != "dae_to_glb" || facts.Operations[1].Operator != "3ds_to_glb" {
		t.Fatalf("operations must identify their producing operators: %#v", facts.Operations)
	}
	if facts.Outputs[0].WriteMode != "replace" || facts.Outputs[1].WriteMode != "create" {
		t.Fatalf("actual write modes lost: %#v", facts.Outputs)
	}
	// The facts must not change when map insertion or the DAG's display order changes.
	before, _ := json.Marshal(facts)
	nodes := task.Content["workflow_definition"].(map[string]interface{})["tasks"].([]interface{})
	slices.Reverse(nodes)
	after, _ := json.Marshal(developLineageFacts(task, outputs))
	if string(before) != string(after) {
		t.Fatalf("lineage depends on DAG display order: %s != %s", before, after)
	}
}

func TestWorkflowLineageTracesNestedFanInAndBatchOutputs(t *testing.T) {
	task := lineageWorkflowTask(
		lineageWorkflowNode("load.a", "raster_load", map[string]interface{}{"locator": "a.tif"}),
		lineageWorkflowNode("load_b", "raster_load", map[string]interface{}{"locator": "b.tif"}),
		lineageWorkflowNode("load_c", "raster_load", map[string]interface{}{"locator": "c.tif"}),
		lineageWorkflowNode("mosaic", "raster_mosaic", map[string]interface{}{"input_rasters": []interface{}{
			map[string]interface{}{"$ref": "load.a", "port": "default"},
			map[string]interface{}{"$ref": "load_b"},
			map[string]interface{}{"$ref": "load.a"},
		}}, "load.a", "load_b", "load_c"),
		lineageWorkflowNode("tile", "raster_tile", map[string]interface{}{"input_raster": map[string]interface{}{"$ref": "mosaic"}}, "mosaic"),
		lineageWorkflowNode("save_tiles", "raster_save_tiles", map[string]interface{}{"input_tiles": map[string]interface{}{"$ref": "tile"}}, "tile"),
		lineageWorkflowNode("save_c", "raster_save", map[string]interface{}{"input_raster": map[string]interface{}{"$ref": "load_c"}}, "load_c"),
	)
	outputs := workflowExecutionOutputs([]WorkflowProducedTarget{
		{TaskID: "save_tiles", TileKey: "r0_c1", Locator: "tile-1.tif", WriteMode: "create"},
		{TaskID: "save_tiles", TileKey: "r0_c0", Locator: "tile-0.tif", WriteMode: "create"},
		{TaskID: "save_c", Locator: "c-output.tif", WriteMode: "replace"},
	})
	facts := developLineageFacts(task, outputs)
	assertWorkflowLineageEdges(t, facts,
		"a.tif -> tile-0.tif", "a.tif -> tile-1.tif", "b.tif -> tile-0.tif", "b.tif -> tile-1.tif", "c.tif -> c-output.tif")
	if len(facts.Operations) != 2 || len(facts.Outputs) != 3 {
		t.Fatalf("batch outputs lost or operations duplicated: %#v", facts)
	}
}

func TestWorkflowLineageDoesNotInventSourcesFromExecutionOrder(t *testing.T) {
	task := lineageWorkflowTask(
		lineageWorkflowNode("load", "load", map[string]interface{}{"locator": "unrelated-source"}),
		lineageWorkflowNode("generate", "generate", map[string]interface{}{"value": 7}, "load"),
	)
	outputs := workflowExecutionOutputs([]WorkflowProducedTarget{{TaskID: "generate", Locator: "generated-result"}})
	if facts := developLineageFacts(task, outputs); facts != nil {
		t.Fatalf("execution ordering was mistaken for data flow: %#v", facts)
	}
}

func TestWorkflowLineageUsesEffectiveResourceOverridesAndActualOutputs(t *testing.T) {
	task := lineageWorkflowTask(
		lineageWorkflowNode("load", "load", map[string]interface{}{"locator": "saved-source"}),
		lineageWorkflowNode("save", "save", map[string]interface{}{"input": map[string]interface{}{"$ref": "load"}, "target_parent_locator": "saved-parent", "target_name": "saved-result"}, "load"),
	)
	workflow := task.Content["workflow_definition"].(map[string]interface{})
	effective, err := cloneWorkflowDefinition(workflow)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyWorkflowExecutionOverrides(effective, map[string]interface{}{
		"load": map[string]interface{}{"source_resource": map[string]interface{}{"locator": "addp://engine/8/path/public/once?type=table"}},
	}, workflowExecutionOperatorsFixture()); err != nil {
		t.Fatal(err)
	}
	snapshot := &models.DevTask{DevType: "workflow", Content: models.DevTaskContent{"workflow_definition": effective}}
	outputs := workflowExecutionOutputs([]WorkflowProducedTarget{{TaskID: "save", Locator: "actual-result", WriteMode: "append"}})
	assertWorkflowLineageEdges(t, developLineageFacts(snapshot, outputs), "addp://engine/8/path/public/once?type=table -> actual-result")
	if workflow["tasks"].([]interface{})[0].(map[string]interface{})["params"].(map[string]interface{})["locator"] != "saved-source" {
		t.Fatal("saved definition was mutated")
	}
}

func TestWorkflowLineageRejectsUnresolvableDataReferences(t *testing.T) {
	for _, reference := range []string{"missing", "save"} {
		t.Run(reference, func(t *testing.T) {
			task := lineageWorkflowTask(
				lineageWorkflowNode("load", "load", map[string]interface{}{"locator": "source"}),
				lineageWorkflowNode("save", "save", map[string]interface{}{"input": map[string]interface{}{"$ref": reference}}, "load"),
			)
			outputs := commonModels.JSONMap{"save": map[string]interface{}{"resource": map[string]interface{}{"locator": "target"}}}
			if facts := developLineageFacts(task, outputs); facts != nil {
				t.Fatalf("invalid data reference produced guessed lineage: %#v", facts)
			}
		})
	}
}
