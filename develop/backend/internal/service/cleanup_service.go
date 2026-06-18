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
	"github.com/addp/develop/backend/internal/models"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type CleanupService struct {
	db           *gorm.DB
	redis        *redis.Client
	taskExecRepo *commonExecution.TaskExecutionRepository
	log          *slog.Logger
	stopCh       chan struct{}
}

type DevelopCleanupStats struct {
	DevTasks        int      `json:"dev_tasks"`
	ArchivedTasks   int      `json:"archived_tasks,omitempty"`
	DeletedTasks    int      `json:"deleted_tasks,omitempty"`
	SkippedTasks    int      `json:"skipped_tasks,omitempty"`
	UnscheduledRuns int      `json:"unscheduled_runs,omitempty"`
	Errors          []string `json:"errors,omitempty"`
}

func NewCleanupService(db *gorm.DB, redisClient *redis.Client, taskExecRepo *commonExecution.TaskExecutionRepository) *CleanupService {
	return &CleanupService{
		db:           db,
		redis:        redisClient,
		taskExecRepo: taskExecRepo,
		log:          logger.With("component", "develop_cleanup_service"),
		stopCh:       make(chan struct{}),
	}
}

func (s *CleanupService) Start(ctx context.Context) error {
	if s == nil || s.redis == nil {
		return nil
	}
	go s.consumeCleanupRequests(ctx)
	s.log.Info("Develop 资源回收事件订阅已启动")
	return nil
}

func (s *CleanupService) Stop() {
	if s == nil || s.stopCh == nil {
		return
	}
	close(s.stopCh)
}

func (s *CleanupService) consumeCleanupRequests(ctx context.Context) {
	groupName := "develop-cleanup-consumer"
	consumerName := "develop-worker"
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

func (s *CleanupService) handleCleanupRequest(ctx context.Context, message redis.XMessage) {
	event, err := events.ParseCleanupRequest(message.Values)
	if err != nil {
		s.log.Error("解析资源回收请求失败", "error", err, "message_id", message.ID)
		return
	}
	if !events.CleanupExpectedForModule(event.ExpectedModules, events.ModuleDevelop) {
		return
	}

	result := events.CleanupResultData{
		Module:      events.ModuleDevelop,
		Action:      event.Action,
		TenantID:    event.TenantID,
		TaskID:      event.TaskID,
		CleanupMode: event.CleanupMode,
		TriggerType: event.TriggerType,
		Timestamp:   time.Now(),
	}

	exec, startedAt, execErr := s.createExecutorExecution(ctx, event)
	if execErr != nil {
		s.log.Error("创建 Develop 资源回收执行记录失败", "error", execErr, "task_id", event.TaskID)
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
		result.Statistics = developCleanupStatsToMap(stats)
		result.Summary = developScanSummary(stats)
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
		result.Statistics = developCleanupStatsToMap(stats)
		result.Summary = developExecuteSummary(stats)
	default:
		result.Status = events.CleanupResultFailed
		result.Errors = []string{"unknown resource reclaim action: " + event.Action}
		result.Summary = events.CleanupResultSummary{ErrorCount: 1, RiskLevel: "low"}
	}
}

func (s *CleanupService) ScanReclaimCandidates(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (*DevelopCleanupStats, error) {
	candidates, err := s.listCandidates(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}
	return candidates.stats(), nil
}

func (s *CleanupService) ExecuteCleanup(ctx context.Context, tenantID uint, cleanupMode string, cleanupContext map[string]interface{}) (*DevelopCleanupStats, error) {
	if err := events.ValidateCleanupMode(cleanupMode); err != nil {
		return nil, err
	}
	candidates, err := s.listCandidates(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}

	stats := candidates.stats()
	switch cleanupMode {
	case events.CleanupModeLogical:
		s.archiveTasks(ctx, candidates, stats)
	case events.CleanupModePhysical:
		s.deleteTasks(ctx, candidates, stats)
	}
	return stats, nil
}

type developCleanupCandidates struct {
	devTasks []models.DevTask
}

func (c developCleanupCandidates) stats() *DevelopCleanupStats {
	return &DevelopCleanupStats{DevTasks: len(c.devTasks)}
}

func (s *CleanupService) listCandidates(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (developCleanupCandidates, error) {
	if tenantID == 0 {
		return developCleanupCandidates{}, fmt.Errorf("develop resource reclaim requires tenant_id")
	}
	if s == nil || s.db == nil {
		return developCleanupCandidates{}, fmt.Errorf("develop resource reclaim database is not configured")
	}
	contextTenantID, hasContextTenantID := developCleanupContextUint(cleanupContext, "tenant_id")
	if hasContextTenantID && contextTenantID == tenantID {
		return s.listTenantCandidates(ctx, tenantID)
	}
	return developCleanupCandidates{}, nil
}

func (s *CleanupService) listTenantCandidates(ctx context.Context, tenantID uint) (developCleanupCandidates, error) {
	var candidates developCleanupCandidates
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.devTasks).Error; err != nil {
		return candidates, err
	}
	return candidates, nil
}

func (s *CleanupService) archiveTasks(ctx context.Context, candidates developCleanupCandidates, stats *DevelopCleanupStats) {
	for _, item := range candidates.devTasks {
		updates := map[string]interface{}{
			"status":      "archived",
			"enabled":     false,
			"next_run_at": nil,
		}
		if err := s.db.WithContext(ctx).Model(&models.DevTask{}).Where("id = ?", item.ID).Updates(updates).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("archive dev task %d failed: %v", item.ID, err))
			continue
		}
		stats.ArchivedTasks++
		if item.NextRunAt != nil || item.Enabled {
			stats.UnscheduledRuns++
		}
	}
}

