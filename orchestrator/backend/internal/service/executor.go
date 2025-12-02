package service

import (
	"context"
	"fmt"
	"time"

	"github.com/addp/orchestrator/internal/models"
	"github.com/addp/orchestrator/internal/repository"
)

// Executor 编排执行器
type Executor struct {
	execRepo     *repository.ExecutionRepository
	orchRepo     *repository.OrchestrationRepository
	moduleClient *ModuleClient
}

// NewExecutor 创建执行器
func NewExecutor(
	execRepo *repository.ExecutionRepository,
	orchRepo *repository.OrchestrationRepository,
	moduleClient *ModuleClient,
) *Executor {
	return &Executor{
		execRepo:     execRepo,
		orchRepo:     orchRepo,
		moduleClient: moduleClient,
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

		// 执行步骤
		result, err := e.executeStep(ctx, step)
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

// executeStep 执行单个步骤
func (e *Executor) executeStep(ctx context.Context, step *models.Step) (models.StepResult, error) {
	start := time.Now()
	result := models.StepResult{StartedAt: start, Status: "running"}

	// 1. 调用模块 API (启动任务)
	taskID, err := e.moduleClient.Call(ctx, step.Module, step.Endpoint, step.Method, step.Parameters)
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

// pollTaskStatus 轮询任务状态
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
