package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var (
	ErrCleanupExecuteConfirmRequired      = errors.New("resource reclaim execute confirmation is required")
	ErrCleanupExecuteConfirmTokenRequired = errors.New("resource reclaim execute confirmation token is required")
)

// CleanupOrchestratorService 是资源回收协调方。
// 它只负责发起请求、记录 execution、发布事件、汇总结果和写审计，不进入 Orchestrator 任务编排。
type CleanupOrchestratorService struct {
	redis       *redis.Client
	execRepo    *commonExecution.TaskExecutionRepository
	auditWriter CleanupAuditWriter
	registry    *ModuleRegistryService
	log         *slog.Logger
}

type CleanupAuditWriter interface {
	Write(context.Context, iam.AuditEvent) error
}

// NewCleanupOrchestratorService 创建资源回收协调服务
func NewCleanupOrchestratorService(
	redisClient *redis.Client,
	execRepo *commonExecution.TaskExecutionRepository,
	auditWriter CleanupAuditWriter,
	registry *ModuleRegistryService,
) *CleanupOrchestratorService {
	return &CleanupOrchestratorService{
		redis:       redisClient,
		execRepo:    execRepo,
		auditWriter: auditWriter,
		registry:    registry,
		log:         logger.With("component", "cleanup_orchestrator"),
	}
}

// CreateScanTask 创建扫描任务
func (s *CleanupOrchestratorService) CreateScanTask(ctx context.Context, tenantID uint, scope []string, userID uint) (string, error) {
	return s.createScanTask(ctx, tenantID, scope, userID, commonExecution.TriggerTypeManual, "", nil)
}

func (s *CleanupOrchestratorService) CreateEventScanTask(
	ctx context.Context,
	tenantID uint,
	scope []string,
	userID uint,
	causeEvent string,
	cleanupContext map[string]interface{},
) (string, error) {
	triggerType := commonExecution.TriggerTypeEvent
	return s.createScanTask(ctx, tenantID, scope, userID, triggerType, causeEvent, cleanupContext)
}

func (s *CleanupOrchestratorService) CreateEngineDeletionAssessment(
	ctx context.Context,
	tenantID uint,
	userID uint,
	cleanupContext map[string]interface{},
) (string, error) {
	return s.createScanTask(
		ctx,
		tenantID,
		nil,
		userID,
		commonExecution.TriggerTypeManual,
		events.CleanupCauseEngineDeleting,
		cleanupContext,
	)
}

func (s *CleanupOrchestratorService) createScanTask(
	ctx context.Context,
	tenantID uint,
	scope []string,
	userID uint,
	triggerType string,
	causeEvent string,
	cleanupContext map[string]interface{},
) (string, error) {
	taskID := fmt.Sprintf("cleanup-scan-%d-%s", time.Now().Unix(), uuid.New().String()[:8])
	now := time.Now()

	scope, err := s.resolveExpectedModulesForCause(scope, causeEvent)
	if err != nil {
		return "", err
	}

	executionID, err := s.createParentExecution(ctx, tenantID, taskID, events.CleanupActionScan, "", triggerType, causeEvent, "", scope, cleanupContext, userID, 30*time.Second)
	if err != nil {
		return "", err
	}

	task := buildCleanupScanTask(
		taskID,
		tenantID,
		scope,
		userID,
		triggerType,
		causeEvent,
		cleanupContext,
		executionID,
		now.Format(time.RFC3339),
		now.Add(30*time.Second).Format(time.RFC3339),
	)

	// 写入Redis
	taskJSON, err := json.Marshal(task)
	if err != nil {
		return "", fmt.Errorf("序列化任务失败: %w", err)
	}

	taskKey := fmt.Sprintf("cleanup:tasks:%s", taskID)
	err = s.redis.HSet(ctx, taskKey, "data", string(taskJSON)).Err()
	if err != nil {
		return "", fmt.Errorf("保存任务失败: %w", err)
	}

	// 设置过期时间（1小时）
	s.redis.Expire(ctx, taskKey, 1*time.Hour)

	// 发布事件到 Redis Stream
	event := events.CleanupRequestEvent{
		TaskID:            taskID,
		Action:            events.CleanupActionScan,
		TenantID:          tenantID,
		TriggerType:       triggerType,
		CauseEvent:        causeEvent,
		ExpectedModules:   scope,
		ParentExecutionID: executionID,
		Context:           cleanupContext,
		RequestedBy:       userID,
		RequestedAt:       now,
	}

	if err := s.publishEvent(ctx, event); err != nil {
		return "", fmt.Errorf("发布事件失败: %w", err)
	}
	s.startTaskStatusWatcher(taskID, now, 30*time.Second)

	// 记录历史
	historyKey := fmt.Sprintf("cleanup:history:%d", tenantID)
	s.redis.LPush(ctx, historyKey, taskID)
	s.redis.LTrim(ctx, historyKey, 0, 99) // 只保留最近100条
	s.writeAuditLog(ctx, userID, &tenantID, "cleanup.scan.created", taskID, map[string]interface{}{
		"expected_modules": scope,
		"trigger_type":     triggerType,
		"cause_event":      causeEvent,
		"execution_id":     executionID,
		"context":          cleanupContext,
	})

	s.log.Info("扫描任务已创建",
		"task_id", taskID,
		"tenant_id", tenantID,
		"scope", scope,
		"user_id", userID)

	return taskID, nil
}

