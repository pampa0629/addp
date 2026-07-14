package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type TransferCleanupService struct {
	db             *gorm.DB
	redis          *redis.Client
	taskExecRepo   *commonExecution.TaskExecutionRepository
	captureControl CaptureControl
	log            *slog.Logger
	stopCh         chan struct{}
}

type TransferCleanupStats struct {
	TaskDefinitions         int      `json:"task_definitions"`
	DisabledTaskDefinitions int      `json:"disabled_task_definitions,omitempty"`
	DeletedTaskDefinitions  int      `json:"deleted_task_definitions,omitempty"`
	CaptureResources        int      `json:"capture_resources,omitempty"`
	CleanedCaptureResources int      `json:"cleaned_capture_resources,omitempty"`
	Errors                  []string `json:"errors,omitempty"`
}

func (s *TransferCleanupService) SetCaptureControl(control CaptureControl) {
	s.captureControl = control
}

func NewTransferCleanupService(db *gorm.DB, redisClient *redis.Client, taskExecRepo *commonExecution.TaskExecutionRepository) *TransferCleanupService {
	return &TransferCleanupService{
		db:           db,
		redis:        redisClient,
		taskExecRepo: taskExecRepo,
		log:          logger.With("component", "transfer_cleanup_service"),
		stopCh:       make(chan struct{}),
	}
}

func (s *TransferCleanupService) Start(ctx context.Context) error {
	if s == nil || s.redis == nil {
		return nil
	}
	go s.consumeCleanupRequests(ctx)
	s.log.Info("Transfer 资源回收事件订阅已启动")
	return nil
}

func (s *TransferCleanupService) Stop() {
	if s == nil || s.stopCh == nil {
		return
	}
	close(s.stopCh)
}

func (s *TransferCleanupService) consumeCleanupRequests(ctx context.Context) {
	groupName := "transfer-cleanup-consumer"
	consumerName := "transfer-worker"
	_ = s.redis.XGroupCreateMkStream(ctx, events.EventCleanupRequest, groupName, "$").Err()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		default:
			streams, err := s.redis.XReadGroup(ctx, &redis.XReadGroupArgs{
				Group:    groupName,
				Consumer: consumerName,
				Streams:  []string{events.EventCleanupRequest, ">"},
				Count:    1,
				Block:    5 * time.Second,
			}).Result()
			if err != nil {
				if err != redis.Nil {
					s.log.Error("读取资源回收请求失败", "error", err)
				}
				continue
			}
			for _, stream := range streams {
				for _, message := range stream.Messages {
					s.handleCleanupRequest(ctx, message)
					_ = s.redis.XAck(ctx, events.EventCleanupRequest, groupName, message.ID).Err()
				}
			}
		}
	}
}

func (s *TransferCleanupService) handleCleanupRequest(ctx context.Context, message redis.XMessage) {
	event, err := events.ParseCleanupRequest(message.Values)
	if err != nil {
		s.log.Error("解析资源回收请求失败", "error", err, "message_id", message.ID)
		return
	}
	if !events.CleanupExpectedForModule(event.ExpectedModules, events.ModuleTransfer) {
		return
	}

	result := events.CleanupResultData{
		Module:      events.ModuleTransfer,
		Action:      event.Action,
		TenantID:    event.TenantID,
		TaskID:      event.TaskID,
		CleanupMode: event.CleanupMode,
		TriggerType: event.TriggerType,
		Timestamp:   time.Now(),
	}

	exec, startedAt, execErr := s.createExecutorExecution(ctx, event)
	if execErr != nil {
		s.log.Error("创建 Transfer 资源回收执行记录失败", "error", execErr, "task_id", event.TaskID)
	}
	defer func() {
		if exec != nil {
			s.finishExecutorExecution(ctx, exec.ExecutionID, event.TenantID, startedAt, result)
		}
		s.writeResult(ctx, event.TaskID, result)
	}()

	switch event.Action {
	case events.CleanupActionScan:
		stats, err := s.ScanReclaimCandidates(ctx, event.TenantID, event.Context)
		if err != nil {
			result.Status = events.CleanupResultFailed
			result.Errors = []string{err.Error()}
			result.Summary = events.CleanupResultSummary{ErrorCount: 1, RiskLevel: "low"}
			return
		}
		result.Status = events.CleanupResultSuccess
		result.Statistics = transferCleanupStatsToMap(stats)
		result.Summary = transferScanSummary(stats)
	case events.CleanupActionExecute:
		stats, err := s.ExecuteCleanup(ctx, event.TenantID, event.CleanupMode, event.Context)
		if err != nil {
			result.Status = events.CleanupResultFailed
			result.Errors = []string{err.Error()}
			result.Summary = events.CleanupResultSummary{ErrorCount: 1, RiskLevel: "low"}
			return
		}
		if len(stats.Errors) > 0 {
			result.Status = events.CleanupResultPartialSuccess
			result.Errors = stats.Errors
		} else {
			result.Status = events.CleanupResultSuccess
		}
		result.Statistics = transferCleanupStatsToMap(stats)
		result.Summary = transferExecuteSummary(stats)
	default:
		result.Status = events.CleanupResultFailed
		result.Errors = []string{"unknown resource reclaim action: " + event.Action}
		result.Summary = events.CleanupResultSummary{ErrorCount: 1, RiskLevel: "low"}
	}
}

