package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"time"

	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/quality/internal/models"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CleanupService struct {
	db           *gorm.DB
	redis        *redis.Client
	taskExecRepo *commonExecution.TaskExecutionRepository
	log          *slog.Logger
	stopCh       chan struct{}
}

type QualityCleanupStats struct {
	RuleApplications        int      `json:"rule_applications"`
	CheckTasks              int      `json:"check_tasks"`
	Issues                  int      `json:"issues"`
	DisabledRuleApps        int      `json:"disabled_rule_applications,omitempty"`
	IgnoredIssues           int      `json:"ignored_issues,omitempty"`
	SkippedIssues           int      `json:"skipped_issues,omitempty"`
	DeletedRuleApplications int      `json:"deleted_rule_applications,omitempty"`
	DeletedCheckTasks       int      `json:"deleted_check_tasks,omitempty"`
	DeletedIssues           int      `json:"deleted_issues,omitempty"`
	Errors                  []string `json:"errors,omitempty"`
}

func NewCleanupService(db *gorm.DB, redisClient *redis.Client, taskExecRepo *commonExecution.TaskExecutionRepository) *CleanupService {
	return &CleanupService{
		db:           db,
		redis:        redisClient,
		taskExecRepo: taskExecRepo,
		log:          logger.With("component", "quality_cleanup_service"),
		stopCh:       make(chan struct{}),
	}
}

func (s *CleanupService) Start(ctx context.Context) error {
	if s == nil || s.redis == nil {
		return nil
	}
	go s.consumeCleanupRequests(ctx)
	s.log.Info("Quality 资源回收事件订阅已启动")
	return nil
}

func (s *CleanupService) Stop() {
	if s == nil || s.stopCh == nil {
		return
	}
	close(s.stopCh)
}

func (s *CleanupService) consumeCleanupRequests(ctx context.Context) {
	groupName := "quality-cleanup-consumer"
	consumerName := "quality-worker"
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
	if !events.CleanupExpectedForModule(event.ExpectedModules, events.ModuleQuality) {
		return
	}

	result := events.CleanupResultData{
		Module:      events.ModuleQuality,
		Action:      event.Action,
		TenantID:    event.TenantID,
		TaskID:      event.TaskID,
		CleanupMode: event.CleanupMode,
		TriggerType: event.TriggerType,
		Timestamp:   time.Now(),
	}

	exec, startedAt, execErr := s.createExecutorExecution(ctx, event)
	if execErr != nil {
		s.log.Error("创建 Quality 资源回收执行记录失败", "error", execErr, "task_id", event.TaskID)
	}
	defer func() {
		if exec != nil {
			s.finishExecutorExecution(ctx, exec.ExecutionID, event.TenantID, startedAt, result)
		}
		s.writeResult(ctx, event.TaskID, result)
	}()

	switch event.Action {
	case events.CleanupActionScan:
		candidates, err := s.listCandidates(ctx, event.TenantID, event.Context)
		if err != nil {
			result.Status = events.CleanupResultFailed
			result.Errors = []string{err.Error()}
			result.Summary = events.CleanupResultSummary{ErrorCount: 1, RiskLevel: "low"}
			return
		}
		stats := candidates.stats()
		if event.CauseEvent == events.CleanupCauseEngineDeleting {
			activeTaskIDs, err := s.listActiveQualityCleanupTaskIDs(ctx, candidates.checkTasks)
			if err != nil {
				result.Status = events.CleanupResultFailed
				result.Errors = []string{err.Error()}
				result.Summary = events.CleanupResultSummary{ErrorCount: 1, RiskLevel: "low"}
				return
			}
			impact, err := qualityEngineDeletionImpact(candidates, activeTaskIDs)
			if err != nil {
				result.Status = events.CleanupResultFailed
				result.Errors = []string{err.Error()}
				result.Summary = events.CleanupResultSummary{ErrorCount: 1, RiskLevel: "low"}
				return
			}
			result.Impact = &impact
		}
		result.Status = events.CleanupResultSuccess
		result.Statistics = qualityCleanupStatsToMap(stats)
		result.Summary = qualityScanSummary(stats)
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
		result.Statistics = qualityCleanupStatsToMap(stats)
		result.Summary = qualityExecuteSummary(stats)
	default:
		result.Status = events.CleanupResultFailed
		result.Errors = []string{"unknown resource reclaim action: " + event.Action}
		result.Summary = events.CleanupResultSummary{ErrorCount: 1, RiskLevel: "low"}
	}
}