// CreateExecuteTask 创建资源回收执行任务
type CleanupExecuteConfirmation struct {
	Confirmed         bool
	ConfirmationToken string
}

func (s *CleanupOrchestratorService) CreateExecuteTask(
	ctx context.Context,
	basedOnScan string,
	cleanupMode string,
	userID uint,
	confirmation CleanupExecuteConfirmation,
) (string, error) {
	if err := events.ValidateCleanupMode(cleanupMode); err != nil {
		return "", err
	}

	scanTask, err := s.GetTaskStatus(ctx, basedOnScan)
	if err != nil {
		return "", fmt.Errorf("扫描任务不存在: %w", err)
	}
	if err := validateExecutableScanTask(scanTask); err != nil {
		return "", err
	}
	scanSummary := scanSummaryForConfirmation(scanTask.Summary)
	if err := validateCleanupExecuteConfirmation(cleanupMode, scanSummary, confirmation); err != nil {
		return "", err
	}

	// 生成任务ID
	taskID := fmt.Sprintf("cleanup-exec-%d-%s", time.Now().Unix(), uuid.New().String()[:8])
	now := time.Now()
	triggerType := commonExecution.TriggerTypeManual

	executionID, err := s.createParentExecution(ctx, scanTask.Task.TenantID, taskID, events.CleanupActionExecute, cleanupMode, triggerType, scanTask.Task.CauseEvent, basedOnScan, scanTask.Task.ExpectedModules, scanTask.Task.Context, userID, 5*time.Minute)
	if err != nil {
		return "", err
	}

	// 创建任务
	task := models.CleanupTask{
		TaskID:          taskID,
		Action:          events.CleanupActionExecute,
		TenantID:        scanTask.Task.TenantID,
		CleanupMode:     cleanupMode,
		TriggerType:     triggerType,
		CauseEvent:      scanTask.Task.CauseEvent,
		Status:          "pending",
		ExpectedModules: scanTask.Task.ExpectedModules,
		Context:         scanTask.Task.Context,
		ExecutionID:     executionID,
		RequestedBy:     userID,
		StartedAt:       now.Format(time.RFC3339),
		TimeoutAt:       now.Add(5 * time.Minute).Format(time.RFC3339), // 执行任务超时5分钟
		BasedOnScan:     basedOnScan,
	}

	// 保存任务
	taskJSON, err := json.Marshal(task)
	if err != nil {
		return "", fmt.Errorf("序列化任务失败: %w", err)
	}

	taskKey := fmt.Sprintf("cleanup:tasks:%s", taskID)
	err = s.redis.HSet(ctx, taskKey, "data", string(taskJSON)).Err()
	if err != nil {
		return "", fmt.Errorf("保存任务失败: %w", err)
	}

	s.redis.Expire(ctx, taskKey, 1*time.Hour)

	// 发布事件
	event := events.CleanupRequestEvent{
		TaskID:            taskID,
		Action:            events.CleanupActionExecute,
		TenantID:          task.TenantID,
		CleanupMode:       cleanupMode,
		TriggerType:       triggerType,
		CauseEvent:        scanTask.Task.CauseEvent,
		ExpectedModules:   task.ExpectedModules,
		BasedOnScan:       basedOnScan,
		ParentExecutionID: executionID,
		Context:           scanTask.Task.Context,
		RequestedBy:       userID,
		RequestedAt:       now,
	}

	if err := s.publishEvent(ctx, event); err != nil {
		return "", fmt.Errorf("发布事件失败: %w", err)
	}
	s.startTaskStatusWatcher(taskID, now, 5*time.Minute)

	// 记录历史
	historyKey := fmt.Sprintf("cleanup:history:%d", task.TenantID)
	s.redis.LPush(ctx, historyKey, taskID)
	s.redis.LTrim(ctx, historyKey, 0, 99)
	s.writeAuditLog(ctx, userID, &task.TenantID, "cleanup.execute.confirmed", taskID, map[string]interface{}{
		"based_on_scan":      basedOnScan,
		"cleanup_mode":       cleanupMode,
		"risk_level":         scanSummary.RiskLevel,
		"scanned_items":      scanSummary.ScannedItems,
		"affected_records":   scanSummary.AffectedRecords,
		"freed_bytes":        scanSummary.FreedBytes,
		"expected_modules":   task.ExpectedModules,
		"confirmation_token": confirmation.ConfirmationToken != "",
		"confirmed_at":       now.Format(time.RFC3339),
	})
	s.writeAuditLog(ctx, userID, &task.TenantID, "cleanup.execute.created", taskID, map[string]interface{}{
		"based_on_scan":    basedOnScan,
		"cleanup_mode":     cleanupMode,
		"expected_modules": task.ExpectedModules,
		"cause_event":      task.CauseEvent,
		"context":          task.Context,
		"execution_id":     executionID,
	})

	s.log.Info("执行任务已创建",
		"task_id", taskID,
		"tenant_id", task.TenantID,
		"cleanup_mode", cleanupMode,
		"based_on_scan", basedOnScan,
		"user_id", userID)

	return taskID, nil
}

