package dbbridge

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/addp/common/engine/plugin"
	engineselection "github.com/addp/common/engine/selection"
	"github.com/addp/common/models"
)

// ListWorkflowOperators 通过工作流运行时 Provider 动态发现算子。
func ListWorkflowOperators(ctx context.Context, engine *models.Engine) ([]models.OperatorDescriptor, error) {
	workflowProvider, err := WorkflowRuntimeProviderForEngine(engine)
	if err != nil {
		return nil, err
	}

	operators, err := workflowProvider.ListOperators(ctx, plugin.ConnectionInfo(engine.ConnectionInfo))
	if err != nil {
		return nil, err
	}
	return toModelOperators(engine, operators)
}

// ExecuteWorkflow 通过工作流运行时 Provider 执行工作流。
func ExecuteWorkflow(ctx context.Context, engine *models.Engine, req plugin.WorkflowExecuteRequest) (*plugin.WorkflowExecuteResult, error) {
	workflowProvider, err := WorkflowRuntimeProviderForEngine(engine)
	if err != nil {
		return nil, err
	}
	operators, err := workflowProvider.ListOperators(ctx, plugin.ConnectionInfo(engine.ConnectionInfo))
	if err != nil {
		return nil, fmt.Errorf("list workflow operators: %w", err)
	}
	modelOperators, err := toModelOperators(engine, operators)
	if err != nil {
		return nil, err
	}
	if err := ensureWorkflowOperators(modelOperators, req.WorkflowDef); err != nil {
		return nil, err
	}
	return workflowProvider.ExecuteWorkflow(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), req)
}

// InvokeOperator 通过工作流运行时 Provider direct 调用单个算子。
func InvokeOperator(ctx context.Context, engine *models.Engine, operatorName string, req plugin.OperatorInvokeRequest) (*plugin.OperatorInvokeResult, error) {
	workflowProvider, err := WorkflowRuntimeProviderForEngine(engine)
	if err != nil {
		return nil, err
	}
	operators, err := workflowProvider.ListOperators(ctx, plugin.ConnectionInfo(engine.ConnectionInfo))
	if err != nil {
		return nil, fmt.Errorf("list workflow operators: %w", err)
	}
	modelOperators, err := toModelOperators(engine, operators)
	if err != nil {
		return nil, err
	}
	if err := ensureDirectOperator(modelOperators, operatorName); err != nil {
		return nil, err
	}
	return workflowProvider.InvokeOperator(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), operatorName, req)
}

// GetWorkflowExecutionStatus 通过工作流运行时 Provider 查询运行时本地执行状态。
func GetWorkflowExecutionStatus(ctx context.Context, engine *models.Engine, executionID string) (*plugin.WorkflowExecutionStatus, error) {
	workflowProvider, err := WorkflowRuntimeProviderForEngine(engine)
	if err != nil {
		return nil, err
	}
	return workflowProvider.GetExecutionStatus(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), executionID)
}

// OpenScriptSession 通过脚本运行时 Provider 打开受控运行会话。
func OpenScriptSession(ctx context.Context, engine *models.Engine, req plugin.ScriptSessionRequest) (*plugin.ScriptSession, error) {
	scriptProvider, err := scriptRuntimeProvider(engine)
	if err != nil {
		return nil, err
	}
	return scriptProvider.OpenSession(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), req)
}

type DirectWorkflowOperatorSelector struct {
	OperatorName string
	EngineName   string
}

