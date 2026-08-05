package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/common/taskprovider"
)

type resolvedWorkflowExecution struct {
	Workflow            map[string]interface{}
	Contract            taskprovider.ExecutionContract
	EffectiveParameters map[string]interface{}
}

type ExecutionParametersError struct {
	Cause error
}

func (e *ExecutionParametersError) Error() string {
	if e == nil || e.Cause == nil {
		return "execution parameters are invalid"
	}
	return "execution parameters are invalid: " + e.Cause.Error()
}

func (e *ExecutionParametersError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (s *OperatorDiscoveryService) WorkflowExecutionContractForTenant(
	ctx context.Context,
	workflowEngineID uint,
	workflowDefinition map[string]interface{},
	tenantID uint,
) (*taskprovider.ExecutionContract, error) {
	operators, err := s.getOperatorsByWorkflowEngineID(ctx, workflowEngineID, tenantID)
	if err != nil {
		return nil, err
	}
	contract, err := buildWorkflowExecutionContract(workflowDefinition, operators)
	if err != nil {
		return nil, err
	}
	return &contract, nil
}

func (s *OperatorDiscoveryService) resolveWorkflowExecutionParameters(
	ctx context.Context,
	workflowEngineID uint,
	workflowDefinition map[string]interface{},
	overrides map[string]interface{},
	tenantID uint,
) (*resolvedWorkflowExecution, error) {
	operators, err := s.getOperatorsByWorkflowEngineID(ctx, workflowEngineID, tenantID)
	if err != nil {
		return nil, err
	}
	contract, err := buildWorkflowExecutionContract(workflowDefinition, operators)
	if err != nil {
		return nil, err
	}
	if err := taskprovider.ValidateExecutionParameters(contract.InputSchema, overrides, taskprovider.ParameterValidationOptions{}); err != nil {
		return nil, err
	}
	resolved, err := cloneWorkflowDefinition(workflowDefinition)
	if err != nil {
		return nil, err
	}
	if err := applyWorkflowExecutionOverrides(resolved, overrides, operators); err != nil {
		return nil, err
	}
	effective := mergeWorkflowExecutionDefaults(contract.InputDefaults, overrides)
	return &resolvedWorkflowExecution{Workflow: resolved, Contract: contract, EffectiveParameters: effective}, nil
}

func buildWorkflowExecutionContract(
	workflowDefinition map[string]interface{},
	operators []PublicOperatorDescriptor,
) (taskprovider.ExecutionContract, error) {
	if err := ValidateWorkflowDefinition(workflowDefinition); err != nil {
		return taskprovider.ExecutionContract{}, err
	}
	operatorByName := make(map[string]PublicOperatorDescriptor, len(operators)*2)
	for _, operator := range operators {
		operatorByName[operator.ID] = operator
		operatorByName[operator.Name] = operator
	}

	inputProperties := map[string]interface{}{}
	inputDefaults := map[string]interface{}{}
	inputUI := map[string]interface{}{}
	outputProperties := map[string]interface{}{}
	tasks, _ := workflowTasksFromInterface(workflowDefinition["tasks"])
	orderedTasks, err := stableWorkflowExecutionTasks(tasks)
	if err != nil {
		return taskprovider.ExecutionContract{}, err
	}
	inputOrder := 0
	for _, task := range orderedTasks {
		taskID, _ := task["id"].(string)
		operatorName, _ := task["operator"].(string)
		operator, ok := operatorByName[operatorName]
		if !ok {
			return taskprovider.ExecutionContract{}, fmt.Errorf("目标工作流引擎不存在算子: %s", operatorName)
		}
		params, _ := task["params"].(map[string]interface{})
		nodeSchema, nodeDefaults, nodeUI := workflowNodeExecutionInputs(operator, params)
		if len(nodeSchema) > 0 {
			inputProperties[taskID] = map[string]interface{}{
				"type":                 "object",
				"title":                operator.DisplayName,
				"properties":           nodeSchema,
				"additionalProperties": false,
			}
			inputDefaults[taskID] = nodeDefaults
			inputUI[taskID] = map[string]interface{}{
				"control": "group",
				"title":   operator.DisplayName,
				"order":   inputOrder,
				"fields":  nodeUI,
			}
			inputOrder++
		}
		if workflowOperatorHasStableResourceOutput(operator) {
			outputProperties[taskID] = workflowResourceOutputSchema(operator.DisplayName)
		}
	}

	contract := taskprovider.ExecutionContract{
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"properties":           inputProperties,
			"additionalProperties": false,
		},
		InputDefaults: inputDefaults,
		InputUISchema: inputUI,
		OutputSchema: map[string]interface{}{
			"type":                 "object",
			"properties":           outputProperties,
			"additionalProperties": false,
		},
	}
	if _, err := taskprovider.ParseExecutionContract(map[string]interface{}{
		"input_schema":    contract.InputSchema,
		"input_defaults":  contract.InputDefaults,
		"input_ui_schema": contract.InputUISchema,
		"output_schema":   contract.OutputSchema,
	}); err != nil {
		return taskprovider.ExecutionContract{}, err
	}
	return contract, nil
}