func (s *CleanupService) ScanReclaimCandidates(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (*QualityCleanupStats, error) {
	candidates, err := s.listCandidates(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}
	return candidates.stats(), nil
}

func (s *CleanupService) ExecuteCleanup(ctx context.Context, tenantID uint, cleanupMode string, cleanupContext map[string]interface{}) (*QualityCleanupStats, error) {
	if err := events.ValidateCleanupMode(cleanupMode); err != nil {
		return nil, err
	}
	candidates, err := s.listCandidates(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}

	stats := candidates.stats()
	if _, hasEngineID := qualityCleanupContextInt64(cleanupContext, "engine_id"); hasEngineID {
		applied, err := s.disableCandidates(ctx, candidates)
		if err != nil {
			stats.Errors = append(stats.Errors, err.Error())
			return stats, nil
		}
		mergeQualityCleanupStats(stats, &applied)
		return stats, nil
	}
	switch cleanupMode {
	case events.CleanupModeLogical:
		applied, err := s.disableCandidates(ctx, candidates)
		if err != nil {
			stats.Errors = append(stats.Errors, err.Error())
			return stats, nil
		}
		mergeQualityCleanupStats(stats, &applied)
	case events.CleanupModePhysical:
		applied, err := s.deleteCandidates(ctx, candidates)
		if err != nil {
			stats.Errors = append(stats.Errors, err.Error())
			return stats, nil
		}
		mergeQualityCleanupStats(stats, &applied)
	}
	return stats, nil
}

func qualityEngineDeletionImpact(candidates qualityCleanupCandidates, activeTaskIDs map[int64]struct{}) (events.CleanupImpactData, error) {
	items := make([]events.CleanupImpactItem, 0, len(candidates.ruleApplications)+len(candidates.checkTasks)*2+len(candidates.issues))
	for _, item := range candidates.ruleApplications {
		items = append(items, events.CleanupImpactItem{StableRef: fmt.Sprintf("quality_rule_application:%d", item.ID), Disposition: events.CleanupImpactWillDisable})
	}
	for _, item := range candidates.checkTasks {
		stableRef := fmt.Sprintf("quality_check_task:%d", item.ID)
		items = append(items, events.CleanupImpactItem{StableRef: stableRef, Disposition: events.CleanupImpactRebindable})
		if _, active := activeTaskIDs[item.ID]; active {
			items = append(items, events.CleanupImpactItem{StableRef: stableRef, Disposition: events.CleanupImpactRunning})
		}
	}
	for _, item := range candidates.issues {
		items = append(items, events.CleanupImpactItem{StableRef: fmt.Sprintf("quality_issue:%d", item.ID), Disposition: events.CleanupImpactWillDisable})
	}
	return events.BuildCleanupImpactData(items, "/quality/check-tasks")
}

func (s *CleanupService) listActiveQualityCleanupTaskIDs(ctx context.Context, tasks []models.CheckTask) (map[int64]struct{}, error) {
	activeTaskIDs := make(map[int64]struct{})
	if len(tasks) == 0 {
		return activeTaskIDs, nil
	}
	sourceTaskIDs := make([]string, 0, len(tasks))
	taskIDBySource := make(map[string]int64, len(tasks))
	for _, task := range tasks {
		sourceTaskID := strconv.FormatInt(task.ID, 10)
		sourceTaskIDs = append(sourceTaskIDs, sourceTaskID)
		taskIDBySource[sourceTaskID] = task.ID
	}
	var executions []commonExecution.TaskExecution
	if err := s.db.WithContext(ctx).Select("source_task_id").
		Where("tenant_id = ? AND module = ? AND task_type = ? AND source_task_id IN ? AND status IN ?",
			tasks[0].TenantID, commonExecution.ModuleQuality, commonExecution.TaskTypeQualityCheck, sourceTaskIDs,
			[]string{commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning}).
		Find(&executions).Error; err != nil {
		return nil, fmt.Errorf("inspect active quality executions failed: %w", err)
	}
	for _, execution := range executions {
		if execution.SourceTaskID == nil {
			continue
		}
		if taskID, ok := taskIDBySource[*execution.SourceTaskID]; ok {
			activeTaskIDs[taskID] = struct{}{}
		}
	}
	return activeTaskIDs, nil
}

type qualityCleanupCandidates struct {
	ruleApplications []models.RuleApplication
	checkTasks       []models.CheckTask
	issues           []models.Issue
}

func (c qualityCleanupCandidates) stats() *QualityCleanupStats {
	return &QualityCleanupStats{
		RuleApplications: len(c.ruleApplications),
		CheckTasks:       len(c.checkTasks),
		Issues:           len(c.issues),
	}
}

func (s *CleanupService) listCandidates(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (qualityCleanupCandidates, error) {
	if tenantID == 0 {
		return qualityCleanupCandidates{}, fmt.Errorf("quality resource reclaim requires tenant_id")
	}
	if s == nil || s.db == nil {
		return qualityCleanupCandidates{}, fmt.Errorf("quality resource reclaim database is not configured")
	}
	engineID, hasEngineID := qualityCleanupContextInt64(cleanupContext, "engine_id")
	contextTenantID, hasContextTenantID := qualityCleanupContextInt64(cleanupContext, "tenant_id")
	if !hasEngineID {
		if hasContextTenantID && contextTenantID == int64(tenantID) {
			return s.listTenantCandidates(ctx, int64(tenantID))
		}
		return qualityCleanupCandidates{}, nil
	}
	if engineID <= 0 {
		return qualityCleanupCandidates{}, nil
	}
	return s.listEngineCandidates(ctx, int64(tenantID), engineID)
}

func (s *CleanupService) listTenantCandidates(ctx context.Context, tenantID int64) (qualityCleanupCandidates, error) {
	var candidates qualityCleanupCandidates
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.ruleApplications).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.checkTasks).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.issues).Error; err != nil {
		return candidates, err
	}
	return candidates, nil
}

