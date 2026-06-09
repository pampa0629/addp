package service

import (
	"context"
	"encoding/json"
	"fmt"
	commonExecution "github.com/addp/common/execution"
	"log"
	"strings"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/google/uuid"
)

// DevExecutor 统一的开发任务执行器
// 负责异步执行任务，管理执行状态，记录执行结果
// 【已重构】使用统一执行表 common.task_executions
type DevExecutor struct {
	devTaskRepo              *repository.DevTaskRepository
	taskExecutionRepo        *commonExecution.TaskExecutionRepository // 统一执行记录仓库
	workflowEngine           *WorkflowEngineService
	sqlEngine                *SQLEngineService
	jupyterService           *JupyterService
	notebookExecutionService *NotebookExecutionService
}

// NewDevExecutor 创建开发任务执行器
func NewDevExecutor(
	devTaskRepo *repository.DevTaskRepository,
	taskExecutionRepo *commonExecution.TaskExecutionRepository, // 使用统一执行记录仓库
	workflowEngine *WorkflowEngineService,
	sqlEngine *SQLEngineService,
	jupyterService *JupyterService,
	notebookExecutionService *NotebookExecutionService,
) *DevExecutor {
	return &DevExecutor{
		devTaskRepo:              devTaskRepo,
		taskExecutionRepo:        taskExecutionRepo,
		workflowEngine:           workflowEngine,
		sqlEngine:                sqlEngine,
		jupyterService:           jupyterService,
		notebookExecutionService: notebookExecutionService,
	}
}

