package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/resourcetree"

	commonModels "github.com/addp/common/models"
	"github.com/addp/common/taskprovider"
	commonUtils "github.com/addp/common/utils"
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
	operatorDiscovery        *OperatorDiscoveryService
	metaClient               *commonClient.MetaClient
	sqlEngine                *SQLEngineService
	federatedQuery           federatedQueryExecutor
	notebookExecutionService *NotebookExecutionService
	queryResultLimit         int
}

type federatedQueryExecutor interface {
	IsRuntime(ctx context.Context, tenantID, engineID uint) bool
	ReferencedEngineIDs(ctx context.Context, tenantID, runtimeEngineID uint, query string) ([]uint, error)
	ExecuteQuery(ctx context.Context, tenantID, runtimeEngineID uint, executionID uuid.UUID, authorizationID int64, query string, timeout int, limit int, sourceEngineIDs []uint) (*FederatedQueryResult, error)
}

type preparedContentExecution struct {
	execution             *commonExecution.TaskExecution
	devTask               *models.DevTask
	tenantID              int
	sqlAuthorization      *IssuedSQLExecutionAuthorization
	workflowAuthorization *IssuedWorkflowExecutionAuthorization
}

type preparedParameterizedDevTask struct {
	template *models.DevTask
	task     *models.DevTask
	inputs   commonModels.JSONMap
}

// NewDevExecutor 创建开发任务执行器
func NewDevExecutor(
	devTaskRepo *repository.DevTaskRepository,
	taskExecutionRepo *commonExecution.TaskExecutionRepository, // 使用统一执行记录仓库
	workflowEngine *WorkflowEngineService,
	operatorDiscovery *OperatorDiscoveryService,
	metaClient *commonClient.MetaClient,
	sqlEngine *SQLEngineService,
	federatedQuery federatedQueryExecutor,
	notebookExecutionService *NotebookExecutionService,
	queryResultLimit int,
) *DevExecutor {
	return &DevExecutor{
		devTaskRepo:              devTaskRepo,
		taskExecutionRepo:        taskExecutionRepo,
		workflowEngine:           workflowEngine,
		operatorDiscovery:        operatorDiscovery,
		metaClient:               metaClient,
		sqlEngine:                sqlEngine,
		federatedQuery:           federatedQuery,
		notebookExecutionService: notebookExecutionService,
		queryResultLimit:         queryResultLimit,
	}
}

// ExecuteDevTask 执行开发任务（异步）
func (e *DevExecutor) ExecuteDevTask(
	ctx context.Context,
	devTaskID uint,
	tenantID uint,
	userID uint,
	userAccessToken string,
	triggerType string,
) (string, error) {
	return e.ExecuteWithParamsWithContext(
		ctx,
		devTaskID,
		map[string]interface{}{},
		tenantID,
		userID,
		userAccessToken,
		triggerType,
		commonExecution.ModuleDevelop,
		nil,
		"",
	)
}

// ExecuteContent 执行临时内容（不关联开发任务）
func (e *DevExecutor) ExecuteContent(
	ctx context.Context,
	devType string,
	content map[string]interface{},
	executionConfig map[string]interface{},
	parameters map[string]interface{},
	tenantID uint,
	userID uint,
	userAccessToken string,
	triggerType string,
	timeout int,
	queryConfirmationToken string,
) (string, error) {
	prepared, err := e.prepareContentExecutionWithConfirmation(
		ctx,
		devType,
		content,
		executionConfig,
		parameters,
		tenantID,
		userID,
		userAccessToken,
		triggerType,
		timeout,
		queryConfirmationToken,
	)
	if err != nil {
		return "", err
	}
	if err := e.persistPreparedContentExecution(ctx, e.taskExecutionRepo, prepared); err != nil {
		return "", err
	}
	e.startPreparedContentExecution(prepared)
	return prepared.execution.ExecutionID, nil
}

func (e *DevExecutor) prepareContentExecution(
	ctx context.Context,
	devType string,
	content map[string]interface{},
	executionConfig map[string]interface{},
	parameters map[string]interface{},
	tenantID uint,
	userID uint,
	userAccessToken string,
	triggerType string,
	timeout int,
) (*preparedContentExecution, error) {
	return e.prepareContentExecutionWithConfirmation(ctx, devType, content, executionConfig, parameters, tenantID, userID, userAccessToken, triggerType, timeout, "")
}

