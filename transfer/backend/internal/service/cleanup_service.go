package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/addp/common/cleanup"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	"github.com/addp/transfer/internal/deadletter"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TransferCleanupService struct {
	db               *gorm.DB
	runtime          *cleanup.Runner
	captureControl   CaptureControl
	deadLetterRepo   *repository.DeadLetterRepository
	deadLetterTopics DeadLetterTopicCleaner
	taskRepo         *repository.TaskRepository
	taskResources    *repository.TaskResourceRepository
	config           TaskOwnedCleanupConfig
	log              *slog.Logger
}

type DeadLetterTopicCleaner interface {
	DeleteTaskTopic(ctx context.Context, tenantID, taskID uint) error
}

type TaskOwnedResourceCleanup interface {
	DeleteTaskAndOwnedResources(ctx context.Context, task *models.TransferTask, mode repository.TaskDefinitionDeleteMode) (TaskOwnedResourceCleanupResult, error)
}

type TaskOwnedCleanupConfig struct {
	RuntimeStopTimeout      time.Duration
	RuntimeStopPollInterval time.Duration
}

type TaskOwnedResourceCleanupResult struct {
	CaptureCleaned              bool
	DeadLetterTopicCleaned      bool
	DeletedDeadLetterRecords    int64
	DeletedSyncStates           int64
	DeletedRuntimeLeases        int64
	DeletedSchemaChangeRequests int64
	DeletedCaptureResources     int64
	CancelledExecutions         int64
}

type TransferCleanupStats struct {
	TaskDefinitions             int      `json:"task_definitions"`
	DisabledTaskDefinitions     int      `json:"disabled_task_definitions,omitempty"`
	DeletedTaskDefinitions      int      `json:"deleted_task_definitions,omitempty"`
	CaptureResources            int      `json:"capture_resources,omitempty"`
	CleanedCaptureResources     int      `json:"cleaned_capture_resources,omitempty"`
	DeadLetterTopics            int      `json:"dead_letter_topics,omitempty"`
	CleanedDeadLetterTopics     int      `json:"cleaned_dead_letter_topics,omitempty"`
	DeletedDeadLetterRecords    int64    `json:"deleted_dead_letter_records,omitempty"`
	DeletedSyncStates           int64    `json:"deleted_sync_states,omitempty"`
	DeletedRuntimeLeases        int64    `json:"deleted_runtime_leases,omitempty"`
	DeletedSchemaChangeRequests int64    `json:"deleted_schema_change_requests,omitempty"`
	DeletedCaptureResources     int64    `json:"deleted_capture_resources,omitempty"`
	CancelledExecutions         int64    `json:"cancelled_executions,omitempty"`
	Errors                      []string `json:"errors,omitempty"`
}

func (s *TransferCleanupService) SetCaptureControl(control CaptureControl) {
	s.captureControl = control
}

func (s *TransferCleanupService) SetDeadLetterTopicCleaner(cleaner DeadLetterTopicCleaner) {
	s.deadLetterTopics = cleaner
}

func NewTransferCleanupService(db *gorm.DB, systemClient *commonClient.SystemServiceClient, config TaskOwnedCleanupConfig) *TransferCleanupService {
	s := &TransferCleanupService{db: db, deadLetterRepo: repository.NewDeadLetterRepository(db), taskRepo: repository.NewTaskRepository(db), taskResources: repository.NewTaskResourceRepository(db), config: config, log: logger.With("component", "transfer_cleanup_service")}
	s.runtime = &cleanup.Runner{DB: db, Owner: events.ModuleTransfer, System: systemClient, Log: s.log, Handle: s.runCleanup}
	return s
}