// ExecuteDevTask 执行开发任务（异步）
func (e *DevExecutor) ExecuteDevTask(
	ctx context.Context,
	devTaskID uint,
	tenantID uint,
	userID uint,
	triggerType string,
) (string, error) {
	// 获取开发任务
	devTask, err := e.devTaskRepo.FindByID(devTaskID, tenantID)
	if err != nil {
		return "", fmt.Errorf("开发任务不存在")
	}

	// 验证状态
	if devTask.Status != "active" {
		return "", fmt.Errorf("开发任务状态为 %s，无法执行", devTask.Status)
	}

	// 生成执行ID
	executionID := uuid.New().String()

	// 提取输入参数
	var inputs commonModels.JSONMap
	if devTask.Content != nil {
		if inputData, ok := devTask.Content["inputs"].(map[string]interface{}); ok {
			inputs = inputData
		}
	}

	// 创建统一执行记录
	startTime := time.Now()
	userIDInt := int(userID)

	execution := &commonExecution.TaskExecution{
		TenantID:       int(tenantID),
		ExecutionID:    executionID,
		Module:         commonExecution.ModuleDevelop,
		TaskType:       devTask.DevType, // "query"/"workflow"/"script"
		Source:         commonExecution.ModuleDevelop,
		SourceTaskID:   commonExecution.NewSourceTaskIDFromUint(devTaskID),
		SourceTaskName: &devTask.Name,
		Status:         commonExecution.ExecutionStatusPending,
		Progress:       0,
		TriggerType:    triggerType,
		TriggeredBy:    &userIDInt,
		ExecutionConfig: commonModels.JSONMap{
			"engine_id": devTask.GetEngineID(),
			"inputs":    inputs,
		},
		StartedAt: &startTime,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := e.taskExecutionRepo.Create(ctx, execution); err != nil {
		return "", fmt.Errorf("failed to create execution record: %w", err)
	}

	log.Printf("🚀 [DevExecutor] 开始执行 execution_id=%s source_task_id=%d type=%s trigger=%s",
		executionID, devTaskID, devTask.DevType, triggerType)

	// 异步执行任务
	go e.executeAsync(execution.ID, executionID, devTask, int(tenantID))

	return executionID, nil
}

// ExecuteContent 执行临时内容（不关联开发任务）
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
	if devType != "query" && devType != "workflow" && devType != "script" {
		return "", fmt.Errorf("无效的 dev_type: %s", devType)
	}

	// 生成执行ID
	executionID := uuid.New().String()

	// 提取输入参数
	var inputs commonModels.JSONMap
	if content != nil {
		if inputData, ok := content["inputs"].(map[string]interface{}); ok {
			inputs = inputData
		}
	}

	// 创建统一执行记录
	startTime := time.Now()
	userIDInt := int(userID)
	var resourceIDInt *int
	if resourceID != nil {
		rid := int(*resourceID)
		resourceIDInt = &rid
	}

	execution := &commonExecution.TaskExecution{
		TenantID:    int(tenantID),
		ExecutionID: executionID,
		Module:      commonExecution.ModuleDevelop,
		TaskType:    devType,
		Source:      commonExecution.ModuleDevelop,
		Status:      commonExecution.ExecutionStatusPending,
		Progress:    0,
		TriggerType: commonExecution.TriggerTypeManual,
		TriggeredBy: &userIDInt,
		ExecutionConfig: commonModels.JSONMap{
			"engine_id": resourceIDInt,
			"content":   content,
			"timeout":   timeout,
			"inputs":    inputs,
		},
		StartedAt: &startTime,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := e.taskExecutionRepo.Create(ctx, execution); err != nil {
		return "", fmt.Errorf("failed to create execution record: %w", err)
	}

	log.Printf("🚀 [DevExecutor] 执行临时内容 execution_id=%s type=%s", executionID, devType)

	// 构造临时 DevTask
	tempItem := &models.DevTask{
		DevType: devType,
		Content: content,
		Timeout: timeout,
	}

	// 如果提供了 resourceID，设置到 execution_config
	if resourceID != nil {
		tempItem.ExecutionConfig = models.DevTaskContent{
			"engine_id": *resourceID,
		}
	}

	// 异步执行任务
	go e.executeAsync(execution.ID, executionID, tempItem, int(tenantID))

	return executionID, nil
}

// executeAsync 异步执行任务（核心执行逻辑）
func (e *DevExecutor) executeAsync(recordID int64, executionID string, devTask *models.DevTask, tenantID int) {
	log.Printf("🟢 [DevExecutor] executeAsync 开始: execution_id=%s", executionID)
	ctx := context.Background()
	startTime := time.Now()

	// 更新状态为 running
	_ = e.updateExecutionStatus(ctx, executionID, tenantID, commonExecution.ExecutionStatusRunning, 10, "开始执行")

	var result commonModels.JSONMap
	var errorMessage string
	var rowsAffected *int64

	// 根据类型分发到不同引擎
	log.Printf("🟢 [DevExecutor] 开始分发到引擎: execution_id=%s type=%s", executionID, devTask.DevType)
	switch devTask.DevType {
	case "workflow":
		result, errorMessage = e.executeWorkflow(ctx, devTask, executionID, tenantID)
	case "query":
		result, errorMessage, rowsAffected = e.executeQuery(ctx, devTask, executionID, tenantID)
	case "script":
		result, errorMessage = e.executeScript(ctx, devTask, executionID, tenantID)
	default:
		errorMessage = fmt.Sprintf("不支持的类型: %s", devTask.DevType)
	}
	log.Printf("🟢 [DevExecutor] 引擎执行完成: execution_id=%s errorMessage=%v", executionID, errorMessage)

	// 计算执行时间
	executionTime := time.Since(startTime).Milliseconds()
	completedAt := time.Now()

	// 确定最终状态
	var status string
	if errorMessage != "" {
		status = commonExecution.ExecutionStatusFailed
	} else {
		status = commonExecution.ExecutionStatusSuccess
	}
	log.Printf("🟢 [DevExecutor] 准备更新执行记录: execution_id=%s status=%s", executionID, status)

	// 更新执行记录
	execution, err := e.taskExecutionRepo.GetByExecutionID(ctx, executionID, tenantID)
	if err != nil {
		log.Printf("❌ [DevExecutor] 查找执行记录失败: %v", err)
		return
	}
	log.Printf("🟢 [DevExecutor] 找到执行记录: execution_id=%s id=%d", executionID, execution.ID)

	execution.Status = status
	execution.Progress = 100
	execution.ExecutionTimeMs = &executionTime
	execution.CompletedAt = &completedAt
	execution.RowsAffected = rowsAffected
	execution.CurrentStep = nil // 清空当前步骤

	// 将 result 和 size 存入 metadata JSONB
	metadata := execution.Metadata
	if metadata == nil {
		metadata = commonModels.JSONMap{}
	}
	if result != nil {
		metadata["result"] = result
		// 计算结果大小
		if resultBytes, err := json.Marshal(result); err == nil {
			metadata["result_size_bytes"] = int64(len(resultBytes))
		}
	}
	execution.Metadata = metadata

	if errorMessage != "" {
		execution.ErrorDetails = commonModels.JSONMap{
			"message": errorMessage,
		}
	}

	log.Printf("🟢 [DevExecutor] 准备调用 Update: execution_id=%s", executionID)
	if err := e.taskExecutionRepo.Update(ctx, execution); err != nil {
		log.Printf("❌ [DevExecutor] 更新执行记录失败: %v", err)
		return
	}
	log.Printf("🟢 [DevExecutor] Update 调用完成: execution_id=%s", executionID)

	log.Printf("✅ [DevExecutor] 执行记录已更新: execution_id=%s status=%s progress=100", executionID, status)

	// 更新开发任务的最后执行信息
	if execution.SourceTaskID != nil {
		if taskID, err := commonExecution.ParseSourceTaskIDUint(execution.SourceTaskID); err == nil {
			_ = e.devTaskRepo.UpdateLastExecution(
				taskID,
				uint(execution.TenantID),
				execution.ExecutionID, // UUID 字符串，软引用 common.task_executions
				status,
				completedAt,
			)
		}
	}

	log.Printf("✅ [DevExecutor] 执行完成 execution_id=%s status=%s time=%dms",
		executionID, status, executionTime)
}

// updateExecutionStatus 更新执行状态和进度
func (e *DevExecutor) updateExecutionStatus(ctx context.Context, executionID string, tenantID int, status string, progress int, currentStep string) error {
	fields := map[string]interface{}{
		"status":   status,
		"progress": progress,
	}
	if currentStep != "" {
		fields["current_step"] = currentStep
	}
	return e.taskExecutionRepo.UpdateFields(ctx, executionID, tenantID, fields)
}

// executeWorkflow 执行工作流（支持 JSONB 配置）
func (e *DevExecutor) executeWorkflow(ctx context.Context, devTask *models.DevTask, executionID string, tenantID int) (commonModels.JSONMap, string) {
	log.Printf("🔵 [DevExecutor] executeWorkflow 开始: execution_id=%s", executionID)
	_ = e.updateExecutionStatus(ctx, executionID, tenantID, commonExecution.ExecutionStatusRunning, 30, "执行工作流")

	// 验证执行配置
	if devTask.ExecutionConfig == nil || len(devTask.ExecutionConfig) == 0 {
		return nil, "工作流缺少执行配置，请配置工作流引擎"
	}

	// 解析工作流定义
	workflowDef, ok := devTask.Content["workflow_definition"].(map[string]interface{})
	if !ok {
		return nil, "无效的工作流定义"
	}

	// 解析输入数据（可选）
	inputData, _ := devTask.Content["inputs"].(map[string]interface{})

	// 设置超时
	timeout := devTask.Timeout
	if timeout <= 0 {
		timeout = 300 // 默认5分钟
	}
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// 将 ExecutionConfig 转为 JSON 字符串
	configJSON, _ := json.Marshal(devTask.ExecutionConfig)
	configStr := string(configJSON)

	log.Printf("🔵 [DevExecutor] 调用工作流引擎: execution_id=%s config=%s", executionID, configStr)
	// 调用工作流引擎
	resp, err := e.workflowEngine.ExecuteWorkflow(execCtx, workflowDef, inputData, configStr)
	if err != nil {
		log.Printf("❌ [DevExecutor] 工作流引擎调用失败: execution_id=%s err=%v", executionID, err)
		return nil, fmt.Sprintf("工作流执行失败: %v", err)
	}

	log.Printf("🔵 [DevExecutor] 工作流引擎返回成功: execution_id=%s", executionID)
	_ = e.updateExecutionStatus(ctx, executionID, tenantID, commonExecution.ExecutionStatusRunning, 90, "工作流执行成功")

	// 构造结果摘要
	result := commonModels.JSONMap{
		"status":       resp.Status,
		"execution_id": resp.ExecutionID,
		"logs":         resp.Logs,
		"traceback":    resp.Traceback,
		"summary": map[string]interface{}{
			"has_result":        resp.FinalResult != "",
			"result_size_bytes": len(resp.FinalResult),
		},
	}

	log.Printf("🔵 [DevExecutor] executeWorkflow 结束: execution_id=%s", executionID)
	return result, ""
}

// executeQuery 执行查询（根据query_type路由）
func (e *DevExecutor) executeQuery(ctx context.Context, devTask *models.DevTask, executionID string, tenantID int) (commonModels.JSONMap, string, *int64) {
	// 使用兼容方法获取查询类型
	queryType := devTask.GetQueryType()

	// 根据 query_type 路由到不同执行器
	switch queryType {
	case "sql":
		return e.executeSQL(ctx, devTask, executionID, tenantID)
	case "mql":
		return nil, "MongoDB 查询功能尚未实现", nil
	case "dsl":
		return nil, "Elasticsearch 查询功能尚未实现", nil
	default:
		return nil, fmt.Sprintf("不支持的查询类型: %s", queryType), nil
	}
}

// executeSQL 执行SQL
func (e *DevExecutor) executeSQL(ctx context.Context, devTask *models.DevTask, executionID string, tenantID int) (commonModels.JSONMap, string, *int64) {
	_ = e.updateExecutionStatus(ctx, executionID, tenantID, commonExecution.ExecutionStatusRunning, 30, "执行SQL")

	// 使用兼容方法获取引擎ID
	engineID := devTask.GetEngineID()
	if engineID == nil {
		return nil, "SQL执行需要指定资源", nil
	}

	// 解析SQL内容
	sqlContent, ok := devTask.Content["query"].(string)
	if !ok {
		return nil, "无效的查询内容", nil
	}

	// 设置超时
	timeout := devTask.Timeout
	if timeout <= 0 {
		timeout = 30
	}
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// 调用SQL引擎
	sqlResult, err := e.sqlEngine.ExecuteSQL(execCtx, *engineID, sqlContent, timeout)
	if err != nil {
		return nil, fmt.Sprintf("SQL执行失败: %v", err), nil
	}

	_ = e.updateExecutionStatus(ctx, executionID, tenantID, commonExecution.ExecutionStatusRunning, 90, "SQL执行成功")

	// 构造结果摘要（只保存元数据和少量示例数据）
	var previewRows []map[string]interface{}
	previewLimit := 10
	if len(sqlResult.Rows) > previewLimit {
		previewRows = sqlResult.Rows[:previewLimit]
	} else {
		previewRows = sqlResult.Rows
	}

	result := commonModels.JSONMap{
		"columns":       sqlResult.Columns,
		"rows_affected": sqlResult.RowsAffected,
		"summary": map[string]interface{}{
			"total_rows":   len(sqlResult.Rows),
			"column_count": len(sqlResult.Columns),
			"preview_rows": previewRows,
		},
	}

	rowsAffected := sqlResult.RowsAffected
	return result, "", &rowsAffected
}

// executeScript 执行脚本任务。当前脚本开发由 Jupyter Notebook runtime 承载。
func (e *DevExecutor) executeScript(ctx context.Context, devTask *models.DevTask, executionID string, tenantID int) (commonModels.JSONMap, string) {
	_ = e.updateExecutionStatus(ctx, executionID, tenantID, commonExecution.ExecutionStatusRunning, 20, "准备执行 Notebook")

	// 如果没有 NotebookExecutionService，降级到简单模式
	if e.notebookExecutionService == nil {
		_ = e.updateExecutionStatus(ctx, executionID, tenantID, commonExecution.ExecutionStatusRunning, 30, "执行 Notebook（简单模式）")

		// 解析 Notebook 路径
		inputPath, ok := devTask.Content["input_path"].(string)
		if !ok {
			return nil, "无效的 Notebook 输入路径"
		}

		outputPath, ok := devTask.Content["output_path"].(string)
		if !ok {
			return nil, "无效的 Notebook 输出路径"
		}

		// 解析参数（可选）
		parameters, _ := devTask.Content["parameters"].(map[string]interface{})
		if parameters == nil {
			parameters = make(map[string]interface{})
		}

		// 设置超时
		timeout := devTask.Timeout
		if timeout <= 0 {
			timeout = 300
		}
		execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()

		// 调用 Jupyter Engine 执行
		resp, err := e.jupyterService.ExecuteNotebook(execCtx, inputPath, outputPath, parameters, timeout)
		if err != nil {
			errorMsg := fmt.Sprintf("Notebook 执行失败: %v", err)
			if resp != nil && resp.ErrorMessage != "" {
				errorMsg = resp.ErrorMessage
			}
			return nil, errorMsg
		}

		_ = e.updateExecutionStatus(ctx, executionID, tenantID, commonExecution.ExecutionStatusRunning, 90, "Notebook 执行成功")

		// 构造结果摘要
		result := commonModels.JSONMap{
			"status":                 resp.Status,
			"execution_time_seconds": resp.ExecutionTimeSeconds,
			"output_path":            resp.OutputPath,
			"output_count":           resp.OutputCount,
			"summary": map[string]interface{}{
				"has_outputs": resp.Outputs != nil && len(resp.Outputs) > 0,
			},
		}

		return result, ""
	}

	// 使用 NotebookExecutionService（新架构）
	_ = e.updateExecutionStatus(ctx, executionID, tenantID, commonExecution.ExecutionStatusRunning, 30, "执行 Notebook（完整模式）")

	// 获取执行记录以获取 userID
	execution, err := e.taskExecutionRepo.GetByExecutionID(ctx, executionID, tenantID)
	if err != nil {
		return nil, fmt.Sprintf("无法获取执行记录: %v", err)
	}

	// 获取 userID
	var userID uint
	if execution.TriggeredBy != nil {
		userID = uint(*execution.TriggeredBy)
	} else {
		userID = 1
	}

	// 调用 NotebookExecutionService 执行
	result, errorMsg, err := e.notebookExecutionService.ExecuteNotebook(ctx, devTask, executionID, userID)
	if err != nil {
		return nil, fmt.Sprintf("Notebook 执行失败: %v", err)
	}

	if errorMsg != "" {
		return nil, errorMsg
	}

	_ = e.updateExecutionStatus(ctx, executionID, tenantID, commonExecution.ExecutionStatusRunning, 90, "Notebook 执行成功")

	// 转换为 JSONMap
	executionResult := commonModels.JSONMap{
		"status":            result.Status,
		"output_path":       result.OutputPath,
		"cell_count":        result.CellCount,
		"execution_count":   result.ExecutionCount,
		"execution_time_ms": result.ExecutionTimeMs,
		"summary": map[string]interface{}{
			"has_outputs": result.OutputsPreview != nil && len(result.OutputsPreview) > 0,
		},
	}

	return executionResult, ""
}

// GetExecution 获取执行详情
func (e *DevExecutor) GetExecution(executionID string, tenantID uint) (*models.ExecutionWithDevTask, error) {
	execution, err := e.taskExecutionRepo.GetByExecutionID(context.Background(), executionID, int(tenantID))
	if err != nil {
		return nil, fmt.Errorf("执行记录不存在")
	}

	result := &models.ExecutionWithDevTask{
		TaskExecution: execution,
	}

	// 加载关联的开发任务
	if execution.SourceTaskID != nil {
		if taskID, parseErr := commonExecution.ParseSourceTaskIDUint(execution.SourceTaskID); parseErr == nil {
			devTask, err := e.devTaskRepo.FindByID(taskID, tenantID)
			if err == nil {
				result.DevTask = devTask
			}
		}
	}

	return result, nil
}

// ListExecutions 查询执行列表
func (e *DevExecutor) ListExecutions(req *models.ListExecutionsRequest, tenantID uint) ([]models.ExecutionWithDevTask, int64, error) {
	// 设置默认分页
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}

	// 构建过滤器
	filter := commonExecution.TaskExecutionFilter{
		TenantID:    int(tenantID),
		Module:      commonExecution.ModuleDevelop,
		TaskType:    req.DevType,
		Status:      req.Status,
		TriggerType: req.TriggerType,
		Page:        req.Page,
		PageSize:    req.PageSize,
	}

	if req.SourceTaskID != "" {
		filter.SourceTaskID = &req.SourceTaskID
	}

	if req.StartDate != "" {
		if t, err := time.Parse("2006-01-02", req.StartDate); err == nil {
			filter.StartDate = &t
		}
	}
	if req.EndDate != "" {
		if t, err := time.Parse("2006-01-02", req.EndDate); err == nil {
			filter.EndDate = &t
		}
	}

	// 查询统一表
	executions, total, err := e.taskExecutionRepo.List(context.Background(), filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list executions: %w", err)
	}

	// 直接映射，加载关联开发任务
	result := make([]models.ExecutionWithDevTask, len(executions))
	for i, exec := range executions {
		result[i] = models.ExecutionWithDevTask{
			TaskExecution: exec,
		}

		// 加载关联的开发任务
		if exec.SourceTaskID != nil {
			if taskID, parseErr := commonExecution.ParseSourceTaskIDUint(exec.SourceTaskID); parseErr == nil {
				devTask, err := e.devTaskRepo.FindByID(taskID, tenantID)
				if err == nil {
					result[i].DevTask = devTask
				}
			}
		}
	}

	return result, total, nil
}