func stableWorkflowExecutionTasks(tasks []map[string]interface{}) ([]map[string]interface{}, error) {
	ordered := make([]map[string]interface{}, 0, len(tasks))
	emitted := make(map[string]struct{}, len(tasks))

	for len(ordered) < len(tasks) {
		ready := make([]map[string]interface{}, 0)
		for _, task := range tasks {
			taskID, _ := task["id"].(string)
			if _, exists := emitted[taskID]; exists {
				continue
			}
			dependencies, _ := stringSliceFromInterface(task["depends_on"])
			allDependenciesEmitted := true
			for _, dependencyID := range dependencies {
				if _, exists := emitted[dependencyID]; !exists {
					allDependenciesEmitted = false
					break
				}
			}
			if allDependenciesEmitted {
				ready = append(ready, task)
			}
		}
		if len(ready) == 0 {
			return nil, fmt.Errorf("content.workflow_definition 存在循环依赖")
		}
		for _, task := range ready {
			taskID, _ := task["id"].(string)
			emitted[taskID] = struct{}{}
			ordered = append(ordered, task)
		}
	}

	return ordered, nil
}

func workflowNodeExecutionInputs(
	operator PublicOperatorDescriptor,
	params map[string]interface{},
) (map[string]interface{}, map[string]interface{}, map[string]interface{}) {
	properties := map[string]interface{}{}
	defaults := map[string]interface{}{}
	ui := map[string]interface{}{}
	managed := workflowResourceManagedParameters(operator.PublicParameters)
	order := 0

	for _, parameter := range operator.PublicParameters {
		switch parameter.ParamType {
		case "input", "resource":
			continue
		case "ui":
			if parameter.UIType != "resource_tree_picker" {
				continue
			}
			schema, value, ok := workflowResourceExecutionInput(parameter, params)
			if !ok {
				continue
			}
			properties[parameter.Name] = schema
			defaults[parameter.Name] = value
			fieldUI := workflowResourceExecutionUI(parameter)
			fieldUI["order"] = order
			ui[parameter.Name] = fieldUI
			order++
			continue
		}
		if _, associated := managed[parameter.Name]; associated {
			continue
		}
		value, exists := params[parameter.Name]
		if !exists || isWorkflowReference(value) {
			continue
		}
		properties[parameter.Name] = workflowParameterSchema(parameter)
		defaults[parameter.Name] = value
		ui[parameter.Name] = map[string]interface{}{
			"control":   workflowParameterControl(parameter),
			"show_when": parameter.ShowWhen,
			"order":     order,
		}
		order++
	}
	return properties, defaults, ui
}

func workflowResourceManagedParameters(parameters []commonModels.ParameterDescriptor) map[string]struct{} {
	result := map[string]struct{}{}
	for _, parameter := range parameters {
		if parameter.UIType != "resource_tree_picker" {
			continue
		}
		binding, _ := parameter.UIConfig["resource_binding"].(map[string]interface{})
		for _, key := range []string{"locator_param", "parent_locator_param", "name_param", "type_param", "geometry_column_param"} {
			if name, _ := binding[key].(string); strings.TrimSpace(name) != "" {
				result[name] = struct{}{}
			}
		}
		if values, ok := binding["default_params"].(map[string]interface{}); ok {
			for name := range values {
				result[name] = struct{}{}
			}
		}
	}
	return result
}

