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
	commonquery "github.com/addp/common/query"
	"github.com/addp/common/resourcetree"

	engineselection "github.com/addp/common/engine/selection"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/taskprovider"
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
	notifyExecution          func()
	executionQueue           *repository.ExecutionRepository
	queryResults             *queryResultCache
}

// SetExecutionNotifier wakes the embedded query supervisor after a
// pending execution has committed. PostgreSQL polling remains the durable
// cross-instance fallback.
func (e *DevExecutor) SetExecutionNotifier(notify func()) {
	if e != nil {
		e.notifyExecution = notify
	}
}

func (e *DevExecutor) notifyPendingExecution() {
	if e != nil && e.notifyExecution != nil {
		e.notifyExecution()
	}
}

type federatedQueryExecutor interface {
	IsRuntime(ctx context.Context, tenantID, engineID uint) bool
	ReferencedEngineIDs(ctx context.Context, tenantID, runtimeEngineID uint, query string) ([]uint, error)
	ExecuteQuery(ctx context.Context, tenantID, runtimeEngineID uint, executionID uuid.UUID, authorizationID int64, query string, timeout int, limit int, sourceEngineIDs []uint) (*FederatedQueryResult, error)
}

type preparedContentExecution struct {
	execution *commonExecution.TaskExecution
	devTask   *models.DevTask
	tenantID  int
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
) *DevExecutor {
	return &DevExecutor{
		devTaskRepo:              devTaskRepo,
		queryResults:             newQueryResultCache(),
		taskExecutionRepo:        taskExecutionRepo,
		workflowEngine:           workflowEngine,
		operatorDiscovery:        operatorDiscovery,
		metaClient:               metaClient,
		sqlEngine:                sqlEngine,
		federatedQuery:           federatedQuery,
		notebookExecutionService: notebookExecutionService,
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
	if timeout <= 0 && devType != commonExecution.TaskTypeQuery {
		timeout = 300
	}

	// 生成执行ID
	executionID := uuid.New().String()

	inputs := commonModels.JSONMap{"submitted_parameters": parameters}
	var effectiveParameters map[string]interface{}
	var effectiveInputs map[string]interface{}
	var executionContract *taskprovider.ExecutionContract
	if devType == commonExecution.TaskTypeQuery {
		contract, effective, resolvedInputs, resolveErr := resolveQueryPreviewParameters(content, parameters)
		if resolveErr != nil {
			return nil, &ExecutionParametersError{Cause: resolveErr}
		}
		executionContract = contract
		effectiveParameters = effective
		effectiveInputs = resolvedInputs
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
	inputs["effective_inputs"] = effectiveInputs
	if devType == commonExecution.TaskTypeQuery {
		source, _ := content["query"].(string)
		inputs["query_source_digest"] = commonquery.SourceDigest(source)
	}

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
		TenantID:          tenantID,
		Content:           content,
		Timeout:           timeout,
		ExecutionConfig:   models.DevTaskContent(executionConfig),
		RuntimeParameters: effectiveParameters,
	}
	if devType == commonExecution.TaskTypeQuery {
		tempItem, err = e.compileRelationQueryPreview(ctx, tempItem, effectiveInputs, tenantID)
		if err != nil {
			return nil, err
		}
	}
	if devType == commonExecution.TaskTypeQuery {
		if err := e.freezeExecutionPolicy(ctx, tempItem, tenantID); err != nil {
			return nil, err
		}
	}
	if devType == commonExecution.TaskTypeQuery && tempItem.GetQueryType() == "sql" {
		engineID := tempItem.GetEngineID()
		queryText, _ := tempItem.Content["query"].(string)
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
	if devType == commonExecution.TaskTypeScript {
		if err := e.notebookExecutionService.FreezeContent(ctx, tempItem); err != nil {
			return nil, err
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
	applyScriptExecutionActor(ctx, execution)
	execution.ExecutionBoundary = commonExecution.ExecutionBoundaryBounded
	execution.MaxAttempts = 1
	execution.ExecutionConfig = devTaskExecutionRecordConfig(tempItem, inputs)

	return &preparedContentExecution{
		execution: execution, devTask: tempItem, tenantID: int(tenantID),
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
	e.notifyPendingExecution()
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
	ctx = contextWithQuerySnapshot(ctx, devTask)
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

// Progress has one path: only the current bounded lease can commit it and its event.
func (e *DevExecutor) updateExecutionStatus(ctx context.Context, executionID string, tenantID int, status string, progress int, currentStep string) error {
	lease, ok := commonExecution.LeaseFromContext(ctx)
	if !ok || lease.ExecutionID != executionID || lease.TenantID != tenantID || status != commonExecution.ExecutionStatusRunning {
		return fmt.Errorf("Develop progress requires the matching bounded execution lease")
	}
	if e.executionQueue == nil {
		return fmt.Errorf("Develop execution queue is unavailable")
	}
	return e.executionQueue.UpdateProgressWithLease(ctx, lease, progress, currentStep)
}

// executeWorkflow 执行工作流（支持 JSONB 配置）
func (e *DevExecutor) executeWorkflow(
	ctx context.Context,
	devTask *models.DevTask,
	executionID string,
	tenantID int,
	authorization *IssuedWorkflowExecutionAuthorization,
) (commonModels.JSONMap, commonModels.JSONMap, string) {
	log.Printf("🔵 [DevExecutor] executeWorkflow 开始: execution_id=%s", executionID)

	// 验证执行配置
	if devTask.ExecutionConfig == nil || len(devTask.ExecutionConfig) == 0 {
		return nil, nil, "工作流缺少执行配置，请配置工作流引擎"
	}

	// 解析工作流定义
	workflowDef, ok := devTask.Content["workflow_definition"].(map[string]interface{})
	if !ok {
		return nil, nil, "无效的工作流定义"
	}
	if err := ValidateWorkflowDefinition(workflowDef); err != nil {
		return nil, nil, err.Error()
	}
	if authorization == nil {
		return nil, nil, "异步工作流执行缺少 Execution Authorization"
	}

	if err := e.updateExecutionStatus(ctx, executionID, tenantID, commonExecution.ExecutionStatusRunning, 30, "执行工作流"); err != nil {
		return nil, nil, err.Error()
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

	log.Printf("[DevExecutor] 调用工作流引擎: execution_id=%s", executionID)
	// 调用工作流引擎
	parsedExecutionID, err := uuid.Parse(executionID)
	if err != nil {
		return nil, nil, "工作流执行 ID 无效"
	}
	resp, err := e.workflowEngine.ExecuteWorkflow(
		execCtx, uint(tenantID), parsedExecutionID, workflowDef, inputData, configStr, authorization,
	)
	if err != nil {
		log.Printf("❌ [DevExecutor] 工作流引擎调用失败: execution_id=%s err=%v", executionID, err)
		if resp == nil {
			return nil, nil, fmt.Sprintf("工作流执行失败: %v", err)
		}
	}

	if err == nil {
		log.Printf("🔵 [DevExecutor] 工作流引擎返回成功: execution_id=%s", executionID)
		if err := e.updateExecutionStatus(ctx, executionID, tenantID, commonExecution.ExecutionStatusRunning, 90, "工作流执行成功"); err != nil {
			return nil, nil, err.Error()
		}
	}

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
	if finalResult, ok := workflowFinalResultForExecution(resp.FinalResult); ok {
		result["final_result"] = finalResult
	}
	if resp.ExecutionTimeMs != nil {
		result["execution_time_ms"] = *resp.ExecutionTimeMs
	}
	if resp.RuntimeStatus != nil {
		result["runtime_status"] = resp.RuntimeStatus
	}
	outputs := commonModels.JSONMap{}
	if len(resp.ProducedTargets) > 0 {
		result["produced_targets"] = resp.ProducedTargets
		outputs = workflowExecutionOutputs(resp.ProducedTargets)
		result["meta_scan_runs"] = e.createWorkflowProducedTargetScanRuns(ctx, uint(tenantID), executionID, resp.ProducedTargets)
	}

	log.Printf("🔵 [DevExecutor] executeWorkflow 结束: execution_id=%s", executionID)
	if err != nil {
		return result, outputs, fmt.Sprintf("工作流执行失败: %v", err)
	}
	return result, outputs, ""
}

const workflowExecutionResultPreviewLimitBytes = 64 * 1024

// workflowFinalResultForExecution keeps small, inspectable workflow results in the
// execution record. Large results remain runtime-only and are represented by the
// summary above so execution history does not become a data store.
func workflowFinalResultForExecution(raw string) (interface{}, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len([]byte(raw)) > workflowExecutionResultPreviewLimitBytes {
		return nil, false
	}
	var decoded interface{}
	if err := json.Unmarshal([]byte(raw), &decoded); err == nil {
		return decoded, true
	}
	return raw, true
}

func workflowExecutionOutputs(targets []WorkflowProducedTarget) commonModels.JSONMap {
	outputs := commonModels.JSONMap{}
	for _, target := range targets {
		if strings.TrimSpace(target.TaskID) == "" {
			continue
		}
		resource := map[string]interface{}{"locator": target.Locator, "type": target.Type}
		if target.WriteMode != "" {
			resource["write_mode"] = target.WriteMode
		}
		if target.TileKey != "" {
			node, ok := outputs[target.TaskID].(map[string]interface{})
			if !ok {
				node = map[string]interface{}{"tiles": map[string]interface{}{}}
				outputs[target.TaskID] = node
			}
			node["tiles"].(map[string]interface{})[target.TileKey] = map[string]interface{}{"resource": resource}
		} else {
			outputs[target.TaskID] = map[string]interface{}{"resource": resource}
		}
	}
	return outputs
}

func developLineageFacts(devTask *models.DevTask, outputs commonModels.JSONMap) *commonExecution.LineageFacts {
	if devTask == nil {
		return nil
	}
	if devTask.DevType == commonExecution.TaskTypeWorkflow {
		return workflowLineageFacts(devTask.Content["workflow_definition"], outputs)
	}
	inputs := collectLineageRefs(devTask.Content["inputs"], "input")
	outputRefs := collectLineageRefs(outputs, "output")
	if targetLocator := devTask.GetTargetLocator(); strings.TrimSpace(targetLocator) != "" {
		outputRefs = append(outputRefs, commonExecution.LineageResourceRef{Port: "output", Locator: targetLocator})
	}
	if len(outputRefs) == 0 || len(inputs) == 0 {
		return nil
	}
	return &commonExecution.LineageFacts{
		SchemaVersion: commonExecution.LineageFactsSchemaVersion,
		Inputs:        inputs, Outputs: outputRefs,
		Operations: []commonExecution.LineageOperation{{Kind: "derive", Operator: "develop", InputPorts: []string{"input"}, OutputPorts: []string{"output"}}},
	}
}

func collectLineageRefs(value interface{}, port string) []commonExecution.LineageResourceRef {
	refs := make([]commonExecution.LineageResourceRef, 0)
	var visit func(interface{})
	visit = func(current interface{}) {
		switch typed := current.(type) {
		case commonModels.JSONMap:
			visit(map[string]interface{}(typed))
		case models.DevTaskContent:
			visit(map[string]interface{}(typed))
		case []interface{}:
			for _, item := range typed {
				visit(item)
			}
		case map[string]interface{}:
			locator, _ := typed["locator"].(string)
			if strings.TrimSpace(locator) != "" {
				ref := commonExecution.LineageResourceRef{Port: port, Locator: locator}
				if writeMode, ok := typed["write_mode"].(string); ok {
					ref.WriteMode = strings.TrimSpace(writeMode)
				}
				if rawID, ok := typed["item_id"].(float64); ok && rawID > 0 {
					id := uint(rawID)
					ref.ItemID = &id
				}
				refs = append(refs, ref)
			}
			for _, key := range workflowLineageMapKeys(typed) {
				visit(typed[key])
			}
		}
	}
	visit(value)
	return refs
}

func (e *DevExecutor) notifyExecutionLineage(ctx context.Context, tenantID uint, executionID string) {
	if e == nil || e.metaClient == nil || tenantID == 0 || strings.TrimSpace(executionID) == "" {
		return
	}
	result, err := e.metaClient.WithTenantID(tenantID).CollectExecutionLineage(ctx, executionID)
	if err != nil {
		log.Printf("⚠️  [DevExecutor] Meta 血缘立即采集失败，等待周期 collector 重试 execution_id=%s err=%v", executionID, err)
		return
	}
	log.Printf("✅ [DevExecutor] Meta 血缘采集完成 execution_id=%s observed=%d skipped=%d", executionID, result.Observed, result.Skipped)
}

func (e *DevExecutor) createWorkflowProducedTargetScanRuns(
	ctx context.Context,
	tenantID uint,
	executionID string,
	targets []WorkflowProducedTarget,
) []map[string]interface{} {
	if e.metaClient == nil || len(targets) == 0 {
		return nil
	}
	// Freeze actual outputs before the service call: Meta verifies this durable
	// provenance inside the transaction that creates each scan execution.
	parent, err := e.taskExecutionRepo.GetByExecutionID(ctx, executionID, int(tenantID))
	if err == nil && parent.Module == commonExecution.ModuleDevelop && parent.TaskType == commonExecution.TaskTypeWorkflow && parent.Status == commonExecution.ExecutionStatusRunning {
		if parent.Metadata == nil {
			parent.Metadata = commonModels.JSONMap{}
		}
		parent.Metadata["outputs"] = workflowExecutionOutputs(targets)
		result := map[string]interface{}{}
		if existing, exists := parent.Metadata["result"]; exists {
			encoded, encodeErr := json.Marshal(existing)
			if encodeErr != nil || json.Unmarshal(encoded, &result) != nil || result == nil {
				return []map[string]interface{}{{"status": "failed", "error_code": "develop.workflow.scan_provenance_unavailable"}}
			}
		}
		result["produced_targets"] = targets
		parent.Metadata["result"] = result
		err = e.taskExecutionRepo.UpdateFields(ctx, executionID, int(tenantID), map[string]interface{}{"metadata": parent.Metadata})
	} else if err == nil {
		err = fmt.Errorf("workflow output provenance is unavailable")
	}
	if err != nil {
		return []map[string]interface{}{{"status": "failed", "error_code": "develop.workflow.scan_provenance_unavailable"}}
	}
	scanRuns := make([]map[string]interface{}, 0, len(targets))
	metaClient := e.metaClient.WithTenantID(tenantID)
	for _, target := range targets {
		if strings.TrimSpace(target.Locator) == "" || target.EngineID == 0 {
			continue
		}
		opts := workflowProducedTargetScanOptions(target)
		opts.ParentExecutionID = executionID
		run, err := metaClient.CreateManualScanRun(opts)
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
		Source:      commonExecution.ModuleDevelop,
		Targets:     []string{target.Locator},
	}
	if target.Layout == "whole" {
		locator, err := resourcetree.ParseURI(target.Locator)
		if err == nil {
			locator.Type = resourcetree.TypePrefix
			opts.Targets = []string{locator.ToURI()}
		}
		return opts
	}
	if strings.EqualFold(target.Type, "file") && len(target.Path) > 0 {
		opts.Targets = nil
		opts.RefGroups = []commonClient.MetaScanRefGroup{{
			Primary: strings.Join(target.Path, "/"),
		}}
		return opts
	}
	opts.Targets = []string{target.Locator}
	return opts
}

// executeQuery 执行查询（根据query_type路由）
func (e *DevExecutor) executeQuery(ctx context.Context, devTask *models.DevTask, executionID string, tenantID int, authorization *IssuedSQLExecutionAuthorization) (commonModels.JSONMap, *int64, error) {
	queryType := devTask.GetQueryType()

	// 根据 query_type 路由到不同执行器
	switch queryType {
	case "sql", "mql", "cypher", "es_dsl":
		return e.executeSQL(ctx, devTask, executionID, tenantID, authorization)
	default:
		return nil, nil, fmt.Errorf("不支持的查询类型: %s", queryType)
	}
}

// executeSQL 执行SQL
func (e *DevExecutor) executeSQL(ctx context.Context, devTask *models.DevTask, executionID string, tenantID int, authorization *IssuedSQLExecutionAuthorization) (commonModels.JSONMap, *int64, error) {
	sqlContent, ok := devTask.Content["query"].(string)
	parsedExecutionID, err := uuid.Parse(executionID)
	if !ok || err != nil {
		return nil, nil, fmt.Errorf("异步 SQL 执行上下文无效")
	}
	if e.isFederatedQuery(ctx, devTask, uint(tenantID)) {
		if devTask.RuntimeParameters != nil {
			return nil, nil, fmt.Errorf("联邦查询 Runtime 不支持查询参数")
		}
		if e.federatedQuery == nil || authorization == nil {
			return nil, nil, fmt.Errorf("联邦查询服务或 Execution Authorization 未初始化")
		}
		runtimeEngineID := devTask.GetEngineID()
		if runtimeEngineID == nil {
			return nil, nil, fmt.Errorf("联邦查询 Runtime Engine 无效")
		}
		federatedResult, executeErr := e.federatedQuery.ExecuteQuery(
			ctx, uint(tenantID), *runtimeEngineID, parsedExecutionID, authorization.AuthorizationID,
			sqlContent, devTask.Timeout, (devTask.QueryResultLimit + 1), authorization.EngineIDs,
		)
		if executeErr != nil {
			return nil, nil, fmt.Errorf("联邦查询执行失败: %w", executeErr)
		}
		response, affected := e.queryResult(
			devTask.QueryResultLimit,
			federatedResult.Columns, federatedResult.Rows, int64(federatedResult.RowCount),
			SQLExecutionEffectRead, "table", nil,
		)
		return response, affected, nil
	}
	if authorization == nil {
		return nil, nil, fmt.Errorf("异步 SQL 执行缺少 Execution Authorization")
	}
	engineID := devTask.GetEngineID()
	if engineID == nil {
		return nil, nil, fmt.Errorf("异步 SQL 执行上下文无效")
	}
	var result *SQLResult
	if devTask.GetQueryType() == "sql" {
		result, err = e.sqlEngine.ExecuteIssuedSQLAuthorization(
			ctx, uint(tenantID), parsedExecutionID, *engineID, sqlContent, devTask.RuntimeParameters, devTask.Timeout, (devTask.QueryResultLimit + 1), authorization,
		)
	} else {
		engine, accessErr := e.sqlEngine.executionEngine(ctx, uint(tenantID), parsedExecutionID, *engineID, authorization)
		if accessErr != nil {
			return nil, nil, fmt.Errorf("查询执行授权失败: %w", accessErr)
		}
		if supportErr := requireQueryParameterCapability(engine, devTask.GetQueryType(), devTask.RuntimeParameters); supportErr != nil {
			return nil, nil, supportErr
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
		var targetPath *plugin.EngineCatalogPath
		if locatorURI := devTask.GetTargetLocator(); locatorURI != "" {
			locator, locatorErr := resourcetree.ParseURI(locatorURI)
			if locatorErr != nil || locator.EngineID != *engineID {
				if locatorErr == nil {
					locatorErr = fmt.Errorf("资源定位符引擎 ID 不匹配")
				}
				return nil, nil, fmt.Errorf("查询目标无效: %w", locatorErr)
			}
			model, modelErr := dbbridge.EngineCatalogModel(engine.EngineType)
			if modelErr != nil {
				return nil, nil, fmt.Errorf("查询目标无效: %w", modelErr)
			}
			path, pathErr := resourcetree.EngineCatalogPathFromLocator(model, locator)
			if pathErr != nil {
				return nil, nil, fmt.Errorf("查询目标无效: %w", pathErr)
			}
			targetPath = &path
		}
		if engineSupportsQueryResultKind(engine, "graph") {
			if e.sqlEngine == nil || e.sqlEngine.protectionGate == nil {
				return nil, nil, fmt.Errorf("Develop 图查询保护门禁未配置")
			}
			enginePlugin, pluginErr := plugin.Get(engine.EngineType)
			if pluginErr != nil {
				return nil, nil, pluginErr
			}
			provider, ok := enginePlugin.(plugin.PreparedGraphQueryProvider)
			if !ok {
				return nil, nil, fmt.Errorf("引擎未提供可验证的图查询运行时")
			}
			prepared, prepareErr := provider.PrepareGraphQuery(execCtx, plugin.ConnectionInfo(engine.ConnectionInfo), plugin.QueryRequest{
				EngineID: engine.ID, Language: devTask.GetQueryType(), Query: sqlContent, TargetPath: targetPath,
				Options: plugin.QueryOptions{EngineID: engine.ID, EngineType: engine.EngineType, Limit: devTask.QueryResultLimit + 1, ReadOnly: true, Parameters: devTask.RuntimeParameters},
			})
			if prepareErr != nil {
				return nil, nil, fmt.Errorf("查询执行失败: %w", prepareErr)
			}
			endProtection, protectionErr := e.sqlEngine.protectionGate.BeginPreparedGraphQuery(execCtx, uint(tenantID), enginePlugin, prepared)
			if protectionErr != nil {
				return nil, nil, fmt.Errorf("查询执行失败: %w", protectionErr)
			}
			graphResult, graphErr := prepared.ExecuteGraph(execCtx)
			endProtection()
			queryErr = graphErr
			if graphResult != nil {
				queryResult = &graphResult.QueryResult
				graphData = graphResult.GraphData
			}
		} else {
			enginePlugin, pluginErr := plugin.Get(engine.EngineType)
			if pluginErr != nil {
				queryErr = pluginErr
			} else if provider, ok := enginePlugin.(plugin.QueryRuntimeProvider); !ok {
				queryErr = fmt.Errorf("引擎不支持普通查询运行时")
			} else if e.sqlEngine == nil || e.sqlEngine.protectionGate == nil {
				queryErr = fmt.Errorf("Develop 查询保护门禁未配置")
			} else {
				prepared, prepareErr := provider.PrepareQuery(execCtx, plugin.ConnectionInfo(engine.ConnectionInfo), plugin.QueryRequest{
					EngineID: engine.ID, Language: devTask.GetQueryType(), Query: sqlContent, TargetPath: targetPath,
					Options: plugin.QueryOptions{
						EngineID: engine.ID, EngineType: engine.EngineType, Limit: (devTask.QueryResultLimit + 1),
						ReadOnly: true, Parameters: devTask.RuntimeParameters,
					},
				})
				if prepareErr != nil {
					queryErr = prepareErr
				} else if protectResult, endProtection, gateErr := e.sqlEngine.protectionGate.BeginPreparedQuery(execCtx, uint(tenantID), enginePlugin, prepared); gateErr != nil {
					queryErr = gateErr
				} else {
					queryResult, queryErr = prepared.Execute(execCtx)
					if queryErr == nil {
						queryErr = protectResult(queryResult)
					}
					endProtection()
				}
			}
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
				response, affected := e.queryResult(devTask.QueryResultLimit, result.Columns, result.Rows, result.RowsAffected, result.Effect, resultKind, graphData)
				return response, affected, nil
			}
		}
	}
	if err != nil {
		return nil, nil, fmt.Errorf("查询执行失败: %w", err)
	}
	response, affected := e.queryResult(devTask.QueryResultLimit, result.Columns, result.Rows, result.RowsAffected, result.Effect, "table", nil)
	return response, affected, nil
}

func (e *DevExecutor) queryResult(
	limit int,
	columns []string,
	rows []map[string]interface{},
	rowsAffected int64,
	effect SQLExecutionEffect,
	resultKind string,
	graphData *plugin.GraphData,
) (commonModels.JSONMap, *int64) {
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
	result := commonModels.JSONMap{
		"columns":      columns,
		"rows_count":   len(rows),
		"effect":       effect,
		"result_kind":  resultKind,
		"result_limit": limit,
		"truncated":    truncated,
		"summary": map[string]interface{}{
			"column_count": len(columns),
			"preview_rows": rows,
		},
	}
	if graphData != nil {
		result["graph_data"] = graphData
	}
	if effect == SQLExecutionEffectRead {
		return result, nil
	}
	result["rows_affected"] = rowsAffected
	return result, &rowsAffected
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
	capabilities, err := engineselection.ParseCapabilities(engine.Capabilities)
	if err != nil || capabilities.Compute == nil || capabilities.Compute.Query == nil {
		return false
	}
	return slices.Contains(capabilities.Compute.Query.ResultKinds, kind)
}

func requireQueryParameterCapability(engine *commonModels.Engine, language string, parameters map[string]interface{}) error {
	if parameters == nil {
		return nil
	}
	capabilities, err := engineselection.ParseCapabilities(engine.Capabilities)
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
func (e *DevExecutor) GetExecution(ctx context.Context, executionID string, tenantID uint) (*models.ExecutionWithDevTask, error) {
	repo, readCtx := e.professionalExecutionRepository(ctx, tenantID)
	execution, err := repo.GetByExecutionID(readCtx, executionID, int(tenantID))
	if err != nil {
		return nil, err
	}

	result := professionalExecutionResponse(execution)

	// 加载关联的开发任务
	if execution.SourceTaskID != nil && e.devTaskRepo != nil {
		if taskID, parseErr := commonExecution.ParseSourceTaskIDUint(execution.SourceTaskID); parseErr == nil {
			devTask, err := e.devTaskRepo.FindByID(taskID, tenantID)
			if err == nil {
				result.DevTask = devTask
			}
		}
	}

	return result, nil
}

// GetTaskProviderExecution 返回 Develop 的统一执行记录，供 TaskProvider 状态接口使用。
func (e *DevExecutor) GetTaskProviderExecution(ctx context.Context, executionID string, tenantID uint) (*commonExecution.TaskExecution, error) {
	execution, err := e.taskExecutionRepo.GetByExecutionID(ctx, executionID, int(tenantID))
	if err != nil || execution.Module != commonExecution.ModuleDevelop || execution.Source != commonExecution.ModuleOrchestrator || execution.ParentExecutionID == nil {
		return nil, fmt.Errorf("执行记录不存在")
	}
	parent, err := e.taskExecutionRepo.GetByExecutionID(ctx, *execution.ParentExecutionID, int(tenantID))
	if err != nil || parent.Module != commonExecution.ModuleOrchestrator {
		return nil, fmt.Errorf("执行记录不存在")
	}
	return execution, nil
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

func (e *DevExecutor) GetExecutionTaskType(ctx context.Context, executionID string, tenantID uint) (string, error) {
	if e == nil || e.taskExecutionRepo == nil || strings.TrimSpace(executionID) == "" || tenantID == 0 {
		return "", fmt.Errorf("执行查询上下文无效")
	}
	repo, readCtx := e.professionalExecutionRepository(ctx, tenantID)
	execution, err := repo.GetByExecutionID(readCtx, executionID, int(tenantID))
	if err != nil {
		return "", fmt.Errorf("执行记录不存在")
	}
	return execution.TaskType, nil
}

// ListExecutions 查询执行列表
func (e *DevExecutor) ListExecutions(ctx context.Context, req *models.ListExecutionsRequest, tenantID uint) ([]models.ExecutionWithDevTask, int64, error) {
	// 设置默认分页
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize > 100 {
		req.PageSize = 100
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
	repo, readCtx := e.professionalExecutionRepository(ctx, tenantID)
	executions, total, err := repo.List(readCtx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list executions: %w", err)
	}

	// 直接映射，加载关联开发任务
	result := make([]models.ExecutionWithDevTask, len(executions))
	for i, exec := range executions {
		visible := professionalExecutionResponse(exec)
		result[i] = *visible

		// 加载关联的开发任务
		if exec.SourceTaskID != nil && e.devTaskRepo != nil {
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
func (e *DevExecutor) RetryExecution(ctx context.Context, executionID string, tenantID uint, userID uint, userAccessToken string) (string, error) {
	// 获取原执行记录
	repo, readCtx := e.professionalExecutionRepository(ctx, tenantID)
	execution, err := repo.GetByExecutionID(readCtx, executionID, int(tenantID))
	if err != nil {
		return "", fmt.Errorf("执行记录不存在")
	}

	// 如果有关联的开发任务，直接重新执行
	if execution.SourceTaskID != nil {
		taskID, err := commonExecution.ParseSourceTaskIDUint(execution.SourceTaskID)
		if err != nil {
			return "", err
		}
		return e.ExecuteDevTask(ctx, taskID, tenantID, userID, userAccessToken, "manual")
	}

	// 否则报错（临时内容不支持重试）
	return "", fmt.Errorf("临时执行内容不支持重试")
}

// GetStatistics 获取执行统计信息
func (e *DevExecutor) GetStatistics(ctx context.Context, tenantID uint, sourceTaskID string, startDate, endDate string) (*models.ExecutionStatistics, error) {
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

	repo, readCtx := e.professionalExecutionRepository(ctx, tenantID)
	stats, err := repo.GetStatistics(readCtx, filter)
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
	preparedTask, err := e.prepareParameterizedDevTask(ctx, itemID, params, tenantID, expectedTaskType, false)
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
		ExecutionConfig:   devTaskExecutionRecordConfig(tempItem, executionInputs),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	execution.ExecutionBoundary = commonExecution.ExecutionBoundaryBounded
	execution.MaxAttempts = 1
	applySQLExecutionAuthorizationFacts(execution, sqlAuthorization)
	applyWorkflowExecutionAuthorizationFacts(execution, workflowAuthorization)
	applyScriptExecutionActor(ctx, execution)

	if err := e.taskExecutionRepo.Create(ctx, execution); err != nil {
		return "", fmt.Errorf("failed to create execution record: %w", err)
	}

	e.notifyPendingExecution()

	return executionID, nil
}

func (e *DevExecutor) prepareParameterizedDevTask(
	ctx context.Context,
	itemID uint,
	params map[string]interface{},
	tenantID uint,
	expectedTaskType string,
	materializeRelationResult bool,
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
	var effectiveInputs map[string]interface{}
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
		var contract *taskprovider.ExecutionContract
		var resolveErr error
		if materializeRelationResult {
			contract, effectiveParameters, effectiveInputs, resolveErr = resolveQueryOrchestrationParameters(resolvedContent, params)
		} else {
			contract, effectiveParameters, effectiveInputs, resolveErr = resolveQueryPreviewParameters(resolvedContent, params)
		}
		if resolveErr != nil {
			return nil, &ExecutionParametersError{Cause: resolveErr}
		}
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
	inputs["effective_inputs"] = effectiveInputs
	if devTask.DevType == commonExecution.TaskTypeQuery {
		source, _ := resolvedContent["query"].(string)
		inputs["query_source_digest"] = commonquery.SourceDigest(source)
	}
	task := &models.DevTask{
		ID: devTask.ID, DevType: devTask.DevType, Content: resolvedContent,
		Timeout: devTask.Timeout, TenantID: tenantID, Status: devTask.Status,
		ExecutionConfig:   devTask.ExecutionConfig,
		RuntimeParameters: effectiveParameters,
	}
	if task.DevType == commonExecution.TaskTypeQuery && !materializeRelationResult {
		task, err = e.compileRelationQueryPreview(ctx, task, effectiveInputs, tenantID)
		if err != nil {
			return nil, err
		}
	}
	if task.DevType == commonExecution.TaskTypeWorkflow {
		if err := e.validateWorkflowBeforeExecution(ctx, task.Content, task.ExecutionConfig, tenantID); err != nil {
			return nil, err
		}
	}
	if task.DevType == commonExecution.TaskTypeQuery {
		if err := e.freezeExecutionPolicy(ctx, task, tenantID); err != nil {
			return nil, err
		}
	}
	if task.DevType == commonExecution.TaskTypeScript {
		if err := e.notebookExecutionService.FreezeContent(ctx, task); err != nil {
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
	preparedTask, err := e.prepareParameterizedDevTask(ctx, itemID, params, tenantID, expectedTaskType, true)
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
		ExecutionBoundary: commonExecution.ExecutionBoundaryBounded, MaxAttempts: 1,
		TriggerType: normalizedTriggerType, TriggeredBy: &triggeredBy,
		ActorPrincipalID: &principalID, ActorTenantMembershipID: &membershipID,
		IssuedAuthorizationVersion: &authorizationVersion,
		ExecutionConfig:            devTaskExecutionRecordConfig(preparedTask.task, preparedTask.inputs),
		CreatedAt:                  now, UpdatedAt: now,
	}
	if err := e.taskExecutionRepo.Create(ctx, execution); err != nil {
		return "", fmt.Errorf("failed to create child execution record: %w", err)
	}
	e.notifyPendingExecution()
	return executionID, nil
}

func validateExpectedDevTaskType(devType, expectedTaskType string) error {
	if expectedTaskType != "" && devType != expectedTaskType {
		return fmt.Errorf("开发任务类型不匹配: task_type=%s, dev_type=%s", expectedTaskType, devType)
	}
	return nil
}

func devTaskExecutionRecordConfig(devTask *models.DevTask, inputs commonModels.JSONMap) commonModels.JSONMap {
	config := commonModels.JSONMap{}
	if devTask != nil {
		for key, value := range devTask.ExecutionConfig {
			config[key] = value
		}
		config["engine_id"] = devTask.GetEngineID()
		config["content"] = devTask.Content
		config["timeout"] = devTask.Timeout
		if devTask.DevType == commonExecution.TaskTypeQuery {
			config["query_result_limit"] = devTask.QueryResultLimit
		}
		if devTask.RuntimeParameters != nil {
			config["runtime_parameters"] = devTask.RuntimeParameters
		}
		if _, relationResult, _ := relationParameterBindingsFromContent(devTask.Content); relationResult {
			if effective, ok := mapValue(inputs["effective_inputs"]); ok {
				config["runtime_inputs"] = commonModels.JSONMap(effective)
			}
		}
	}
	config["inputs"] = inputs
	return config
}

func (e *DevExecutor) compileRelationQueryPreview(
	ctx context.Context,
	task *models.DevTask,
	effectiveInputs map[string]interface{},
	tenantID uint,
) (*models.DevTask, error) {
	if task == nil {
		return nil, fmt.Errorf("查询预览任务不完整")
	}
	_, hasRelationParameters, err := relationParameterBindingsFromContent(task.Content)
	if err != nil || !hasRelationParameters {
		return task, err
	}
	engineID := task.GetEngineID()
	if engineID == nil || *engineID == 0 || e == nil || e.sqlEngine == nil || e.sqlEngine.systemService == nil {
		return nil, fmt.Errorf("relation 查询参数预览缺少查询引擎")
	}
	descriptor, err := e.sqlEngine.systemService.WithTenantID(tenantID).GetEngineRuntimeDescriptor(ctx, *engineID)
	if err != nil {
		return nil, fmt.Errorf("获取查询引擎描述失败: %w", err)
	}
	return compileRelationPreviewQuery(task, effectiveInputs, descriptor.EngineType)
}