// RetryExecution 重试执行
func (e *DevExecutor) RetryExecution(executionID string, tenantID uint, userID uint) (string, error) {
	// 获取原执行记录
	execution, err := e.taskExecutionRepo.GetByExecutionID(context.Background(), executionID, int(tenantID))
	if err != nil {
		return "", fmt.Errorf("执行记录不存在")
	}

	// 如果有关联的开发任务，直接重新执行
	if execution.SourceTaskID != nil {
		taskID, err := commonExecution.ParseSourceTaskIDUint(execution.SourceTaskID)
		if err != nil {
			return "", err
		}
		return e.ExecuteDevTask(context.Background(), taskID, tenantID, userID, "manual")
	}

	// 否则报错（临时内容不支持重试）
	return "", fmt.Errorf("临时执行内容不支持重试")
}

// GetStatistics 获取执行统计信息
func (e *DevExecutor) GetStatistics(tenantID uint, sourceTaskID string, startDate, endDate string) (*models.ExecutionStatistics, error) {
	var startDatePtr, endDatePtr *time.Time
	if startDate != "" {
		if t, err := time.Parse("2006-01-02", startDate); err == nil {
			startDatePtr = &t
		}
	}
	if endDate != "" {
		if t, err := time.Parse("2006-01-02", endDate); err == nil {
			endDatePtr = &t
		}
	}

	filter := commonExecution.TaskExecutionFilter{
		TenantID:  int(tenantID),
		Module:    commonExecution.ModuleDevelop,
		StartDate: startDatePtr,
		EndDate:   endDatePtr,
	}
	if sourceTaskID != "" {
		filter.SourceTaskID = &sourceTaskID
	}

	stats, err := e.taskExecutionRepo.GetStatistics(context.Background(), filter)
	if err != nil {
		return nil, fmt.Errorf("failed to get statistics: %w", err)
	}

	// 转换为兼容格式
	return &models.ExecutionStatistics{
		TotalExecutions:  stats.Total,
		SuccessCount:     stats.SuccessCount,
		FailedCount:      stats.FailedCount,
		RunningCount:     stats.RunningCount,
		SuccessRate:      stats.SuccessRate,
		AvgExecutionTime: stats.AvgExecutionTimeMs,
	}, nil
}