func ResolveDirectWorkflowOperator(ctx context.Context, engines []models.Engine, selector DirectWorkflowOperatorSelector) (models.Engine, models.OperatorDescriptor, error) {
	operatorName := strings.TrimSpace(selector.OperatorName)
	if operatorName == "" {
		return models.Engine{}, models.OperatorDescriptor{}, fmt.Errorf("direct workflow operator name is required")
	}
	engineName := strings.TrimSpace(selector.EngineName)

	candidates := append([]models.Engine(nil), engines...)
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].IsBuiltin != candidates[j].IsBuiltin {
			return !candidates[i].IsBuiltin
		}
		return candidates[i].ID < candidates[j].ID
	})

	failures := make([]string, 0)
	workflowOnlyMatches := make([]string, 0)
	for _, candidate := range candidates {
		if !engineselection.IsAvailableForComputeEntrypoint(&candidate, "workflow") {
			continue
		}
		if engineName != "" && candidate.Name != engineName {
			continue
		}
		operators, err := ListWorkflowOperators(ctx, &candidate)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", candidate.Name, err))
			continue
		}
		for _, operator := range operators {
			if operator.Name != operatorName && operator.ID != operatorName {
				continue
			}
			if operatorSupportsExecutionMode(operator.ExecutionModes, "direct") {
				return candidate, operator, nil
			}
			workflowOnlyMatches = append(workflowOnlyMatches, candidate.Name)
		}
	}

	message := fmt.Sprintf("direct workflow operator %s is not available", operatorName)
	if len(workflowOnlyMatches) > 0 {
		message = fmt.Sprintf("workflow operator %s does not support direct invocation", operatorName)
		if engineName == "" {
			message = fmt.Sprintf("%s in matching workflow runtimes: %s", message, strings.Join(workflowOnlyMatches, ", "))
		}
	}
	if engineName != "" {
		message = fmt.Sprintf("%s in workflow engine %s", message, engineName)
	}
	if len(failures) > 0 {
		message = fmt.Sprintf("%s; runtime discovery failures: %s", message, strings.Join(failures, "; "))
	}
	return models.Engine{}, models.OperatorDescriptor{}, fmt.Errorf("%s", message)
}

// WorkflowRuntimeProviderForEngine resolves the provider from one registered
// Engine Instance. Standard addp.workflow/v1 runtimes use the generic HTTP
// provider and do not require a compiled plugin for their engine_type.
func WorkflowRuntimeProviderForEngine(engine *models.Engine) (plugin.WorkflowRuntimeProvider, error) {
	if engine == nil {
		return nil, fmt.Errorf("workflow engine cannot be nil")
	}
	if supportsADDPWorkflowRuntime(engine) {
		return plugin.NewHTTPWorkflowRuntimeProvider(engine.EngineType, engine.Name), nil
	}
	p, pluginErr := plugin.Get(engine.EngineType)
	if pluginErr == nil {
		if workflowProvider, ok := p.(plugin.WorkflowRuntimeProvider); ok {
			return workflowProvider, nil
		}
	}
	if pluginErr != nil {
		return nil, pluginErr
	}
	return nil, fmt.Errorf("plugin %s does not implement WorkflowRuntimeProvider", engine.EngineType)
}

// RequireDirectWorkflowOperators verifies that one runtime exposes every
// required operator and that each operator explicitly supports direct mode.
func RequireDirectWorkflowOperators(ctx context.Context, engine *models.Engine, operatorNames ...string) error {
	operators, err := ListWorkflowOperators(ctx, engine)
	if err != nil {
		return err
	}
	for _, operatorName := range operatorNames {
		operatorName = strings.TrimSpace(operatorName)
		if operatorName == "" {
			return fmt.Errorf("direct workflow operator name is required")
		}
		if err := ensureDirectOperator(operators, operatorName); err != nil {
			return err
		}
	}
	return nil
}

func scriptRuntimeProvider(engine *models.Engine) (plugin.ScriptRuntimeProvider, error) {
	if engine == nil {
		return nil, fmt.Errorf("script engine cannot be nil")
	}
	p, err := plugin.Get(engine.EngineType)
	if err != nil {
		return nil, err
	}
	scriptProvider, ok := p.(plugin.ScriptRuntimeProvider)
	if !ok {
		return nil, fmt.Errorf("plugin %s does not implement ScriptRuntimeProvider", engine.EngineType)
	}
	return scriptProvider, nil
}

