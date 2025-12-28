package service

import (
	"context"
	"fmt"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/orchestrator/internal/models"
	"github.com/addp/orchestrator/internal/repository"
)

// Executor 编排执行器
type Executor struct {
	execRepo       *repository.ExecutionRepository
	orchRepo       *repository.OrchestrationRepository
	engineRegistry *EngineRegistry
	taskClient     *TaskClient
	// 保留旧客户端以支持向后兼容
	moduleClient *ModuleClient
}

// NewExecutor 创建执行器
func NewExecutor(
	execRepo *repository.ExecutionRepository,
	orchRepo *repository.OrchestrationRepository,
	engineRegistry *EngineRegistry,
	taskClient *TaskClient,
	moduleClient *ModuleClient, // 可选，用于向后兼容
) *Executor {
	return &Executor{
		execRepo:       execRepo,
		orchRepo:       orchRepo,
		engineRegistry: engineRegistry,
		taskClient:     taskClient,
		moduleClient:   moduleClient,
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
	execution, err := e.execRepo.GetByID(executionID)
	if err != nil {
		return err
	}

	orch, err := e.orchRepo.GetByID(execution.OrchestrationID)
	if err != nil {
		return err
	}

	// 标记开始
	now := time.Now()
	execution.Status = "running"
	execution.StartedAt = &now
	e.execRepo.Update(execution)

	// 构建 DAG 并拓扑排序
	graph := buildDAG(orch.Steps)
	sorted, err := topologicalSort(graph)
	if err != nil {
		return e.markFailed(execution, fmt.Errorf("拓扑排序失败: %w", err))
	}

	// 逐步执行
	stepResults := make(models.StepResults)
	for _, stepID := range sorted {
		step := findStep(orch.Steps, stepID)
		if step == nil {
			continue
		}

		// 更新当前步骤
		execution.CurrentStep = step.ID
		e.execRepo.Update(execution)

		// 执行步骤（传递 stepResults 以支持参数模板化）
		result, err := e.executeStep(ctx, step, stepResults)
		stepResults[step.ID] = result

		if err != nil {
			execution.StepResults = stepResults
			e.execRepo.Update(execution)
			return e.markFailed(execution, fmt.Errorf("步骤 %s 失败: %w", step.Name, err))
		}
	}

	// 标记完成
	execution.Status = "completed"
	execution.StepResults = stepResults
	completedAt := time.Now()
	execution.CompletedAt = &completedAt
	e.execRepo.Update(execution)

	return nil
}

// executeStep 执行单个步骤（支持新旧两种模式）
func (e *Executor) executeStep(ctx context.Context, step *models.Step, stepResults models.StepResults) (models.StepResult, error) {
	start := time.Now()
	result := models.StepResult{StartedAt: start, Status: "running"}

	// 解析参数模板引用
	resolvedParams := e.resolveTemplateReferences(step.Parameters, stepResults)

	// 判断使用新架构还是旧架构
	if step.EngineIdentifier != "" {
		// 新架构：动态引擎调用
		return e.executeWithDynamicEngine(ctx, step, resolvedParams, start)
	} else if step.Module != "" {
		// 旧架构：硬编码模块调用（向后兼容）
		return e.executeWithModuleClient(ctx, step, resolvedParams, start)
	}

	result.Status = "failed"
	result.Error = "invalid step: neither engine_identifier nor module specified"
	result.EndedAt = time.Now()
	result.Duration = time.Since(start).Milliseconds()
	return result, fmt.Errorf("%s", result.Error)
}

// executeWithDynamicEngine 使用动态引擎执行步骤（新架构）
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

	// 2. 创建任务（使用解析后的参数）
	taskID, err := e.taskClient.CreateTask(ctx, engine, resolvedParams)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("failed to create task: %v", err)
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, fmt.Errorf("%s", result.Error)
	}

	// 3. 执行任务（使用解析后的参数）
	executionID, err := e.taskClient.ExecuteTask(ctx, engine, taskID, resolvedParams)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("failed to execute task: %v", err)
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, fmt.Errorf("%s", result.Error)
	}

	// 4. 轮询任务状态
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