func (s *CleanupService) listEngineCandidates(ctx context.Context, tenantID int64, engineID int64) (qualityCleanupCandidates, error) {
	var candidates qualityCleanupCandidates
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND engine_id = ?", tenantID, engineID).Find(&candidates.ruleApplications).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND engine_id = ?", tenantID, engineID).Find(&candidates.checkTasks).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND engine_id = ?", tenantID, engineID).Find(&candidates.issues).Error; err != nil {
		return candidates, err
	}
	return candidates, nil
}

func (s *CleanupService) disableCandidates(ctx context.Context, candidates qualityCleanupCandidates) (QualityCleanupStats, error) {
	var applied QualityCleanupStats
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		candidates.sortByID()
		if err := lockQualityCleanupTasks(tx, candidates.checkTasks); err != nil {
			return err
		}
		for _, item := range candidates.ruleApplications {
			result := tx.Model(&models.RuleApplication{}).
				Where("id = ? AND tenant_id = ?", item.ID, item.TenantID).
				Update("enabled", false)
			if result.Error != nil {
				return fmt.Errorf("disable rule application %d failed: %w", item.ID, result.Error)
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("disable rule application %d failed: resource changed during cleanup", item.ID)
			}
			applied.DisabledRuleApps++
		}
		resolvedAt := time.Now().UTC()
		for _, item := range candidates.issues {
			if item.Status != "open" {
				applied.SkippedIssues++
				continue
			}
			result := tx.Model(&models.Issue{}).
				Where("id = ? AND tenant_id = ? AND status = ?", item.ID, item.TenantID, "open").
				Updates(map[string]interface{}{
					"status": "ignored", "resolved_at": resolvedAt, "resolved_by": nil,
					"resolution_note": "", "updated_at": resolvedAt,
				})
			if result.Error != nil {
				return fmt.Errorf("ignore issue %d failed: %w", item.ID, result.Error)
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("ignore issue %d failed: resource changed during cleanup", item.ID)
			}
			applied.IgnoredIssues++
		}
		return nil
	})
	if err != nil {
		return QualityCleanupStats{}, err
	}
	return applied, nil
}