func workflowResourceExecutionInput(
	parameter commonModels.ParameterDescriptor,
	params map[string]interface{},
) (map[string]interface{}, map[string]interface{}, bool) {
	binding, _ := parameter.UIConfig["resource_binding"].(map[string]interface{})
	mode, _ := binding["mode"].(string)
	properties := map[string]interface{}{}
	defaults := map[string]interface{}{}
	required := []interface{}{}
	switch mode {
	case "existing":
		locatorParam, _ := binding["locator_param"].(string)
		locator, ok := params[locatorParam].(string)
		if !ok || strings.TrimSpace(locator) == "" {
			return nil, nil, false
		}
		properties["locator"] = map[string]interface{}{"type": "string", "format": "resource-locator"}
		defaults["locator"] = locator
		required = append(required, "locator")
		if geometryParam, _ := binding["geometry_column_param"].(string); geometryParam != "" {
			properties["geometry_column"] = map[string]interface{}{"type": "string"}
			if value, exists := params[geometryParam]; exists && value != nil {
				defaults["geometry_column"] = value
			}
		}
	case "target":
		parentParam, _ := binding["parent_locator_param"].(string)
		nameParam, _ := binding["name_param"].(string)
		parent, parentOK := params[parentParam].(string)
		name, nameOK := params[nameParam].(string)
		if !parentOK || strings.TrimSpace(parent) == "" || !nameOK || strings.TrimSpace(name) == "" {
			return nil, nil, false
		}
		properties["parent_locator"] = map[string]interface{}{"type": "string", "format": "resource-locator"}
		properties["name"] = map[string]interface{}{"type": "string", "minLength": float64(1)}
		defaults["parent_locator"] = parent
		defaults["name"] = name
		required = append(required, "parent_locator", "name")
	default:
		return nil, nil, false
	}
	if values, ok := binding["default_params"].(map[string]interface{}); ok {
		for name, fallback := range values {
			properties[name] = workflowValueSchema(params[name], fallback)
			if value, exists := params[name]; exists {
				defaults[name] = value
			} else {
				defaults[name] = fallback
			}
		}
	}
	return map[string]interface{}{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}, defaults, true
}

func workflowResourceExecutionUI(parameter commonModels.ParameterDescriptor) map[string]interface{} {
	result := map[string]interface{}{
		"control":      "resource_tree_picker",
		"display_name": parameter.DisplayName,
	}
	for key, value := range parameter.UIConfig {
		result[key] = value
	}
	return result
}

func workflowParameterSchema(parameter commonModels.ParameterDescriptor) map[string]interface{} {
	schema := map[string]interface{}{
		"type":        normalizeWorkflowParameterType(parameter.Type),
		"title":       firstNonEmpty(parameter.DisplayName, parameter.Name),
		"description": parameter.Description,
	}
	if len(parameter.Enum) > 0 {
		values := make([]interface{}, len(parameter.Enum))
		for index, value := range parameter.Enum {
			values[index] = value
		}
		schema["enum"] = values
	}
	if parameter.Min != nil {
		schema["minimum"] = *parameter.Min
	}
	if parameter.Max != nil {
		schema["maximum"] = *parameter.Max
	}
	return schema
}

func workflowValueSchema(value, fallback interface{}) map[string]interface{} {
	if value == nil {
		value = fallback
	}
	switch value.(type) {
	case bool:
		return map[string]interface{}{"type": "boolean"}
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return map[string]interface{}{"type": "integer"}
	case float32, float64:
		return map[string]interface{}{"type": "number"}
	default:
		return map[string]interface{}{"type": "string"}
	}
}

func normalizeWorkflowParameterType(value string) string {
	switch value {
	case "float", "double":
		return "number"
	case "int":
		return "integer"
	case "string", "integer", "number", "boolean", "array", "object":
		return value
	default:
		return "string"
	}
}

func workflowParameterControl(parameter commonModels.ParameterDescriptor) string {
	if len(parameter.Enum) > 0 {
		return "select"
	}
	switch normalizeWorkflowParameterType(parameter.Type) {
	case "integer", "number":
		return "number"
	case "boolean":
		return "switch"
	default:
		return "text"
	}
}

func workflowOperatorHasStableResourceOutput(operator PublicOperatorDescriptor) bool {
	for _, parameter := range operator.PublicParameters {
		if parameter.UIType != "resource_tree_picker" {
			continue
		}
		binding, _ := parameter.UIConfig["resource_binding"].(map[string]interface{})
		if binding["mode"] == "target" {
			return true
		}
	}
	return false
}

func workflowResourceOutputSchema(title string) map[string]interface{} {
	return map[string]interface{}{
		"type":  "object",
		"title": title,
		"properties": map[string]interface{}{
			"resource": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"locator": map[string]interface{}{"type": "string", "format": "resource-locator"},
					"type":    map[string]interface{}{"type": "string"},
				},
				"required":             []interface{}{"locator", "type"},
				"additionalProperties": false,
			},
		},
		"required":             []interface{}{"resource"},
		"additionalProperties": false,
	}
}

