package dbbridge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
	"github.com/addp/common/sqldialect"
	"github.com/beltran/gohive"
	"gorm.io/gorm"

	// 导入所有内置引擎插件，触发 init() 注册
	_ "github.com/addp/common/engine/plugins/builtin/all"
)

// BuildDSN 使用插件系统构建连接字符串
func BuildDSN(engine *models.Engine) (string, error) {
	return plugin.BuildDSN(toPluginEngine(engine))
}

// TestConnection 使用插件系统测试连接
func TestConnection(ctx context.Context, engine *models.Engine) error {
	if engine == nil {
		return fmt.Errorf("engine cannot be nil")
	}
	if _, err := plugin.Get(engine.EngineType); err == nil {
		return plugin.TestConnection(ctx, toPluginEngine(engine))
	}
	if supportsADDPWorkflowRuntime(engine) {
		workflowProvider, err := workflowRuntimeProvider(engine)
		if err != nil {
			return err
		}
		return workflowProvider.TestConnection(ctx, plugin.ConnectionInfo(engine.ConnectionInfo))
	}
	return plugin.TestConnection(ctx, toPluginEngine(engine))
}

// ProbeWorkflowRuntimeContract validates the addp.workflow/v1 control-plane
// contract exposed by a workflow runtime before it is saved or used.
func ProbeWorkflowRuntimeContract(ctx context.Context, engine *models.Engine) (int, error) {
	workflowProvider, err := workflowRuntimeProvider(engine)
	if err != nil {
		return 0, err
	}
	if err := workflowProvider.TestConnection(ctx, plugin.ConnectionInfo(engine.ConnectionInfo)); err != nil {
		return 0, err
	}
	operators, err := ListWorkflowOperators(ctx, engine)
	if err != nil {
		return 0, err
	}
	return len(operators), nil
}

// GenerateCapabilities 使用插件系统生成结构化能力声明 JSON
func GenerateCapabilities(engineType string) (string, error) {
	return plugin.GenerateCapabilities(engineType)
}

// GenerateResolvedCapabilities 使用插件系统生成具体引擎实例的结构化能力声明 JSON。
func GenerateResolvedCapabilities(ctx context.Context, engine *models.Engine) (string, error) {
	if engine == nil {
		return "", fmt.Errorf("engine cannot be nil")
	}
	return plugin.GenerateResolvedCapabilities(ctx, toPluginEngine(engine))
}

// GetSensitiveFields 获取敏感字段列表
func GetSensitiveFields(engineType string) ([]string, error) {
	return plugin.GetSensitiveFields(engineType)
}

// GetRequiredFields 获取必填字段列表
func GetRequiredFields(engineType string) ([]string, error) {
	return plugin.GetRequiredFields(engineType)
}

// GetDefaultPort 获取默认端口
func GetDefaultPort(engineType string) (int, error) {
	return plugin.GetDefaultPort(engineType)
}

// ListAllTypes 列出所有已注册的数据库类型
func ListAllTypes() []string {
	return plugin.List()
}

// CatalogModel 获取引擎插件声明的 catalog model。
func CatalogModel(engineType string) (plugin.CatalogModelSpec, error) {
	p, err := plugin.Get(engineType)
	if err != nil {
		return plugin.CatalogModelSpec{}, err
	}
	modelProvider, ok := p.(plugin.CatalogModelProvider)
	if !ok {
		return plugin.CatalogModelSpec{}, fmt.Errorf("plugin %s does not implement CatalogModelProvider", engineType)
	}
	return modelProvider.CatalogModel(), nil
}

// GetAllPlugins 获取所有插件信息（用于前端API）
func GetAllPlugins() map[string]PluginInfo {
	plugins := plugin.GetAll()
	result := make(map[string]PluginInfo)

	for dbType, p := range plugins {
		result[dbType] = PluginInfo{
			Type:            p.Type(),
			DisplayName:     p.DisplayName(),
			Origin:          p.EngineOrigin(),
			DefaultPort:     p.DefaultPort(),
			RequiredFields:  p.RequiredFields(),
			SensitiveFields: p.SensitiveFields(),
		}
	}

	return result
}

// PluginInfo 插件信息（用于API响应）
type PluginInfo struct {
	Type            string   `json:"type"`
	DisplayName     string   `json:"display_name"`
	Origin          string   `json:"origin"`
	DefaultPort     int      `json:"default_port"`
	RequiredFields  []string `json:"required_fields"`
	SensitiveFields []string `json:"sensitive_fields"`
}

