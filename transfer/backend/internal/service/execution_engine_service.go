package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/logger"
	"github.com/addp/transfer/internal/executor"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/repository"
)

// ExecutionEngineService executes Transfer tasks through common engine/format.
type ExecutionEngineService struct {
	taskRepo         *repository.TaskRepository
	executionService *ExecutionService
	systemClient     *commonClient.SystemClient
	metaClient       *commonClient.MetaClient
	logger           *slog.Logger
}

func NewExecutionEngineService(
	taskRepo *repository.TaskRepository,
	executionService *ExecutionService,
	systemClient *commonClient.SystemClient,
	metaClient *commonClient.MetaClient,
) *ExecutionEngineService {
	return &ExecutionEngineService{
		taskRepo:         taskRepo,
		executionService: executionService,
		systemClient:     systemClient,
		metaClient:       metaClient,
		logger:           logger.With("component", "execution_engine_service"),
	}
}

// ExecuteTask 执行任务（由 Worker 调用）
func (s *ExecutionEngineService) ExecuteTask(ctx context.Context, taskID, executionID uint) error {
	s.logger.Info("executing task", "task_id", taskID, "execution_id", executionID)

	task, err := s.taskRepo.GetByID(taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	return s.executeCommonTransferTask(ctx, task, executionID)
}

func (s *ExecutionEngineService) executeCommonTransferTask(ctx context.Context, task *models.TransferTask, executionID uint) error {
	if s.systemClient == nil {
		err := fmt.Errorf("system client is required for common engine/format transfer task")
		s.updateExecutionError(task, executionID, err)
		return err
	}

	if err := s.executionService.UpdateStatus(ctx, executionID, models.ExecutionStatusRunning); err != nil {
		s.logger.Warn("failed to update execution status", "error", err)
	}

	rawSpec, rawErr := planner.ParseRawCopyTaskSpec(task.Config)
	if rawErr == nil {
		if err := s.attachRawCopySourceMetaAttributes(task, &rawSpec); err != nil {
			wrapped := fmt.Errorf("load source meta item attributes: %w", err)
			s.updateExecutionError(task, executionID, wrapped)
			return wrapped
		}
		resolver := planner.NewSystemEngineResolver(s.systemClient)
		return s.executeCommonRawCopyTask(ctx, task, executionID, rawSpec, resolver)
	}

	spec, err := planner.ParseTableExportTaskSpec(task.Config, task.BatchSize)
	if err != nil {
		wrapped := fmt.Errorf("parse common transfer task config: table=%v; raw_copy=%v", err, rawErr)
		s.updateExecutionError(task, executionID, wrapped)
		return wrapped
	}
	if err := s.attachSourceMetaAttributes(task, &spec); err != nil {
		wrapped := fmt.Errorf("load source meta item attributes: %w", err)
		s.updateExecutionError(task, executionID, wrapped)
		return wrapped
	}

	resolver := planner.NewSystemEngineResolver(s.systemClient)
	return s.executeCommonTableTransferTask(ctx, task, executionID, spec, resolver)
}

func (s *ExecutionEngineService) attachRawCopySourceMetaAttributes(task *models.TransferTask, spec *planner.RawCopyTaskSpec) error {
	if task == nil || spec == nil || spec.Source.MetaItemID == 0 {
		return nil
	}
	if s.metaClient == nil {
		return fmt.Errorf("meta client is required when source meta_item_id is set")
	}
	item, err := s.metaClient.WithTenantID(task.TenantID).GetMetaItemByID(spec.Source.MetaItemID)
	if err != nil {
		return err
	}
	if item.EngineID != spec.Source.Engine.ID {
		return fmt.Errorf("source meta item engine_id %d does not match source engine id %d", item.EngineID, spec.Source.Engine.ID)
	}
	spec.Source.Attributes = item.Attributes
	return nil
}

func (s *ExecutionEngineService) attachSourceMetaAttributes(task *models.TransferTask, spec *planner.TableExportTaskSpec) error {
	if task == nil || spec == nil || spec.Source.MetaItemID == 0 {
		return nil
	}
	if s.metaClient == nil {
		return fmt.Errorf("meta client is required when source meta_item_id is set")
	}
	item, err := s.metaClient.WithTenantID(task.TenantID).GetMetaItemByID(spec.Source.MetaItemID)
	if err != nil {
		return err
	}
	if item.EngineID != spec.Source.Engine.ID {
		return fmt.Errorf("source meta item engine_id %d does not match source engine id %d", item.EngineID, spec.Source.Engine.ID)
	}
	spec.Source.Attributes = item.Attributes
	return nil
}

func (s *ExecutionEngineService) executeCommonTableTransferTask(ctx context.Context, task *models.TransferTask, executionID uint, spec planner.TableExportTaskSpec, resolver planner.EngineResolver) error {
	buildResult, err := planner.BuildTableTransferPlan(spec, resolver)
	if err != nil {
		wrapped := fmt.Errorf("build common table transfer plan: %w", err)
		s.updateExecutionError(task, executionID, wrapped)
		return wrapped
	}
	buildResult.Plan.ProgressCallback = s.tableProgressCallback(task, executionID)

	tableExecutor, err := executor.NewTableTransferExecutor(
		buildResult.SourceEngineType,
		buildResult.TargetEngineType,
		buildResult.Plan.Source.Format,
		buildResult.Plan.Target.Format,
	)
	if err != nil {
		wrapped := fmt.Errorf("create common table transfer executor: %w", err)
		s.updateExecutionError(task, executionID, wrapped)
		return wrapped
	}

	metrics, err := tableExecutor.Execute(ctx, buildResult.Plan)
	if err != nil {
		wrapped := fmt.Errorf("execute common table transfer plan: %w", err)
		if metrics != nil {
			s.updateTableTransferMetrics(executionID, metrics.RecordsRead, metrics.RecordsWritten)
		}
		s.updateExecutionError(task, executionID, wrapped)
		return wrapped
	}
	s.updateTableTransferMetrics(executionID, metrics.RecordsRead, metrics.RecordsWritten)

	if err := s.executionService.FinishExecution(ctx, executionID, models.ExecutionStatusSuccess, ""); err != nil {
		s.logger.Warn("failed to finish execution", "error", err)
	}
	if err := s.taskRepo.UpdateFields(task.ID, map[string]interface{}{
		"status":   models.TaskStatusIdle,
		"progress": 100.0,
	}); err != nil {
		s.logger.Warn("failed to update task after successful table transfer", "error", err, "task_id", task.ID)
	}
	if task.AutoScanMetadata {
		s.triggerMetadataScan(task, spec)
	}
	return nil
}

func (s *ExecutionEngineService) executeCommonRawCopyTask(ctx context.Context, task *models.TransferTask, executionID uint, spec planner.RawCopyTaskSpec, resolver planner.EngineResolver) error {
	buildResult, err := planner.BuildRawCopyPlan(spec, resolver)
	if err != nil {
		wrapped := fmt.Errorf("build common raw copy plan: %w", err)
		s.updateExecutionError(task, executionID, wrapped)
		return wrapped
	}
	buildResult.Plan.ProgressCallback = s.rawCopyProgressCallback(task, executionID)

	rawCopyExecutor, err := executor.NewRawCopyExecutor(buildResult.SourceEngineType, buildResult.TargetEngineType)
	if err != nil {
		wrapped := fmt.Errorf("create common raw copy executor: %w", err)
		s.updateExecutionError(task, executionID, wrapped)
		return wrapped
	}

	metrics, err := rawCopyExecutor.Execute(ctx, buildResult.Plan)
	if err != nil {
		wrapped := fmt.Errorf("execute common raw copy plan: %w", err)
		if metrics != nil {
			s.updateRawCopyMetrics(executionID, metrics)
		}
		s.updateExecutionError(task, executionID, wrapped)
		return wrapped
	}
	s.updateRawCopyMetrics(executionID, metrics)

	if err := s.executionService.FinishExecution(ctx, executionID, models.ExecutionStatusSuccess, ""); err != nil {
		s.logger.Warn("failed to finish execution", "error", err)
	}
	if err := s.taskRepo.UpdateFields(task.ID, map[string]interface{}{
		"status":   models.TaskStatusIdle,
		"progress": 100.0,
	}); err != nil {
		s.logger.Warn("failed to update task after successful raw copy", "error", err, "task_id", task.ID)
	}
	if task.AutoScanMetadata {
		s.triggerMetadataScan(task, planner.TableExportTaskSpec{Target: spec.Target})
	}
	return nil
}

func (s *ExecutionEngineService) updateExecutionError(task *models.TransferTask, executionID uint, execErr error) {
	if execErr == nil {
		return
	}

	ctx := context.Background()
	if err := s.executionService.FinishExecution(ctx, executionID, models.ExecutionStatusFailed, execErr.Error()); err != nil {
		s.logger.Error("CRITICAL: failed to mark execution as failed - status inconsistency may occur",
			"error", err,
			"execution_id", executionID,
			"task_id", task.ID)
	} else {
		s.logger.Info("execution marked as failed", "execution_id", executionID)
	}

	if err := s.taskRepo.UpdateFields(task.ID, map[string]interface{}{
		"status":   models.TaskStatusIdle,
		"progress": 0,
	}); err != nil {
		s.logger.Error("CRITICAL: failed to update task after execution error - status inconsistency may occur",
			"error", err,
			"task_id", task.ID,
			"execution_id", executionID)
	} else {
		s.logger.Info("task status updated after execution failure",
			"task_id", task.ID,
			"status", "idle")
	}
}

func (s *ExecutionEngineService) updateTableTransferMetrics(executionID uint, recordsRead, recordsWritten int64) {
	ctx := context.Background()
	if err := s.executionService.UpdateMetrics(ctx, executionID, map[string]interface{}{
		"records_read":    recordsRead,
		"records_written": recordsWritten,
	}); err != nil {
		s.logger.Error("failed to update common transfer execution metrics", "error", err, "execution_id", executionID)
	}
}

func (s *ExecutionEngineService) updateRawCopyMetrics(executionID uint, metrics *executor.RawCopyMetrics) {
	if metrics == nil {
		return
	}
	ctx := context.Background()
	if err := s.executionService.UpdateMetrics(ctx, executionID, map[string]interface{}{
		"records_read":    metrics.RecordsRead,
		"records_written": metrics.RecordsWritten,
		"bytes_read":      metrics.BytesRead,
		"bytes_written":   metrics.BytesWritten,
	}); err != nil {
		s.logger.Error("failed to update raw copy execution metrics", "error", err, "execution_id", executionID)
	}
}

func (s *ExecutionEngineService) tableProgressCallback(task *models.TransferTask, executionID uint) executor.TableProgressCallback {
	return func(ctx context.Context, event executor.TableProgressEvent) error {
		if s.executionService == nil {
			return nil
		}
		if err := s.executionService.UpdateMetrics(ctx, executionID, map[string]interface{}{
			"records_read":    event.RecordsRead,
			"records_written": event.RecordsWritten,
		}); err != nil {
			return fmt.Errorf("update metrics: %w", err)
		}
		checkpointState := map[string]interface{}{
			"version":          "v1",
			"batch_index":      event.BatchIndex,
			"source_offset":    event.SourceOffset,
			"records_read":     event.RecordsRead,
			"records_written":  event.RecordsWritten,
			"target_committed": true,
			"updated_at":       time.Now().Format(time.RFC3339),
		}
		if event.ResumeMarker != nil {
			checkpointState["resume_marker"] = event.ResumeMarker.Clone()
		}
		if event.CommitMarker != nil {
			checkpointState["commit_marker"] = event.CommitMarker.Clone()
		}
		if event.Final {
			checkpointState["final"] = true
		}
		if err := s.executionService.UpdateExecution(ctx, executionID, map[string]interface{}{
			"checkpoint_offset": event.RecordsRead,
			"checkpoint_state":  checkpointState,
		}); err != nil {
			return fmt.Errorf("update checkpoint: %w", err)
		}
		if task != nil {
			progress := runningProgress(event.BatchIndex)
			if err := s.taskRepo.UpdateFields(task.ID, map[string]interface{}{"progress": progress}); err != nil {
				s.logger.Warn("failed to update task progress", "error", err, "task_id", task.ID, "progress", progress)
			}
		}
		logLine := fmt.Sprintf(
			"%s batch=%d source_offset=%d batch_rows=%d records_read=%d records_written=%d target_committed=true final=%t resume_marker=%t commit_marker=%t",
			time.Now().Format(time.RFC3339),
			event.BatchIndex,
			event.SourceOffset,
			event.BatchRows,
			event.RecordsRead,
			event.RecordsWritten,
			event.Final,
			event.ResumeMarker != nil,
			event.CommitMarker != nil,
		)
		if err := s.executionService.AppendLog(ctx, executionID, logLine); err != nil {
			return fmt.Errorf("append progress log: %w", err)
		}
		return nil
	}
}

func (s *ExecutionEngineService) rawCopyProgressCallback(task *models.TransferTask, executionID uint) executor.RawCopyProgressCallback {
	return func(ctx context.Context, event executor.RawCopyProgressEvent) error {
		if s.executionService == nil {
			return nil
		}
		if err := s.executionService.UpdateMetrics(ctx, executionID, map[string]interface{}{
			"records_read":    event.RecordsRead,
			"records_written": event.RecordsWritten,
			"bytes_read":      event.BytesRead,
			"bytes_written":   event.BytesWritten,
		}); err != nil {
			return fmt.Errorf("update raw copy metrics: %w", err)
		}
		checkpointState := map[string]interface{}{
			"version":          "v1",
			"records_read":     event.RecordsRead,
			"records_written":  event.RecordsWritten,
			"bytes_read":       event.BytesRead,
			"bytes_written":    event.BytesWritten,
			"target_committed": true,
			"updated_at":       time.Now().Format(time.RFC3339),
		}
		if event.Final {
			checkpointState["final"] = true
		}
		if err := s.executionService.UpdateExecution(ctx, executionID, map[string]interface{}{
			"checkpoint_offset": event.RecordsRead,
			"checkpoint_state":  checkpointState,
		}); err != nil {
			return fmt.Errorf("update raw copy checkpoint: %w", err)
		}
		if task != nil {
			progress := 99.0
			if event.Final {
				progress = 100.0
			}
			if err := s.taskRepo.UpdateFields(task.ID, map[string]interface{}{"progress": progress}); err != nil {
				s.logger.Warn("failed to update raw copy task progress", "error", err, "task_id", task.ID, "progress", progress)
			}
		}
		logLine := fmt.Sprintf(
			"%s raw_copy bytes_read=%d bytes_written=%d records_read=%d records_written=%d target_committed=true final=%t",
			time.Now().Format(time.RFC3339),
			event.BytesRead,
			event.BytesWritten,
			event.RecordsRead,
			event.RecordsWritten,
			event.Final,
		)
		if err := s.executionService.AppendLog(ctx, executionID, logLine); err != nil {
			return fmt.Errorf("append raw copy progress log: %w", err)
		}
		return nil
	}
}

func runningProgress(batchIndex int64) float64 {
	if batchIndex <= 0 {
		return 0
	}
	progress := float64(batchIndex)
	if progress > 99 {
		return 99
	}
	return progress
}

func (s *ExecutionEngineService) triggerMetadataScan(task *models.TransferTask, spec planner.TableExportTaskSpec) {
	if s.metaClient == nil {
		s.logger.Warn("meta client not available, skipping metadata scan", "task_id", task.ID)
		return
	}

	targetEngineID := spec.Target.Engine.ID
	if targetEngineID == 0 {
		s.logger.Warn("no target engine id found, skipping metadata scan", "task_id", task.ID)
		return
	}

	metaClient := s.metaClient.WithTenantID(task.TenantID)
	catalogPaths := targetCatalogPaths(spec.Target)

	s.logger.Info("triggering metadata scan",
		"task_id", task.ID,
		"engine_id", targetEngineID,
		"catalog_paths", catalogPaths)

	run, err := metaClient.CreateManualScanRun(commonClient.MetaScanOptions{
		EngineID:     targetEngineID,
		CatalogPaths: catalogPaths,
		ScanDepth:    "deep",
		Force:        true,
		TriggerType:  "transfer",
	})
	if err != nil {
		s.logger.Error("failed to trigger metadata scan",
			"error", err,
			"task_id", task.ID,
			"engine_id", targetEngineID)
		return
	}
	s.logger.Info("metadata scan triggered successfully",
		"task_id", task.ID,
		"engine_id", targetEngineID,
		"execution_id", run.ExecutionID)
}

func targetCatalogPaths(endpoint planner.EndpointSpec) []string {
	path, ok := endpoint.EndpointResource.Path.(map[string]interface{})
	if !ok {
		if endpoint.EndpointResource.Kind == planner.EndpointResourceKindFile {
			return parentFileCatalogPaths(fmt.Sprint(endpoint.EndpointResource.Path))
		}
		return nil
	}

	switch endpoint.EndpointResource.Kind {
	case planner.EndpointResourceKindNativeTable:
		if qualified := strings.TrimSpace(fmt.Sprintf("%v", path["name"])); qualified != "" && qualified != "<nil>" {
			parts := strings.Split(qualified, ".")
			if len(parts) == 2 {
				return []string{strings.TrimSpace(parts[0])}
			}
		}
		for _, key := range []string{"schema", "database"} {
			if value := cleanPathValue(path[key]); value != "" {
				return []string{value}
			}
		}
	case planner.EndpointResourceKindObject:
		bucket := cleanPathValue(path["bucket"])
		objectPath := cleanPathValue(path["path"])
		if objectPath == "" {
			objectPath = cleanPathValue(path["object"])
		}
		return parentObjectCatalogPaths(bucket, objectPath)
	case planner.EndpointResourceKindFile:
		filePath := cleanPathValue(path["path"])
		if filePath == "" {
			filePath = cleanPathValue(path["file"])
		}
		return parentFileCatalogPaths(filePath)
	}

	for _, key := range []string{"schema", "database", "bucket"} {
		if value := cleanPathValue(path[key]); value != "" {
			return []string{value}
		}
	}
	return nil
}

func parentObjectCatalogPaths(bucket, objectPath string) []string {
	if bucket == "" {
		return nil
	}
	cleanObjectPath := strings.Trim(objectPath, "/")
	if cleanObjectPath == "" {
		return []string{bucket}
	}
	if idx := strings.LastIndex(cleanObjectPath, "/"); idx > 0 {
		return []string{bucket + "/" + cleanObjectPath[:idx]}
	}
	return []string{bucket}
}

func parentFileCatalogPaths(filePath string) []string {
	cleanFilePath := strings.Trim(strings.TrimSpace(filePath), "/")
	if cleanFilePath == "" || cleanFilePath == "." || cleanFilePath == "<nil>" {
		return nil
	}
	if idx := strings.LastIndex(cleanFilePath, "/"); idx > 0 {
		return []string{cleanFilePath[:idx]}
	}
	return []string{"/"}
}

func cleanPathValue(value interface{}) string {
	if value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprintf("%v", value))
	if text == "<nil>" {
		return ""
	}
	return text
}