// DeleteTaskAndOwnedResources 是直接 task 删除与 System physical cleanup 的唯一删除入口。
func (s *TransferCleanupService) DeleteTaskAndOwnedResources(ctx context.Context, task *models.TransferTask, mode repository.TaskDefinitionDeleteMode) (TaskOwnedResourceCleanupResult, error) {
	var result TaskOwnedResourceCleanupResult
	if s == nil || s.db == nil || s.deadLetterRepo == nil || s.taskRepo == nil || s.taskResources == nil {
		return result, fmt.Errorf("transfer task-owned resource cleanup is not configured")
	}
	if task == nil || task.ID == 0 || task.TenantID == 0 {
		return result, fmt.Errorf("transfer task-owned resource cleanup requires task and tenant IDs")
	}
	boundary, err := planner.TaskRuntimeBoundary(task.Config)
	if err != nil {
		return result, err
	}
	continuous := boundary == planner.RuntimeBoundaryContinuous
	if !continuous && task.Status == models.TaskStatusRunning {
		return result, repository.ErrTaskDeletionRuntimeActive
	}
	if continuous {
		if s.config.RuntimeStopTimeout <= 0 || s.config.RuntimeStopPollInterval <= 0 {
			return result, fmt.Errorf("continuous runtime stop policy is not configured")
		}
		if err := repository.MutateCleanup(ctx, s.db, func(tx *gorm.DB) error {
			return repository.NewTaskRepository(tx).SetContinuousDesiredState(ctx, task.ID, task.TenantID, models.TaskDesiredStateStopped, "cleanup")
		}); err != nil {
			return result, fmt.Errorf("request continuous runtime cleanup stop: %w", err)
		}
		if err := s.taskRepo.WaitContinuousRuntimeStopped(ctx, task.ID, s.config.RuntimeStopTimeout, s.config.RuntimeStopPollInterval); err != nil {
			return result, fmt.Errorf("wait for continuous runtime cleanup stop: %w", err)
		}
		task.DesiredState = models.TaskDesiredStateStopped
	}
	if planner.IsDatabaseCDCTaskConfig(task.Config) {
		if s.captureControl == nil {
			return result, ErrCDCCaptureControlUnavailable
		}
		if err := s.captureControl.Stop(ctx, task); err != nil {
			return result, fmt.Errorf("cleanup database CDC capture: %w", err)
		}
		result.CaptureCleaned = true
	}
	hasDeadLetterResources, err := s.hasDeadLetterResources(ctx, task)
	if err != nil {
		return result, err
	}
	if hasDeadLetterResources {
		if s.deadLetterTopics == nil {
			return result, fmt.Errorf("dead-letter Kafka topic cleanup is unavailable")
		}
		if err := s.deleteDeadLetterTopic(ctx, task); err != nil {
			return result, err
		}
		result.DeadLetterTopicCleaned = true
	}
	deleted, err := s.taskResources.DeleteTaskAndPrivateState(ctx, task.TenantID, task.ID, continuous, mode, time.Now().UTC())
	if err != nil {
		return result, err
	}
	result.DeletedDeadLetterRecords = deleted.DeadLetters
	result.DeletedSyncStates = deleted.SyncStates
	result.DeletedRuntimeLeases = deleted.RuntimeLeases
	result.DeletedSchemaChangeRequests = deleted.SchemaChangeRequests
	result.DeletedCaptureResources = deleted.CaptureResources
	result.CancelledExecutions = deleted.CancelledExecutions
	return result, nil
}

func isBusinessKafkaContinuousTask(task *models.TransferTask) bool {
	if task == nil {
		return false
	}
	_, err := planner.ParseContinuousTaskSpec(task.Config)
	return err == nil
}

func (s *TransferCleanupService) hasDeadLetterResources(ctx context.Context, task *models.TransferTask) (bool, error) {
	if isBusinessKafkaContinuousTask(task) {
		return true, nil
	}
	if task == nil || s == nil || s.deadLetterRepo == nil {
		return false, nil
	}
	return s.deadLetterRepo.ExistsByTask(ctx, task.TenantID, task.ID)
}

func (s *TransferCleanupService) Start(ctx context.Context) error { return s.runtime.Start(ctx) }
func (s *TransferCleanupService) Stop()                           { s.runtime.Stop() }

func (s *TransferCleanupService) runCleanup(ctx context.Context, run *cleanup.Run) error {
	if run.Input.Context.TenantID != 0 {
		return fmt.Errorf("historical Tenant cleanup unsupported")
	}
	ctx = repository.WithCleanupMutation(repository.WithCleanupAdmission(ctx, run.PrepareExternal), run.Effect)
	finish := func(tx *gorm.DB, stats *TransferCleanupStats) error {
		summary := transferScanSummary(stats)
		if run.Input.Action == events.CleanupActionExecute {
			summary = transferExecuteSummary(stats)
		}
		scoped := *s
		scoped.db = tx
		tasks, err := scoped.listCandidateTaskDefinitions(ctx, run.Input.TenantID, run.Input.Context.Map())
		if err != nil {
			return err
		}
		impact, err := events.BuildCleanupImpactData(nil, "")
		if run.Input.Context.EngineID > 0 {
			impact, err = transferEngineDeletionImpact(tasks)
		}
		if err != nil {
			return err
		}
		result := events.CleanupResultData{TaskID: run.Input.RequestID, TenantID: run.Input.TenantID, Module: events.ModuleTransfer, Action: run.Input.Action, CleanupMode: run.Input.CleanupMode, Status: events.CleanupResultSuccess, Summary: summary, Impact: &impact}
		receipt, err := commonExecution.NewCleanupReceipt(run.Execution, result, "")
		if err != nil {
			return err
		}
		return cleanup.Complete(ctx, tx, run.Lease, receipt, transferCleanupCounters(stats))
	}
	if run.Input.Action == events.CleanupActionScan {
		return run.Effect(ctx, func(tx *gorm.DB) error {
			scoped := *s
			scoped.db = tx
			scoped.deadLetterRepo = repository.NewDeadLetterRepository(tx)
			stats, err := scoped.ScanReclaimCandidates(ctx, run.Input.TenantID, run.Input.Context.Map())
			if err != nil {
				return err
			}
			return finish(tx, stats)
		})
	}
	_, err := s.executeCleanup(ctx, run.Input.TenantID, run.Input.CleanupMode, run.Input.Context.Map(), finish)
	return err
}

