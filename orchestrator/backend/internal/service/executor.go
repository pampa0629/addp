package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/taskprovider"
	"github.com/addp/orchestrator/internal/models"
)

// Executor 编排执行器
type Executor struct {
	executionService     *ExecutionService
	taskProviderResolver *TaskProviderResolver
	serviceTokens        commonClient.ServiceTokenProvider
}

// NewExecutor 创建执行器
func NewExecutor(
	executionService *ExecutionService,
	taskProviderResolver *TaskProviderResolver,
	serviceTokens commonClient.ServiceTokenProvider,
) *Executor {
	return &Executor{
		executionService:     executionService,
		taskProviderResolver: taskProviderResolver,
		serviceTokens:        serviceTokens,
	}
}

// submitTaskProviderStep 通过 TaskProvider API 执行步骤（模式二：任务引用）
func (e *Executor) submitTaskProviderStep(ctx context.Context, step *models.Step, resolvedParams map[string]interface{}, start time.Time, parentExecutionID string, triggerType string, tenantID int) (models.StepResult, error) {
	result := models.StepResult{StartedAt: start, Status: "running", Phase: "dispatching", ErrorCode: "orchestrator.execution.contract_invalid"}

	// 1. 从 System 模块控制面动态解析 TaskProvider 声明和当前 Backend
	provider, err := e.taskProviderResolver.GetProvider(ctx, step.Provider)
	if err != nil {
		result.Status = "failed"
		result.ErrorCode = "orchestrator.execution.provider_unavailable"
		result.Error = fmt.Sprintf("获取任务提供者 %s 失败: %v", step.Provider, err)
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, fmt.Errorf("%s", result.Error)
	}
	if err := validateProviderTaskCapability(provider, step); err != nil {
		result.Status = "failed"
		result.Error = err.Error()
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, fmt.Errorf("%s", result.Error)
	}
	contract, err := e.taskProviderResolver.GetTaskExecutionContract(ctx, provider, step.TaskType, step.TaskID, uint(tenantID))
	if err != nil {
		result.Status = "failed"
		result.ErrorCode = "orchestrator.execution.contract_unavailable"
		result.Error = fmt.Sprintf("获取任务执行契约失败: %v", err)
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, fmt.Errorf("%s", result.Error)
	}
	if err := validateProviderStepExecutable(provider, step, contract, resolvedParams); err != nil {
		result.Status = "failed"
		result.Error = err.Error()
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, fmt.Errorf("%s", result.Error)
	}

	// 2. 构建执行 URL（替换 {task_type} 和 {id} 占位符）
	taskIDStr := fmt.Sprintf("%d", step.TaskID)
	executeEndpoint := replaceTaskProviderEndpoint(provider.TaskExecuteEndpoint, step.TaskType, taskIDStr, "")
	targetURL := provider.ResolvedBaseURL + executeEndpoint

	// 3. 构建请求体
	normalizedTriggerType, err := commonExecution.NormalizeTriggerType(triggerType)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("非法触发类型: %v", err)
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, fmt.Errorf("%s", result.Error)
	}
	reqBody := map[string]interface{}{
		"trigger_type":        normalizedTriggerType,
		"source":              commonExecution.ModuleOrchestrator,
		"parent_execution_id": parentExecutionID,
		"parameters":          resolvedParams,
	}
	bodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("序列化请求失败: %v", err)
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, fmt.Errorf("%s", result.Error)
	}

	// 4. 发送 POST 请求触发执行
	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(bodyJSON))
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("创建 HTTP 请求失败: %v", err)
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, fmt.Errorf("%s", result.Error)
	}
	req.Header.Set("Content-Type", "application/json")
	token, err := e.serviceToken(ctx, tenantID)
	if err != nil {
		result.Status = "failed"
		result.ErrorCode = "orchestrator.execution.service_auth_unavailable"
		result.Error = fmt.Sprintf("获取服务访问令牌失败: %v", err)
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, fmt.Errorf("%s", result.Error)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	lease, ok := commonExecution.LeaseFromContext(ctx)
	if !ok || e.executionService == nil || lease.ExecutionID != parentExecutionID || lease.TenantID != tenantID {
		return result, fmt.Errorf("orchestrator submission requires an active lease")
	}
	if _, err := e.executionService.OwnedExecution(ctx, lease); err != nil {
		return result, err
	}
	result.ErrorCode = "orchestrator.execution.dispatch_uncertain"
	httpClient := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := httpClient.Do(req)
	if err != nil {
		result.Status = "failed"
		result.Error = "下游请求的提交结果不确定"
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, fmt.Errorf("%s", result.Error)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Status = "failed"
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			result.ErrorCode = "orchestrator.execution.dispatch_rejected"
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			result.ErrorCode = "orchestrator.execution.dispatch_denied"
		}
		result.Error = fmt.Sprintf("下游任务入口返回状态 %d", resp.StatusCode)
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, fmt.Errorf("%s", result.Error)
	}

	var respData map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		result.Status = "failed"
		result.Error = "下游响应无效，提交结果不确定"
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, fmt.Errorf("%s", result.Error)
	}

	// 5. 提取 execution_id
	executionID := extractProviderExecutionID(respData)
	if executionID == "" {
		result.Status = "failed"
		result.Error = "下游响应缺少执行身份，提交结果不确定"
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, fmt.Errorf("%s", result.Error)
	}

	result.Status = "running"
	result.Phase = "waiting"
	result.ErrorCode = ""
	result.Result = map[string]interface{}{"execution_id": executionID}
	return result, nil
}