// === 连接池管理方法（供Develop模块使用）===

// GetOrCreatePool 获取或创建连接池
// 这是推荐的获取连接池的方式，会自动管理连接池的生命周期
func GetOrCreatePool(engine *models.Engine, config *plugin.PoolConfig) (*gorm.DB, error) {
	return plugin.GetOrCreatePoolFromFactory(toPluginEngine(engine), config)
}

// DefaultPoolConfig 返回默认连接池配置
func DefaultPoolConfig() *plugin.PoolConfig {
	return plugin.DefaultPoolConfig()
}

// ClosePool 关闭指定引擎的连接池
// 通常在引擎被删除或更新时调用
func ClosePool(engineID uint) error {
	return plugin.ClosePool(engineID)
}

// CloseAllPools 关闭所有连接池
// 在应用关闭时调用，确保优雅关闭
func CloseAllPools() {
	plugin.CloseAllPools()
}

// GetPoolStats 获取所有连接池的统计信息
func GetPoolStats() map[uint]plugin.PoolStats {
	return plugin.GetPoolStats()
}

// === Catalog / facts 查询方法 ===

func toPluginEngine(engine *models.Engine) *plugin.Engine {
	pluginEngine := &plugin.Engine{
		ID:             engine.ID,
		EngineType:     engine.EngineType,
		ConnectionInfo: plugin.ConnectionInfo(engine.ConnectionInfo),
	}
	return pluginEngine
}