func supportsADDPWorkflowRuntime(engine *models.Engine) bool {
	if engine == nil || engine.Capabilities == nil || *engine.Capabilities == "" {
		return false
	}
	capabilities, err := plugin.ParseEngineCapabilities(string(*engine.Capabilities))
	if err != nil || capabilities == nil {
		return false
	}
	if capabilities.SchemaVersion != plugin.CapabilitiesSchemaVersion {
		return false
	}
	if capabilities.EngineType != engine.EngineType {
		return false
	}
	if capabilities.Compute == nil || capabilities.Compute.Workflow == nil {
		return false
	}
	workflow := capabilities.Compute.Workflow
	return workflow.Supported && workflow.RuntimeAPI == plugin.WorkflowRuntimeAPIAddpV1
}

func ensureDirectOperator(operators []models.OperatorDescriptor, operatorName string) error {
	return ensureOperatorExecutionMode(operators, operatorName, "direct", "direct invocation")
}

func ensureWorkflowOperators(operators []models.OperatorDescriptor, workflowDef map[string]interface{}) error {
	tasksValue, ok := workflowDef["tasks"]
	if !ok {
		return fmt.Errorf("workflow definition tasks is required")
	}
	tasks, ok := workflowTasksFromInterface(tasksValue)
	if !ok || len(tasks) == 0 {
		return fmt.Errorf("workflow definition tasks must be a non-empty array")
	}
	normalizedTasks, err := normalizeWorkflowTasks(tasks)
	if err != nil {
		return err
	}
	if err := ensureWorkflowTaskGraph(normalizedTasks); err != nil {
		return err
	}
	for _, task := range normalizedTasks {
		if err := ensureOperatorExecutionMode(operators, task.operator, "workflow", "workflow execution"); err != nil {
			return err
		}
	}
	return ensureWorkflowReferences(operators, normalizedTasks)
}

type normalizedWorkflowTask struct {
	id        string
	operator  string
	params    map[string]interface{}
	dependsOn []string
}

func normalizeWorkflowTasks(tasks []map[string]interface{}) ([]normalizedWorkflowTask, error) {
	result := make([]normalizedWorkflowTask, 0, len(tasks))
	for i, task := range tasks {
		taskID, ok := task["id"].(string)
		taskID = strings.TrimSpace(taskID)
		if !ok || taskID == "" {
			return nil, fmt.Errorf("workflow definition tasks[%d].id is required", i)
		}
		operatorName, ok := task["operator"].(string)
		operatorName = strings.TrimSpace(operatorName)
		if !ok || operatorName == "" {
			return nil, fmt.Errorf("workflow definition tasks[%d].operator is required", i)
		}
		params, ok := task["params"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("workflow definition tasks[%d].params must be an object", i)
		}
		dependsOn, ok := workflowStringSlice(task["depends_on"])
		if !ok {
			return nil, fmt.Errorf("workflow definition tasks[%d].depends_on must be a string array", i)
		}
		result = append(result, normalizedWorkflowTask{
			id:        taskID,
			operator:  operatorName,
			params:    params,
			dependsOn: dependsOn,
		})
	}
	return result, nil
}

func workflowStringSlice(value interface{}) ([]string, bool) {
	switch items := value.(type) {
	case []interface{}:
		result := make([]string, 0, len(items))
		for _, item := range items {
			text, ok := item.(string)
			if !ok {
				return nil, false
			}
			result = append(result, text)
		}
		return result, true
	case []string:
		return items, true
	default:
		return nil, false
	}
}