// executeWithModuleClient 使用旧的 ModuleClient 执行步骤（向后兼容）
func (e *Executor) executeWithModuleClient(ctx context.Context, step *models.Step, resolvedParams map[string]interface{}, start time.Time) (models.StepResult, error) {
	result := models.StepResult{StartedAt: start, Status: "running"}

	if e.moduleClient == nil {
		result.Status = "failed"
		result.Error = "module client not available"
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, fmt.Errorf("%s", result.Error)
	}

	// 1. 调用模块 API (启动任务，使用解析后的参数)
	taskID, err := e.moduleClient.Call(ctx, step.Module, step.Endpoint, step.Method, resolvedParams)
	if err != nil {
		result.Status = "failed"
		result.Error = err.Error()
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, err
	}

	// 2. 轮询任务状态
	timeout := time.Duration(step.Timeout) * time.Second
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	pollCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	taskResult, err := e.pollTaskStatus(pollCtx, step.Module, taskID)
	if err != nil {
		result.Status = "failed"
		result.Error = err.Error()
		result.EndedAt = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		return result, err
	}

	// 3. 成功
	result.Status = "success"
	result.Result = taskResult
	result.EndedAt = time.Now()
	result.Duration = time.Since(start).Milliseconds()
	return result, nil
}

// pollTaskStatusDynamic 轮询任务状态（新架构）
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

// pollTaskStatus 轮询任务状态（旧架构）
func (e *Executor) pollTaskStatus(ctx context.Context, module string, taskID interface{}) (map[string]interface{}, error) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("轮询超时")
		case <-ticker.C:
			status, result, err := e.moduleClient.GetTaskStatus(ctx, module, taskID)
			if err != nil {
				continue
			}

			switch status {
			case "completed", "success":
				return result, nil
			case "failed":
				return nil, fmt.Errorf("任务失败")
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
		// 检查是否是模板引用 {{step.result.field}}
		return e.resolveStringTemplate(v, stepResults)
	case map[string]interface{}:
		// 递归解析嵌套 map
		resolved := make(map[string]interface{})
		for k, val := range v {
			resolved[k] = e.resolveValue(val, stepResults)
		}
		return resolved
	case []interface{}:
		// 递归解析数组
		resolved := make([]interface{}, len(v))
		for i, val := range v {
			resolved[i] = e.resolveValue(val, stepResults)
		}
		return resolved
	default:
		// 其他类型直接返回
		return value
	}
}

// resolveStringTemplate 解析字符串模板（支持 {{stepID.field1.field2}} 格式）
func (e *Executor) resolveStringTemplate(template string, stepResults models.StepResults) interface{} {
	// 检查是否包含模板语法 {{...}}
	if len(template) < 5 || template[:2] != "{{" || template[len(template)-2:] != "}}" {
		return template
	}

	// 提取模板内容 "stepID.field1.field2"
	path := template[2 : len(template)-2]

	// 分割路径
	parts := splitPath(path)
	if len(parts) < 1 {
		// 路径格式错误，返回原始字符串
		return template
	}

	// 第一部分是 stepID
	stepID := parts[0]
	result, exists := stepResults[stepID]
	if !exists {
		// 步骤结果不存在，返回 nil
		return nil
	}

	// 如果只有 stepID（如 {{stepID}}），返回整个结果
	if len(parts) == 1 {
		return result.Result
	}

	// 从步骤结果中提取数据（parts[1:] 是字段路径）
	// 注意：stepResults[stepID].Result 已经是 map[string]interface{}
	var data interface{} = result.Result
	for _, field := range parts[1:] {
		if data == nil {
			return nil
		}

		// 尝试从 map 中获取字段
		if mapData, ok := data.(map[string]interface{}); ok {
			var fieldExists bool
			data, fieldExists = mapData[field]
			if !fieldExists {
				return nil
			}
		} else {
			// 类型不匹配，返回 nil
			return nil
		}
	}

	return data
}

// splitPath 分割路径字符串（支持 . 分隔符）
func splitPath(path string) []string {
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
func (e *Executor) markFailed(execution *models.Execution, err error) error {
	execution.Status = "failed"
	execution.ErrorMessage = err.Error()
	completedAt := time.Now()
	execution.CompletedAt = &completedAt
	e.execRepo.Update(execution)
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
	// 计算入度
	inDegree := make(map[string]int)
	for node := range graph {
		inDegree[node] = 0
	}
	for _, deps := range graph {
		for _, dep := range deps {
			inDegree[dep]++
		}
	}

	// 找出所有入度为 0 的节点
	queue := []string{}
	for node, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, node)
		}
	}

	// 拓扑排序
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

	// 检查是否有环
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