// ListWorkflowOperators 通过工作流运行时 Provider 动态发现算子。
func ListWorkflowOperators(ctx context.Context, engine *models.Engine) ([]models.OperatorDescriptor, error) {
	workflowProvider, err := workflowRuntimeProvider(engine)
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
	workflowProvider, err := workflowRuntimeProvider(engine)
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
	workflowProvider, err := workflowRuntimeProvider(engine)
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
	workflowProvider, err := workflowRuntimeProvider(engine)
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
		if candidate.LifecycleState != models.EngineLifecycleActive {
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

func workflowRuntimeProvider(engine *models.Engine) (plugin.WorkflowRuntimeProvider, error) {
	if engine == nil {
		return nil, fmt.Errorf("workflow engine cannot be nil")
	}
	p, pluginErr := plugin.Get(engine.EngineType)
	if pluginErr == nil {
		if workflowProvider, ok := p.(plugin.WorkflowRuntimeProvider); ok {
			return workflowProvider, nil
		}
	}
	if supportsADDPWorkflowRuntime(engine) {
		return plugin.NewHTTPWorkflowRuntimeProvider(engine.EngineType, engine.Name), nil
	}
	if pluginErr != nil {
		return nil, pluginErr
	}
	return nil, fmt.Errorf("plugin %s does not implement WorkflowRuntimeProvider", engine.EngineType)
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

// ListCatalogChildren 列出指定 catalog 路径下的实时子节点。
func ListCatalogChildren(ctx context.Context, engine *models.Engine, parent plugin.CatalogPath, opts plugin.ListOptions) ([]plugin.CatalogEntry, error) {
	pluginEngine := toPluginEngine(engine)

	p, err := plugin.Get(pluginEngine.EngineType)
	if err != nil {
		return nil, err
	}
	modelProvider, ok := p.(plugin.CatalogModelProvider)
	if !ok {
		return nil, plugin.WrapCatalogError(plugin.CatalogErrorUnsupported,
			fmt.Errorf("plugin %s does not implement CatalogModelProvider", pluginEngine.EngineType))
	}
	if len(parent.Segments) == 0 {
		return []plugin.CatalogEntry{
			plugin.CatalogRootEntry(modelProvider.CatalogModel(), engine.ID, engine.Name),
		}, nil
	}
	if parent.Version == "" {
		parent.Version = plugin.CatalogPathVersion
	}
	if parent.EngineID == 0 {
		parent.EngineID = pluginEngine.ID
	}
	catalogProvider, ok := p.(plugin.CatalogProvider)
	if !ok {
		return nil, plugin.WrapCatalogError(plugin.CatalogErrorUnsupported,
			fmt.Errorf("plugin %s does not implement CatalogProvider", pluginEngine.EngineType))
	}
	return catalogProvider.ListChildren(ctx, pluginEngine.ConnectionInfo, parent, opts)
}

// DescribeCatalogFacts 描述 catalog leaf 的 engine-native facts。
func DescribeCatalogFacts(ctx context.Context, engine *models.Engine, path plugin.CatalogPath, opts plugin.CatalogFactsOptions) (*plugin.CatalogFacts, error) {
	return plugin.DescribeCatalogFacts(ctx, toPluginEngine(engine), path, opts)
}

// CountCatalogItemRows 获取 tabular catalog leaf 的行数。
func CountCatalogItemRows(ctx context.Context, engine *models.Engine, path plugin.CatalogPath) (int64, error) {
	return plugin.CountCatalogItemRows(ctx, toPluginEngine(engine), path)
}

// ============ 统一查询执行 ============

// SupportsDirectQuery 检查引擎是否实现了非 SQL 原生查询运行时（MongoDB/Neo4j 等）
func SupportsDirectQuery(engineType string) bool {
	p, err := plugin.Get(engineType)
	if err != nil {
		return false
	}
	if _, ok := p.(plugin.SQLQueryRuntimeProvider); ok {
		return false
	}
	if qp, ok := p.(plugin.QueryRuntimeProvider); ok {
		if _, isSQLRuntime := qp.(plugin.SQLQueryRuntimeProvider); !isSQLRuntime {
			return true
		}
	}
	if _, ok := p.(plugin.GraphQueryProvider); ok {
		return true
	}
	return false
}

var ErrSampleQueryUnavailable = errors.New("当前引擎没有可生成样例查询的真实数据")

// ExecutableSampleQueryOptions separates the query returned to the caller from
// the bounded query used to validate it. QueryLimit only applies to generated
// SQL; ValidationLimit never changes the returned query.
type ExecutableSampleQueryOptions struct {
	QueryLimit      int
	ValidationLimit int
}

// GenerateSampleQuery 从当前引擎的实时 Catalog 生成一个可直接执行的样例查询。
func GenerateSampleQuery(ctx context.Context, engine *models.Engine, queryLimit int) (query string, language string, err error) {
	if engine == nil {
		return "", "", fmt.Errorf("%w: 引擎不能为空", ErrSampleQueryUnavailable)
	}
	engineType := strings.ToLower(engine.EngineType)

	sampleCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	p, err := plugin.Get(engineType)
	if err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrSampleQueryUnavailable, err)
	}
	connInfo := plugin.ConnectionInfo(engine.ConnectionInfo)

	if _, ok := p.(plugin.SQLQueryRuntimeProvider); ok {
		cp, ok := p.(plugin.CatalogProvider)
		if !ok {
			return "", "", fmt.Errorf("%w: 引擎未提供实时 Catalog", ErrSampleQueryUnavailable)
		}
		q, catalogErr := generateCatalogSampleQuery(sampleCtx, p, cp, connInfo, engine.ID, engineType, queryLimit)
		if catalogErr != nil {
			return "", "", catalogErr
		}
		return q, "sql", nil
	}

	qp, ok := p.(plugin.QueryRuntimeProvider)
	if !ok {
		return "", "", fmt.Errorf("%w: 引擎未提供查询运行时", ErrSampleQueryUnavailable)
	}
	q, language := qp.GenerateSampleQuery(sampleCtx, connInfo, plugin.SampleQueryOptions{})
	if strings.TrimSpace(q) == "" || strings.TrimSpace(language) == "" {
		return "", "", ErrSampleQueryUnavailable
	}
	return q, language, nil
}

// GenerateExecutableSampleQuery returns a real catalog-based sample only after
// the same read-only runtime path has produced at least one row.
func GenerateExecutableSampleQuery(
	ctx context.Context,
	engine *models.Engine,
	requiredLanguage string,
	options ExecutableSampleQueryOptions,
) (string, string, error) {
	query, language, err := GenerateSampleQuery(ctx, engine, options.QueryLimit)
	if err != nil {
		return "", "", err
	}
	if requiredLanguage != "" && !strings.EqualFold(language, requiredLanguage) {
		return "", "", fmt.Errorf("%w: 查询语言 %s 不受当前入口支持", ErrSampleQueryUnavailable, language)
	}
	validationQuery := query
	if options.ValidationLimit > 0 && strings.EqualFold(language, "sql") {
		validationQuery = sqldialect.PaginateQuerySQL(query, options.ValidationLimit, 0)
	}
	result, err := ExecuteReadOnlyRuntimeQuery(ctx, engine, language, validationQuery)
	if err != nil {
		return "", "", fmt.Errorf("%w: 样例查询执行失败: %v", ErrSampleQueryUnavailable, err)
	}
	if result == nil || len(result.Rows) == 0 {
		return "", "", fmt.Errorf("%w: 样例查询没有返回数据", ErrSampleQueryUnavailable)
	}
	return query, language, nil
}

func generateCatalogSampleQuery(ctx context.Context, enginePlugin plugin.EnginePlugin, cp plugin.CatalogProvider, connInfo plugin.ConnectionInfo, engineID uint, engineType string, queryLimit int) (string, error) {
	modelProvider, ok := enginePlugin.(plugin.CatalogModelProvider)
	if !ok {
		return "", fmt.Errorf("%w: 引擎未声明 Catalog 模型", ErrSampleQueryUnavailable)
	}
	model := modelProvider.CatalogModel()
	if plugin.CatalogLeafTerm(model) != plugin.CatalogTermTable {
		return "", fmt.Errorf("%w: Catalog leaf 不是表", ErrSampleQueryUnavailable)
	}

	namespaces, err := cp.ListChildren(ctx, connInfo, plugin.CatalogRootPath(model, engineID), plugin.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("%w: 列出 Catalog namespace 失败: %v", ErrSampleQueryUnavailable, err)
	}

	resource := &plugin.Engine{ID: engineID, EngineType: engineType, ConnectionInfo: connInfo}
	foundTable := false
	for _, namespace := range namespaces {
		if namespace.Role != plugin.CatalogRoleBranch {
			continue
		}

		items, err := cp.ListChildren(ctx, connInfo, namespace.Path, plugin.ListOptions{})
		if err != nil {
			return "", fmt.Errorf("%w: 列出 Catalog leaf 失败: %v", ErrSampleQueryUnavailable, err)
		}

		for _, item := range items {
			if item.Role != plugin.CatalogRoleLeaf {
				continue
			}
			foundTable = true
			if catalogEntryRowCount(item) > 0 {
				return tableSampleSQL(engineType, namespace.Name, item.Name, queryLimit), nil
			}
			count, countErr := plugin.CountCatalogItemRows(ctx, resource, item.Path)
			if countErr == nil && count > 0 {
				return tableSampleSQL(engineType, namespace.Name, item.Name, queryLimit), nil
			}
		}
	}

	if foundTable {
		return "", fmt.Errorf("%w: Catalog 中没有有数据的表", ErrSampleQueryUnavailable)
	}
	return "", fmt.Errorf("%w: Catalog 中没有表", ErrSampleQueryUnavailable)
}

func tableSampleSQL(engineType, namespace, table string, limit int) string {
	return sqldialect.SelectAllSampleSQL(engineType, namespace, table, limit)
}

func catalogEntryRowCount(entry plugin.CatalogEntry) int64 {
	if entry.Table == nil || entry.Table.RowCount == nil {
		return 0
	}
	return *entry.Table.RowCount
}

// ExecuteQuery 统一查询执行入口（适用于所有引擎类型）
//
// 路由规则（按优先级）：
//  1. 引擎实现了 QueryRuntimeProvider（MongoDB/Neo4j）→ 委托给插件原生执行
//  2. engineType == "spark" → gohive Thrift 协议执行
//  3. 其他 SQL 引擎（PostgreSQL/MySQL/Doris/ClickHouse）→ GORM 连接池执行
func ExecuteQuery(ctx context.Context, engine *models.Engine, query string) (*plugin.QueryResult, error) {
	engineType := strings.ToLower(engine.EngineType)
	queryOptions := plugin.QueryOptions{
		EngineID:   engine.ID,
		EngineType: engine.EngineType,
	}

	// 1. 原生查询运行时（MongoDB MQL、Neo4j Cypher 等）
	p, err := plugin.Get(engineType)
	if err == nil {
		if qp, ok := p.(plugin.QueryRuntimeProvider); ok {
			if _, isSQLRuntime := qp.(plugin.SQLQueryRuntimeProvider); !isSQLRuntime {
				return qp.ExecuteRuntimeQuery(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), plugin.QueryRequest{
					Language: firstQueryLanguage(qp.QueryLanguages()),
					Query:    query,
					Options:  queryOptions,
				})
			}
		}
	}

	// 2. Spark SQL（gohive Thrift 协议）
	if engineType == "spark" {
		return executeSparkQuery(ctx, engine, query)
	}

	// 3. 标准 SQL 运行时。当前通过 QueryOptions 传入 engine 上下文，以便复用连接池。
	if p != nil {
		if sqlRuntime, ok := p.(plugin.SQLQueryRuntimeProvider); ok {
			return sqlRuntime.ExecuteSQL(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), query, queryOptions)
		}
	}

	// 4. 标准 SQL 兜底（GORM 连接池）
	return executeSQLQuery(ctx, engine, query)
}