func ensureWorkflowTaskGraph(tasks []normalizedWorkflowTask) error {
	taskIDs := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		if _, exists := taskIDs[task.id]; exists {
			return fmt.Errorf("workflow definition duplicate task id %q", task.id)
		}
		taskIDs[task.id] = struct{}{}
	}

	inDegree := make(map[string]int, len(tasks))
	children := make(map[string][]string, len(tasks))
	for _, task := range tasks {
		inDegree[task.id] = 0
	}
	for _, task := range tasks {
		seenDeps := map[string]struct{}{}
		for _, dep := range task.dependsOn {
			dep = strings.TrimSpace(dep)
			if dep == "" {
				return fmt.Errorf("workflow definition task %q has empty dependency", task.id)
			}
			if dep == task.id {
				return fmt.Errorf("workflow definition task %q depends on itself", task.id)
			}
			if _, ok := taskIDs[dep]; !ok {
				return fmt.Errorf("workflow definition task %q depends on missing task %q", task.id, dep)
			}
			if _, exists := seenDeps[dep]; exists {
				return fmt.Errorf("workflow definition task %q has duplicate dependency %q", task.id, dep)
			}
			seenDeps[dep] = struct{}{}
			children[dep] = append(children[dep], task.id)
			inDegree[task.id]++
		}
	}

	queue := make([]string, 0, len(tasks))
	for taskID, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, taskID)
		}
	}
	visited := 0
	for len(queue) > 0 {
		taskID := queue[0]
		queue = queue[1:]
		visited++
		for _, child := range children[taskID] {
			inDegree[child]--
			if inDegree[child] == 0 {
				queue = append(queue, child)
			}
		}
	}
	if visited != len(tasks) {
		return fmt.Errorf("workflow definition contains cycle")
	}
	return nil
}

func ensureWorkflowReferences(operators []models.OperatorDescriptor, tasks []normalizedWorkflowTask) error {
	taskByID := make(map[string]normalizedWorkflowTask, len(tasks))
	for _, task := range tasks {
		taskByID[task.id] = task
	}
	operatorByName := make(map[string]models.OperatorDescriptor, len(operators)*2)
	for _, operator := range operators {
		operatorByName[operator.Name] = operator
		operatorByName[operator.ID] = operator
	}

	for _, task := range tasks {
		dependencies := make(map[string]struct{}, len(task.dependsOn))
		for _, dep := range task.dependsOn {
			dependencies[dep] = struct{}{}
		}
		for _, ref := range collectWorkflowRefs(task.params) {
			refTask, ok := taskByID[ref.taskID]
			if !ok {
				return fmt.Errorf("workflow definition task %q references missing task %q", task.id, ref.taskID)
			}
			if _, ok := dependencies[ref.taskID]; !ok {
				return fmt.Errorf("workflow definition task %q references task %q but does not list it in depends_on", task.id, ref.taskID)
			}
			if ref.port != "" && ref.port != "default" {
				operator, ok := operatorByName[refTask.operator]
				if !ok {
					return fmt.Errorf("workflow operator %s is not available", refTask.operator)
				}
				if !operatorHasOutputPort(operator, ref.port) {
					return fmt.Errorf("workflow definition task %q references missing output port %q on task %q", task.id, ref.port, ref.taskID)
				}
			}
		}
	}
	return nil
}

type workflowRef struct {
	taskID string
	port   string
}

func collectWorkflowRefs(value interface{}) []workflowRef {
	refs := []workflowRef{}
	switch v := value.(type) {
	case map[string]interface{}:
		if rawRef, ok := v["$ref"].(string); ok && strings.TrimSpace(rawRef) != "" {
			ref := workflowRef{taskID: strings.TrimSpace(rawRef)}
			if rawPort, ok := v["port"].(string); ok {
				ref.port = strings.TrimSpace(rawPort)
			}
			refs = append(refs, ref)
			return refs
		}
		for _, item := range v {
			refs = append(refs, collectWorkflowRefs(item)...)
		}
	case []interface{}:
		for _, item := range v {
			refs = append(refs, collectWorkflowRefs(item)...)
		}
	}
	return refs
}

func operatorHasOutputPort(operator models.OperatorDescriptor, portName string) bool {
	for _, port := range operator.OutputPorts {
		if port.Name == portName {
			return true
		}
	}
	return false
}

