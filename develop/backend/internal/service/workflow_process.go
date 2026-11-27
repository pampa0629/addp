package service

import (
	"strings"

	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/develop/backend/internal/models"
)

// workflowProcess consumes only the authorized execution row. The associated
// task definition is mutable and never participates in this projection.
func workflowProcess(execution *commonExecution.TaskExecution, observation *commonExecution.Observation) *models.WorkflowProcess {
	if execution.TaskType != commonExecution.TaskTypeWorkflow {
		return nil
	}
	content, _ := mapValue(execution.ExecutionConfig["content"])
	definition, _ := mapValue(content["workflow_definition"])
	tasks, ok := workflowTasksFromInterface(definition["tasks"])
	if !ok || len(tasks) == 0 {
		return nil
	}
	process := &models.WorkflowProcess{
		Nodes:           make([]models.WorkflowProcessNode, 0, len(tasks)),
		DefinitionLinks: []models.WorkflowProcessLink{},
	}
	indexByID := make(map[string]int, len(tasks))
	for i, task := range tasks {
		id, _ := task["id"].(string)
		operator, _ := task["operator"].(string)
		id, operator = strings.TrimSpace(id), strings.TrimSpace(operator)
		if _, seen := indexByID[id]; id == "" || operator == "" || seen {
			return nil
		}
		indexByID[id] = i
		process.Nodes = append(process.Nodes, models.WorkflowProcessNode{ID: id, Operator: operator, Status: "unknown"})
	}
	seen := make(map[models.WorkflowProcessLink]struct{})
	for i, task := range tasks {
		params, ok := mapValue(task["params"])
		if !ok {
			return nil
		}
		for _, parameter := range workflowLineageMapKeys(params) {
			link := models.WorkflowProcessLink{ToNode: process.Nodes[i].ID, InputParameter: parameter}
			valid := visitWorkflowProcessLinks(params[parameter], link, func(link models.WorkflowProcessLink) bool {
				if _, known := indexByID[link.FromNode]; !known || link.FromNode == link.ToNode || parameter == "" {
					return false
				}
				if _, exists := seen[link]; !exists {
					seen[link] = struct{}{}
					process.DefinitionLinks = append(process.DefinitionLinks, link)
				}
				return true
			})
			if !valid {
				return nil
			}
		}
	}
	applyWorkflowProcessObservations(process.Nodes, indexByID, execution, observation)
	return process
}

func visitWorkflowProcessLinks(value interface{}, link models.WorkflowProcessLink, visit func(models.WorkflowProcessLink) bool) bool {
	if object, ok := mapValue(value); ok {
		if raw, exists := object["$ref"]; exists {
			id, _ := raw.(string)
			link.FromNode = strings.TrimSpace(id)
			if rawPort := object["port"]; rawPort != nil {
				port, _ := rawPort.(string)
				link.OutputPort = strings.TrimSpace(port)
				if link.OutputPort == "" {
					return false
				}
			}
			return visit(link)
		}
		for _, key := range workflowLineageMapKeys(object) {
			if !visitWorkflowProcessLinks(object[key], link, visit) {
				return false
			}
		}
	} else if items, ok := value.([]interface{}); ok {
		for _, item := range items {
			if !visitWorkflowProcessLinks(item, link, visit) {
				return false
			}
		}
	}
	return true
}

func applyWorkflowProcessObservations(nodes []models.WorkflowProcessNode, indexByID map[string]int, execution *commonExecution.TaskExecution, observation *commonExecution.Observation) {
	// Legacy Runtime logs carry no attempt identifier. Do not promote a node
	// from a prior attempt or from a missing or malformed log sequence.
	if execution.Attempt != 1 {
		return
	}
	result, _ := mapValue(execution.Metadata["result"])
	logs, err := plugin.DecodeWorkflowExecutionLogs(result["logs"])
	if err != nil || len(logs) == 0 || logs[0].Event != "workflow_started" {
		return
	}
	for i, log := range logs[1:] {
		last := i+2 == len(logs)
		if log.Event != "task_progress" && !(last && (log.Event == "workflow_succeeded" || log.Event == "workflow_failed")) {
			return
		}
	}
	lastTask := ""
	for _, log := range logs {
		if log.Event == "task_progress" {
			lastTask = log.TaskID
			if i, known := indexByID[lastTask]; known {
				nodes[i].Status = "observed"
			}
		}
	}
	switch logs[len(logs)-1].Event {
	case "workflow_succeeded":
		for i := range nodes {
			if nodes[i].Status == "observed" {
				nodes[i].Status = "succeeded"
			}
		}
	case "workflow_failed":
		diagnostic, _ := observation.ErrorDetails["diagnostic"].(*commonExecution.FailureDiagnostic)
		if diagnostic == nil || diagnostic.Operator == nil {
			return
		}
		if i, found := indexByID[lastTask]; found &&
			diagnostic.Operator.NodeIndex == i+1 && diagnostic.Operator.OperatorID == nodes[i].Operator {
			nodes[i].Status = "failed"
		}
	}
}