func transferCleanupCounters(s *TransferCleanupStats) map[string]int64 {
	return map[string]int64{"task_definitions": int64(s.TaskDefinitions), "disabled_task_definitions": int64(s.DisabledTaskDefinitions), "deleted_task_definitions": int64(s.DeletedTaskDefinitions), "capture_resources": int64(s.CaptureResources), "cleaned_capture_resources": int64(s.CleanedCaptureResources), "dead_letter_topics": int64(s.DeadLetterTopics), "cleaned_dead_letter_topics": int64(s.CleanedDeadLetterTopics), "deleted_dead_letter_records": s.DeletedDeadLetterRecords, "deleted_sync_states": s.DeletedSyncStates, "deleted_runtime_leases": s.DeletedRuntimeLeases, "deleted_schema_change_requests": s.DeletedSchemaChangeRequests, "deleted_capture_resources": s.DeletedCaptureResources, "cancelled_executions": s.CancelledExecutions}
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
		if planner.IsDatabaseCDCTaskConfig(task.Config) {
			stats.CaptureResources++
		}
		hasDeadLetterResources, err := s.hasDeadLetterResources(ctx, &task)
		if err != nil {
			return nil, err
		}
		if hasDeadLetterResources {
			stats.DeadLetterTopics++
		}
	}
	return stats, nil
}

func (s *TransferCleanupService) ExecuteCleanup(ctx context.Context, tenantID uint, mode string, scope map[string]interface{}) (*TransferCleanupStats, error) {
	return s.executeCleanup(ctx, tenantID, mode, scope, nil)
}

func (s *TransferCleanupService) executeCleanup(ctx context.Context, tenantID uint, mode string, scope map[string]interface{}, finish func(*gorm.DB, *TransferCleanupStats) error) (*TransferCleanupStats, error) {
	if tenantID == 0 || events.ValidateCleanupMode(mode) != nil {
		return nil, fmt.Errorf("invalid Transfer cleanup scope")
	}
	tasks, err := s.listCandidateTaskDefinitions(ctx, tenantID, scope)
	if err != nil {
		return nil, err
	}
	stats := &TransferCleanupStats{TaskDefinitions: len(tasks)}
	_, engineDeletion := cleanupContextUint(scope, "engine_id")
	physical := mode == events.CleanupModePhysical && !engineDeletion
	for i := range tasks {
		task := &tasks[i]
		boundary, err := planner.TaskRuntimeBoundary(task.Config)
		if err != nil {
			return stats, err
		}
		if boundary == planner.RuntimeBoundaryContinuous {
			if err := repository.MutateCleanup(ctx, s.db, func(tx *gorm.DB) error {
				return repository.NewTaskRepository(tx).SetContinuousDesiredState(ctx, task.ID, tenantID, models.TaskDesiredStateStopped, "cleanup")
			}); err != nil {
				return stats, err
			}
			if err := s.taskRepo.WaitContinuousRuntimeStopped(ctx, task.ID, s.config.RuntimeStopTimeout, s.config.RuntimeStopPollInterval); err != nil {
				return stats, err
			}
		} else if task.Status == models.TaskStatusRunning {
			return stats, repository.ErrTaskDeletionRuntimeActive
		}
		if planner.IsDatabaseCDCTaskConfig(task.Config) {
			stats.CaptureResources++
			if s.captureControl == nil {
				return stats, ErrCDCCaptureControlUnavailable
			}
			if err := s.captureControl.Stop(ctx, task); err != nil {
				return stats, err
			}
			stats.CleanedCaptureResources++
		}
		hasDLQ, err := s.hasDeadLetterResources(ctx, task)
		if err != nil {
			return stats, err
		}
		if hasDLQ {
			stats.DeadLetterTopics++
		}
		if physical && hasDLQ {
			if err := s.deleteDeadLetterTopic(ctx, task); err != nil {
				return stats, err
			}
			stats.CleanedDeadLetterTopics++
		}
	}
	err = repository.MutateCleanup(ctx, s.db, func(tx *gorm.DB) error {
		for _, expected := range tasks {
			var current models.TransferTask
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", expected.ID, tenantID).First(&current).Error; err != nil {
				return err
			}
			originalConfig, _ := json.Marshal(expected.Config)
			currentConfig, _ := json.Marshal(current.Config)
			if string(originalConfig) != string(currentConfig) {
				return repository.ErrCleanupOperationUnresolved
			}
			boundary, err := planner.TaskRuntimeBoundary(current.Config)
			if err != nil {
				return err
			}
			if boundary == planner.RuntimeBoundaryContinuous {
				if current.DesiredState != models.TaskDesiredStateStopped {
					return repository.ErrTaskDeletionRuntimeActive
				}
				if err := repository.RequireRuntimeStopped(ctx, tx, tenantID, current.ID); err != nil {
					return err
				}
			} else {
				if current.Status == models.TaskStatusRunning {
					return repository.ErrTaskDeletionRuntimeActive
				}
				var active int64
				if err := tx.WithContext(ctx).Model(&commonExecution.TaskExecution{}).Where("tenant_id=? AND module='transfer' AND task_type='sync' AND source_task_id=? AND status IN ?", tenantID, strconv.FormatUint(uint64(current.ID), 10), []string{"pending", "running"}).Count(&active).Error; err != nil {
					return err
				}
				if active != 0 {
					return repository.ErrTaskDeletionRuntimeActive
				}
			}
			if physical {
				deleted, err := repository.NewTaskResourceRepository(tx).DeleteTaskAndPrivateState(ctx, tenantID, current.ID, boundary == planner.RuntimeBoundaryContinuous, repository.TaskDefinitionDeletePhysical, time.Now().UTC())
				if err != nil {
					return err
				}
				stats.DeletedTaskDefinitions++
				stats.DeletedDeadLetterRecords += deleted.DeadLetters
				stats.DeletedSyncStates += deleted.SyncStates
				stats.DeletedRuntimeLeases += deleted.RuntimeLeases
				stats.DeletedSchemaChangeRequests += deleted.SchemaChangeRequests
				stats.DeletedCaptureResources += deleted.CaptureResources
				stats.CancelledExecutions += deleted.CancelledExecutions
			} else {
				if err := tx.Model(&current).Updates(map[string]interface{}{"enabled": false, "next_run_at": nil, "status": models.TaskStatusIdle, "updated_at": time.Now().UTC()}).Error; err != nil {
					return err
				}
				stats.DisabledTaskDefinitions++
			}
		}
		if finish != nil {
			return finish(tx, stats)
		}
		return nil
	})
	return stats, err
}