func applyWorkflowExecutionOverrides(
	workflow map[string]interface{},
	overrides map[string]interface{},
	operators []PublicOperatorDescriptor,
) error {
	operatorByName := make(map[string]PublicOperatorDescriptor, len(operators)*2)
	for _, operator := range operators {
		operatorByName[operator.ID] = operator
		operatorByName[operator.Name] = operator
	}
	tasks, _ := workflowTasksFromInterface(workflow["tasks"])
	for _, task := range tasks {
		taskID, _ := task["id"].(string)
		nodeOverrides, ok := overrides[taskID].(map[string]interface{})
		if !ok || len(nodeOverrides) == 0 {
			continue
		}
		operatorName, _ := task["operator"].(string)
		operator := operatorByName[operatorName]
		params, _ := task["params"].(map[string]interface{})
		parameterByName := make(map[string]commonModels.ParameterDescriptor, len(operator.PublicParameters))
		for _, parameter := range operator.PublicParameters {
			parameterByName[parameter.Name] = parameter
		}
		for name, value := range nodeOverrides {
			parameter := parameterByName[name]
			if parameter.UIType == "resource_tree_picker" {
				if err := applyWorkflowResourceOverride(params, parameter, value); err != nil {
					return fmt.Errorf("parameters.%s.%s: %w", taskID, name, err)
				}
				continue
			}
			params[name] = value
		}
	}
	return nil
}

func applyWorkflowResourceOverride(params map[string]interface{}, parameter commonModels.ParameterDescriptor, raw interface{}) error {
	value, ok := raw.(map[string]interface{})
	if !ok {
		return fmt.Errorf("resource override must be an object")
	}
	binding, _ := parameter.UIConfig["resource_binding"].(map[string]interface{})
	mode, _ := binding["mode"].(string)
	var locatorText string
	switch mode {
	case "existing":
		locatorText, _ = value["locator"].(string)
		locatorParam, _ := binding["locator_param"].(string)
		params[locatorParam] = locatorText
		if geometryParam, _ := binding["geometry_column_param"].(string); geometryParam != "" {
			if geometry, exists := value["geometry_column"]; exists {
				params[geometryParam] = geometry
			} else {
				delete(params, geometryParam)
			}
		}
	case "target":
		locatorText, _ = value["parent_locator"].(string)
		parentParam, _ := binding["parent_locator_param"].(string)
		nameParam, _ := binding["name_param"].(string)
		params[parentParam] = locatorText
		params[nameParam] = value["name"]
	default:
		return fmt.Errorf("unsupported resource binding mode %q", mode)
	}
	locator, err := resourcetree.ParseURI(locatorText)
	if err != nil {
		return fmt.Errorf("invalid ResourceLocator: %w", err)
	}
	if typeParam, _ := binding["type_param"].(string); typeParam != "" {
		typeValues, _ := binding["type_values"].(map[string]interface{})
		mapped, exists := typeValues[string(locator.Type)]
		if !exists {
			return fmt.Errorf("resource type %s is not supported", locator.Type)
		}
		params[typeParam] = mapped
	}
	if defaults, ok := binding["default_params"].(map[string]interface{}); ok {
		for name, fallback := range defaults {
			if override, exists := value[name]; exists {
				params[name] = override
			} else {
				params[name] = fallback
			}
		}
	}
	return nil
}

func mergeWorkflowExecutionDefaults(defaults, overrides map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(defaults))
	for nodeID, defaultValue := range defaults {
		defaultNode, _ := defaultValue.(map[string]interface{})
		mergedNode := make(map[string]interface{}, len(defaultNode))
		for name, value := range defaultNode {
			mergedNode[name] = value
		}
		if overrideNode, ok := overrides[nodeID].(map[string]interface{}); ok {
			for name, value := range overrideNode {
				mergedNode[name] = value
			}
		}
		result[nodeID] = mergedNode
	}
	return result
}

func cloneWorkflowDefinition(source map[string]interface{}) (map[string]interface{}, error) {
	encoded, err := json.Marshal(source)
	if err != nil {
		return nil, fmt.Errorf("encode workflow definition: %w", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, fmt.Errorf("decode workflow definition: %w", err)
	}
	return result, nil
}

func isWorkflowReference(value interface{}) bool {
	reference, ok := value.(map[string]interface{})
	if !ok {
		return false
	}
	_, ok = reference["$ref"].(string)
	return ok
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