func lockQualityCleanupTasks(tx *gorm.DB, tasks []models.CheckTask) error {
	if len(tasks) == 0 {
		return nil
	}
	tenantID := tasks[0].TenantID
	sourceTaskIDs := make([]string, 0, len(tasks))
	for _, item := range tasks {
		if item.TenantID != tenantID {
			return fmt.Errorf("quality cleanup task candidates span multiple tenants")
		}
		var current models.CheckTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", item.ID, item.TenantID).First(&current).Error; err != nil {
			return fmt.Errorf("lock check task %d failed: %w", item.ID, err)
		}
		sourceTaskIDs = append(sourceTaskIDs, strconv.FormatInt(current.ID, 10))
	}
	var active commonExecution.TaskExecution
	result := tx.Select("source_task_id").
		Where("tenant_id = ? AND module = ? AND task_type = ? AND source_task_id IN ? AND status IN ?",
			tenantID, commonExecution.ModuleQuality, commonExecution.TaskTypeQualityCheck, sourceTaskIDs,
			[]string{commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning}).
		Order("source_task_id ASC").Limit(1).Find(&active)
	if result.Error != nil {
		return fmt.Errorf("inspect active quality executions failed: %w", result.Error)
	}
	if result.RowsAffected > 0 && active.SourceTaskID != nil {
		return fmt.Errorf("quality check task %s has an active execution", *active.SourceTaskID)
	}
	return nil
}

func mergeQualityCleanupStats(target, applied *QualityCleanupStats) {
	if target == nil || applied == nil {
		return
	}
	target.DisabledRuleApps += applied.DisabledRuleApps
	target.IgnoredIssues += applied.IgnoredIssues
	target.SkippedIssues += applied.SkippedIssues
	target.DeletedRuleApplications += applied.DeletedRuleApplications
	target.DeletedCheckTasks += applied.DeletedCheckTasks
	target.DeletedIssues += applied.DeletedIssues
}

func (s *CleanupService) deleteCandidates(ctx context.Context, candidates qualityCleanupCandidates) (QualityCleanupStats, error) {
	var applied QualityCleanupStats
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		candidates.sortByID()
		if err := lockQualityCleanupTasks(tx, candidates.checkTasks); err != nil {
			return err
		}
		for _, item := range candidates.issues {
			result := tx.Unscoped().Where("id = ? AND tenant_id = ?", item.ID, item.TenantID).Delete(&models.Issue{})
			if result.Error != nil {
				return fmt.Errorf("delete issue %d failed: %w", item.ID, result.Error)
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("delete issue %d failed: resource changed during cleanup", item.ID)
			}
			applied.DeletedIssues++
		}
		for _, item := range candidates.checkTasks {
			result := tx.Unscoped().Where("id = ? AND tenant_id = ?", item.ID, item.TenantID).Delete(&models.CheckTask{})
			if result.Error != nil {
				return fmt.Errorf("delete check task %d failed: %w", item.ID, result.Error)
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("delete check task %d failed: resource changed during cleanup", item.ID)
			}
			applied.DeletedCheckTasks++
		}
		for _, item := range candidates.ruleApplications {
			result := tx.Unscoped().Where("id = ? AND tenant_id = ?", item.ID, item.TenantID).Delete(&models.RuleApplication{})
			if result.Error != nil {
				return fmt.Errorf("delete rule application %d failed: %w", item.ID, result.Error)
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("delete rule application %d failed: resource changed during cleanup", item.ID)
			}
			applied.DeletedRuleApplications++
		}
		return nil
	})
	if err != nil {
		return QualityCleanupStats{}, err
	}
	return applied, nil
}

func (c *qualityCleanupCandidates) sortByID() {
	if c == nil {
		return
	}
	sort.Slice(c.ruleApplications, func(i, j int) bool { return c.ruleApplications[i].ID < c.ruleApplications[j].ID })
	sort.Slice(c.checkTasks, func(i, j int) bool { return c.checkTasks[i].ID < c.checkTasks[j].ID })
	sort.Slice(c.issues, func(i, j int) bool { return c.issues[i].ID < c.issues[j].ID })
}