func (s *CleanupService) deleteTasks(ctx context.Context, candidates developCleanupCandidates, stats *DevelopCleanupStats) {
	for _, item := range candidates.devTasks {
		if err := s.db.WithContext(ctx).Unscoped().Delete(&models.DevTask{}, item.ID).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("delete dev task %d failed: %v", item.ID, err))
			continue
		}
		stats.DeletedTasks++
	}
}

func developCleanupContextUint(cleanupContext map[string]interface{}, key string) (uint, bool) {
	if cleanupContext == nil {
		return 0, false
	}
	raw, ok := cleanupContext[key]
	if !ok || raw == nil {
		return 0, false
	}
	switch value := raw.(type) {
	case uint:
		return value, value > 0
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
		return uint(parsed), err == nil && parsed > 0
	case string:
		parsed, err := strconv.ParseUint(value, 10, 32)
		return uint(parsed), err == nil && parsed > 0
	}
	return 0, false
}

func (s *CleanupService) createExecutorExecution(ctx context.Context, event events.CleanupRequestEvent) (*commonExecution.TaskExecution, time.Time, error) {
	if s.taskExecRepo == nil || event.ParentExecutionID == "" {
		return nil, time.Time{}, nil
	}
	startedAt := time.Now()
	currentStep := fmt.Sprintf("Develop 资源回收 %s", event.Action)
	triggerType, err := commonExecution.NormalizeTriggerType(event.TriggerType)
	if err != nil {
		triggerType = commonExecution.TriggerTypeManual
	}
	exec := &commonExecution.TaskExecution{
		TenantID:          int(event.TenantID),
		ExecutionID:       uuid.NewString(),
		Module:            commonExecution.ModuleDevelop,
		TaskType:          commonExecution.TaskTypeCleanupExecutor,
		Source:            commonExecution.ModuleSystem,
		ParentExecutionID: &event.ParentExecutionID,
		Status:            commonExecution.ExecutionStatusRunning,
		Progress:          0,
		CurrentStep:       &currentStep,
		TriggerType:       triggerType,
		TriggeredBy:       developIntPtr(int(event.RequestedBy)),
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

func (s *CleanupService) finishExecutorExecution(ctx context.Context, executionID string, tenantID uint, startedAt time.Time, result events.CleanupResultData) {
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
		s.log.Warn("更新 Develop 资源回收执行记录失败", "execution_id", executionID, "error", err)
	}
}

func (s *CleanupService) writeResult(ctx context.Context, taskID string, result events.CleanupResultData) {
	if s.redis == nil || taskID == "" {
		return
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		s.log.Error("序列化 Develop 资源回收结果失败", "error", err, "task_id", taskID)
		return
	}
	key := fmt.Sprintf("cleanup:results:%s", taskID)
	if err := s.redis.HSet(ctx, key, events.ModuleDevelop, string(resultJSON)).Err(); err != nil {
		s.log.Error("写入 Develop 资源回收结果失败", "error", err, "task_id", taskID)
	}
}

func developCleanupStatsToMap(stats *DevelopCleanupStats) map[string]interface{} {
	if stats == nil {
		return nil
	}
	data, _ := json.Marshal(stats)
	var result map[string]interface{}
	_ = json.Unmarshal(data, &result)
	return result
}

func developScanSummary(stats *DevelopCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	return events.CleanupResultSummary{
		ScannedItems: stats.DevTasks,
		ErrorCount:   len(stats.Errors),
		RiskLevel:    developRiskLevelForCount(stats.DevTasks),
	}
}

func developExecuteSummary(stats *DevelopCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	affected := stats.ArchivedTasks + stats.DeletedTasks
	return events.CleanupResultSummary{
		AffectedRecords:         affected,
		DisabledTaskDefinitions: stats.ArchivedTasks,
		SkippedItems:            stats.SkippedTasks,
		ErrorCount:              len(stats.Errors),
		RiskLevel:               "low",
	}
}

func developRiskLevelForCount(count int) string {
	if count > 1000 {
		return "high"
	}
	if count > 100 {
		return "medium"
	}
	return "low"
}

func developIntPtr(value int) *int {
	return &value
}