func (s *TransferCleanupService) deleteDeadLetterTopic(ctx context.Context, task *models.TransferTask) error {
	if s.deadLetterTopics == nil {
		return fmt.Errorf("dead-letter topic cleaner unavailable")
	}
	topic, err := deadletter.TopicName(task.TenantID, task.ID)
	if err != nil {
		return err
	}
	return repository.ExecuteCleanupOperation(ctx, s.db, task.TenantID, task.ID, "dead_letter_topic", nil, struct {
		TenantID, TaskID uint
		Topic            string
	}{task.TenantID, task.ID, topic}, func(callCtx context.Context) error {
		return s.deadLetterTopics.DeleteTaskTopic(callCtx, task.TenantID, task.ID)
	})
}

func transferEngineDeletionImpact(tasks []models.TransferTask) (events.CleanupImpactData, error) {
	items := make([]events.CleanupImpactItem, 0, len(tasks)*2)
	for _, task := range tasks {
		stableRef := fmt.Sprintf("transfer_task:%d", task.ID)
		items = append(items, events.CleanupImpactItem{StableRef: stableRef, Disposition: events.CleanupImpactWillDisable})
		if transferTaskIsRunning(task) {
			items = append(items, events.CleanupImpactItem{StableRef: stableRef, Disposition: events.CleanupImpactRunning})
		}
	}
	return events.BuildCleanupImpactData(items, "/transfer/tasks")
}

func transferTaskIsRunning(task models.TransferTask) bool {
	return task.Status == models.TaskStatusRunning || task.DesiredState == models.TaskDesiredStateRunning
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
	if spec, err := planner.ParseDatabaseCDCTaskSpec(task.Config); err == nil {
		return endpointReferencesEngine(spec.Source, engineID) || endpointReferencesEngine(spec.Target, engineID)
	}
	if spec, err := planner.ParseRawCopyTaskSpec(task.Config); err == nil {
		return endpointReferencesEngine(spec.Source, engineID) || endpointReferencesEngine(spec.Target, engineID)
	}
	if spec, err := planner.ParseEncodedRecordExportTaskSpec(task.Config, task.BatchSize); err == nil {
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

func transferScanSummary(stats *TransferCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	return events.CleanupResultSummary{
		ScannedItems: stats.TaskDefinitions,
		ErrorCount:   len(stats.Errors),
		RiskLevel:    transferRiskLevelForCount(stats.TaskDefinitions),
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
