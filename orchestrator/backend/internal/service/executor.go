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

	commonModels "github.com/addp/common/models"
	"github.com/addp/orchestrator/internal/models"
	"github.com/addp/orchestrator/internal/repository"
)

// Executor 编排执行器
type Executor struct {
	executionService     *ExecutionService
	orchRepo             *repository.OrchestrationRepository
	engineRegistry       *EngineRegistry
	taskClient           *TaskClient
	taskProviderRegistry *TaskProviderRegistry
	internalAPIKey       string
}

// NewExecutor 创建执行器
func NewExecutor(
	executionService *ExecutionService,
	orchRepo *repository.OrchestrationRepository,
	engineRegistry *EngineRegistry,
	taskClient *TaskClient,
	taskProviderRegistry *TaskProviderRegistry,
	internalAPIKey string,
) *Executor {
	return &Executor{
		executionService:     executionService,
		orchRepo:             orchRepo,
		engineRegistry:       engineRegistry,
		taskClient:           taskClient,
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

	orch, err := e.orchRepo.GetByID(uint(*execution.SourceTaskID))
	if err != nil {
		return err
	}

	// 标记开始
	if err := e.executionService.UpdateStatus(ctx, executionID, "running"); err != nil {
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
		result, err := e.executeStep(ctx, step, stepResults, execution.ExecutionID)
		stepResults[step.ID] = result

		if err != nil {
			e.executionService.UpdateStepResults(ctx, executionID, stepResults)
			return e.markFailed(ctx, executionID, fmt.Errorf("步骤 %s 失败: %w", step.Name, err))
		}
	}

	// 标记完成
	if err := e.executionService.FinishExecution(ctx, executionID, "completed", "", stepResults); err != nil {
		return err
	}

	return nil
}

// executeStep 执行单个步骤（支持两种模式）
func (e *Executor) executeStep(ctx context.Context, step *models.Step, stepResults models.StepResults, parentExecutionID string) (models.StepResult, error) {
	start := time.Now()
	result := models.StepResult{StartedAt: start, Status: "running"}

	// 解析参数模板引用
	resolvedParams := e.resolveTemplateReferences(step.Parameters, stepResults)

	if step.EngineIdentifier != "" {
		// 模式一：动态引擎调用（工作流引擎）
		return e.executeWithDynamicEngine(ctx, step, resolvedParams, start)
	} else if step.Provider != "" {
		// 模式二：任务引用（TaskProvider 模块）
		return e.executeWithTaskProvider(ctx, step, resolvedParams, start, parentExecutionID)
	}

	result.Status = "failed"
	result.Error = "无效步骤：未指定 engine_identifier 或 provider"
	result.EndedAt = time.Now()
	result.Duration = time.Since(start).Milliseconds()
	return result, fmt.Errorf("%s", result.Error)
}

// executeWithDynamicEngine 使用动态引擎执行步骤（模式一：引擎调用）
func (e *Executor) executeWithDynamicEngine(ctx context.Context, step *models.Step, resolvedParams map[string]interface{}, start time.Time) (models.StepResult, error) {
	result := models.StepResult{StartedAt: start, Status: "running"}

	// 1. 从注册中心获取引擎配置
	engine, err := e.engineRegistry.GetEngine(ctx, step.EngineIdentifier)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("failed to get engine %s: %v", step.EngineIdentifier, err)
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, fmt.Errorf("%s", result.Error)
	}

	// 2. 提交工作流执行请求（工作流引擎的 execute 端点会直接返回 execution_id）
	executionID, err := e.taskClient.CreateTask(ctx, engine, resolvedParams)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("failed to submit workflow: %v", err)
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, fmt.Errorf("%s", result.Error)
	}

	// 3. 轮询任务状态
	timeout := time.Duration(step.Timeout) * time.Second
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	pollCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	taskResult, err := e.pollTaskStatusDynamic(pollCtx, engine, executionID)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("task execution failed: %v", err)
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, fmt.Errorf("%s", result.Error)
	}

	// 5. 成功
	result.Status = "success"
	result.Result = taskResult
	result.EndedAt = time.Now()
	result.Duration = time.Since(start).Milliseconds()
	return result, nil
}

