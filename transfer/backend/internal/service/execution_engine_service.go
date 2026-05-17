package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

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

	return s.executeCommonTableExportTask(ctx, task, executionID)
}

func (s *ExecutionEngineService) executeCommonTableExportTask(ctx context.Context, task *models.TransferTask, executionID uint) error {
	if s.systemClient == nil {
		err := fmt.Errorf("system client is required for common engine/format transfer task")
		s.updateExecutionError(task, executionID, err)
		return err
	}

	if err := s.executionService.UpdateStatus(ctx, executionID, models.ExecutionStatusRunning); err != nil {
		s.logger.Warn("failed to update execution status", "error", err)
	}

	spec, err := planner.ParseTableExportTaskSpec(task.Config, task.BatchSize)
	if err != nil {
		wrapped := fmt.Errorf("parse common transfer task config: %w", err)
		s.updateExecutionError(task, executionID, wrapped)
		return wrapped
	}

	resolver := planner.NewSystemEngineResolver(s.systemClient)
	if planner.IsTableImportSpec(spec) {
		return s.executeCommonTableImportTask(ctx, task, executionID, spec, resolver)
	}
	if planner.IsEncodedTableTransferSpec(spec) {
		return s.executeEncodedTableTransferTask(ctx, task, executionID, spec, resolver)
	}
	return s.executeCommonTableExportPlan(ctx, task, executionID, spec, resolver)
}

func (s *ExecutionEngineService) executeCommonTableExportPlan(ctx context.Context, task *models.TransferTask, executionID uint, spec planner.TableExportTaskSpec, resolver planner.EngineResolver) error {
	buildResult, err := planner.BuildTableExportPlan(spec, resolver)
	if err != nil {
		wrapped := fmt.Errorf("build common transfer plan: %w", err)
		s.updateExecutionError(task, executionID, wrapped)
		return wrapped
	}

	tableExecutor, err := executor.NewTableExportExecutor(buildResult.SourceEngineType, buildResult.TargetEngineType, buildResult.Format)
	if err != nil {
		wrapped := fmt.Errorf("create common transfer executor: %w", err)
		s.updateExecutionError(task, executionID, wrapped)
		return wrapped
	}

	metrics, err := tableExecutor.Execute(ctx, buildResult.Plan)
	if err != nil {
		wrapped := fmt.Errorf("execute common transfer plan: %w", err)
		if metrics != nil {
			s.updateTableExportMetrics(executionID, metrics)
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
		s.logger.Warn("failed to update task after successful execution", "error", err, "task_id", task.ID)
	}
	if task.AutoScanMetadata {
		s.triggerMetadataScan(task, spec)
	}
	return nil
}

func (s *ExecutionEngineService) executeCommonTableImportTask(ctx context.Context, task *models.TransferTask, executionID uint, spec planner.TableExportTaskSpec, resolver planner.EngineResolver) error {
	buildResult, err := planner.BuildTableImportPlan(spec, resolver)
	if err != nil {
		wrapped := fmt.Errorf("build common import plan: %w", err)
		s.updateExecutionError(task, executionID, wrapped)
		return wrapped
	}

	tableExecutor, err := executor.NewTableImportExecutor(buildResult.SourceEngineType, buildResult.TargetEngineType, buildResult.Format)
	if err != nil {
		wrapped := fmt.Errorf("create common import executor: %w", err)
		s.updateExecutionError(task, executionID, wrapped)
		return wrapped
	}

	metrics, err := tableExecutor.Execute(ctx, buildResult.Plan)
	if err != nil {
		wrapped := fmt.Errorf("execute common import plan: %w", err)
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
		s.logger.Warn("failed to update task after successful import", "error", err, "task_id", task.ID)
	}
	if task.AutoScanMetadata {
		s.triggerMetadataScan(task, spec)
	}
	return nil
}

func (s *ExecutionEngineService) executeEncodedTableTransferTask(ctx context.Context, task *models.TransferTask, executionID uint, spec planner.TableExportTaskSpec, resolver planner.EngineResolver) error {
	buildResult, err := planner.BuildEncodedTableTransferPlan(spec, resolver)
	if err != nil {
		wrapped := fmt.Errorf("build encoded table transfer plan: %w", err)
		s.updateExecutionError(task, executionID, wrapped)
		return wrapped
	}

	tableExecutor, err := executor.NewEncodedTableTransferExecutor(buildResult.SourceEngineType, buildResult.TargetEngineType, buildResult.SourceFormat, buildResult.TargetFormat)
	if err != nil {
		wrapped := fmt.Errorf("create encoded table transfer executor: %w", err)
		s.updateExecutionError(task, executionID, wrapped)
		return wrapped
	}

	metrics, err := tableExecutor.Execute(ctx, buildResult.Plan)
	if err != nil {
		wrapped := fmt.Errorf("execute encoded table transfer plan: %w", err)
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
		s.logger.Warn("failed to update task after successful encoded transfer", "error", err, "task_id", task.ID)
	}
	if task.AutoScanMetadata {
		s.triggerMetadataScan(task, spec)
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

func (s *ExecutionEngineService) updateTableExportMetrics(executionID uint, metrics *executor.TableExportMetrics) {
	if metrics == nil {
		return
	}
	s.updateTableTransferMetrics(executionID, metrics.RecordsRead, metrics.RecordsWritten)
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

	if err := metaClient.TriggerScan(commonClient.MetaScanOptions{
		EngineID:     targetEngineID,
		CatalogPaths: catalogPaths,
		ScanDepth:    "deep",
		Force:        true,
		TriggerType:  "transfer",
	}); err != nil {
		s.logger.Error("failed to trigger metadata scan",
			"error", err,
			"task_id", task.ID,
			"engine_id", targetEngineID)
		return
	}
	s.logger.Info("metadata scan triggered successfully",
		"task_id", task.ID,
		"engine_id", targetEngineID)
}

func targetCatalogPaths(endpoint planner.EndpointSpec) []string {
	path, ok := endpoint.Resource.Path.(map[string]interface{})
	if !ok {
		return nil
	}
	for _, key := range []string{"schema", "database", "bucket"} {
		if value := strings.TrimSpace(fmt.Sprintf("%v", path[key])); value != "" && value != "<nil>" {
			return []string{value}
		}
	}
	return nil
}