func ensureOperatorExecutionMode(operators []models.OperatorDescriptor, operatorName, requiredMode, description string) error {
	for _, operator := range operators {
		if operator.Name != operatorName && operator.ID != operatorName {
			continue
		}
		if operatorSupportsExecutionMode(operator.ExecutionModes, requiredMode) {
			return nil
		}
		return fmt.Errorf("workflow operator %s does not support %s", operatorName, description)
	}
	return fmt.Errorf("workflow operator %s is not available", operatorName)
}

func operatorSupportsExecutionMode(modes []string, requiredMode string) bool {
	for _, mode := range modes {
		if mode == requiredMode {
			return true
		}
	}
	return false
}

func workflowTasksFromInterface(value interface{}) ([]map[string]interface{}, bool) {
	switch tasks := value.(type) {
	case []interface{}:
		result := make([]map[string]interface{}, 0, len(tasks))
		for _, item := range tasks {
			task, ok := item.(map[string]interface{})
			if !ok {
				return nil, false
			}
			result = append(result, task)
		}
		return result, true
	case []map[string]interface{}:
		return tasks, true
	default:
		return nil, false
	}
}

func toModelOperators(engine *models.Engine, operators []plugin.OperatorDescriptor) ([]models.OperatorDescriptor, error) {
	result := make([]models.OperatorDescriptor, 0, len(operators))
	for _, op := range operators {
		if err := validateWorkflowOperatorDescriptor(engine, op); err != nil {
			return nil, err
		}
		result = append(result, models.OperatorDescriptor{
			ID:                  op.ID,
			Name:                op.Name,
			DisplayName:         op.DisplayName,
			EngineType:          op.EngineType,
			Type:                op.Type,
			Category:            op.Category,
			CategoryPath:        op.CategoryPath,
			Description:         op.Description,
			BriefDescription:    op.BriefDescription,
			DetailedDescription: op.DetailedDescription,
			Parameters:          toModelParameters(op.Parameters),
			Inputs:              toStringInputs(op.Inputs),
			OutputPorts:         toModelOutputPorts(op.OutputPorts),
			ExecutionModes:      op.ExecutionModes,
			Effects:             op.Effects,
			Attributes:          op.Attributes,
		})
	}
	return result, nil
}

func validateWorkflowOperatorDescriptor(engine *models.Engine, op plugin.OperatorDescriptor) error {
	name := strings.TrimSpace(op.Name)
	if strings.TrimSpace(op.ID) == "" {
		return fmt.Errorf("workflow operator metadata invalid: id is required for operator %q", name)
	}
	if name == "" {
		return fmt.Errorf("workflow operator metadata invalid: name is required")
	}
	if strings.TrimSpace(op.DisplayName) == "" {
		return fmt.Errorf("workflow operator metadata invalid: display_name is required for operator %q", op.Name)
	}
	if strings.TrimSpace(op.EngineType) == "" {
		return fmt.Errorf("workflow operator metadata invalid: engine_type is required for operator %q", op.Name)
	}
	if op.EngineType != engine.EngineType {
		return fmt.Errorf("workflow operator metadata invalid: operator %q engine_type=%s does not match runtime engine_type=%s", op.Name, op.EngineType, engine.EngineType)
	}
	if strings.TrimSpace(op.Category) == "" {
		return fmt.Errorf("workflow operator metadata invalid: category is required for operator %q", op.Name)
	}
	if len(op.CategoryPath) == 0 {
		return fmt.Errorf("workflow operator metadata invalid: category_path is required for operator %q", op.Name)
	}
	for _, item := range op.CategoryPath {
		if strings.TrimSpace(item) == "" {
			return fmt.Errorf("workflow operator metadata invalid: category_path contains empty item for operator %q", op.Name)
		}
	}
	if strings.TrimSpace(op.Description) == "" {
		return fmt.Errorf("workflow operator metadata invalid: description is required for operator %q", op.Name)
	}
	if op.Parameters == nil {
		return fmt.Errorf("workflow operator metadata invalid: parameters is required for operator %q", op.Name)
	}
	if op.OutputPorts == nil {
		return fmt.Errorf("workflow operator metadata invalid: output_ports is required for operator %q", op.Name)
	}
	if len(op.ExecutionModes) == 0 {
		return fmt.Errorf("workflow operator metadata invalid: execution_modes is required for operator %q", op.Name)
	}
	for _, mode := range op.ExecutionModes {
		if mode != "workflow" && mode != "direct" {
			return fmt.Errorf("workflow operator metadata invalid: unsupported execution mode %q for operator %q", mode, op.Name)
		}
	}
	if len(op.Effects) == 0 {
		return fmt.Errorf("workflow operator metadata invalid: effects is required for operator %q", op.Name)
	}
	seenEffects := make(map[string]struct{}, len(op.Effects))
	for _, effect := range op.Effects {
		switch effect {
		case "read", "write", "ddl", "external_effect":
		default:
			return fmt.Errorf("workflow operator metadata invalid: unsupported effect %q for operator %q", effect, op.Name)
		}
		if _, exists := seenEffects[effect]; exists {
			return fmt.Errorf("workflow operator metadata invalid: duplicated effect %q for operator %q", effect, op.Name)
		}
		seenEffects[effect] = struct{}{}
	}
	return nil
}