// GetTaskStatus 查询任务状态
func (s *CleanupOrchestratorService) GetTaskStatus(ctx context.Context, taskID string) (*models.TaskStatusResponse, error) {
	// 读取任务信息
	taskKey := fmt.Sprintf("cleanup:tasks:%s", taskID)
	taskDataStr, err := s.redis.HGet(ctx, taskKey, "data").Result()
	if err == redis.Nil {
		return nil, fmt.Errorf("task not found")
	}
	if err != nil {
		return nil, err
	}

	var task models.CleanupTask
	if err := json.Unmarshal([]byte(taskDataStr), &task); err != nil {
		return nil, err
	}

	// 读取各模块结果
	resultsKey := fmt.Sprintf("cleanup:results:%s", taskID)
	resultsMap, err := s.redis.HGetAll(ctx, resultsKey).Result()
	if err != nil {
		return nil, err
	}

	// 解析结果
	results := make(map[string]interface{})
	progress := models.TaskProgress{
		Total:   len(task.ExpectedModules),
		Modules: make(map[string]string),
	}

	// 检查超时时间
	timeoutAt, _ := time.Parse(time.RFC3339, task.TimeoutAt)
	isTimeout := time.Now().After(timeoutAt)

	for _, module := range task.ExpectedModules {
		if resultStr, ok := resultsMap[module]; ok {
			var result events.CleanupResultData
			if err := json.Unmarshal([]byte(resultStr), &result); err == nil {
				results[module] = result
				progress.Modules[module] = result.Status
				if isCleanupResultTerminal(result.Status) {
					progress.Completed++
				}
			}
		} else {
			// 检查超时
			if isTimeout {
				progress.Modules[module] = "timeout"
				progress.Completed++
			} else {
				progress.Modules[module] = "pending"
			}
		}
	}

	// 计算整体状态
	overallStatus := s.calculateOverallStatus(&task, &progress, isTimeout)

	// 汇总统计
	var summary interface{}
	if task.Action == events.CleanupActionScan {
		summary = s.aggregateScanSummary(results)
	} else {
		summary = s.aggregateExecuteSummary(results)
	}
	if err := s.updateTaskAndExecutionStatus(ctx, &task, overallStatus, summaryFromResults(results)); err != nil {
		s.log.Error("更新资源回收 execution 状态失败", "error", err, "task_id", taskID)
	}

	return &models.TaskStatusResponse{
		TaskID:   taskID,
		Action:   task.Action,
		Status:   overallStatus,
		Progress: progress,
		Results:  results,
		Summary:  summary,
		Task:     task,
	}, nil
}

