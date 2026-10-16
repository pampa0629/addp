package service

import (
	"slices"
	"strings"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
)

// workflowLineageFacts consumes the effective execution snapshot and confirmed,
// node-named outputs. Ordering dependencies never imply data dependencies.
func workflowLineageFacts(definition interface{}, outputs commonModels.JSONMap) *commonExecution.LineageFacts {
	workflow, ok := definition.(map[string]interface{})
	if !ok {
		return nil
	}
	tasks, ok := workflowTasksFromInterface(workflow["tasks"])
	if !ok {
		return nil
	}
	byID := make(map[string]map[string]interface{}, len(tasks))
	for _, task := range tasks {
		id, _ := task["id"].(string)
		byID[id] = task
	}

	facts := &commonExecution.LineageFacts{SchemaVersion: commonExecution.LineageFactsSchemaVersion}
	directInputs := make(map[string][]commonExecution.LineageResourceRef, len(tasks))
	for _, id := range workflowLineageMapKeys(byID) {
		// Adapters normalize the selected source into the public locator
		// parameter. JSON conversion options are not resource bindings.
		params, _ := byID[id]["params"].(map[string]interface{})
		locator, _ := params["locator"].(string)
		if strings.TrimSpace(locator) != "" {
			ref := commonExecution.LineageResourceRef{Port: "input." + id, Locator: locator}
			directInputs[id] = []commonExecution.LineageResourceRef{ref}
			facts.Inputs = append(facts.Inputs, ref)
		}
	}

	// Memoize each node's physical read sources; runtime objects stay within the
	// DAG and do not become resources. A reference's port selects data from the
	// referenced node, whose contributing reads remain node-scoped.
	sources := make(map[string][]string, len(tasks))
	visiting := make(map[string]bool, len(tasks))
	var sourcePorts func(string) ([]string, bool)
	sourcePorts = func(id string) ([]string, bool) {
		if ports, resolved := sources[id]; resolved {
			return ports, true
		}
		task, exists := byID[id]
		if !exists || visiting[id] {
			return nil, false
		}
		visiting[id] = true
		var ports []string
		if len(directInputs[id]) > 0 {
			ports = append(ports, "input."+id)
		}
		for _, reference := range workflowLineageReferenceNodes(task["params"]) {
			upstream, valid := sourcePorts(reference)
			if !valid {
				return nil, false
			}
			ports = append(ports, upstream...)
		}
		slices.Sort(ports)
		ports = slices.Compact(ports)
		sources[id] = ports
		visiting[id] = false
		return ports, true
	}
	for _, id := range workflowLineageMapKeys(outputs) {
		ports, valid := sourcePorts(id)
		if !valid {
			// Invalid DAGs are rejected before execution; never invent a lineage
			// relation if a caller supplies an unresolvable execution snapshot.
			return nil
		}
		refs := collectLineageRefs(outputs[id], "output."+id)
		facts.Outputs = append(facts.Outputs, refs...)
		if len(refs) > 0 && len(ports) > 0 {
			operator, _ := byID[id]["operator"].(string)
			facts.Operations = append(facts.Operations, commonExecution.LineageOperation{
				Kind: "derive", Operator: operator, InputPorts: ports, OutputPorts: []string{"output." + id},
			})
		}
	}
	if len(facts.Operations) == 0 {
		return nil
	}
	return facts
}

func workflowLineageReferenceNodes(value interface{}) []string {
	var refs []string
	switch typed := value.(type) {
	case commonModels.JSONMap:
		return workflowLineageReferenceNodes(map[string]interface{}(typed))
	case map[string]interface{}:
		if ref, ok := typed["$ref"].(string); ok {
			return []string{strings.TrimSpace(ref)}
		}
		for _, key := range workflowLineageMapKeys(typed) {
			refs = append(refs, workflowLineageReferenceNodes(typed[key])...)
		}
	case []interface{}:
		for _, item := range typed {
			refs = append(refs, workflowLineageReferenceNodes(item)...)
		}
	}
	return refs
}

func workflowLineageMapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