func toModelParameters(parameters []plugin.ParameterDescriptor) []models.ParameterDescriptor {
	result := make([]models.ParameterDescriptor, 0, len(parameters))
	for _, param := range parameters {
		result = append(result, models.ParameterDescriptor{
			Name:        param.Name,
			DisplayName: param.DisplayName,
			Type:        param.Type,
			ParamType:   param.ParamType,
			Required:    param.Required,
			Default:     param.Default,
			Description: param.Description,
			Enum:        param.Enum,
			Min:         param.Min,
			Max:         param.Max,
			Pattern:     param.Pattern,
			ItemType:    param.ItemType,
			Properties:  toModelParameterMap(param.Properties),
			DependsOn:   param.DependsOn,
			ShowWhen:    param.ShowWhen,
			Notes:       param.Notes,
			UIType:      param.UIType,
			UIConfig:    param.UIConfig,
		})
	}
	return result
}

func toModelParameterMap(parameters map[string]plugin.ParameterDescriptor) map[string]models.ParameterDescriptor {
	if len(parameters) == 0 {
		return nil
	}
	result := make(map[string]models.ParameterDescriptor, len(parameters))
	for name, param := range parameters {
		result[name] = models.ParameterDescriptor{
			Name:        param.Name,
			DisplayName: param.DisplayName,
			Type:        param.Type,
			ParamType:   param.ParamType,
			Required:    param.Required,
			Default:     param.Default,
			Description: param.Description,
			Enum:        param.Enum,
			Min:         param.Min,
			Max:         param.Max,
			Pattern:     param.Pattern,
			ItemType:    param.ItemType,
			Properties:  toModelParameterMap(param.Properties),
			DependsOn:   param.DependsOn,
			ShowWhen:    param.ShowWhen,
			Notes:       param.Notes,
			UIType:      param.UIType,
			UIConfig:    param.UIConfig,
		}
	}
	return result
}

func toStringInputs(inputs []interface{}) []string {
	if len(inputs) == 0 {
		return nil
	}
	result := make([]string, 0, len(inputs))
	for _, input := range inputs {
		switch v := input.(type) {
		case string:
			result = append(result, v)
		case map[string]interface{}:
			if name, _ := v["name"].(string); name != "" {
				result = append(result, name)
			} else if typ, _ := v["type"].(string); typ != "" {
				result = append(result, typ)
			}
		default:
			if text := fmt.Sprintf("%v", v); text != "" {
				result = append(result, text)
			}
		}
	}
	return result
}

func toModelOutputPorts(ports []plugin.OutputPortDescriptor) []models.OutputPortDescriptor {
	result := make([]models.OutputPortDescriptor, 0, len(ports))
	for _, port := range ports {
		result = append(result, models.OutputPortDescriptor{
			Name:        port.Name,
			Type:        port.Type,
			Description: port.Description,
			IsDefault:   port.IsDefault,
		})
	}
	return result
}