// GetTaskHistory 获取历史任务列表
func (s *CleanupOrchestratorService) GetTaskHistory(ctx context.Context, tenantID uint, limit int) ([]models.CleanupTask, error) {
	if limit <= 0 {
		limit = 20
	}

	historyKey := fmt.Sprintf("cleanup:history:%d", tenantID)
	taskIDs, err := s.redis.LRange(ctx, historyKey, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}

	tasks := make([]models.CleanupTask, 0, len(taskIDs))
	for _, taskID := range taskIDs {
		taskKey := fmt.Sprintf("cleanup:tasks:%s", taskID)
		taskDataStr, err := s.redis.HGet(ctx, taskKey, "data").Result()
		if err != nil {
			continue // 任务可能已过期
		}

		var task models.CleanupTask
		if err := json.Unmarshal([]byte(taskDataStr), &task); err != nil {
			continue
		}

		tasks = append(tasks, task)
	}

	return tasks, nil
}

func (s *CleanupOrchestratorService) startTaskStatusWatcher(taskID string, startedAt time.Time, timeout time.Duration) {
	if s.redis == nil || taskID == "" {
		return
	}
	deadline := cleanupWatchDeadline(startedAt, timeout)
	go s.watchTaskStatus(taskID, deadline)
}

func (s *CleanupOrchestratorService) watchTaskStatus(taskID string, deadline time.Time) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	deadlineTimer := time.NewTimer(time.Until(deadline))
	defer deadlineTimer.Stop()

	for {
		status, err := s.GetTaskStatus(context.Background(), taskID)
		if err != nil {
			s.log.Warn("刷新资源回收任务状态失败", "task_id", taskID, "error", err)
			return
		}
		if status != nil && isTaskTerminal(status.Status) {
			return
		}
		select {
		case <-deadlineTimer.C:
			_, _ = s.GetTaskStatus(context.Background(), taskID)
			return
		case <-ticker.C:
		}
	}
}

func cleanupWatchDeadline(startedAt time.Time, timeout time.Duration) time.Time {
	if timeout <= 0 {
		return startedAt
	}
	return startedAt.Add(timeout + 2*time.Second)
}