func (e *DevExecutor) prepareContentExecutionWithConfirmation(
	ctx context.Context,
	devType string,
	content map[string]interface{},
	executionConfig map[string]interface{},
	parameters map[string]interface{},
	tenantID uint,
	userID uint,
	userAccessToken string,
	triggerType string,
	timeout int,
	queryConfirmationToken string,
) (*preparedContentExecution, error) {
	normalizedTriggerType, err := commonExecution.NormalizeTriggerType(triggerType)
	if err != nil {
		return nil, err
	}
	// 验证 dev_type
	if devType != "query" && devType != "workflow" && devType != "script" {
		return nil, fmt.Errorf("无效的 dev_type: %s", devType)
	}
	if content == nil {
		content = map[string]interface{}{}
	}
	if executionConfig == nil {
		return nil, fmt.Errorf("临时执行必须提供 execution_config")
	}
	if parameters == nil {
		parameters = map[string]interface{}{}
	}
	if err := validateDevTaskContent(devType, content); err != nil {
		return nil, err
	}
	if err := validateDevTaskExecutionConfig(devType, content, executionConfig); err != nil {
		return nil, err
	}
	if devType == commonExecution.TaskTypeWorkflow {
		if err := e.validateWorkflowBeforeExecution(ctx, content, executionConfig, tenantID); err != nil {
			return nil, err
		}
	}
	if timeout <= 0 {
		timeout = 300
	}

	// 生成执行ID
	executionID := uuid.New().String()

	inputs := commonModels.JSONMap{"submitted_parameters": parameters}
	var effectiveParameters map[string]interface{}
	var executionContract *taskprovider.ExecutionContract
	if devType == commonExecution.TaskTypeQuery {
		contract, effective, resolveErr := resolveQueryExecutionParameters(content, parameters)
		if resolveErr != nil {
			return nil, &ExecutionParametersError{Cause: resolveErr}
		}
		executionContract = contract
		effectiveParameters = effective
	} else {
		emptyContract := taskprovider.EmptyExecutionContract()
		if err := taskprovider.ValidateExecutionParameters(
			emptyContract.InputSchema,
			parameters,
			taskprovider.ParameterValidationOptions{},
		); err != nil {
			return nil, &ExecutionParametersError{Cause: err}
		}
		executionContract = &emptyContract
		if inputData, ok := content["inputs"].(map[string]interface{}); ok {
			inputs["content_inputs"] = inputData
		}
	}
	inputs["execution_contract"] = executionContract
	inputs["effective_parameters"] = effectiveParameters

	// 创建统一执行记录
	now := time.Now()
	var triggeredBy *int
	if userID > 0 {
		userIDInt := int(userID)
		triggeredBy = &userIDInt
	}
	recordConfig := commonModels.JSONMap{}
	for key, value := range executionConfig {
		recordConfig[key] = value
	}
	recordConfig["content"] = content
	recordConfig["timeout"] = timeout
	recordConfig["inputs"] = inputs

	execution := &commonExecution.TaskExecution{
		TenantID:        int(tenantID),
		ExecutionID:     executionID,
		Module:          commonExecution.ModuleDevelop,
		TaskType:        devType,
		Source:          commonExecution.ModuleDevelop,
		Status:          commonExecution.ExecutionStatusPending,
		Progress:        0,
		TriggerType:     normalizedTriggerType,
		TriggeredBy:     triggeredBy,
		ExecutionConfig: recordConfig,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	// 构造临时 DevTask
	tempItem := &models.DevTask{
		DevType:           devType,
		Content:           content,
		Timeout:           timeout,
		ExecutionConfig:   models.DevTaskContent(executionConfig),
		RuntimeParameters: effectiveParameters,
	}
	if devType == commonExecution.TaskTypeQuery && tempItem.GetQueryType() == "sql" {
		engineID := tempItem.GetEngineID()
		queryText, _ := content["query"].(string)
		targetLocator, _ := content["target_locator"].(string)
		analysis, analysisErr := AnalyzeQuery("sql", queryText)
		if analysisErr != nil {
			return nil, analysisErr
		}
		if analysis.RequiresConfirmation {
			if engineID == nil || *engineID == 0 {
				return nil, &QueryConfirmationError{Message: "高风险查询缺少执行引擎"}
			}
			if err := e.sqlEngine.VerifyQueryConfirmationToken(
				queryConfirmationToken, tenantID, userID, *engineID, targetLocator, analysis.Fingerprint, analysis.Effect,
			); err != nil {
				return nil, &QueryConfirmationError{Message: err.Error()}
			}
		}
	}
	sqlAuthorization, err := e.prepareSQLExecutionAuthorization(
		ctx, tempItem, tenantID, userAccessToken, executionID,
	)
	if err != nil {
		return nil, err
	}
	workflowAuthorization, err := e.prepareWorkflowExecutionAuthorization(
		ctx, tempItem, tenantID, userAccessToken, executionID,
	)
	if err != nil {
		return nil, err
	}
	applySQLExecutionAuthorizationFacts(execution, sqlAuthorization)
	applyWorkflowExecutionAuthorizationFacts(execution, workflowAuthorization)

	return &preparedContentExecution{
		execution: execution, devTask: tempItem, tenantID: int(tenantID),
		sqlAuthorization: sqlAuthorization, workflowAuthorization: workflowAuthorization,
	}, nil
}

func (e *DevExecutor) persistPreparedContentExecution(
	ctx context.Context,
	repo *commonExecution.TaskExecutionRepository,
	prepared *preparedContentExecution,
) error {
	if err := repo.Create(ctx, prepared.execution); err != nil {
		return fmt.Errorf("failed to create execution record: %w", err)
	}
	return nil
}

func (e *DevExecutor) startPreparedContentExecution(prepared *preparedContentExecution) {
	log.Printf(
		"🚀 [DevExecutor] 执行临时内容 execution_id=%s type=%s",
		prepared.execution.ExecutionID,
		prepared.devTask.DevType,
	)
	go e.executeAsync(
		prepared.execution.ID,
		prepared.execution.ExecutionID,
		prepared.devTask,
		prepared.tenantID,
		prepared.sqlAuthorization,
		prepared.workflowAuthorization,
	)
}

func (e *DevExecutor) prepareSQLExecutionAuthorization(
	ctx context.Context,
	devTask *models.DevTask,
	tenantID uint,
	userAccessToken string,
	executionID string,
) (*IssuedSQLExecutionAuthorization, error) {
	if devTask == nil || devTask.DevType != commonExecution.TaskTypeQuery {
		return nil, nil
	}
	if strings.TrimSpace(userAccessToken) == "" {
		return nil, fmt.Errorf("异步 SQL 执行必须由当前 User Access Token 派生 Execution Authorization")
	}
	if e.isFederatedQuery(ctx, devTask, tenantID) {
		engineIDs, err := e.federatedReadEngineIDs(ctx, devTask, tenantID)
		if err != nil || len(engineIDs) == 0 {
			return nil, err
		}
		parsedExecutionID, err := uuid.Parse(executionID)
		if err != nil {
			return nil, fmt.Errorf("执行 ID 无效: %w", err)
		}
		return e.sqlEngine.IssueFederatedReadExecutionAuthorization(
			ctx, tenantID, userAccessToken, parsedExecutionID, engineIDs, devTask.Timeout,
		)
	}
	engineID := devTask.GetEngineID()
	if engineID == nil || *engineID == 0 {
		return nil, fmt.Errorf("SQL执行需要指定资源")
	}
	sqlContent, ok := devTask.Content["query"].(string)
	if !ok || strings.TrimSpace(sqlContent) == "" {
		return nil, fmt.Errorf("无效的查询内容")
	}
	parsedExecutionID, err := uuid.Parse(executionID)
	if err != nil {
		return nil, fmt.Errorf("执行 ID 无效: %w", err)
	}
	if devTask.GetQueryType() != "sql" {
		return e.sqlEngine.IssueReadExecutionAuthorization(
			ctx, tenantID, userAccessToken, parsedExecutionID, []uint{*engineID}, devTask.Timeout,
		)
	}
	return e.sqlEngine.IssueSQLExecutionAuthorization(
		ctx, tenantID, userAccessToken, parsedExecutionID, *engineID, sqlContent, devTask.Timeout,
	)
}

func (e *DevExecutor) isFederatedQuery(ctx context.Context, devTask *models.DevTask, tenantID uint) bool {
	if e == nil || e.federatedQuery == nil || devTask == nil || devTask.DevType != commonExecution.TaskTypeQuery || devTask.GetQueryType() != "sql" {
		return false
	}
	engineID := devTask.GetEngineID()
	return engineID != nil && e.federatedQuery.IsRuntime(ctx, tenantID, *engineID)
}

func (e *DevExecutor) federatedReadEngineIDs(ctx context.Context, devTask *models.DevTask, tenantID uint) ([]uint, error) {
	if !e.isFederatedQuery(ctx, devTask, tenantID) {
		return nil, fmt.Errorf("无效的联邦查询任务")
	}
	sqlContent, ok := devTask.Content["query"].(string)
	if !ok || strings.TrimSpace(sqlContent) == "" {
		return nil, fmt.Errorf("无效的查询内容")
	}
	effect, err := ClassifySQLExecutionEffect(sqlContent)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSQLExecutionUnclassifiable, err)
	}
	if effect != SQLExecutionEffectRead {
		return nil, fmt.Errorf("联邦查询 Runtime 仅支持只读 SQL")
	}
	return e.federatedQuery.ReferencedEngineIDs(ctx, tenantID, *devTask.GetEngineID(), sqlContent)
}