func validateProviderTaskCapability(provider *commonModels.TaskProvider, step *models.Step) error {
	taskTypeCapability, err := providerTaskCapability(provider, step.TaskType)
	if err != nil {
		return fmt.Errorf("provider %q capabilities invalid: %w", step.Provider, err)
	}
	if taskTypeCapability == nil {
		return fmt.Errorf("task_type %q is not declared by provider %q", step.TaskType, step.Provider)
	}
	if taskTypeCapability.Deprecated {
		return fmt.Errorf("task_type %q of provider %q is deprecated", step.TaskType, step.Provider)
	}
	return nil
}

func validateProviderStepExecutable(provider *commonModels.TaskProvider, step *models.Step, contract *taskprovider.ExecutionContract, resolvedParams map[string]interface{}) error {
	if err := validateProviderTaskCapability(provider, step); err != nil {
		return err
	}
	stepForValidation := *step
	stepForValidation.Parameters = resolvedParams
	return validateStepParametersByExecutionContract(stepForValidation, contract, false)
}

func extractProviderExecutionID(respData map[string]interface{}) string {
	if executionID, ok := respData["execution_id"].(string); ok && strings.TrimSpace(executionID) != "" {
		return executionID
	}
	return ""
}

func (e *Executor) serviceToken(ctx context.Context, tenantID int) (string, error) {
	if e == nil || e.serviceTokens == nil || tenantID <= 0 {
		return "", fmt.Errorf("tenant service token source is required")
	}
	return e.serviceTokens.Token(ctx, uint(tenantID))
}

func replaceTaskProviderEndpoint(endpoint string, taskType string, taskID string, executionID string) string {
	return strings.NewReplacer(
		"{task_type}", taskType,
		"{id}", taskID,
		"{execution_id}", executionID,
	).Replace(endpoint)
}

// resolveTemplateReferences resolves declared stable outputs into this execution's parameters.
func (e *Executor) resolveTemplateReferences(params map[string]interface{}, stepResults models.StepResults) (map[string]interface{}, error) {
	resolved := make(map[string]interface{})

	for key, value := range params {
		resolvedValue, err := e.resolveValue(value, stepResults)
		if err != nil {
			return nil, fmt.Errorf("parameters.%s: %w", key, err)
		}
		resolved[key] = resolvedValue
	}

	return resolved, nil
}