func (s *CleanupOrchestratorService) resolveExpectedModules(scope []string) ([]string, error) {
	enabledModules, err := s.enabledCleanupExecutorModules()
	if err != nil {
		return nil, err
	}
	if len(enabledModules) == 0 {
		return nil, fmt.Errorf("未发现已注册且启用的资源回收执行方模块")
	}

	enabledSet := make(map[string]struct{}, len(enabledModules))
	for _, module := range enabledModules {
		enabledSet[module] = struct{}{}
	}

	if len(scope) == 0 {
		return enabledModules, nil
	}

	result := make([]string, 0, len(scope))
	seen := make(map[string]struct{}, len(scope))
	for _, module := range scope {
		normalized := strings.TrimSpace(module)
		if normalized == "" {
			continue
		}
		if _, duplicated := seen[normalized]; duplicated {
			continue
		}
		if _, ok := enabledSet[normalized]; !ok {
			return nil, fmt.Errorf("模块 %s 未注册或未启用资源回收执行方", normalized)
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("资源回收评估范围不能为空")
	}
	return result, nil
}

func (s *CleanupOrchestratorService) resolveExpectedModulesForCause(scope []string, causeEvent string) ([]string, error) {
	if causeEvent != events.CleanupCauseEngineDeleting {
		return s.resolveExpectedModules(scope)
	}
	if len(scope) > 0 {
		return nil, fmt.Errorf("Engine 删除影响评估不允许缩小模块范围")
	}
	if s.registry == nil {
		return nil, fmt.Errorf("module registry service is not configured")
	}
	modules, err := s.registry.ListModules()
	if err != nil {
		return nil, fmt.Errorf("查询 Engine 删除影响评估模块失败: %w", err)
	}

	expected := make([]string, 0, len(modules))
	unavailable := make([]string, 0)
	for _, module := range modules {
		if module == nil || !cleanupExecutorSupportsCause(module.Metadata, causeEvent) {
			continue
		}
		expected = append(expected, module.ModuleName)
		if module.Status != "up" {
			unavailable = append(unavailable, module.ModuleName)
		}
	}
	sort.Strings(expected)
	sort.Strings(unavailable)
	if len(expected) == 0 {
		return nil, fmt.Errorf("未发现支持 Engine 删除影响评估的模块")
	}
	if len(unavailable) > 0 {
		return nil, fmt.Errorf("以下模块当前不可用，无法完整评估 Engine 删除影响: %s", strings.Join(unavailable, ", "))
	}
	return expected, nil
}

func (s *CleanupOrchestratorService) enabledCleanupExecutorModules() ([]string, error) {
	if s.registry == nil {
		return nil, fmt.Errorf("module registry service is not configured")
	}
	modules, err := s.registry.ListActiveModules()
	if err != nil {
		return nil, fmt.Errorf("查询已注册资源回收执行方模块失败: %w", err)
	}

	result := make([]string, 0, len(modules))
	for _, module := range modules {
		if module == nil {
			continue
		}
		if cleanupExecutorEnabled(module.Metadata) {
			result = append(result, module.ModuleName)
		}
	}
	sort.Strings(result)
	return result, nil
}

func cleanupExecutorEnabled(metadata map[string]interface{}) bool {
	capabilities, ok := metadata["capabilities"].(map[string]interface{})
	if !ok {
		return false
	}
	cleanupExecutor, ok := capabilities["cleanup_executor"].(map[string]interface{})
	if !ok {
		return false
	}
	enabled, ok := cleanupExecutor["enabled"].(bool)
	return ok && enabled
}

func cleanupExecutorSupportsCause(metadata map[string]interface{}, causeEvent string) bool {
	if !cleanupExecutorEnabled(metadata) {
		return false
	}
	capabilities, _ := metadata["capabilities"].(map[string]interface{})
	cleanupExecutor, _ := capabilities["cleanup_executor"].(map[string]interface{})
	switch causes := cleanupExecutor["causes"].(type) {
	case []interface{}:
		for _, value := range causes {
			if cause, ok := value.(string); ok && strings.TrimSpace(cause) == causeEvent {
				return true
			}
		}
	case []string:
		for _, cause := range causes {
			if strings.TrimSpace(cause) == causeEvent {
				return true
			}
		}
	}
	return false
}

func validateExecutableScanTask(scanTask *models.TaskStatusResponse) error {
	if scanTask == nil {
		return fmt.Errorf("扫描任务不存在")
	}
	if scanTask.Task.Action != events.CleanupActionScan {
		return fmt.Errorf("based_on_scan 必须指向 scan 任务")
	}
	if scanTask.Status != "completed" {
		return fmt.Errorf("扫描任务未完成，当前状态: %s", scanTask.Status)
	}
	return nil
}

func validateCleanupExecuteConfirmation(cleanupMode string, summary events.CleanupResultSummary, confirmation CleanupExecuteConfirmation) error {
	if !confirmation.Confirmed {
		return ErrCleanupExecuteConfirmRequired
	}
	if cleanupMode != events.CleanupModePhysical && summary.RiskLevel != "high" {
		return nil
	}
	if confirmation.ConfirmationToken != "CONFIRM" {
		return ErrCleanupExecuteConfirmTokenRequired
	}
	return nil
}

func scanSummaryForConfirmation(summary interface{}) events.CleanupResultSummary {
	switch value := summary.(type) {
	case models.TaskSummary:
		return value.CleanupResultSummary
	case *models.TaskSummary:
		if value != nil {
			return value.CleanupResultSummary
		}
	case models.ExecuteSummary:
		return value.CleanupResultSummary
	case *models.ExecuteSummary:
		if value != nil {
			return value.CleanupResultSummary
		}
	case events.CleanupResultSummary:
		return value
	case *events.CleanupResultSummary:
		if value != nil {
			return *value
		}
	case map[string]interface{}:
		return cleanupSummaryFromMap(value)
	}
	return events.CleanupResultSummary{}
}

func cleanupSummaryFromMap(summary map[string]interface{}) events.CleanupResultSummary {
	return events.CleanupResultSummary{
		ScannedItems:             intFromSummaryValue(summary["scanned_items"]),
		AffectedRecords:          intFromSummaryValue(summary["affected_records"]),
		DeletedPhysicalArtifacts: intFromSummaryValue(summary["deleted_physical_artifacts"]),
		FreedBytes:               int64FromSummaryValue(summary["freed_bytes"]),
		MarkedMissingSource:      intFromSummaryValue(summary["marked_missing_source"]),
		MarkedOutdated:           intFromSummaryValue(summary["marked_outdated"]),
		DisabledTaskDefinitions:  intFromSummaryValue(summary["disabled_task_definitions"]),
		SkippedItems:             intFromSummaryValue(summary["skipped_items"]),
		ErrorCount:               intFromSummaryValue(summary["error_count"]),
		RiskLevel:                stringFromSummaryValue(summary["risk_level"]),
	}
}

func intFromSummaryValue(value interface{}) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	default:
		return 0
	}
}

func int64FromSummaryValue(value interface{}) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	default:
		return 0
	}
}

