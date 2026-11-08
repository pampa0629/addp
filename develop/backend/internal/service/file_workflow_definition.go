package service

import (
	"bytes"
	"encoding/json"
	"reflect"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/resourcetree"
)

// The graph enters the trusted projection once through the ordinary validator.
// UseNumber preserves numeric lexemes; neither the frozen config nor projected
// parameters pass through JSONMap/float64. Resource display data is not authority.
func fileWorkflowRuntimeDefinition(facts *client.DevelopFileExecutionFacts) (string, error) {
	var config, content map[string]json.RawMessage
	if json.Unmarshal(facts.ConfigJSON, &config) != nil || json.Unmarshal(config["content"], &content) != nil {
		return "", errFileWorkflowInputs
	}
	var definition map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(content["workflow_definition"]))
	decoder.UseNumber()
	if decoder.Decode(&definition) != nil || ValidateWorkflowDefinition(definition) != nil {
		return "", errFileWorkflowInputs
	}
	tasks, _ := workflowTasksFromInterface(definition["tasks"])
	sources := map[string]execution.DevelopFileSourceIntent{}
	for _, source := range facts.Intent.Sources {
		sources[source.NodeID] = source
	}
	read := map[string]bool{}
	saved := false
	projectedTasks := make([]map[string]interface{}, 0, len(tasks))
	for _, task := range tasks {
		id := task["id"].(string)
		operator := task["operator"].(string)
		params := task["params"].(map[string]interface{})
		projected := params
		switch operator {
		case "load":
			source, known := sources[id]
			if !known || read[id] || !fileWorkflowOnlyKeys(params, "locator", "source_resource", "geom_column") {
				return "", errFileWorkflowInputs
			}
			locator, ok := params["locator"].(string)
			if !ok {
				return "", errFileWorkflowInputs
			}
			target, err := fileWorkflowLocatorPath(locator)
			if err != nil || !reflect.DeepEqual(target, source.Target) {
				return "", errFileWorkflowInputs
			}
			read[id] = true
			projected = map[string]interface{}{}
			if geometry, present := params["geom_column"]; present {
				projected["geom_column"] = geometry
			}
		case "save":
			if facts.Intent.Target == nil || saved || id != facts.Intent.Target.NodeID || !fileWorkflowOnlyKeys(params, "input_df", "mode", "target_parent_locator", "target_name", "target_resource") {
				return "", errFileWorkflowInputs
			}
			parent, parentOK := params["target_parent_locator"].(string)
			name, nameOK := params["target_name"].(string)
			mode, modeOK := params["mode"].(string)
			_, hasInput := params["input_df"]
			if !parentOK || !nameOK || !modeOK || mode != "fail" || !hasInput {
				return "", errFileWorkflowInputs
			}
			loc, err := resourcetree.ParseURI(parent)
			if err != nil || (loc.Type != resourcetree.TypeRoot && loc.Type != resourcetree.TypeDirectory && loc.Type != resourcetree.TypeDir) {
				return "", errFileWorkflowInputs
			}
			child := &resourcetree.ResourceLocator{EngineID: loc.EngineID, Type: resourcetree.TypeFile, Path: append(append([]string(nil), loc.Path...), name)}
			target, err := resourcetree.EngineCatalogPathFromLocator(plugin.FileCatalogModel(), child)
			if err != nil || !reflect.DeepEqual(target, facts.Intent.Target.Target) {
				return "", errFileWorkflowInputs
			}
			saved = true
			projected = map[string]interface{}{"input_df": params["input_df"], "mode": mode}
		default:
			if _, boundSource := sources[id]; boundSource || (facts.Intent.Target != nil && facts.Intent.Target.NodeID == id) {
				return "", errFileWorkflowInputs
			}
			// Runtime additionally admits only its own pure vector operator metadata.
			for _, key := range []string{"connection_info", "path", "access_plan", "locator", "source_resource", "target_resource", "target_parent_locator", "target_name", "engine_id", "schema", "table"} {
				if _, present := params[key]; present {
					return "", errFileWorkflowInputs
				}
			}
		}
		projectedTasks = append(projectedTasks, map[string]interface{}{"id": id, "operator": operator, "params": projected, "depends_on": task["depends_on"]})
	}
	if len(read) != len(sources) || saved != (facts.Intent.Target != nil) {
		return "", errFileWorkflowInputs
	}
	raw, err := json.Marshal(map[string]interface{}{"tasks": projectedTasks})
	if err != nil {
		return "", errFileWorkflowInputs
	}
	return string(raw), nil
}

func fileWorkflowOnlyKeys(params map[string]interface{}, keys ...string) bool {
	allowed := map[string]bool{}
	for _, key := range keys {
		allowed[key] = true
	}
	for key := range params {
		if !allowed[key] {
			return false
		}
	}
	return true
}
func fileWorkflowLocatorPath(raw string) (plugin.EngineCatalogPath, error) {
	loc, err := resourcetree.ParseURI(raw)
	if err != nil || loc.Type != resourcetree.TypeFile {
		return plugin.EngineCatalogPath{}, errFileWorkflowInputs
	}
	return resourcetree.EngineCatalogPathFromLocator(plugin.FileCatalogModel(), loc)
}