// ExecuteGraphQuery 统一图查询执行入口
// 对支持 GraphQueryProvider 的引擎（Neo4j 等）同时返回表格数据和图结构数据（节点/关系）
// 对其他引擎回退到 ExecuteQuery 并包装结果（GraphData 为 nil）
func ExecuteGraphQuery(ctx context.Context, engine *models.Engine, query string) (*plugin.GraphQueryResult, error) {
	engineType := strings.ToLower(engine.EngineType)

	p, err := plugin.Get(engineType)
	if err == nil {
		if gqp, ok := p.(plugin.GraphQueryProvider); ok {
			return gqp.ExecuteGraphQuery(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), query, plugin.QueryOptions{
				EngineID:   engine.ID,
				EngineType: engine.EngineType,
			})
		}
	}

	// 回退：普通查询，无图数据
	qr, err := ExecuteQuery(ctx, engine, query)
	if err != nil {
		return nil, err
	}
	return &plugin.GraphQueryResult{QueryResult: *qr}, nil
}

// SupportsReadOnlySQLExecution reports whether dbbridge can establish a real
// database read-only transaction for the engine. Unsupported engines must be
// rejected instead of falling back to an ordinary privileged connection.
func SupportsReadOnlySQLExecution(engineType string) bool {
	switch strings.ToLower(strings.TrimSpace(engineType)) {
	case "postgresql", "mysql", "doris", "spark":
		return true
	default:
		return false
	}
}