func (s *TransferCleanupService) ScanReclaimCandidates(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (*TransferCleanupStats, error) {
	if tenantID == 0 {
		return nil, fmt.Errorf("transfer resource reclaim requires tenant_id")
	}
	tasks, err := s.listCandidateTaskDefinitions(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}
	stats := &TransferCleanupStats{TaskDefinitions: len(tasks)}
	for _, task := range tasks {
		if planner.IsPostgreSQLCDCTaskConfig(task.Config) {
			stats.CaptureResources++
		}
	}
	return stats, nil
}

func (s *TransferCleanupService) ExecuteCleanup(ctx context.Context, tenantID uint, cleanupMode string, cleanupContext map[string]interface{}) (*TransferCleanupStats, error) {
	if err := events.ValidateCleanupMode(cleanupMode); err != nil {
		return nil, err
	}
	if tenantID == 0 {
		return nil, fmt.Errorf("transfer resource reclaim requires tenant_id")
	}
	tasks, err := s.listCandidateTaskDefinitions(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}

	stats := &TransferCleanupStats{TaskDefinitions: len(tasks)}
	now := time.Now()
	for _, task := range tasks {
		if planner.IsPostgreSQLCDCTaskConfig(task.Config) {
			stats.CaptureResources++
			if s.captureControl == nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("cleanup PostgreSQL CDC task %d failed: capture control is unavailable", task.ID))
				continue
			}
			if err := s.captureControl.Stop(ctx, &task); err != nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("cleanup PostgreSQL CDC task %d failed: %v", task.ID, err))
				continue
			}
			stats.CleanedCaptureResources++
		}
		switch cleanupMode {
		case events.CleanupModeLogical:
			updates := map[string]interface{}{
				"enabled":     false,
				"next_run_at": nil,
				"status":      models.TaskStatusIdle,
				"updated_at":  now,
			}
			if err := s.db.WithContext(ctx).Model(&models.TransferTask{}).Where("id = ?", task.ID).Updates(updates).Error; err != nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("disable transfer task %d failed: %v", task.ID, err))
				continue
			}
			stats.DisabledTaskDefinitions++
		case events.CleanupModePhysical:
			if err := s.db.WithContext(ctx).Unscoped().Delete(&models.TransferTask{}, task.ID).Error; err != nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("delete transfer task %d failed: %v", task.ID, err))
				continue
			}
			stats.DeletedTaskDefinitions++
		}
	}
	return stats, nil
}

func (s *TransferCleanupService) listCandidateTaskDefinitions(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) ([]models.TransferTask, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("transfer resource reclaim database is not configured")
	}
	var tasks []models.TransferTask
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&tasks).Error; err != nil {
		return nil, err
	}
	engineID, hasEngineID := cleanupContextUint(cleanupContext, "engine_id")
	contextTenantID, hasContextTenantID := cleanupContextUint(cleanupContext, "tenant_id")
	if !hasEngineID {
		if hasContextTenantID && contextTenantID == tenantID {
			return tasks, nil
		}
		// Transfer 不拥有 target 业务数据；没有明确生命周期上下文时，不把普通任务定义视作待回收对象。
		return nil, nil
	}
	if engineID == 0 {
		return nil, nil
	}

	candidates := make([]models.TransferTask, 0, len(tasks))
	for _, task := range tasks {
		if taskReferencesEngine(task, engineID) {
			candidates = append(candidates, task)
		}
	}
	return candidates, nil
}

func taskReferencesEngine(task models.TransferTask, engineID uint) bool {
	if engineID == 0 {
		return false
	}
	if spec, err := planner.ParsePostgreSQLCDCTaskSpec(task.Config); err == nil {
		return endpointReferencesEngine(spec.Source, engineID) || endpointReferencesEngine(spec.Target, engineID)
	}
	if spec, err := planner.ParseRawCopyTaskSpec(task.Config); err == nil {
		return endpointReferencesEngine(spec.Source, engineID) || endpointReferencesEngine(spec.Target, engineID)
	}
	if spec, err := planner.ParseTableExportTaskSpec(task.Config, task.BatchSize); err == nil {
		return endpointReferencesEngine(spec.Source, engineID) || endpointReferencesEngine(spec.Target, engineID)
	}
	return false
}

func endpointReferencesEngine(endpoint planner.EndpointSpec, engineID uint) bool {
	return endpoint.LocatorEngineID() == engineID
}

func cleanupContextUint(cleanupContext map[string]interface{}, key string) (uint, bool) {
	if cleanupContext == nil {
		return 0, false
	}
	raw, ok := cleanupContext[key]
	if !ok || raw == nil {
		return 0, false
	}
	switch value := raw.(type) {
	case uint:
		return value, true
	case int:
		if value > 0 {
			return uint(value), true
		}
	case int64:
		if value > 0 {
			return uint(value), true
		}
	case float64:
		if value > 0 {
			return uint(value), true
		}
	case json.Number:
		parsed, err := strconv.ParseUint(string(value), 10, 32)
		return uint(parsed), err == nil
	case string:
		parsed, err := strconv.ParseUint(value, 10, 32)
		return uint(parsed), err == nil
	}
	return 0, false
}