func qualityCleanupContextInt64(cleanupContext map[string]interface{}, key string) (int64, bool) {
	if cleanupContext == nil {
		return 0, false
	}
	raw, ok := cleanupContext[key]
	if !ok || raw == nil {
		return 0, false
	}
	switch value := raw.(type) {
	case int64:
		if value > 0 {
			return value, true
		}
	case int:
		if value > 0 {
			return int64(value), true
		}
	case uint:
		if value > 0 {
			return int64(value), true
		}
	case float64:
		if value > 0 {
			return int64(value), true
		}
	case json.Number:
		parsed, err := strconv.ParseInt(string(value), 10, 64)
		return parsed, err == nil && parsed > 0
	case string:
		parsed, err := strconv.ParseInt(value, 10, 64)
		return parsed, err == nil && parsed > 0
	}
	return 0, false
}

func (s *CleanupService) createExecutorExecution(ctx context.Context, event events.CleanupRequestEvent) (*commonExecution.TaskExecution, time.Time, error) {
	if s.taskExecRepo == nil || event.ParentExecutionID == "" {
		return nil, time.Time{}, nil
	}
	startedAt := time.Now()
	currentStep := fmt.Sprintf("Quality 资源回收 %s", event.Action)
	triggerType, err := commonExecution.NormalizeTriggerType(event.TriggerType)
	if err != nil {
		triggerType = commonExecution.TriggerTypeManual
	}
	exec := &commonExecution.TaskExecution{
		TenantID:          int(event.TenantID),
		ExecutionID:       uuid.NewString(),
		Module:            commonExecution.ModuleQuality,
		TaskType:          commonExecution.TaskTypeCleanupExecutor,
		Source:            commonExecution.ModuleSystem,
		ParentExecutionID: &event.ParentExecutionID,
		Status:            commonExecution.ExecutionStatusRunning,
		Progress:          0,
		CurrentStep:       &currentStep,
		TriggerType:       triggerType,
		TriggeredBy:       qualityIntPtr(int(event.RequestedBy)),
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
	status := commonExecution.StatusFromCleanupResult(result.Status)
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
		s.log.Warn("更新 Quality 资源回收执行记录失败", "execution_id", executionID, "error", err)
	}
}

func (s *CleanupService) writeResult(ctx context.Context, taskID string, result events.CleanupResultData) {
	if s.redis == nil || taskID == "" {
		return
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		s.log.Error("序列化 Quality 资源回收结果失败", "error", err, "task_id", taskID)
		return
	}
	key := fmt.Sprintf("cleanup:results:%s", taskID)
	if err := s.redis.HSet(ctx, key, events.ModuleQuality, string(resultJSON)).Err(); err != nil {
		s.log.Error("写入 Quality 资源回收结果失败", "error", err, "task_id", taskID)
	}
}

func qualityCleanupStatsToMap(stats *QualityCleanupStats) map[string]interface{} {
	if stats == nil {
		return nil
	}
	data, _ := json.Marshal(stats)
	var result map[string]interface{}
	_ = json.Unmarshal(data, &result)
	return result
}

func qualityScanSummary(stats *QualityCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	scanned := stats.RuleApplications + stats.CheckTasks + stats.Issues
	return events.CleanupResultSummary{
		ScannedItems: scanned,
		ErrorCount:   len(stats.Errors),
		RiskLevel:    qualityRiskLevelForCount(scanned),
	}
}

func qualityExecuteSummary(stats *QualityCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	affected := stats.DisabledRuleApps + stats.IgnoredIssues + stats.DeletedRuleApplications + stats.DeletedCheckTasks + stats.DeletedIssues
	return events.CleanupResultSummary{
		AffectedRecords:         affected,
		DisabledTaskDefinitions: 0,
		SkippedItems:            stats.SkippedIssues,
		ErrorCount:              len(stats.Errors),
		RiskLevel:               "low",
	}
}

func qualityRiskLevelForCount(count int) string {
	if count > 1000 {
		return "high"
	}
	if count > 100 {
		return "medium"
	}
	return "low"
}

func qualityIntPtr(value int) *int {
	return &value
}