// ExecuteWithParams 执行带参数的 DevTask。
func (e *DevExecutor) ExecuteWithParams(
	ctx context.Context,
	itemID uint,
	params map[string]interface{},
	tenantID uint,
	userID uint,
) (string, error) {
	return e.ExecuteWithParamsWithContext(ctx, itemID, params, tenantID, userID, commonExecution.TriggerTypeManual, commonExecution.ModuleDevelop, nil, "")
}

// ExecuteWithParamsWithContext 执行带参数的 DevTask，并记录统一任务体系上下文。
func (e *DevExecutor) ExecuteWithParamsWithContext(
	ctx context.Context,
	itemID uint,
	params map[string]interface{},
	tenantID uint,
	userID uint,
	triggerType string,
	source string,
	parentExecutionID *string,
	expectedTaskType string,
) (string, error) {
	normalizedTriggerType, err := commonExecution.NormalizeTriggerType(triggerType)
	if err != nil {
		return "", err
	}
	normalizedSource := strings.TrimSpace(source)
	if normalizedSource == "" {
		normalizedSource = commonExecution.ModuleDevelop
	}
	if params == nil {
		params = map[string]interface{}{}
	}
	// 1. 获取 DevTask 模板
	devTask, err := e.devTaskRepo.FindByID(itemID, tenantID)
	if err != nil {
		return "", fmt.Errorf("开发任务不存在")
	}

	if err := validateExpectedDevTaskType(devTask.DevType, expectedTaskType); err != nil {
		return "", err
	}

	if devTask.Status != "active" {
		return "", fmt.Errorf("开发任务状态为 %s，无法执行", devTask.Status)
	}

	// 2. 解析并合并参数
	var contentMap map[string]interface{}
	if devTask.Content != nil {
		contentMap = devTask.Content
	} else {
		contentMap = make(map[string]interface{})
	}

	var defaultParams map[string]interface{}
	if dp, ok := contentMap["default_parameters"].(map[string]interface{}); ok {
		defaultParams = dp
	} else {
		defaultParams = make(map[string]interface{})
	}

	mergedParams := deepMerge(defaultParams, params)

	// 3. 验证参数
	if schema, ok := contentMap["parameter_schema"].(map[string]interface{}); ok {
		if err := validateParameters(schema, mergedParams); err != nil {
			return "", fmt.Errorf("parameter validation failed: %w", err)
		}
	}

	// 4. 模板替换
	resolvedContent := resolveTemplates(contentMap, mergedParams)
	resolvedContentMap, ok := resolvedContent.(map[string]interface{})
	if !ok {
		resolvedContentMap = make(map[string]interface{})
	}

	resolvedContentMap["parameters"] = mergedParams
	if dataSourceIDs, ok := params["data_source_ids"]; ok {
		resolvedContentMap["data_sources"] = dataSourceIDs
	}

	// 5. 创建临时 DevTask
	tempItem := &models.DevTask{
		ID:              devTask.ID,
		DevType:         devTask.DevType,
		Content:         resolvedContentMap,
		Timeout:         devTask.Timeout,
		TenantID:        tenantID,
		Status:          devTask.Status,
		ExecutionConfig: devTask.ExecutionConfig,
	}

	// 6. 直接创建执行记录并执行
	executionID := uuid.New().String()
	startTime := time.Now()

	executionInputs := commonModels.JSONMap{
		"parameters": mergedParams,
	}
	if dataSourceIDs, ok := params["data_source_ids"]; ok {
		executionInputs["data_sources"] = dataSourceIDs
	}

	userIDInt := int(userID)

	execution := &commonExecution.TaskExecution{
		TenantID:          int(tenantID),
		ExecutionID:       executionID,
		Module:            commonExecution.ModuleDevelop,
		TaskType:          devTask.DevType,
		Source:            normalizedSource,
		SourceTaskID:      commonExecution.NewSourceTaskIDFromUint(itemID),
		SourceTaskName:    &devTask.Name,
		ParentExecutionID: parentExecutionID,
		Status:            commonExecution.ExecutionStatusPending,
		Progress:          0,
		TriggerType:       normalizedTriggerType,
		TriggeredBy:       &userIDInt,
		ExecutionConfig: commonModels.JSONMap{
			"engine_id": devTask.GetEngineID(),
			"inputs":    executionInputs,
		},
		StartedAt: &startTime,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := e.taskExecutionRepo.Create(ctx, execution); err != nil {
		return "", fmt.Errorf("failed to create execution record: %w", err)
	}

	log.Printf("🚀 [DevExecutor] 参数化执行已创建 execution_id=%s task_id=%d params=%v",
		executionID, itemID, mergedParams)

	// 异步执行任务
	go e.executeAsync(execution.ID, executionID, tempItem, int(tenantID))

	return executionID, nil
}

func validateExpectedDevTaskType(devType, expectedTaskType string) error {
	if expectedTaskType != "" && devType != expectedTaskType {
		return fmt.Errorf("开发任务类型不匹配: task_type=%s, dev_type=%s", expectedTaskType, devType)
	}
	return nil
}

// ==================== 辅助函数 ====================

// resolveTemplates 递归解析模板变量 {{path.to.value}}
func resolveTemplates(data interface{}, params map[string]interface{}) interface{} {
	switch v := data.(type) {
	case string:
		if len(v) > 4 && v[:2] == "{{" && v[len(v)-2:] == "}}" {
			path := v[2 : len(v)-2]
			return getNestedValue(params, path)
		}
		return v
	case map[string]interface{}:
		result := make(map[string]interface{})
		for key, val := range v {
			result[key] = resolveTemplates(val, params)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, val := range v {
			result[i] = resolveTemplates(val, params)
		}
		return result
	default:
		return v
	}
}

func getNestedValue(data map[string]interface{}, path string) interface{} {
	parts := splitPath(path)
	current := data
	for i, part := range parts {
		if i == len(parts)-1 {
			return current[part]
		}
		if next, ok := current[part].(map[string]interface{}); ok {
			current = next
		} else {
			return nil
		}
	}
	return nil
}

func splitPath(path string) []string {
	var parts []string
	var current string
	for _, char := range path {
		if char == '.' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(char)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

func deepMerge(base, override map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range base {
		result[k] = v
	}
	for k, v := range override {
		if vm, ok := v.(map[string]interface{}); ok {
			if bv, exists := result[k]; exists {
				if bvm, ok := bv.(map[string]interface{}); ok {
					result[k] = deepMerge(bvm, vm)
					continue
				}
			}
		}
		result[k] = v
	}
	return result
}

func validateParameters(schema map[string]interface{}, params map[string]interface{}) error {
	for fieldName, fieldSchemaInterface := range schema {
		fieldSchema, ok := fieldSchemaInterface.(map[string]interface{})
		if !ok {
			continue
		}

		if required, ok := fieldSchema["required"].(bool); ok && required {
			if _, exists := params[fieldName]; !exists {
				return fmt.Errorf("required parameter missing: %s", fieldName)
			}
		}

		if params[fieldName] != nil {
			expectedType, ok := fieldSchema["type"].(string)
			if ok {
				actualValue := params[fieldName]
				if !validateType(actualValue, expectedType) {
					return fmt.Errorf("parameter %s has wrong type: expected %s", fieldName, expectedType)
				}
			}
		}

		if enumValues, ok := fieldSchema["enum"].([]interface{}); ok && len(enumValues) > 0 {
			if paramValue := params[fieldName]; paramValue != nil {
				found := false
				for _, enumVal := range enumValues {
					if paramValue == enumVal {
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("parameter %s must be one of: %v", fieldName, enumValues)
				}
			}
		}
	}
	return nil
}

func validateType(value interface{}, expectedType string) bool {
	switch expectedType {
	case "string":
		_, ok := value.(string)
		return ok
	case "number", "int":
		switch value.(type) {
		case int, int32, int64, float32, float64:
			return true
		default:
			return false
		}
	case "bool", "boolean":
		_, ok := value.(bool)
		return ok
	case "object":
		_, ok := value.(map[string]interface{})
		return ok
	case "array":
		_, ok := value.([]interface{})
		return ok
	case "DataLocation":
		if m, ok := value.(map[string]interface{}); ok {
			_, hasSourceType := m["source_type"]
			return hasSourceType
		}
		return false
	default:
		return true
	}
}