func (s *TransferCleanupService) createExecutorExecution(ctx context.Context, event events.CleanupRequestEvent) (*commonExecution.TaskExecution, time.Time, error) {
	if s.taskExecRepo == nil || event.ParentExecutionID == "" {
		return nil, time.Time{}, nil
	}
	startedAt := time.Now()
	currentStep := fmt.Sprintf("Transfer 资源回收 %s", event.Action)
	triggerType, err := commonExecution.NormalizeTriggerType(event.TriggerType)
	if err != nil {
		triggerType = commonExecution.TriggerTypeManual
	}
	exec := &commonExecution.TaskExecution{
		TenantID:          int(event.TenantID),
		ExecutionID:       uuid.NewString(),
		Module:            commonExecution.ModuleTransfer,
		TaskType:          commonExecution.TaskTypeCleanupExecutor,
		Source:            commonExecution.ModuleSystem,
		ParentExecutionID: &event.ParentExecutionID,
		Status:            commonExecution.ExecutionStatusRunning,
		Progress:          0,
		CurrentStep:       &currentStep,
		TriggerType:       triggerType,
		TriggeredBy:       transferIntPtr(int(event.RequestedBy)),
		ExecutionConfig: commonModels.JSONMap{
			"task_id":       event.TaskID,
			"action":        event.Action,
			"cleanup_mode":  event.CleanupMode,
			"based_on_scan": event.BasedOnScan,
			"cause_event":   event.CauseEvent,
			"context":       event.Context,
		},
		StartedAt: &startedAt,
		CreatedAt: startedAt,
		UpdatedAt: startedAt,
	}
	if err := s.taskExecRepo.Create(ctx, exec); err != nil {
		return nil, startedAt, err
	}
	return exec, startedAt, nil
}

func (s *TransferCleanupService) finishExecutorExecution(ctx context.Context, executionID string, tenantID uint, startedAt time.Time, result events.CleanupResultData) {
	if s.taskExecRepo == nil || executionID == "" {
		return
	}
	now := time.Now()
	status := commonExecution.ExecutionStatusSuccess
	if result.Status == events.CleanupResultFailed {
		status = commonExecution.ExecutionStatusFailed
	}
	var errDetails commonModels.JSONMap
	if len(result.Errors) > 0 {
		errDetails = commonModels.JSONMap{"errors": result.Errors}
	}
	if err := s.taskExecRepo.UpdateFields(ctx, executionID, int(tenantID), map[string]interface{}{
		"status":            status,
		"progress":          100,
		"metadata":          commonModels.JSONMap{"cleanup_result": result, "summary": result.Summary},
		"error_details":     errDetails,
		"completed_at":      now,
		"execution_time_ms": now.Sub(startedAt).Milliseconds(),
		"updated_at":        now,
	}); err != nil {
		s.log.Warn("更新 Transfer 资源回收执行记录失败", "execution_id", executionID, "error", err)
	}
}

func (s *TransferCleanupService) writeResult(ctx context.Context, taskID string, result events.CleanupResultData) {
	if s.redis == nil || taskID == "" {
		return
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		s.log.Error("序列化 Transfer 资源回收结果失败", "error", err, "task_id", taskID)
		return
	}
	key := fmt.Sprintf("cleanup:results:%s", taskID)
	if err := s.redis.HSet(ctx, key, events.ModuleTransfer, string(resultJSON)).Err(); err != nil {
		s.log.Error("写入 Transfer 资源回收结果失败", "error", err, "task_id", taskID)
	}
}

func transferCleanupStatsToMap(stats *TransferCleanupStats) map[string]interface{} {
	if stats == nil {
		return nil
	}
	data, _ := json.Marshal(stats)
	var result map[string]interface{}
	_ = json.Unmarshal(data, &result)
	return result
}

func transferScanSummary(stats *TransferCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	return events.CleanupResultSummary{
		ScannedItems:            stats.TaskDefinitions,
		DisabledTaskDefinitions: stats.TaskDefinitions,
		ErrorCount:              len(stats.Errors),
		RiskLevel:               transferRiskLevelForCount(stats.TaskDefinitions),
	}
}

func transferExecuteSummary(stats *TransferCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	affected := stats.DisabledTaskDefinitions + stats.DeletedTaskDefinitions
	return events.CleanupResultSummary{
		AffectedRecords:         affected,
		DisabledTaskDefinitions: stats.DisabledTaskDefinitions,
		ErrorCount:              len(stats.Errors),
		RiskLevel:               "low",
	}
}

func transferRiskLevelForCount(count int) string {
	if count > 1000 {
		return "high"
	}
	if count > 100 {
		return "medium"
	}
	return "low"
}

func transferIntPtr(value int) *int {
	return &value
}