// ExecuteReadOnlyQuery executes one SQL query in a database-enforced read-only
// transaction. It is the only dbbridge path for User executions classified as
// read.
func ExecuteReadOnlyQuery(ctx context.Context, engine *models.Engine, query string) (*plugin.QueryResult, error) {
	if engine == nil || !SupportsReadOnlySQLExecution(engine.EngineType) {
		return nil, fmt.Errorf("引擎不支持受控只读 SQL 执行")
	}
	if strings.EqualFold(engine.EngineType, "spark") || strings.EqualFold(engine.EngineType, "doris") {
		p, err := plugin.Get(engine.EngineType)
		if err != nil {
			return nil, err
		}
		sqlRuntime, ok := p.(plugin.SQLQueryRuntimeProvider)
		if !ok {
			return nil, fmt.Errorf("%s 引擎未提供 SQL 查询运行时", engine.EngineType)
		}
		return sqlRuntime.ExecuteSQL(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), query, plugin.QueryOptions{
			EngineID: engine.ID, EngineType: engine.EngineType, ReadOnly: true,
		})
	}
	db, err := GetOrCreatePool(engine, DefaultPoolConfig())
	if err != nil {
		return nil, fmt.Errorf("获取连接池失败：%w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("获取数据库连接失败：%w", err)
	}
	tx, err := sqlDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("开启只读事务失败：%w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	result, err := scanSQLRows(rows)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("提交只读事务失败：%w", err)
	}
	committed = true
	return result, nil
}

// ExecuteReadOnlyRuntimeQuery executes a non-SQL query through the engine's
// native QueryRuntimeProvider. The provider must enforce QueryOptions.ReadOnly.
func ExecuteReadOnlyRuntimeQuery(ctx context.Context, engine *models.Engine, language, query string) (*plugin.QueryResult, error) {
	if engine == nil {
		return nil, fmt.Errorf("引擎不能为空")
	}
	p, err := plugin.Get(engine.EngineType)
	if err != nil {
		return nil, err
	}
	qp, ok := p.(plugin.QueryRuntimeProvider)
	if !ok {
		return nil, fmt.Errorf("引擎不支持普通查询运行时")
	}
	language = strings.ToLower(strings.TrimSpace(language))
	if language == "" || !slices.Contains(qp.QueryLanguages(), language) {
		return nil, fmt.Errorf("引擎不支持查询语言: %s", language)
	}
	return qp.ExecuteRuntimeQuery(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), plugin.QueryRequest{
		Language: language,
		Query:    query,
		Options: plugin.QueryOptions{
			EngineID: engine.ID, EngineType: engine.EngineType, ReadOnly: true,
		},
	})
}