// executeWithTaskProvider 通过 TaskProvider API 执行步骤（模式二：任务引用）
func (e *Executor) executeWithTaskProvider(ctx context.Context, step *models.Step, resolvedParams map[string]interface{}, start time.Time, parentExecutionID string) (models.StepResult, error) {
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

	// 2. 构建执行 URL（替换 {task_type} 和 {id} 占位符）
	taskIDStr := fmt.Sprintf("%d", step.TaskID)
	executeEndpoint := strings.NewReplacer(
		"{task_type}", step.TaskType,
		"{id}", taskIDStr,
	).Replace(provider.TaskExecuteEndpoint)
	targetURL := provider.BaseURL + executeEndpoint

	// 3. 构建请求体
	reqBody := map[string]interface{}{
		"trigger_type":        "orchestrator",
		"parent_execution_id": parentExecutionID,
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
	executionID, ok := respData["execution_id"].(string)
	if !ok || executionID == "" {
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

	taskResult, err := e.pollTaskProviderExecution(pollCtx, provider, executionID)
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

// pollTaskStatusDynamic 轮询任务状态（引擎调用模式）
func (e *Executor) pollTaskStatusDynamic(ctx context.Context, engine *commonModels.Engine, taskID string) (map[string]interface{}, error) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("轮询超时")
		case <-ticker.C:
			status, err := e.taskClient.GetTaskStatus(ctx, engine, taskID)
			if err != nil {
				continue
			}

			switch status.Status {
			case "completed", "success":
				return status.Raw, nil
			case "failed":
				if status.Message != "" {
					return nil, fmt.Errorf("任务失败: %s", status.Message)
				}
				return nil, fmt.Errorf("任务失败")
			}
		}
	}
}

// pollTaskProviderExecution 轮询 TaskProvider 执行状态（任务引用模式）
func (e *Executor) pollTaskProviderExecution(ctx context.Context, provider *commonModels.TaskProvider, executionID string) (map[string]interface{}, error) {
	// 构建状态查询 URL（替换 {execution_id} 占位符）
	statusEndpoint := strings.ReplaceAll(provider.TaskStatusEndpoint, "{execution_id}", executionID)
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

			resp, err := httpClient.Do(req)
			if err != nil {
				continue
			}

			var respData map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
				resp.Body.Close()
				continue
			}
			resp.Body.Close()

			// 从嵌套 data 中提取状态
			// 响应格式: {"status": "success", "data": {"execution_id": "...", "status": "running"}}
			var execStatus string
			if data, ok := respData["data"].(map[string]interface{}); ok {
				execStatus, _ = data["status"].(string)
			}

			switch execStatus {
			case "success", "completed":
				if data, ok := respData["data"].(map[string]interface{}); ok {
					return data, nil
				}
				return map[string]interface{}{}, nil
			case "failed":
				var errMsg string
				if data, ok := respData["data"].(map[string]interface{}); ok {
					if errDetails, ok := data["error_details"].(map[string]interface{}); ok {
						errMsg, _ = errDetails["message"].(string)
					}
				}
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

// resolveTemplateReferences 解析参数中的模板引用（支持 {{step.result.*}} 语法）
func (e *Executor) resolveTemplateReferences(params map[string]interface{}, stepResults models.StepResults) map[string]interface{} {
	resolved := make(map[string]interface{})

	for key, value := range params {
		resolved[key] = e.resolveValue(value, stepResults)
	}

	return resolved
}

// resolveValue 递归解析单个值（支持嵌套结构）
func (e *Executor) resolveValue(value interface{}, stepResults models.StepResults) interface{} {
	switch v := value.(type) {
	case string:
		return e.resolveStringTemplate(v, stepResults)
	case map[string]interface{}:
		resolved := make(map[string]interface{})
		for k, val := range v {
			resolved[k] = e.resolveValue(val, stepResults)
		}
		return resolved
	case []interface{}:
		resolved := make([]interface{}, len(v))
		for i, val := range v {
			resolved[i] = e.resolveValue(val, stepResults)
		}
		return resolved
	default:
		return value
	}
}

// resolveStringTemplate 解析字符串模板（支持 {{stepID.field1.field2}} 格式）
func (e *Executor) resolveStringTemplate(template string, stepResults models.StepResults) interface{} {
	if len(template) < 5 || template[:2] != "{{" || template[len(template)-2:] != "}}" {
		return template
	}

	path := template[2 : len(template)-2]
	parts := splitPath(path)
	if len(parts) < 1 {
		return template
	}

	stepID := parts[0]
	result, exists := stepResults[stepID]
	if !exists {
		return nil
	}

	if len(parts) == 1 {
		return result.Result
	}

	var data interface{} = result.Result
	for _, field := range parts[1:] {
		if data == nil {
			return nil
		}
		if mapData, ok := data.(map[string]interface{}); ok {
			var fieldExists bool
			data, fieldExists = mapData[field]
			if !fieldExists {
				return nil
			}
		} else {
			return nil
		}
	}

	return data
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
	e.executionService.FinishExecution(ctx, executionID, "failed", err.Error(), nil)
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
	for _, deps := range graph {
		for _, dep := range deps {
			inDegree[dep]++
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

		for _, dep := range graph[node] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
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
