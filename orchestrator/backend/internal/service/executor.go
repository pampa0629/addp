package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/orchestrator/internal/models"
	"github.com/addp/orchestrator/internal/repository"
)

// Executor 编排执行器
type Executor struct {
	executionService     *ExecutionService
	orchRepo             *repository.OrchestrationRepository
	taskProviderRegistry *TaskProviderRegistry
	internalAPIKey       string
}

// NewExecutor 创建执行器
func NewExecutor(
	executionService *ExecutionService,
	orchRepo *repository.OrchestrationRepository,
	taskProviderRegistry *TaskProviderRegistry,
	internalAPIKey string,
) *Executor {
	return &Executor{
		executionService:     executionService,
		orchRepo:             orchRepo,
		taskProviderRegistry: taskProviderRegistry,
		internalAPIKey:       internalAPIKey,
	}
}

// ExecuteAsync 异步执行编排 (协程)
func (e *Executor) ExecuteAsync(executionID uint) {
	go func() {
		ctx := context.Background()
		if err := e.executeSync(ctx, executionID); err != nil {
			// 错误已记录到数据库
		}
	}()
}

// executeSync 同步执行编排
func (e *Executor) executeSync(ctx context.Context, executionID uint) error {
	execution, err := e.executionService.GetExecution(ctx, executionID, 0)
	if err != nil {
		return err
	}

	orchestrationID, err := commonExecution.ParseSourceTaskIDUint(execution.SourceTaskID)
	if err != nil {
		return err
	}
	orch, err := e.orchRepo.GetByID(orchestrationID)
	if err != nil {
		return err
	}

	// 标记开始
	if err := e.executionService.UpdateStatus(ctx, executionID, commonExecution.ExecutionStatusRunning); err != nil {
		return err
	}

	// 构建 DAG 并拓扑排序
	graph := buildDAG(orch.Steps)
	sorted, err := topologicalSort(graph)
	if err != nil {
		return e.markFailed(ctx, executionID, fmt.Errorf("拓扑排序失败: %w", err))
	}

	// 逐步执行
	stepResults := make(models.StepResults)
	for _, stepID := range sorted {
		step := findStep(orch.Steps, stepID)
		if step == nil {
			continue
		}

		// 更新当前步骤
		if err := e.executionService.UpdateCurrentStep(ctx, executionID, step.ID); err != nil {
			// 记录日志但继续执行
		}

		// 执行步骤（传递父执行 UUID 用于 parent_execution_id）
		result, err := e.executeStep(ctx, step, stepResults, execution.ExecutionID, execution.TriggerType, execution.TenantID)
		stepResults[step.ID] = result

		if err != nil {
			e.executionService.UpdateStepResults(ctx, executionID, stepResults)
			return e.markFailed(ctx, executionID, fmt.Errorf("步骤 %s 失败: %w", step.Name, err))
		}
	}

	// 标记完成
	if err := e.executionService.FinishExecution(ctx, executionID, commonExecution.ExecutionStatusSuccess, "", stepResults); err != nil {
		return err
	}

	return nil
}

// executeStep 执行单个任务引用步骤
func (e *Executor) executeStep(ctx context.Context, step *models.Step, stepResults models.StepResults, parentExecutionID string, triggerType string, tenantID int) (models.StepResult, error) {
	start := time.Now()
	result := models.StepResult{StartedAt: start, Status: "running"}

	// 解析参数模板引用
	resolvedParams, err := e.resolveTemplateReferences(step.Parameters, stepResults)
	if err != nil {
		result.Status = "failed"
		result.Error = err.Error()
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, err
	}

	if step.Provider != "" {
		return e.executeWithTaskProvider(ctx, step, resolvedParams, start, parentExecutionID, triggerType, tenantID)
	}

	result.Status = "failed"
	result.Error = "无效步骤：未指定 provider"
	result.EndedAt = time.Now()
	result.Duration = time.Since(start).Milliseconds()
	return result, fmt.Errorf("%s", result.Error)
}

// executeWithTaskProvider 通过 TaskProvider API 执行步骤（模式二：任务引用）
func (e *Executor) executeWithTaskProvider(ctx context.Context, step *models.Step, resolvedParams map[string]interface{}, start time.Time, parentExecutionID string, triggerType string, tenantID int) (models.StepResult, error) {
	result := models.StepResult{StartedAt: start, Status: "running"}

	// 1. 从注册表获取 TaskProvider 配置
	provider, err := e.taskProviderRegistry.GetProvider(ctx, step.Provider)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("获取任务提供者 %s 失败: %v", step.Provider, err)
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, fmt.Errorf("%s", result.Error)
	}
	if err := validateProviderStepExecutable(provider, step, resolvedParams); err != nil {
		result.Status = "failed"
		result.Error = err.Error()
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, fmt.Errorf("%s", result.Error)
	}

	// 2. 构建执行 URL（替换 {task_type} 和 {id} 占位符）
	taskIDStr := fmt.Sprintf("%d", step.TaskID)
	executeEndpoint := replaceTaskProviderEndpoint(provider.TaskExecuteEndpoint, step.TaskType, taskIDStr, "")
	targetURL := provider.BaseURL + executeEndpoint

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
	if e.internalAPIKey != "" {
		req.Header.Set("X-Internal-API-Key", e.internalAPIKey)
	}
	if tenantID > 0 {
		req.Header.Set("X-Tenant-ID", fmt.Sprintf("%d", tenantID))
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("调用任务执行 API 失败: %v", err)
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, fmt.Errorf("%s", result.Error)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		result.Status = "failed"
		result.Error = fmt.Sprintf("任务执行 API 返回错误 %d: %s", resp.StatusCode, string(body))
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, fmt.Errorf("%s", result.Error)
	}

	var respData map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("解析执行响应失败: %v", err)
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, fmt.Errorf("%s", result.Error)
	}

	// 5. 提取 execution_id
	executionID := extractProviderExecutionID(respData)
	if executionID == "" {
		result.Status = "failed"
		result.Error = "执行响应中未找到 execution_id"
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, fmt.Errorf("%s", result.Error)
	}

	// 6. 轮询执行状态
	timeout := time.Duration(step.Timeout) * time.Second
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	pollCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	taskResult, err := e.pollTaskProviderExecution(pollCtx, provider, executionID, tenantID)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("任务执行失败: %v", err)
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, fmt.Errorf("%s", result.Error)
	}

	result.Status = "success"
	result.Result = taskResult
	result.EndedAt = time.Now()
	result.Duration = time.Since(start).Milliseconds()
	return result, nil
}