// resolveValue recursively resolves output bindings in nested parameter structures.
func (e *Executor) resolveValue(value interface{}, stepResults models.StepResults) (interface{}, error) {
	switch v := value.(type) {
	case string:
		return e.resolveStringTemplate(v, stepResults)
	case map[string]interface{}:
		resolved := make(map[string]interface{})
		for k, val := range v {
			resolvedValue, err := e.resolveValue(val, stepResults)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			resolved[k] = resolvedValue
		}
		return resolved, nil
	case []interface{}:
		resolved := make([]interface{}, len(v))
		for i, val := range v {
			resolvedValue, err := e.resolveValue(val, stepResults)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			resolved[i] = resolvedValue
		}
		return resolved, nil
	default:
		return value, nil
	}
}

// resolveStringTemplate resolves the internal {{step_id.outputs.path}} serialization.
func (e *Executor) resolveStringTemplate(template string, stepResults models.StepResults) (interface{}, error) {
	trimmed := strings.TrimSpace(template)
	if !strings.HasPrefix(trimmed, "{{") || !strings.HasSuffix(trimmed, "}}") {
		return template, nil
	}

	path := strings.TrimSpace(trimmed[2 : len(trimmed)-2])
	parts := splitPath(path)
	if len(parts) < 3 || parts[1] != "outputs" {
		return nil, fmt.Errorf("template must reference a declared output as {{step_id.outputs.path}}")
	}

	stepID := parts[0]
	result, exists := stepResults[stepID]
	if !exists {
		return nil, fmt.Errorf("referenced step %q has no result", stepID)
	}

	var data interface{} = result.Result
	for _, field := range parts[1:] {
		if data == nil {
			return nil, fmt.Errorf("path %q is missing", path)
		}
		if mapData, ok := data.(map[string]interface{}); ok {
			var fieldExists bool
			data, fieldExists = mapData[field]
			if !fieldExists {
				return nil, fmt.Errorf("path %q is missing", path)
			}
		} else {
			return nil, fmt.Errorf("path %q cannot descend into non-object value", path)
		}
	}

	return data, nil
}

// splitPath 分割路径字符串（支持 . 分隔符）
func splitPath(path string) []string {
	if path == "" {
		return []string{}
	}

	var parts []string
	current := ""

	for _, ch := range path {
		if ch == '.' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(ch)
		}
	}

	if current != "" {
		parts = append(parts, current)
	}

	return parts
}

// DAG 相关函数

// DAG 邻接表
type DAG map[string][]string

// buildDAG 从步骤列表构建 DAG
func buildDAG(steps []models.Step) DAG {
	graph := make(DAG)
	for _, step := range steps {
		graph[step.ID] = step.DependsOn
	}
	return graph
}

// topologicalSort 拓扑排序（Kahn 算法）
func topologicalSort(graph DAG) ([]string, error) {
	inDegree := make(map[string]int)
	for node := range graph {
		inDegree[node] = 0
	}

	dependents := make(map[string][]string, len(graph))
	for node, deps := range graph {
		for _, dep := range deps {
			if _, exists := graph[dep]; !exists {
				return nil, fmt.Errorf("步骤 %s 依赖不存在的步骤 %s", node, dep)
			}
			inDegree[node]++
			dependents[dep] = append(dependents[dep], node)
		}
	}

	queue := []string{}
	for node, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, node)
		}
	}

	sort.Strings(queue)
	sorted := []string{}
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		sorted = append(sorted, node)

		sort.Strings(dependents[node])
		for _, dependent := range dependents[node] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
		sort.Strings(queue)
	}

	if len(sorted) != len(graph) {
		return nil, fmt.Errorf("检测到循环依赖")
	}

	return sorted, nil
}

// findStep 根据 ID 查找步骤
func findStep(steps []models.Step, id string) *models.Step {
	for i := range steps {
		if steps[i].ID == id {
			return &steps[i]
		}
	}
	return nil
}
