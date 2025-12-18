package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/google/uuid"
)

// DevExecutor 统一的开发项执行器
// 负责异步执行任务，管理执行状态，记录执行结果
type DevExecutor struct {
	devItemRepo       *repository.DevItemRepository
	devExecutionRepo  *repository.DevExecutionRepository
	workflowEngine    *WorkflowEngineService
	sqlEngine         *SQLEngineService
}

// NewDevExecutor 创建开发项执行器
func NewDevExecutor(
	devItemRepo *repository.DevItemRepository,
	devExecutionRepo *repository.DevExecutionRepository,
	workflowEngine *WorkflowEngineService,
	sqlEngine *SQLEngineService,
) *DevExecutor {
	return &DevExecutor{
		devItemRepo:      devItemRepo,
		devExecutionRepo: devExecutionRepo,
		workflowEngine:   workflowEngine,
		sqlEngine:        sqlEngine,
	}
}

// ExecuteDevItem 执行开发项（异步）
func (e *DevExecutor) ExecuteDevItem(
	ctx context.Context,
	devItemID uint,
	tenantID uint,
	userID uint,
	triggerType string,
) (string, error) {
	// 获取开发项
	devItem, err := e.devItemRepo.FindByID(devItemID, tenantID)
	if err != nil {
		return "", fmt.Errorf("开发项不存在")
	}

	// 验证状态
	if devItem.Status != "active" {
		return "", fmt.Errorf("开发项状态为 %s，无法执行", devItem.Status)
	}

	// 生成执行ID
	executionID := uuid.New().String()

	// 创建执行记录
	startTime := time.Now()
	execution := &models.DevExecution{
		DevItemID:   &devItemID,
		TenantID:    tenantID,
		ExecutionID: executionID,
		DevType:     devItem.DevType,
		TriggerType: triggerType,
		TriggeredBy: &userID,
		Status:      "pending",
		Progress:    0,
		ResourceID:  devItem.ResourceID,
		StartedAt:   &startTime,
		CreatedAt:   time.Now(),
	}

	if err := e.devExecutionRepo.Create(execution); err != nil {
		return "", fmt.Errorf("failed to create execution record: %w", err)
	}

	log.Printf("🚀 [DevExecutor] 开始执行 execution_id=%s dev_item_id=%d type=%s trigger=%s",
		executionID, devItemID, devItem.DevType, triggerType)

	// 异步执行任务
	go e.executeAsync(execution.ID, executionID, devItem)

	return executionID, nil
}

// ExecuteContent 执行临时内容（不关联开发项）
func (e *DevExecutor) ExecuteContent(
	ctx context.Context,
	devType string,
	content map[string]interface{},
	resourceID *uint,
	tenantID uint,
	userID uint,
	timeout int,
) (string, error) {
	// 验证 dev_type
	if devType != "sql" && devType != "workflow" && devType != "script" {
		return "", fmt.Errorf("无效的 dev_type: %s", devType)
	}

	// 生成执行ID
	executionID := uuid.New().String()

	// 创建执行记录
	startTime := time.Now()
	execution := &models.DevExecution{
		TenantID:    tenantID,
		ExecutionID: executionID,
		DevType:     devType,
		TriggerType: "manual",
		TriggeredBy: &userID,
		Status:      "pending",
		Progress:    0,
		ResourceID:  resourceID,
		StartedAt:   &startTime,
		CreatedAt:   time.Now(),
	}

	if err := e.devExecutionRepo.Create(execution); err != nil {
		return "", fmt.Errorf("failed to create execution record: %w", err)
	}

	log.Printf("🚀 [DevExecutor] 执行临时内容 execution_id=%s type=%s", executionID, devType)

	// 构造临时 DevItem
	tempItem := &models.DevItem{
		DevType:    devType,
		Content:    content,
		ResourceID: resourceID,
		Timeout:    timeout,
	}

	// 异步执行任务
	go e.executeAsync(execution.ID, executionID, tempItem)

	return executionID, nil
}