func applySQLExecutionAuthorizationFacts(
	execution *commonExecution.TaskExecution,
	authorization *IssuedSQLExecutionAuthorization,
) {
	if execution == nil || authorization == nil {
		return
	}
	execution.ActorPrincipalID = &authorization.ActorPrincipalID
	execution.ActorTenantMembershipID = &authorization.ActorTenantMembershipID
	execution.IssuedAuthorizationVersion = &authorization.IssuedAuthorizationVersion
	execution.ExecutionAuthorizationID = &authorization.AuthorizationID
	execution.AuthorizationEffects = []string{string(authorization.Effect)}
	expiresAt := authorization.ExpiresAt.UTC()
	execution.AuthorizationExpiresAt = &expiresAt
}

func applyWorkflowExecutionAuthorizationFacts(
	execution *commonExecution.TaskExecution,
	authorization *IssuedWorkflowExecutionAuthorization,
) {
	if execution == nil || authorization == nil {
		return
	}
	execution.ActorPrincipalID = &authorization.ActorPrincipalID
	execution.ActorTenantMembershipID = &authorization.ActorTenantMembershipID
	execution.IssuedAuthorizationVersion = &authorization.IssuedAuthorizationVersion
	execution.ExecutionAuthorizationID = &authorization.AuthorizationID
	execution.AuthorizationEffects = append([]string(nil), authorization.Effects...)
	expiresAt := authorization.ExpiresAt.UTC()
	execution.AuthorizationExpiresAt = &expiresAt
}

func (e *DevExecutor) validateWorkflowBeforeExecution(
	ctx context.Context,
	content map[string]interface{},
	executionConfig map[string]interface{},
	tenantID uint,
) error {
	if e.operatorDiscovery == nil {
		return fmt.Errorf("工作流校验服务不可用")
	}
	workflowDefinition, ok := content["workflow_definition"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("工作流定义无效")
	}
	workflowEngineID := workflowEngineIDFromExecutionConfig(executionConfig)
	if workflowEngineID == 0 {
		return fmt.Errorf("工作流执行必须提供 execution_config.engine_id")
	}
	result, err := e.operatorDiscovery.ValidateWorkflowForTenant(
		ctx,
		workflowEngineID,
		workflowDefinition,
		tenantID,
	)
	if err != nil {
		return err
	}
	if result.Valid {
		return nil
	}
	messages := make([]string, 0, len(result.Errors))
	for _, issue := range result.Errors {
		messages = append(messages, issue.Message)
	}
	return fmt.Errorf("工作流校验失败: %s", strings.Join(messages, "; "))
}

func workflowEngineIDFromExecutionConfig(executionConfig map[string]interface{}) uint {
	switch value := executionConfig["engine_id"].(type) {
	case float64:
		if value > 0 {
			return uint(value)
		}
	case int:
		if value > 0 {
			return uint(value)
		}
	case uint:
		return value
	}
	return 0
}