// ExecuteStatement executes one non-read SQL statement and returns affected
// rows. The caller must classify and authorize the statement effect first.
func ExecuteStatement(ctx context.Context, engine *models.Engine, query string) (int64, error) {
	db, err := GetOrCreatePool(engine, DefaultPoolConfig())
	if err != nil {
		return 0, fmt.Errorf("获取连接池失败：%w", err)
	}
	result := db.WithContext(ctx).Exec(query)
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// executeSQLQuery 标准 SQL 引擎执行（PostgreSQL/MySQL/Doris/ClickHouse），使用 GORM 连接池
func executeSQLQuery(ctx context.Context, engine *models.Engine, query string) (*plugin.QueryResult, error) {
	db, err := GetOrCreatePool(engine, DefaultPoolConfig())
	if err != nil {
		return nil, fmt.Errorf("获取连接池失败：%w", err)
	}

	rows, err := db.WithContext(ctx).Raw(query).Rows()
	if err != nil {
		return nil, err
	}
	return scanSQLRows(rows)
}

func scanSQLRows(rows *sql.Rows) (*plugin.QueryResult, error) {
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("获取列名失败：%w", err)
	}

	var resultRows []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("扫描行失败：%w", err)
		}
		row := make(map[string]interface{}, len(columns))
		for i, col := range columns {
			val := values[i]
			if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
		}
		resultRows = append(resultRows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历结果失败：%w", err)
	}

	return &plugin.QueryResult{Columns: columns, Rows: resultRows}, nil
}

func firstQueryLanguage(languages []string) string {
	if len(languages) == 0 {
		return ""
	}
	return languages[0]
}

// executeSparkQuery 通过 gohive Thrift 协议执行 Spark SQL
// 逻辑从 develop/backend/internal/service/sql_engine_service.go 迁移而来
func executeSparkQuery(ctx context.Context, engine *models.Engine, query string) (*plugin.QueryResult, error) {
	connInfo := engine.ConnectionInfo

	host, _ := connInfo["host"].(string)
	host = strings.TrimPrefix(strings.TrimPrefix(host, "http://"), "https://")

	portRaw := connInfo["port"]
	var port int
	switch v := portRaw.(type) {
	case float64:
		port = int(v)
	case int:
		port = v
	case string:
		port, _ = strconv.Atoi(v)
	}
	if port == 0 {
		port = 10000
	}

	database, _ := connInfo["database"].(string)
	if database == "" {
		database = "default"
	}
	user, _ := connInfo["user"].(string)
	password, _ := connInfo["password"].(string)

	if host == "" {
		return nil, fmt.Errorf("Spark 引擎缺少 host 配置")
	}

	configuration := gohive.NewConnectConfiguration()
	if user != "" {
		configuration.Username = user
		if password != "" {
			configuration.Password = password
		}
	}
	configuration.ConnectTimeout = 30 * time.Second
	configuration.SocketTimeout = 30 * time.Second

	connection, err := gohive.Connect(host, port, "NONE", configuration)
	if err != nil {
		return nil, fmt.Errorf("连接 Spark Thrift Server 失败：%w", err)
	}
	defer connection.Close()

	cursor := connection.Cursor()

	if database != "default" && database != "" {
		cursor.Exec(ctx, fmt.Sprintf("USE `%s`", database))
		if cursor.Err != nil {
			return nil, fmt.Errorf("切换数据库失败：%w", cursor.Err)
		}
	}

	cursor.Exec(ctx, query)
	if cursor.Err != nil {
		return nil, fmt.Errorf("执行 Spark SQL 失败：%w", cursor.Err)
	}

	var resultRows []map[string]interface{}
	var columns []string

	for cursor.HasMore(ctx) {
		row := cursor.RowMap(ctx)
		if cursor.Err != nil {
			return nil, fmt.Errorf("读取 Spark 结果失败：%w", cursor.Err)
		}
		if len(columns) == 0 {
			for k := range row {
				columns = append(columns, k)
			}
		}
		resultRows = append(resultRows, row)
	}

	return &plugin.QueryResult{Columns: columns, Rows: resultRows}, nil
}