// executeAsync 异步执行任务（核心执行逻辑）
func (e *DevExecutor) executeAsync(recordID uint, executionID string, devItem *models.DevItem) {
	ctx := context.Background()
	startTime := time.Now()

	// 更新状态为 running
	_ = e.devExecutionRepo.UpdateStatus(executionID, "running", "", nil)
	_ = e.devExecutionRepo.UpdateProgress(executionID, 10, "开始执行")

	var result models.ExecutionResult
	var errorMessage string
	var rowsAffected *int64

	// 根据类型分发到不同引擎
	switch devItem.DevType {
	case "workflow":
		result, errorMessage = e.executeWorkflow(ctx, devItem, executionID)
	case "sql":
		result, errorMessage, rowsAffected = e.executeSQL(ctx, devItem, executionID)
	case "script":
		result, errorMessage = e.executeScript(ctx, devItem, executionID)
	default:
		errorMessage = fmt.Sprintf("不支持的类型: %s", devItem.DevType)
	}

	// 计算执行时间
	executionTime := time.Since(startTime).Milliseconds()
	completedAt := time.Now()

	// 确定最终状态
	var status string
	if errorMessage != "" {
		status = "failed"
	} else {
		status = "success"
	}

	// 更新执行记录
	execution, err := e.devExecutionRepo.FindByExecutionID(executionID)
	if err != nil {
		log.Printf("❌ [DevExecutor] 查找执行记录失败: %v", err)
		return
	}

	execution.Status = status
	execution.Progress = 100
	execution.Result = result
	execution.ErrorMessage = errorMessage
	execution.ExecutionTimeMs = &executionTime
	execution.CompletedAt = &completedAt
	execution.RowsAffected = rowsAffected

	// 计算结果大小
	if result != nil {
		if resultBytes, err := json.Marshal(result); err == nil {
			resultSize := int64(len(resultBytes))
			execution.ResultSizeBytes = &resultSize
		}
	}

	if err := e.devExecutionRepo.Update(execution); err != nil {
		log.Printf("❌ [DevExecutor] 更新执行记录失败: %v", err)
		return
	}

	// 更新开发项的最后执行信息
	if execution.DevItemID != nil {
		_ = e.devItemRepo.UpdateLastExecution(
			*execution.DevItemID,
			execution.TenantID,
			execution.ID,
			status,
			completedAt,
		)
	}

	log.Printf("✅ [DevExecutor] 执行完成 execution_id=%s status=%s time=%dms",
		executionID, status, executionTime)
}

// executeWorkflow 执行工作流
func (e *DevExecutor) executeWorkflow(ctx context.Context, devItem *models.DevItem, executionID string) (models.ExecutionResult, string) {
	_ = e.devExecutionRepo.UpdateProgress(executionID, 30, "执行工作流")

	// 解析工作流定义
	workflowDef, ok := devItem.Content["workflow_def"].(map[string]interface{})
	if !ok {
		return nil, "无效的工作流定义"
	}

	// 解析输入数据（可选）
	inputData, _ := devItem.Content["input_data"].(map[string]interface{})

	// 设置超时
	timeout := devItem.Timeout
	if timeout <= 0 {
		timeout = 300 // 默认5分钟
	}
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// 调用工作流引擎
	resp, err := e.workflowEngine.ExecuteWorkflow(execCtx, workflowDef, inputData)
	if err != nil {
		return nil, fmt.Sprintf("工作流执行失败: %v", err)
	}

	_ = e.devExecutionRepo.UpdateProgress(executionID, 90, "工作流执行成功")

	// 构造结果
	result := models.ExecutionResult{
		"status":       resp.Status,
		"execution_id": resp.ExecutionID,
		"final_result": resp.FinalResult,
		"all_results":  resp.AllResults,
	}

	return result, ""
}