func validateProviderStepExecutable(provider *commonModels.TaskProvider, step *models.Step, resolvedParams map[string]interface{}) error {
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
	stepForValidation := *step
	stepForValidation.Parameters = resolvedParams
	return validateStepParametersByExecutionSchema(stepForValidation, taskTypeCapability.ExecutionSchema)
}

func extractProviderExecutionID(respData map[string]interface{}) string {
	if executionID, ok := respData["execution_id"].(string); ok && strings.TrimSpace(executionID) != "" {
		return executionID
	}
	return ""
}

// pollTaskProviderExecution 轮询 TaskProvider 执行状态（任务引用模式）
func (e *Executor) pollTaskProviderExecution(ctx context.Context, provider *commonModels.TaskProvider, executionID string, tenantID int) (map[string]interface{}, error) {
	// 构建状态查询 URL（替换 {execution_id} 占位符）
	statusEndpoint := replaceTaskProviderEndpoint(provider.TaskStatusEndpoint, "", "", executionID)
	targetURL := provider.BaseURL + statusEndpoint

	httpClient := &http.Client{Timeout: 10 * time.Second}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("轮询超时")
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
			if err != nil {
				continue
			}
			if e.internalAPIKey != "" {
				req.Header.Set("X-Internal-API-Key", e.internalAPIKey)
			}
			if tenantID > 0 {
				req.Header.Set("X-Tenant-ID", fmt.Sprintf("%d", tenantID))
			}

			resp, err := httpClient.Do(req)
			if err != nil {
				continue
			}
			if resp.StatusCode >= 400 {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				return nil, fmt.Errorf("任务状态 API 返回错误 %d: %s", resp.StatusCode, string(body))
			}

			var respData map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
				resp.Body.Close()
				continue
			}
			resp.Body.Close()

			execStatus, _ := respData["status"].(string)

			switch execStatus {
			case "success":
				return respData, nil
			case "failed":
				errMsg := providerExecutionErrorMessage(respData)
				if errMsg != "" {
					return nil, fmt.Errorf("任务失败: %s", errMsg)
				}
				return nil, fmt.Errorf("任务失败")
			case "cancelled":
				return nil, fmt.Errorf("任务已取消")
			}
		}
	}
}

func providerExecutionErrorMessage(execData map[string]interface{}) string {
	if errDetails, ok := execData["error_details"].(map[string]interface{}); ok {
		if msg, ok := errDetails["message"].(string); ok && strings.TrimSpace(msg) != "" {
			return msg
		}
	}
	if msg, ok := execData["message"].(string); ok && strings.TrimSpace(msg) != "" {
		return msg
	}
	if msg, ok := execData["error"].(string); ok && strings.TrimSpace(msg) != "" {
		return msg
	}
	return ""
}

func replaceTaskProviderEndpoint(endpoint string, taskType string, taskID string, executionID string) string {
	return strings.NewReplacer(
		"{task_type}", taskType,
		"{id}", taskID,
		"{execution_id}", executionID,
	).Replace(endpoint)
}

// resolveTemplateReferences 解析参数中的模板引用（支持 {{stepID.field1.field2}} 语法）。
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

// resolveValue 递归解析单个值（支持嵌套结构）
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

// resolveStringTemplate 解析字符串模板（支持 {{stepID.field1.field2}} 格式）
func (e *Executor) resolveStringTemplate(template string, stepResults models.StepResults) (interface{}, error) {
	trimmed := strings.TrimSpace(template)
	if !strings.HasPrefix(trimmed, "{{") || !strings.HasSuffix(trimmed, "}}") {
		return template, nil
	}

	path := strings.TrimSpace(trimmed[2 : len(trimmed)-2])
	parts := splitPath(path)
	if len(parts) < 1 {
		return nil, fmt.Errorf("template path is empty")
	}

	stepID := parts[0]
	result, exists := stepResults[stepID]
	if !exists {
		return nil, fmt.Errorf("referenced step %q has no result", stepID)
	}

	if len(parts) == 1 {
		return result.Result, nil
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

// markFailed 标记执行失败
func (e *Executor) markFailed(ctx context.Context, executionID uint, err error) error {
	if finishErr := e.executionService.FinishExecution(ctx, executionID, commonExecution.ExecutionStatusFailed, err.Error(), nil); finishErr != nil {
		return fmt.Errorf("%w; persist failed execution state: %v", err, finishErr)
	}
	return err
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

	sorted := []string{}
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		sorted = append(sorted, node)

		for _, dependent := range dependents[node] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
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