func stringFromSummaryValue(value interface{}) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func buildCleanupScanTask(
	taskID string,
	tenantID uint,
	expectedModules []string,
	requestedBy uint,
	triggerType string,
	causeEvent string,
	cleanupContext map[string]interface{},
	executionID string,
	startedAt string,
	timeoutAt string,
) models.CleanupTask {
	return models.CleanupTask{
		TaskID:          taskID,
		Action:          events.CleanupActionScan,
		TenantID:        tenantID,
		TriggerType:     triggerType,
		CauseEvent:      causeEvent,
		Status:          "pending",
		ExpectedModules: expectedModules,
		Context:         cleanupContext,
		ExecutionID:     executionID,
		RequestedBy:     requestedBy,
		StartedAt:       startedAt,
		TimeoutAt:       timeoutAt,
	}
}

func cleanupEngineContext(engineID uint) map[string]interface{} {
	return map[string]interface{}{"engine_id": engineID}
}

func cleanupTenantContext(tenantID uint) map[string]interface{} {
	return map[string]interface{}{"tenant_id": tenantID}
}

// publishEvent 发布事件到 Redis Stream
func (s *CleanupOrchestratorService) publishEvent(ctx context.Context, event events.CleanupRequestEvent) error {
	// 将 expected_modules 序列化为 JSON
	modulesJSON, err := json.Marshal(event.ExpectedModules)
	if err != nil {
		return fmt.Errorf("序列化 expected_modules 失败: %w", err)
	}
	contextJSON, err := json.Marshal(event.Context)
	if err != nil {
		return fmt.Errorf("序列化 context 失败: %w", err)
	}

	// 将事件转换为 map，注意 Redis Stream 不支持嵌套结构，所以需要序列化复杂字段
	eventMap := map[string]interface{}{
		"task_id":             event.TaskID,
		"action":              event.Action,
		"tenant_id":           event.TenantID,
		"cleanup_mode":        event.CleanupMode,
		"trigger_type":        event.TriggerType,
		"cause_event":         event.CauseEvent,
		"expected_modules":    string(modulesJSON),
		"based_on_scan":       event.BasedOnScan,
		"parent_execution_id": event.ParentExecutionID,
		"context":             string(contextJSON),
		"requested_by":        event.RequestedBy,
		"requested_at":        event.RequestedAt.Format(time.RFC3339),
	}

	// 发布到 Redis Stream
	_, err = s.redis.XAdd(ctx, &redis.XAddArgs{
		Stream: events.EventCleanupRequest,
		Values: eventMap,
	}).Result()

	return err
}

// calculateOverallStatus 计算整体状态
func (s *CleanupOrchestratorService) calculateOverallStatus(
	task *models.CleanupTask,
	progress *models.TaskProgress,
	isTimeout bool,
) string {
	if progress.Completed == progress.Total {
		// 全部完成
		hasFailure := false
		for _, status := range progress.Modules {
			if status == events.CleanupResultFailed || status == events.CleanupResultPartialSuccess || status == events.CleanupResultTimeout {
				hasFailure = true
				break
			}
		}
		if hasFailure {
			return "completed_with_errors"
		}
		return "completed"
	}

	if isTimeout {
		return "timeout"
	}

	if progress.Completed > 0 {
		return "running"
	}

	return "pending"
}

// aggregateScanSummary 汇总扫描统计
func (s *CleanupOrchestratorService) aggregateScanSummary(results map[string]interface{}) models.TaskSummary {
	summary := models.TaskSummary{
		CleanupResultSummary: summaryFromResults(results),
	}
	summary.Impact, summary.ImpactDigest = impactFromResults(results)
	if summary.RiskLevel == "" {
		summary.RiskLevel = "low"
	}
	return summary
}

// aggregateExecuteSummary 汇总执行统计
func (s *CleanupOrchestratorService) aggregateExecuteSummary(results map[string]interface{}) models.ExecuteSummary {
	summary := models.ExecuteSummary{
		CleanupResultSummary: summaryFromResults(results),
	}
	summary.Impact, summary.ImpactDigest = impactFromResults(results)

	for _, result := range results {
		if resultData, ok := result.(events.CleanupResultData); ok {
			if len(resultData.Errors) > 0 {
				summary.HasErrors = true
			}
		}
	}

	return summary
}