// executeSQL 执行SQL
func (e *DevExecutor) executeSQL(ctx context.Context, devItem *models.DevItem, executionID string) (models.ExecutionResult, string, *int64) {
	_ = e.devExecutionRepo.UpdateProgress(executionID, 30, "执行SQL")

	// 验证资源ID
	if devItem.ResourceID == nil {
		return nil, "SQL执行需要指定资源", nil
	}

	// 解析SQL内容
	sqlContent, ok := devItem.Content["sql"].(string)
	if !ok {
		return nil, "无效的SQL内容", nil
	}

	// 设置超时
	timeout := devItem.Timeout
	if timeout <= 0 {
		timeout = 30 // 默认30秒
	}
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// 调用SQL引擎
	sqlResult, err := e.sqlEngine.ExecuteSQL(execCtx, *devItem.ResourceID, sqlContent, timeout)
	if err != nil {
		return nil, fmt.Sprintf("SQL执行失败: %v", err), nil
	}

	_ = e.devExecutionRepo.UpdateProgress(executionID, 90, "SQL执行成功")

	// 构造结果
	result := models.ExecutionResult{
		"columns":       sqlResult.Columns,
		"rows":          sqlResult.Rows,
		"rows_affected": sqlResult.RowsAffected,
	}

	rowsAffected := sqlResult.RowsAffected
	return result, "", &rowsAffected
}

// executeScript 执行脚本（未实现）
func (e *DevExecutor) executeScript(ctx context.Context, devItem *models.DevItem, executionID string) (models.ExecutionResult, string) {
	_ = e.devExecutionRepo.UpdateProgress(executionID, 30, "脚本执行")
	return nil, "脚本执行功能尚未实现"
}

// GetExecution 获取执行详情
func (e *DevExecutor) GetExecution(executionID string, tenantID uint) (*models.DevExecution, error) {
	execution, err := e.devExecutionRepo.FindByExecutionID(executionID)
	if err != nil {
		return nil, fmt.Errorf("执行记录不存在")
	}

	if execution.TenantID != tenantID {
		return nil, fmt.Errorf("无权访问此执行记录")
	}

	return execution, nil
}

// ListExecutions 查询执行列表
func (e *DevExecutor) ListExecutions(req *models.ListExecutionsRequest, tenantID uint) ([]models.ExecutionWithItem, int64, error) {
	// 设置默认分页
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}

	executions, total, err := e.devExecutionRepo.List(req, tenantID)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list executions: %w", err)
	}

	return executions, total, nil
}

// CancelExecution 取消执行（仅支持 pending/running 状态）
func (e *DevExecutor) CancelExecution(executionID string, tenantID uint) error {
	execution, err := e.devExecutionRepo.FindByExecutionID(executionID)
	if err != nil {
		return fmt.Errorf("执行记录不存在")
	}

	if execution.TenantID != tenantID {
		return fmt.Errorf("无权访问此执行记录")
	}

	// 只能取消 pending 或 running 状态的执行
	if execution.Status != "pending" && execution.Status != "running" {
		return fmt.Errorf("只能取消 pending 或 running 状态的执行，当前状态: %s", execution.Status)
	}

	// 更新状态为 cancelled
	now := time.Now()
	if err := e.devExecutionRepo.UpdateStatus(executionID, "cancelled", "用户取消执行", &now); err != nil {
		return fmt.Errorf("failed to cancel execution: %w", err)
	}

	log.Printf("✅ [DevExecutor] 取消执行成功 execution_id=%s", executionID)
	return nil
}

// RetryExecution 重试执行（基于原执行记录创建新执行）
func (e *DevExecutor) RetryExecution(executionID string, tenantID uint, userID uint) (string, error) {
	// 获取原执行记录
	execution, err := e.devExecutionRepo.FindByExecutionID(executionID)
	if err != nil {
		return "", fmt.Errorf("执行记录不存在")
	}

	if execution.TenantID != tenantID {
		return "", fmt.Errorf("无权访问此执行记录")
	}

	// 如果有关联的开发项，直接重新执行
	if execution.DevItemID != nil {
		return e.ExecuteDevItem(context.Background(), *execution.DevItemID, tenantID, userID, "manual")
	}

	// 否则报错（临时内容不支持重试）
	return "", fmt.Errorf("临时执行内容不支持重试")
}

// GetStatistics 获取执行统计信息
func (e *DevExecutor) GetStatistics(tenantID uint, devItemID *uint, startDate, endDate string) (*models.ExecutionStatistics, error) {
	stats, err := e.devExecutionRepo.GetStatistics(tenantID, devItemID, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to get statistics: %w", err)
	}
	return stats, nil
}