// executeAsync 异步执行任务（核心执行逻辑）
func (e *DevExecutor) executeAsync(
	recordID int64,
	executionID string,
	devTask *models.DevTask,
	tenantID int,
	sqlAuthorization *IssuedSQLExecutionAuthorization,
	workflowAuthorization *IssuedWorkflowExecutionAuthorization,
) {
	log.Printf("🟢 [DevExecutor] executeAsync 开始: execution_id=%s", executionID)
	ctx := context.Background()
	startTime := time.Now()

	// 只有真正进入 worker 后才从 pending 切换为 running 并写 started_at。
	if err := e.taskExecutionRepo.StartExecution(ctx, executionID, tenantID, startTime); err != nil {
		log.Printf("❌ [DevExecutor] 启动执行失败: execution_id=%s error=%v", executionID, err)
		return
	}
	_ = e.updateExecutionStatus(ctx, executionID, tenantID, commonExecution.ExecutionStatusRunning, 10, "开始执行")

	var result commonModels.JSONMap
	var errorMessage string
	var errorCode string
	var rowsAffected *int64

	// 根据类型分发到不同引擎
	log.Printf("🟢 [DevExecutor] 开始分发到引擎: execution_id=%s type=%s", executionID, devTask.DevType)
	switch devTask.DevType {
	case "workflow":
		result, errorMessage = e.executeWorkflow(ctx, devTask, executionID, tenantID, workflowAuthorization)
	case "query":
		result, errorMessage, rowsAffected, errorCode = e.executeQuery(ctx, devTask, executionID, tenantID, sqlAuthorization)
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
	status := executionStatusForError(errorMessage)
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
	if status == commonExecution.ExecutionStatusSuccess {
		if facts := developLineageFacts(devTask, result); facts != nil {
			metadata["lineage_facts"] = facts
		}
	}
	execution.Metadata = metadata

	if errorMessage != "" {
		execution.ErrorDetails = commonModels.JSONMap{
			"message": errorMessage,
		}
		if errorCode != "" {
			execution.ErrorDetails["error_code"] = errorCode
			execution.ErrorDetails["details"] = errorMessage
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
func (e *DevExecutor) executeWorkflow(
	ctx context.Context,
	devTask *models.DevTask,
	executionID string,
	tenantID int,
	authorization *IssuedWorkflowExecutionAuthorization,
) (commonModels.JSONMap, string) {
	log.Printf("🔵 [DevExecutor] executeWorkflow 开始: execution_id=%s", executionID)

	// 验证执行配置
	if devTask.ExecutionConfig == nil || len(devTask.ExecutionConfig) == 0 {
		return nil, "工作流缺少执行配置，请配置工作流引擎"
	}

	// 解析工作流定义
	workflowDef, ok := devTask.Content["workflow_definition"].(map[string]interface{})
	if !ok {
		return nil, "无效的工作流定义"
	}
	if err := ValidateWorkflowDefinition(workflowDef); err != nil {
		return nil, err.Error()
	}
	if authorization == nil {
		return nil, "异步工作流执行缺少 Execution Authorization"
	}

	_ = e.updateExecutionStatus(ctx, executionID, tenantID, commonExecution.ExecutionStatusRunning, 30, "执行工作流")

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
	parsedExecutionID, err := uuid.Parse(executionID)
	if err != nil {
		return nil, "工作流执行 ID 无效"
	}
	resp, err := e.workflowEngine.ExecuteWorkflow(
		execCtx, uint(tenantID), parsedExecutionID, workflowDef, inputData, configStr, authorization,
	)
	if err != nil {
		log.Printf("❌ [DevExecutor] 工作流引擎调用失败: execution_id=%s err=%v", executionID, err)
		return nil, fmt.Sprintf("工作流执行失败: %v", err)
	}

	log.Printf("🔵 [DevExecutor] 工作流引擎返回成功: execution_id=%s", executionID)
	_ = e.updateExecutionStatus(ctx, executionID, tenantID, commonExecution.ExecutionStatusRunning, 90, "工作流执行成功")

	// 构造结果摘要
	result := commonModels.JSONMap{
		"status":               resp.Status,
		"runtime_execution_id": resp.ExecutionID,
		"logs":                 resp.Logs,
		"traceback":            resp.Traceback,
		"summary": map[string]interface{}{
			"has_result":        resp.FinalResult != "",
			"result_size_bytes": len(resp.FinalResult),
		},
	}
	if resp.ExecutionTimeMs != nil {
		result["execution_time_ms"] = *resp.ExecutionTimeMs
	}
	if resp.RuntimeStatus != nil {
		result["runtime_status"] = resp.RuntimeStatus
	}
	if len(resp.ProducedTargets) > 0 {
		result["produced_targets"] = resp.ProducedTargets
		result["outputs"] = workflowExecutionOutputs(resp.ProducedTargets)
		result["meta_scan_runs"] = e.createWorkflowProducedTargetScanRuns(ctx, uint(tenantID), resp.ProducedTargets)
	}

	log.Printf("🔵 [DevExecutor] executeWorkflow 结束: execution_id=%s", executionID)
	return result, ""
}

func workflowExecutionOutputs(targets []WorkflowProducedTarget) commonModels.JSONMap {
	outputs := commonModels.JSONMap{}
	for _, target := range targets {
		if strings.TrimSpace(target.TaskID) == "" {
			continue
		}
		outputs[target.TaskID] = map[string]interface{}{
			"resource": map[string]interface{}{
				"locator": target.Locator,
				"type":    target.Type,
			},
		}
	}
	return outputs
}

func developLineageFacts(devTask *models.DevTask, result commonModels.JSONMap) *commonExecution.LineageFacts {
	if devTask == nil {
		return nil
	}
	inputs := collectLineageRefs(devTask.Content["inputs"], "input")
	outputs := collectLineageRefs(result["outputs"], "output")
	if targetLocator := devTask.GetTargetLocator(); strings.TrimSpace(targetLocator) != "" {
		outputs = append(outputs, commonExecution.LineageResourceRef{Port: "output", Locator: targetLocator})
	}
	if len(outputs) == 0 || len(inputs) == 0 {
		return nil
	}
	return &commonExecution.LineageFacts{
		SchemaVersion: commonExecution.LineageFactsSchemaVersion,
		Inputs:        inputs, Outputs: outputs,
		Operations: []commonExecution.LineageOperation{{Kind: "derive", Operator: "develop", InputPorts: []string{"input"}, OutputPorts: []string{"output"}}},
	}
}

func collectLineageRefs(value interface{}, port string) []commonExecution.LineageResourceRef {
	refs := make([]commonExecution.LineageResourceRef, 0)
	var visit func(interface{})
	visit = func(current interface{}) {
		switch typed := current.(type) {
		case []interface{}:
			for _, item := range typed {
				visit(item)
			}
		case map[string]interface{}:
			locator, _ := typed["locator"].(string)
			if strings.TrimSpace(locator) != "" {
				ref := commonExecution.LineageResourceRef{Port: port, Locator: locator}
				if rawID, ok := typed["item_id"].(float64); ok && rawID > 0 {
					id := uint(rawID)
					ref.ItemID = &id
				}
				refs = append(refs, ref)
			}
			for _, item := range typed {
				visit(item)
			}
		}
	}
	visit(value)
	return refs
}

func (e *DevExecutor) createWorkflowProducedTargetScanRuns(
	ctx context.Context,
	tenantID uint,
	targets []WorkflowProducedTarget,
) []map[string]interface{} {
	if e.metaClient == nil || len(targets) == 0 {
		return nil
	}
	scanRuns := make([]map[string]interface{}, 0, len(targets))
	metaClient := e.metaClient.WithTenantID(tenantID)
	for _, target := range targets {
		if strings.TrimSpace(target.Locator) == "" || target.EngineID == 0 {
			continue
		}
		run, err := metaClient.CreateManualScanRun(workflowProducedTargetScanOptions(target))
		entry := map[string]interface{}{
			"target_locator": target.Locator,
			"engine_id":      target.EngineID,
		}
		if err != nil {
			entry["status"] = "failed"
			entry["error"] = err.Error()
			log.Printf("⚠️  [DevExecutor] 工作流产物自动 Meta scan 提交失败 target=%s err=%v", target.Locator, err)
		} else if run != nil {
			entry["status"] = "submitted"
			entry["execution_id"] = run.ExecutionID
		}
		scanRuns = append(scanRuns, entry)
	}
	return scanRuns
}

func workflowProducedTargetScanOptions(target WorkflowProducedTarget) commonClient.MetaScanOptions {
	opts := commonClient.MetaScanOptions{
		EngineID:    target.EngineID,
		ScanDepth:   "deep",
		Force:       true,
		TriggerType: commonExecution.TriggerTypeManual,
		Source:      "develop.workflow.produced_target",
	}
	if strings.EqualFold(target.Type, "file") && len(target.Path) > 0 {
		opts.RefGroups = []commonClient.MetaScanRefGroup{{
			Primary: strings.Join(target.Path, "/"),
		}}
		return opts
	}
	opts.Targets = []string{target.Locator}
	return opts
}

// executeQuery 执行查询（根据query_type路由）
func (e *DevExecutor) executeQuery(ctx context.Context, devTask *models.DevTask, executionID string, tenantID int, authorization *IssuedSQLExecutionAuthorization) (commonModels.JSONMap, string, *int64, string) {
	queryType := devTask.GetQueryType()

	// 根据 query_type 路由到不同执行器
	switch queryType {
	case "sql", "mql", "cypher":
		return e.executeSQL(ctx, devTask, executionID, tenantID, authorization)
	default:
		return nil, fmt.Sprintf("不支持的查询类型: %s", queryType), nil, ""
	}
}

// executeSQL 执行SQL
func (e *DevExecutor) executeSQL(ctx context.Context, devTask *models.DevTask, executionID string, tenantID int, authorization *IssuedSQLExecutionAuthorization) (commonModels.JSONMap, string, *int64, string) {
	_ = e.updateExecutionStatus(ctx, executionID, tenantID, commonExecution.ExecutionStatusRunning, 30, "执行查询")
	sqlContent, ok := devTask.Content["query"].(string)
	parsedExecutionID, err := uuid.Parse(executionID)
	if !ok || err != nil {
		return nil, "异步 SQL 执行上下文无效", nil, ""
	}
	if e.isFederatedQuery(ctx, devTask, uint(tenantID)) {
		if devTask.RuntimeParameters != nil {
			return nil, "联邦查询 Runtime 不支持查询参数", nil, ""
		}
		if e.federatedQuery == nil || authorization == nil {
			return nil, "联邦查询服务或 Execution Authorization 未初始化", nil, ""
		}
		runtimeEngineID := devTask.GetEngineID()
		if runtimeEngineID == nil {
			return nil, "联邦查询 Runtime Engine 无效", nil, ""
		}
		federatedResult, executeErr := e.federatedQuery.ExecuteQuery(
			ctx, uint(tenantID), *runtimeEngineID, parsedExecutionID, authorization.AuthorizationID,
			sqlContent, devTask.Timeout, e.queryFetchLimit(), authorization.EngineIDs,
		)
		if executeErr != nil {
			return nil, fmt.Sprintf("联邦查询执行失败: %v", executeErr), nil, ""
		}
		response, message, affected := e.queryResult(
			federatedResult.Columns, federatedResult.Rows, int64(federatedResult.RowCount),
			SQLExecutionEffectRead, "table", nil,
		)
		return response, message, affected, ""
	}
	if authorization == nil {
		return nil, "异步 SQL 执行缺少 Execution Authorization", nil, ""
	}
	engineID := devTask.GetEngineID()
	if engineID == nil {
		return nil, "异步 SQL 执行上下文无效", nil, ""
	}
	var result *SQLResult
	if devTask.GetQueryType() == "sql" {
		result, err = e.sqlEngine.ExecuteIssuedSQLAuthorization(
			ctx, uint(tenantID), parsedExecutionID, *engineID, sqlContent, devTask.RuntimeParameters, devTask.Timeout, e.queryFetchLimit(), authorization,
		)
	} else {
		engine, accessErr := e.sqlEngine.executionEngine(ctx, uint(tenantID), parsedExecutionID, *engineID, authorization)
		if accessErr != nil {
			return nil, fmt.Sprintf("查询执行授权失败: %v", accessErr), nil, ""
		}
		if supportErr := requireQueryParameterCapability(engine, devTask.GetQueryType(), devTask.RuntimeParameters); supportErr != nil {
			return nil, supportErr.Error(), nil, ""
		}
		queryTimeout := devTask.Timeout
		if queryTimeout <= 0 {
			queryTimeout = 30
		}
		execCtx, cancel := context.WithTimeout(ctx, time.Duration(queryTimeout)*time.Second)
		defer cancel()
		var graphData *plugin.GraphData
		var queryResult *plugin.QueryResult
		var queryErr error
		var targetPath *plugin.CatalogPath
		if locatorURI := devTask.GetTargetLocator(); locatorURI != "" {
			locator, locatorErr := resourcetree.ParseURI(locatorURI)
			if locatorErr != nil || locator.EngineID != *engineID {
				if locatorErr == nil {
					locatorErr = fmt.Errorf("资源定位符引擎 ID 不匹配")
				}
				return nil, fmt.Sprintf("查询目标无效: %v", locatorErr), nil, ""
			}
			model, modelErr := dbbridge.CatalogModel(engine.EngineType)
			if modelErr != nil {
				return nil, fmt.Sprintf("查询目标无效: %v", modelErr), nil, ""
			}
			path, pathErr := resourcetree.ProviderCatalogPathFromLocator(model, locator)
			if pathErr != nil {
				return nil, fmt.Sprintf("查询目标无效: %v", pathErr), nil, ""
			}
			targetPath = &path
		}
		if engineSupportsQueryResultKind(engine, "graph") {
			graphResult, graphErr := dbbridge.ExecuteReadOnlyGraphQueryWithPath(
				execCtx, engine, devTask.GetQueryType(), sqlContent, devTask.RuntimeParameters, e.queryFetchLimit(), targetPath,
			)
			queryErr = graphErr
			if graphResult != nil {
				queryResult = &graphResult.QueryResult
				graphData = graphResult.GraphData
			}
		} else {
			queryResult, queryErr = dbbridge.ExecuteReadOnlyRuntimeQueryWithPath(
				execCtx, engine, devTask.GetQueryType(), sqlContent, devTask.RuntimeParameters, e.queryFetchLimit(), targetPath,
			)
		}
		if queryErr == nil && queryResult == nil {
			queryErr = fmt.Errorf("查询运行时返回空结果")
		}
		if queryErr != nil {
			err = queryErr
		} else {
			resultKind := "table"
			if graphData != nil {
				resultKind = "graph"
			}
			result = &SQLResult{
				Columns: queryResult.Columns, Rows: queryResult.Rows, RowsAffected: int64(len(queryResult.Rows)), Effect: SQLExecutionEffectRead,
			}
			if err == nil {
				response, message, affected := e.queryResult(result.Columns, result.Rows, result.RowsAffected, result.Effect, resultKind, graphData)
				if devTask.GetQueryType() == "mql" && len(result.Rows) == 0 {
					response["diagnostics"] = mongoZeroResultDiagnostics(execCtx, engine, sqlContent, targetPath)
				}
				return response, message, affected, ""
			}
		}
	}
	if err != nil {
		return nil, fmt.Sprintf("查询执行失败: %v", err), nil, queryErrorCode(err)
	}
	response, message, affected := e.queryResult(result.Columns, result.Rows, result.RowsAffected, result.Effect, "table", nil)
	return response, message, affected, ""
}

func queryErrorCode(err error) string {
	return string(plugin.QueryErrorCodeOf(err))
}

// mongoZeroResultDiagnostics adds non-blocking guidance for the common case where
// MongoDB accepts a filter but returns no documents because a field name's case
// does not match the sampled collection schema. It deliberately remains a
// warning: dynamic schemas may be incomplete and zero rows can be legitimate.
func mongoZeroResultDiagnostics(ctx context.Context, engine *commonModels.Engine, query string, targetPath *plugin.CatalogPath) []map[string]interface{} {
	if engine == nil || !strings.EqualFold(engine.EngineType, "mongodb") || targetPath == nil {
		return []map[string]interface{}{{"code": "query_zero_result", "reason": "diagnostic_unavailable"}}
	}
	enginePlugin, err := plugin.Get(engine.EngineType)
	if err != nil {
		return []map[string]interface{}{{"code": "query_zero_result", "reason": "diagnostic_unavailable"}}
	}
	sampler, ok := enginePlugin.(plugin.DynamicSchemaSamplingProvider)
	if !ok {
		return []map[string]interface{}{{"code": "query_zero_result", "reason": "diagnostic_unavailable"}}
	}
	facts, err := sampler.SampleDynamicSchema(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), *targetPath, plugin.CatalogFactsOptions{SampleSize: 100})
	if err != nil || facts == nil || facts.Table == nil {
		return []map[string]interface{}{{"code": "query_zero_result", "reason": "diagnostic_unavailable"}}
	}
	if facts.Table.EstimatedRowCount != nil && *facts.Table.EstimatedRowCount == 0 {
		return []map[string]interface{}{{"code": "query_zero_result", "reason": "collection_empty"}}
	}
	requested := mqlFieldNames(query)
	fields := facts.Table.FieldNames()
	for _, name := range requested {
		for _, field := range fields {
			if strings.EqualFold(name, field) && name != field {
				return []map[string]interface{}{{
					"code": "query_zero_result", "reason": "field_case_mismatch", "field": name, "suggested_field": field,
				}}
			}
		}
		if !containsString(fields, name) {
			return []map[string]interface{}{{"code": "query_zero_result", "reason": "field_not_observed", "field": name}}
		}
	}
	return []map[string]interface{}{{"code": "query_zero_result", "reason": "filter_not_matched"}}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func mqlFieldNames(query string) []string {
	var command map[string]interface{}
	if err := json.Unmarshal([]byte(query), &command); err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	result := make([]string, 0)
	var collectPredicateFields func(interface{})
	collectPredicateFields = func(current interface{}) {
		switch typed := current.(type) {
		case []interface{}:
			for _, item := range typed {
				collectPredicateFields(item)
			}
		case map[string]interface{}:
			for key, child := range typed {
				if strings.HasPrefix(key, "$") {
					if key == "$and" || key == "$or" || key == "$nor" {
						collectPredicateFields(child)
					}
					continue
				}
				field := strings.SplitN(key, ".", 2)[0]
				if field != "" {
					if _, exists := seen[field]; !exists {
						seen[field] = struct{}{}
						result = append(result, field)
					}
				}
			}
		}
	}
	if filter, ok := command["filter"]; ok {
		collectPredicateFields(filter)
	}
	if queryFilter, ok := command["query"]; ok {
		collectPredicateFields(queryFilter)
	}
	if pipeline, ok := command["pipeline"].([]interface{}); ok {
		for _, stage := range pipeline {
			if stageMap, ok := stage.(map[string]interface{}); ok {
				if match, ok := stageMap["$match"]; ok {
					collectPredicateFields(match)
				}
			}
		}
	}
	return result
}

func (e *DevExecutor) queryResultLimitValue() int {
	if e != nil && e.queryResultLimit > 0 {
		return e.queryResultLimit
	}
	return 500
}

func (e *DevExecutor) queryFetchLimit() int {
	return e.queryResultLimitValue() + 1
}

func (e *DevExecutor) queryResult(
	columns []string,
	rows []map[string]interface{},
	rowsAffected int64,
	effect SQLExecutionEffect,
	resultKind string,
	graphData *plugin.GraphData,
) (commonModels.JSONMap, string, *int64) {
	limit := e.queryResultLimitValue()
	if rows == nil {
		rows = []map[string]interface{}{}
	}
	if columns == nil {
		columns = []string{}
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	graphData, graphTruncated := truncateGraphData(graphData, limit)
	truncated = truncated || graphTruncated
	if effect == SQLExecutionEffectRead {
		rowsAffected = int64(len(rows))
	}
	result := commonModels.JSONMap{
		"columns":       columns,
		"rows_count":    len(rows),
		"rows_affected": rowsAffected,
		"effect":        effect,
		"result_kind":   resultKind,
		"result_limit":  limit,
		"truncated":     truncated,
		"summary": map[string]interface{}{
			"column_count": len(columns),
			"preview_rows": rows,
		},
	}
	if graphData != nil {
		result["graph_data"] = graphData
	}
	return result, "", &rowsAffected
}

func executionStatusForError(errorMessage string) string {
	if errorMessage == "" {
		return commonExecution.ExecutionStatusSuccess
	}
	if strings.Contains(strings.ToLower(errorMessage), context.DeadlineExceeded.Error()) {
		return commonExecution.ExecutionStatusTimeout
	}
	return commonExecution.ExecutionStatusFailed
}

func engineSupportsQueryResultKind(engine *commonModels.Engine, kind string) bool {
	if engine == nil {
		return false
	}
	capabilities, err := commonUtils.ParseCapabilities(engine.Capabilities)
	if err != nil || capabilities.Compute == nil || capabilities.Compute.Query == nil {
		return false
	}
	return slices.Contains(capabilities.Compute.Query.ResultKinds, kind)
}

func requireQueryParameterCapability(engine *commonModels.Engine, language string, parameters map[string]interface{}) error {
	if parameters == nil {
		return nil
	}
	capabilities, err := commonUtils.ParseCapabilities(engine.Capabilities)
	if err != nil || capabilities == nil || capabilities.Compute == nil || capabilities.Compute.Query == nil ||
		capabilities.Compute.Query.Parameters == nil || !capabilities.Compute.Query.Parameters.Supported ||
		!slices.Contains(capabilities.Compute.Query.Parameters.Languages, language) {
		return fmt.Errorf("当前引擎不支持 %s 查询参数", language)
	}
	return nil
}

func truncateGraphData(data *plugin.GraphData, limit int) (*plugin.GraphData, bool) {
	if data == nil || limit <= 0 {
		return data, false
	}
	truncated := len(data.Nodes) > limit || len(data.Relationships) > limit
	nodes := data.Nodes
	if len(nodes) > limit {
		nodes = nodes[:limit]
	}
	nodeIDs := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		nodeIDs[node.ElementId] = struct{}{}
	}
	relationships := make([]plugin.GraphRelationship, 0, min(len(data.Relationships), limit))
	for _, relationship := range data.Relationships {
		if len(relationships) == limit {
			truncated = true
			break
		}
		if _, ok := nodeIDs[relationship.StartNodeId]; !ok {
			truncated = true
			continue
		}
		if _, ok := nodeIDs[relationship.EndNodeId]; !ok {
			truncated = true
			continue
		}
		relationships = append(relationships, relationship)
	}
	return &plugin.GraphData{Nodes: nodes, Relationships: relationships}, truncated
}

// executeScript 执行脚本任务。当前脚本开发由 Jupyter Notebook runtime 承载。
func (e *DevExecutor) executeScript(ctx context.Context, devTask *models.DevTask, executionID string, tenantID int) (commonModels.JSONMap, string) {
	_ = e.updateExecutionStatus(ctx, executionID, tenantID, commonExecution.ExecutionStatusRunning, 20, "准备执行 Notebook")

	if e.notebookExecutionService == nil {
		return nil, "Notebook 执行服务不可用"
	}
	_ = e.updateExecutionStatus(ctx, executionID, tenantID, commonExecution.ExecutionStatusRunning, 30, "执行 Notebook")

	// 调用 NotebookExecutionService 执行
	result, errorMsg, err := e.notebookExecutionService.ExecuteNotebook(ctx, devTask, executionID)
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
		Outputs:       executionOutputs(execution.Metadata),
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

func executionOutputs(metadata commonModels.JSONMap) commonModels.JSONMap {
	result, _ := metadata["result"].(map[string]interface{})
	outputs, _ := result["outputs"].(map[string]interface{})
	return commonModels.JSONMap(outputs)
}

func (e *DevExecutor) GetDevTaskType(taskID, tenantID uint) (string, error) {
	if e == nil || e.devTaskRepo == nil || taskID == 0 || tenantID == 0 {
		return "", fmt.Errorf("开发任务查询上下文无效")
	}
	task, err := e.devTaskRepo.FindByID(taskID, tenantID)
	if err != nil {
		return "", fmt.Errorf("开发任务不存在")
	}
	return task.DevType, nil
}

func (e *DevExecutor) GetExecutionTaskType(executionID string, tenantID uint) (string, error) {
	if e == nil || e.taskExecutionRepo == nil || strings.TrimSpace(executionID) == "" || tenantID == 0 {
		return "", fmt.Errorf("执行查询上下文无效")
	}
	execution, err := e.taskExecutionRepo.GetByExecutionID(context.Background(), executionID, int(tenantID))
	if err != nil {
		return "", fmt.Errorf("执行记录不存在")
	}
	return execution.TaskType, nil
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
func (e *DevExecutor) RetryExecution(executionID string, tenantID uint, userID uint, userAccessToken string) (string, error) {
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
		return e.ExecuteDevTask(context.Background(), taskID, tenantID, userID, userAccessToken, "manual")
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
	userAccessToken string,
) (string, error) {
	return e.ExecuteWithParamsWithContext(ctx, itemID, params, tenantID, userID, userAccessToken, commonExecution.TriggerTypeManual, commonExecution.ModuleDevelop, nil, "")
}

// ExecuteWithParamsWithContext 执行带参数的 DevTask，并记录统一任务体系上下文。
func (e *DevExecutor) ExecuteWithParamsWithContext(
	ctx context.Context,
	itemID uint,
	params map[string]interface{},
	tenantID uint,
	userID uint,
	userAccessToken string,
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
	preparedTask, err := e.prepareParameterizedDevTask(ctx, itemID, params, tenantID, expectedTaskType)
	if err != nil {
		return "", err
	}
	devTask := preparedTask.template
	tempItem := preparedTask.task

	// 6. 直接创建执行记录并执行
	executionID := uuid.New().String()
	sqlAuthorization, err := e.prepareSQLExecutionAuthorization(ctx, tempItem, tenantID, userAccessToken, executionID)
	if err != nil {
		return "", err
	}
	workflowAuthorization, err := e.prepareWorkflowExecutionAuthorization(ctx, tempItem, tenantID, userAccessToken, executionID)
	if err != nil {
		return "", err
	}
	now := time.Now()

	executionInputs := preparedTask.inputs

	var triggeredBy *int
	if userID > 0 {
		userIDInt := int(userID)
		triggeredBy = &userIDInt
	}

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
		TriggeredBy:       triggeredBy,
		ExecutionConfig:   devTaskExecutionRecordConfig(devTask, executionInputs),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	applySQLExecutionAuthorizationFacts(execution, sqlAuthorization)
	applyWorkflowExecutionAuthorizationFacts(execution, workflowAuthorization)

	if err := e.taskExecutionRepo.Create(ctx, execution); err != nil {
		return "", fmt.Errorf("failed to create execution record: %w", err)
	}

	log.Printf("🚀 [DevExecutor] 参数化执行已创建 execution_id=%s task_id=%d params=%v",
		executionID, itemID, executionInputs["submitted_parameters"])

	// 异步执行任务
	go e.executeAsync(
		execution.ID, executionID, tempItem, int(tenantID), sqlAuthorization, workflowAuthorization,
	)

	return executionID, nil
}

func (e *DevExecutor) prepareParameterizedDevTask(
	ctx context.Context,
	itemID uint,
	params map[string]interface{},
	tenantID uint,
	expectedTaskType string,
) (*preparedParameterizedDevTask, error) {
	if params == nil {
		params = map[string]interface{}{}
	}
	devTask, err := e.devTaskRepo.FindByID(itemID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("开发任务不存在")
	}
	if err := validateExpectedDevTaskType(devTask.DevType, expectedTaskType); err != nil {
		return nil, err
	}
	if devTask.Status != "active" {
		return nil, fmt.Errorf("开发任务状态为 %s，无法执行", devTask.Status)
	}

	resolvedContent, err := cloneWorkflowContent(devTask.Content)
	if err != nil {
		return nil, fmt.Errorf("复制开发任务内容失败: %w", err)
	}
	var effectiveParameters map[string]interface{}
	inputs := commonModels.JSONMap{
		"submitted_parameters": params,
	}
	if devTask.DevType == commonExecution.TaskTypeWorkflow {
		workflowDefinition, ok := resolvedContent["workflow_definition"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("工作流定义无效")
		}
		workflowEngineID := workflowEngineIDFromExecutionConfig(devTask.ExecutionConfig)
		if workflowEngineID == 0 {
			return nil, fmt.Errorf("工作流执行必须提供 execution_config.engine_id")
		}
		if e.operatorDiscovery == nil {
			return nil, fmt.Errorf("工作流参数契约服务不可用")
		}
		resolved, resolveErr := e.operatorDiscovery.resolveWorkflowExecutionParameters(
			ctx,
			workflowEngineID,
			workflowDefinition,
			params,
			tenantID,
		)
		if resolveErr != nil {
			return nil, &ExecutionParametersError{Cause: resolveErr}
		}
		resolvedContent["workflow_definition"] = resolved.Workflow
		effectiveParameters = resolved.EffectiveParameters
		inputs["workflow_definition"] = resolved.Workflow
		inputs["execution_contract"] = resolved.Contract
	} else if devTask.DevType == commonExecution.TaskTypeQuery {
		contract, effective, resolveErr := resolveQueryExecutionParameters(resolvedContent, params)
		if resolveErr != nil {
			return nil, &ExecutionParametersError{Cause: resolveErr}
		}
		effectiveParameters = effective
		inputs["execution_contract"] = contract
	} else {
		emptyContract := taskprovider.EmptyExecutionContract()
		if err := taskprovider.ValidateExecutionParameters(
			emptyContract.InputSchema,
			params,
			taskprovider.ParameterValidationOptions{},
		); err != nil {
			return nil, &ExecutionParametersError{Cause: err}
		}
		inputs["execution_contract"] = emptyContract
	}
	inputs["effective_parameters"] = effectiveParameters
	task := &models.DevTask{
		ID: devTask.ID, DevType: devTask.DevType, Content: resolvedContent,
		Timeout: devTask.Timeout, TenantID: tenantID, Status: devTask.Status,
		ExecutionConfig:   devTask.ExecutionConfig,
		RuntimeParameters: effectiveParameters,
	}
	if task.DevType == commonExecution.TaskTypeWorkflow {
		if err := e.validateWorkflowBeforeExecution(ctx, task.Content, task.ExecutionConfig, tenantID); err != nil {
			return nil, err
		}
	}
	return &preparedParameterizedDevTask{template: devTask, task: task, inputs: inputs}, nil
}

// ExecuteWithParamsFromParentExecution executes an Orchestrator child through
// the parent's durable User authorization facts. It never accepts a User token.
func (e *DevExecutor) ExecuteWithParamsFromParentExecution(
	ctx context.Context,
	itemID uint,
	params map[string]interface{},
	tenantID uint,
	triggerType string,
	source string,
	parentExecutionID string,
	expectedTaskType string,
) (string, error) {
	if e == nil || e.taskExecutionRepo == nil || tenantID == 0 {
		return "", fmt.Errorf("父执行授权上下文不可用")
	}
	normalizedTriggerType, err := commonExecution.NormalizeTriggerType(triggerType)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(source) != commonExecution.ModuleOrchestrator {
		return "", fmt.Errorf("父执行来源必须为 orchestrator")
	}
	parsedParentExecutionID, err := uuid.Parse(strings.TrimSpace(parentExecutionID))
	if err != nil {
		return "", fmt.Errorf("父执行 ID 无效: %w", err)
	}
	preparedTask, err := e.prepareParameterizedDevTask(ctx, itemID, params, tenantID, expectedTaskType)
	if err != nil {
		return "", err
	}
	if preparedTask.task.DevType != commonExecution.TaskTypeQuery &&
		preparedTask.task.DevType != commonExecution.TaskTypeWorkflow {
		return "", fmt.Errorf("任务类型 %s 尚未接入父执行授权", preparedTask.task.DevType)
	}
	parent, err := e.taskExecutionRepo.GetByExecutionID(ctx, parsedParentExecutionID.String(), int(tenantID))
	if err != nil || parent.Module != commonExecution.ModuleOrchestrator ||
		parent.Status != commonExecution.ExecutionStatusRunning || parent.ActorPrincipalID == nil ||
		parent.ActorTenantMembershipID == nil || parent.IssuedAuthorizationVersion == nil ||
		*parent.ActorPrincipalID <= 0 || *parent.ActorTenantMembershipID <= 0 || *parent.IssuedAuthorizationVersion <= 0 {
		return "", fmt.Errorf("父执行授权主体不可用")
	}

	executionUUID := uuid.New()
	executionID := executionUUID.String()
	parentID := parsedParentExecutionID.String()
	principalID := *parent.ActorPrincipalID
	membershipID := *parent.ActorTenantMembershipID
	authorizationVersion := *parent.IssuedAuthorizationVersion
	triggeredBy := int(principalID)
	now := time.Now()
	execution := &commonExecution.TaskExecution{
		TenantID: int(tenantID), ExecutionID: executionID, Module: commonExecution.ModuleDevelop,
		TaskType: preparedTask.template.DevType, Source: commonExecution.ModuleOrchestrator,
		SourceTaskID: commonExecution.NewSourceTaskIDFromUint(itemID), SourceTaskName: &preparedTask.template.Name,
		ParentExecutionID: &parentID, Status: commonExecution.ExecutionStatusPending, Progress: 0,
		TriggerType: normalizedTriggerType, TriggeredBy: &triggeredBy,
		ActorPrincipalID: &principalID, ActorTenantMembershipID: &membershipID,
		IssuedAuthorizationVersion: &authorizationVersion,
		ExecutionConfig:            devTaskExecutionRecordConfig(preparedTask.template, preparedTask.inputs),
		CreatedAt:                  now, UpdatedAt: now,
	}
	if err := e.taskExecutionRepo.Create(ctx, execution); err != nil {
		return "", fmt.Errorf("failed to create child execution record: %w", err)
	}

	var sqlAuthorization *IssuedSQLExecutionAuthorization
	var workflowAuthorization *IssuedWorkflowExecutionAuthorization
	switch preparedTask.task.DevType {
	case commonExecution.TaskTypeQuery:
		sqlContent, ok := preparedTask.task.Content["query"].(string)
		if !ok || strings.TrimSpace(sqlContent) == "" {
			err = fmt.Errorf("异步 SQL 执行上下文无效")
		} else if e.isFederatedQuery(ctx, preparedTask.task, tenantID) {
			var engineIDs []uint
			engineIDs, err = e.federatedReadEngineIDs(ctx, preparedTask.task, tenantID)
			if err == nil && len(engineIDs) > 0 {
				sqlAuthorization, err = e.sqlEngine.IssueFederatedReadExecutionAuthorizationFromExecution(
					ctx, tenantID, parsedParentExecutionID, executionUUID, engineIDs, preparedTask.task.Timeout,
				)
			}
		} else {
			engineID := preparedTask.task.GetEngineID()
			if engineID == nil || *engineID == 0 {
				err = fmt.Errorf("异步 SQL 执行上下文无效")
			} else {
				sqlAuthorization, err = e.sqlEngine.IssueSQLExecutionAuthorizationFromExecution(
					ctx, tenantID, parsedParentExecutionID, executionUUID, *engineID, sqlContent, preparedTask.task.Timeout,
				)
			}
		}
	case commonExecution.TaskTypeWorkflow:
		workflowAuthorization, err = e.prepareWorkflowExecutionAuthorizationFromExecution(
			ctx, preparedTask.task, tenantID, parsedParentExecutionID, executionUUID,
		)
	}
	if err != nil {
		return executionID, e.failPendingAuthorization(ctx, execution, err)
	}
	applySQLExecutionAuthorizationFacts(execution, sqlAuthorization)
	applyWorkflowExecutionAuthorizationFacts(execution, workflowAuthorization)
	if execution.ActorPrincipalID == nil || *execution.ActorPrincipalID != principalID ||
		execution.ActorTenantMembershipID == nil || *execution.ActorTenantMembershipID != membershipID ||
		execution.IssuedAuthorizationVersion == nil || *execution.IssuedAuthorizationVersion != authorizationVersion {
		return executionID, e.failPendingAuthorization(ctx, execution, fmt.Errorf("子执行授权主体与父执行不一致"))
	}
	execution.UpdatedAt = time.Now()
	if err := e.taskExecutionRepo.Update(ctx, execution); err != nil {
		return executionID, e.failPendingAuthorization(ctx, execution, fmt.Errorf("保存子执行授权失败: %w", err))
	}
	e.startPreparedContentExecution(&preparedContentExecution{
		execution: execution, devTask: preparedTask.task, tenantID: int(tenantID),
		sqlAuthorization: sqlAuthorization, workflowAuthorization: workflowAuthorization,
	})
	return executionID, nil
}

func (e *DevExecutor) failPendingAuthorization(
	ctx context.Context,
	execution *commonExecution.TaskExecution,
	cause error,
) error {
	completedAt := time.Now()
	execution.Status = commonExecution.ExecutionStatusFailed
	execution.Progress = 100
	execution.CompletedAt = &completedAt
	execution.ErrorDetails = commonModels.JSONMap{"message": cause.Error()}
	execution.UpdatedAt = completedAt
	if err := e.taskExecutionRepo.Update(ctx, execution); err != nil {
		return fmt.Errorf("%v; 收敛子执行失败状态失败: %w", cause, err)
	}
	return cause
}

func validateExpectedDevTaskType(devType, expectedTaskType string) error {
	if expectedTaskType != "" && devType != expectedTaskType {
		return fmt.Errorf("开发任务类型不匹配: task_type=%s, dev_type=%s", expectedTaskType, devType)
	}
	return nil
}

func devTaskExecutionRecordConfig(devTask *models.DevTask, inputs commonModels.JSONMap) commonModels.JSONMap {
	config := commonModels.JSONMap{
		"inputs": inputs,
	}
	if devTask != nil {
		config["engine_id"] = devTask.GetEngineID()
	}
	return config
}