func impactFromResults(results map[string]interface{}) (events.CleanupImpactSummary, string) {
	var summary events.CleanupImpactSummary
	tokens := make([]string, 0, len(results))
	for module, resultValue := range results {
		result, ok := resultValue.(events.CleanupResultData)
		if !ok || result.Impact == nil {
			continue
		}
		summary.Add(result.Impact.Summary)
		tokens = append(tokens, module+":"+result.Impact.Digest)
	}
	sort.Strings(tokens)
	if len(tokens) == 0 {
		return summary, ""
	}
	digest, err := events.BuildCleanupImpactData([]events.CleanupImpactItem{{
		StableRef:   strings.Join(tokens, "|"),
		Disposition: events.CleanupImpactWillDelete,
	}}, "")
	if err != nil {
		return summary, ""
	}
	return summary, digest.Digest
}

func (s *CleanupOrchestratorService) createParentExecution(
	ctx context.Context,
	tenantID uint,
	taskID string,
	action string,
	cleanupMode string,
	triggerType string,
	causeEvent string,
	basedOnScan string,
	expectedModules []string,
	cleanupContext map[string]interface{},
	userID uint,
	timeout time.Duration,
) (string, error) {
	if s.execRepo == nil {
		return "", fmt.Errorf("resource reclaim execution repository is not configured")
	}
	now := time.Now()
	executionID := uuid.New().String()
	exec := &commonExecution.TaskExecution{
		TenantID:    int(tenantID),
		ExecutionID: executionID,
		Module:      commonExecution.ModuleSystem,
		TaskType:    commonExecution.TaskTypeCleanup,
		Source:      commonExecution.ModuleSystem,
		Status:      commonExecution.ExecutionStatusRunning,
		Progress:    0,
		TriggerType: triggerType,
		TriggeredBy: ptrInt(int(userID)),
		ExecutionConfig: commonModels.JSONMap{
			"task_id":          taskID,
			"action":           action,
			"cleanup_mode":     cleanupMode,
			"expected_modules": expectedModules,
			"based_on_scan":    basedOnScan,
			"cause_event":      causeEvent,
			"context":          cleanupContext,
			"timeout_seconds":  int(timeout.Seconds()),
		},
		StartedAt: &now,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.execRepo.Create(ctx, exec); err != nil {
		return "", fmt.Errorf("创建资源回收 execution 失败: %w", err)
	}
	return executionID, nil
}

func (s *CleanupOrchestratorService) updateTaskAndExecutionStatus(
	ctx context.Context,
	task *models.CleanupTask,
	overallStatus string,
	summary events.CleanupResultSummary,
) error {
	previousStatus := task.Status
	if previousStatus != overallStatus {
		task.Status = overallStatus
		if isTaskTerminal(overallStatus) {
			nowText := time.Now().Format(time.RFC3339)
			task.CompletedAt = &nowText
		}
		taskJSON, err := json.Marshal(task)
		if err != nil {
			return fmt.Errorf("序列化任务状态失败: %w", err)
		}
		taskKey := fmt.Sprintf("cleanup:tasks:%s", task.TaskID)
		if err := s.redis.HSet(ctx, taskKey, "data", string(taskJSON)).Err(); err != nil {
			return fmt.Errorf("保存任务状态失败: %w", err)
		}
	}

	if task.ExecutionID == "" || s.execRepo == nil {
		return nil
	}

	now := time.Now()
	executionStatus := executionStatusFromTaskStatus(overallStatus)
	fields := map[string]interface{}{
		"status":     executionStatus,
		"progress":   progressFromTaskStatus(overallStatus),
		"metadata":   commonModels.JSONMap{"cleanup_summary": summary, "cleanup_status": overallStatus},
		"updated_at": now,
	}
	if executionStatus == commonExecution.ExecutionStatusFailed || executionStatus == commonExecution.ExecutionStatusTimeout {
		fields["error_details"] = cleanupExecutionErrorDetails(overallStatus, summary)
	}
	if isTaskTerminal(overallStatus) {
		fields["completed_at"] = now
		if started, err := time.Parse(time.RFC3339, task.StartedAt); err == nil {
			duration := now.Sub(started).Milliseconds()
			fields["execution_time_ms"] = duration
		}
	}
	if err := s.execRepo.UpdateFields(ctx, task.ExecutionID, int(task.TenantID), fields); err != nil {
		return err
	}

	if previousStatus != overallStatus && isTaskTerminal(overallStatus) {
		eventName := "cleanup.completed"
		if overallStatus != "completed" {
			eventName = "cleanup.failed"
		}
		s.writeAuditLog(ctx, task.RequestedBy, &task.TenantID, eventName, task.TaskID, map[string]interface{}{
			"execution_id": task.ExecutionID,
			"status":       overallStatus,
			"summary":      summary,
		})
	}
	return nil
}

func cleanupExecutionErrorDetails(overallStatus string, summary events.CleanupResultSummary) commonModels.JSONMap {
	message := "cleanup completed with errors"
	if executionStatusFromTaskStatus(overallStatus) == commonExecution.ExecutionStatusTimeout {
		message = "cleanup timed out"
	}
	return commonModels.JSONMap{
		"message": message, "cleanup_status": overallStatus, "error_count": summary.ErrorCount,
	}
}

func summaryFromResults(results map[string]interface{}) events.CleanupResultSummary {
	summary := events.CleanupResultSummary{RiskLevel: "low"}
	for _, result := range results {
		resultData, ok := result.(events.CleanupResultData)
		if !ok {
			continue
		}
		summary.ScannedItems += resultData.Summary.ScannedItems
		summary.AffectedRecords += resultData.Summary.AffectedRecords
		summary.DeletedPhysicalArtifacts += resultData.Summary.DeletedPhysicalArtifacts
		summary.FreedBytes += resultData.Summary.FreedBytes
		summary.MarkedMissingSource += resultData.Summary.MarkedMissingSource
		summary.MarkedOutdated += resultData.Summary.MarkedOutdated
		summary.DisabledTaskDefinitions += resultData.Summary.DisabledTaskDefinitions
		summary.SkippedItems += resultData.Summary.SkippedItems
		errorCount := resultData.Summary.ErrorCount
		if len(resultData.Errors) > errorCount {
			errorCount = len(resultData.Errors)
		}
		summary.ErrorCount += errorCount
		summary.RiskLevel = higherRisk(summary.RiskLevel, resultData.Summary.RiskLevel)
	}
	return summary
}

func (s *CleanupOrchestratorService) writeAuditLog(
	ctx context.Context,
	userID uint,
	tenantID *uint,
	eventName string,
	taskID string,
	details map[string]interface{},
) {
	if s.auditWriter == nil {
		return
	}
	principalID := int64(userID)
	principalType := iam.PrincipalTypeUser
	contextType := iam.ContextTypeTenant
	metadata := iam.AuditMetadata{PrincipalID: &principalID, PrincipalType: &principalType, ContextType: &contextType}
	if tenantID != nil {
		value := int64(*tenantID)
		metadata.TenantID = &value
	}
	risk := iam.AuditRiskMedium
	result := iam.AuditResultSucceeded
	if eventName == "cleanup.execute.confirmed" {
		risk = iam.AuditRiskHigh
	} else if eventName == "cleanup.failed" {
		risk, result = iam.AuditRiskHigh, iam.AuditResultFailed
	}
	event := iam.AuditEvent{
		Metadata: metadata, EventName: eventName, Result: result, RiskLevel: risk,
		ModuleName: "system", EntityType: "cleanup_task", EntityID: taskID, Details: details,
	}
	if err := s.auditWriter.Write(ctx, event); err != nil {
		s.log.Error("写入资源回收审计日志失败", "error", err, "event", eventName, "task_id", taskID)
	}
}

func isCleanupResultTerminal(status string) bool {
	switch status {
	case events.CleanupResultSuccess, events.CleanupResultFailed, events.CleanupResultPartialSuccess, events.CleanupResultSkipped, events.CleanupResultTimeout:
		return true
	default:
		return false
	}
}

func isTaskTerminal(status string) bool {
	return status == "completed" || status == "completed_with_errors" || status == "timeout" || status == "failed"
}

func executionStatusFromTaskStatus(status string) string {
	switch status {
	case "completed":
		return commonExecution.ExecutionStatusSuccess
	case "completed_with_errors", "failed":
		return commonExecution.ExecutionStatusFailed
	case "timeout":
		return commonExecution.ExecutionStatusTimeout
	case "pending":
		return commonExecution.ExecutionStatusPending
	default:
		return commonExecution.ExecutionStatusRunning
	}
}

func progressFromTaskStatus(status string) int {
	if isTaskTerminal(status) {
		return 100
	}
	if status == "pending" {
		return 0
	}
	return 50
}

func higherRisk(left, right string) string {
	rank := map[string]int{"": 0, "low": 1, "medium": 2, "high": 3}
	if rank[right] > rank[left] {
		return right
	}
	return left
}

func ptrInt(value int) *int {
	return &value
}
