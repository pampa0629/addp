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
	jupyterService    *JupyterService
}

// NewDevExecutor 创建开发项执行器
func NewDevExecutor(
	devItemRepo *repository.DevItemRepository,
	devExecutionRepo *repository.DevExecutionRepository,
	workflowEngine *WorkflowEngineService,
	sqlEngine *SQLEngineService,
	jupyterService *JupyterService,
) *DevExecutor {
	return &DevExecutor{
		devItemRepo:      devItemRepo,
		devExecutionRepo: devExecutionRepo,
		workflowEngine:   workflowEngine,
		sqlEngine:        sqlEngine,
		jupyterService:   jupyterService,
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
	if devType != "sql" && devType != "workflow" && devType != "script" && devType != "notebook" {
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
	case "notebook":
		result, errorMessage = e.executeNotebook(ctx, devItem, executionID)
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

// executeWorkflow 执行工作流（支持 JSONB 配置）
func (e *DevExecutor) executeWorkflow(ctx context.Context, devItem *models.DevItem, executionID string) (models.ExecutionResult, string) {
	_ = e.devExecutionRepo.UpdateProgress(executionID, 30, "执行工作流")

	// 验证执行配置
	if devItem.ExecutionConfig == nil || *devItem.ExecutionConfig == "" {
		return nil, "工作流缺少执行配置，请配置工作流引擎"
	}

	// 解析工作流定义
	workflowDef, ok := devItem.Content["workflow_definition"].(map[string]interface{})
	if !ok {
		// 向后兼容：尝试旧的字段名
		workflowDef, ok = devItem.Content["workflow_def"].(map[string]interface{})
		if !ok {
			return nil, "无效的工作流定义"
		}
	}

	// 解析输入数据（可选）
	inputData, _ := devItem.Content["inputs"].(map[string]interface{})
	if inputData == nil {
		// 向后兼容：尝试旧的字段名
		inputData, _ = devItem.Content["input_data"].(map[string]interface{})
	}

	// 设置超时
	timeout := devItem.Timeout
	if timeout <= 0 {
		timeout = 300 // 默认5分钟
	}
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// 调用工作流引擎（传递 JSONB 配置字符串）
	resp, err := e.workflowEngine.ExecuteWorkflow(execCtx, workflowDef, inputData, *devItem.ExecutionConfig)
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

// executeNotebook 执行 Jupyter Notebook
func (e *DevExecutor) executeNotebook(ctx context.Context, devItem *models.DevItem, executionID string) (models.ExecutionResult, string) {
	_ = e.devExecutionRepo.UpdateProgress(executionID, 30, "执行 Notebook")

	// 解析 Notebook 路径
	inputPath, ok := devItem.Content["input_path"].(string)
	if !ok {
		return nil, "无效的 Notebook 输入路径"
	}

	outputPath, ok := devItem.Content["output_path"].(string)
	if !ok {
		return nil, "无效的 Notebook 输出路径"
	}

	// 解析参数（可选）
	parameters, _ := devItem.Content["parameters"].(map[string]interface{})
	if parameters == nil {
		parameters = make(map[string]interface{})
	}

	// 设置超时
	timeout := devItem.Timeout
	if timeout <= 0 {
		timeout = 300 // 默认5分钟
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

	_ = e.devExecutionRepo.UpdateProgress(executionID, 90, "Notebook 执行成功")

	// 构造结果
	result := models.ExecutionResult{
		"status":                resp.Status,
		"execution_time_seconds": resp.ExecutionTimeSeconds,
		"output_path":           resp.OutputPath,
		"output_count":          resp.OutputCount,
		"outputs":               resp.Outputs,
	}

	return result, ""
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

// ==================== 参数化执行功能（供 Orchestrator 调用）====================

// ExecuteWithParams 执行带参数的 DevItem（Orchestrator 调用此方法）
// 支持模板参数替换，实现工作流的动态参数化
func (e *DevExecutor) ExecuteWithParams(
	ctx context.Context,
	itemID uint,
	params map[string]interface{},
	tenantID uint,
	userID uint,
) (string, error) {
	// 1. 获取 DevItem 模板
	devItem, err := e.devItemRepo.FindByID(itemID, tenantID)
	if err != nil {
		return "", fmt.Errorf("开发项不存在")
	}

	// 验证状态
	if devItem.Status != "active" {
		return "", fmt.Errorf("开发项状态为 %s，无法执行", devItem.Status)
	}

	// 2. 解析 content
	var contentMap map[string]interface{}
	if devItem.Content != nil {
		contentMap = devItem.Content
	} else {
		contentMap = make(map[string]interface{})
	}

	// 3. 获取默认参数（如果有）
	var defaultParams map[string]interface{}
	if dp, ok := contentMap["default_parameters"].(map[string]interface{}); ok {
		defaultParams = dp
	} else {
		defaultParams = make(map[string]interface{})
	}

	// 4. 合并参数（传入参数覆盖默认参数）
	mergedParams := deepMerge(defaultParams, params)

	// 5. 验证参数（根据 parameter_schema）
	if schema, ok := contentMap["parameter_schema"].(map[string]interface{}); ok {
		if err := validateParameters(schema, mergedParams); err != nil {
			return "", fmt.Errorf("parameter validation failed: %w", err)
		}
	}

	// 6. 模板替换 - 将 content 中的 {{...}} 替换为实际值
	resolvedContent := resolveTemplates(contentMap, mergedParams)

	// 7. 创建临时 DevItem（使用解析后的内容）
	tempItem := &models.DevItem{
		ID:         devItem.ID,
		DevType:    devItem.DevType,
		Content:    resolvedContent.(map[string]interface{}),
		ResourceID: devItem.ResourceID,
		Timeout:    devItem.Timeout,
		Status:     devItem.Status,
	}

	// 8. 执行任务（复用现有的执行逻辑）
	executionID, err := e.ExecuteDevItem(ctx, tempItem.ID, tenantID, userID, "orchestrator")
	if err != nil {
		return "", err
	}

	log.Printf("🚀 [DevExecutor] 参数化执行成功 execution_id=%s item_id=%d", executionID, itemID)
	return executionID, nil
}

// resolveTemplates 递归解析模板变量 {{path.to.value}}
func resolveTemplates(data interface{}, params map[string]interface{}) interface{} {
	switch v := data.(type) {
	case string:
		// 匹配 {{...}} 模板
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

// getNestedValue 根据路径获取嵌套值（如 "input.resource_id"）
func getNestedValue(data map[string]interface{}, path string) interface{} {
	// 分割路径
	parts := splitPath(path)
	current := data

	for i, part := range parts {
		if i == len(parts)-1 {
			// 最后一个部分，返回值
			return current[part]
		}

		// 中间部分，继续向下导航
		if next, ok := current[part].(map[string]interface{}); ok {
			current = next
		} else {
			return nil
		}
	}
	return nil
}

// splitPath 分割路径字符串（支持 "." 分隔符）
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

// deepMerge 深度合并两个 map（后者覆盖前者）
func deepMerge(base, override map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	// 复制 base
	for k, v := range base {
		result[k] = v
	}

	// 覆盖 override
	for k, v := range override {
		if vm, ok := v.(map[string]interface{}); ok {
			// 如果是 map，尝试递归合并
			if bv, exists := result[k]; exists {
				if bvm, ok := bv.(map[string]interface{}); ok {
					result[k] = deepMerge(bvm, vm)
					continue
				}
			}
		}
		// 否则直接覆盖
		result[k] = v
	}

	return result
}

// validateParameters 验证参数是否符合 schema
func validateParameters(schema map[string]interface{}, params map[string]interface{}) error {
	// 遍历 schema 中的每个字段
	for fieldName, fieldSchemaInterface := range schema {
		fieldSchema, ok := fieldSchemaInterface.(map[string]interface{})
		if !ok {
			continue
		}

		// 检查必填字段
		if required, ok := fieldSchema["required"].(bool); ok && required {
			if _, exists := params[fieldName]; !exists {
				return fmt.Errorf("required parameter missing: %s", fieldName)
			}
		}

		// 检查类型（简单类型验证）
		if params[fieldName] != nil {
			expectedType, ok := fieldSchema["type"].(string)
			if ok {
				actualValue := params[fieldName]
				if !validateType(actualValue, expectedType) {
					return fmt.Errorf("parameter %s has wrong type: expected %s", fieldName, expectedType)
				}
			}
		}

		// 检查枚举值
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

// validateType 验证值是否符合指定类型
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
		// DataLocation 应该是一个 map
		if m, ok := value.(map[string]interface{}); ok {
			// 简单验证是否包含必要字段
			_, hasSourceType := m["source_type"]
			return hasSourceType
		}
		return false
	default:
		// 未知类型，默认通过
		return true
	}
}
