package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scantask"
)

func normalizeScanRequestTriggerType(triggerType string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(triggerType))
	if normalized == "" {
		return models.TriggerTypeManual, nil
	}
	switch normalized {
	case models.TriggerTypeManual, models.TriggerTypeScheduled:
		return normalized, nil
	default:
		return "", fmt.Errorf("unsupported trigger_type %q: use manual or scheduled", triggerType)
	}
}

// CreateManualRun 创建手动扫描执行并入队
func (s *ScanTaskService) CreateManualRun(ctx context.Context, tenantID, userID uint, token string, req *models.ScanRequest) (*commonExecution.TaskExecution, error) {
	if req == nil {
		return nil, errors.New("请求不能为空")
	}
	if _, err := normalizeScanRequestTriggerType(req.TriggerType); err != nil {
		return nil, err
	}
	scope, err := s.scanService.ResolveScanScope(tenantID, scanflow.Options{
		EngineID:     req.EngineID,
		CatalogPaths: req.CatalogPaths,
		RefGroups:    req.RefGroups,
		NodeID:       req.NodeID,
		ItemID:       req.ItemID,
		Targets:      req.Targets,
		ScanDepth:    req.ScanDepth,
		Force:        req.Force,
		Source:       req.Source,
	})
	if err != nil {
		return nil, fmt.Errorf("解析扫描范围失败: %w", err)
	}

	resource, err := s.engineService.GetResourceByID(scope.EngineID, tenantID, token)
	if err != nil {
		return nil, fmt.Errorf("验证资源失败: %w", err)
	}

	if s.dedupService != nil {
		taskKey := s.dedupService.GenerateTaskKey(tenantID, scope.EngineID, models.TriggerTypeManual)
		if s.dedupService.CheckTaskExists(ctx, taskKey) {
			return nil, fmt.Errorf("该资源正在扫描中，请稍后再试")
		}
		if err := s.dedupService.MarkTaskRunning(ctx, taskKey, 2*time.Hour); err != nil {
			s.log.Warn("标记任务运行失败", "error", err)
		}
	}

	execution := scantask.NewManualExecution(
		tenantID,
		userID,
		scope.EngineID,
		req.ItemID,
		scantask.NormalizeStorageType(resource.EngineType),
		scope.CatalogPaths,
		scope.RefGroups,
		scope.ScanDepth,
		scope.Force,
		scope.Source,
		token,
		time.Now(),
	)

	if err := s.taskExecutionRepo.Create(ctx, execution); err != nil {
		return nil, err
	}

	s.enqueueExecution(execution.ExecutionID)
	return execution, nil
}

func (s *ScanTaskService) CreateAutoRuns(ctx context.Context, tenantID, userID uint, token string) ([]*commonExecution.TaskExecution, error) {
	resources, err := s.engineService.GetEnginesWithStats(tenantID)
	if err != nil {
		return nil, err
	}

	runs := make([]*commonExecution.TaskExecution, 0, len(resources))
	for _, resource := range resources {
		if resource == nil {
			continue
		}
		if resource.ScannedAt != "" && resource.UnscannedCatalogNodes <= 0 {
			continue
		}
		run, err := s.CreateManualRun(ctx, tenantID, userID, token, &models.ScanRequest{
			EngineID:  resource.EngineID,
			ScanDepth: scanflow.ScanDepthDeep,
			Force:     false,
			Source:    commonExecution.ModuleMeta,
		})
		if err != nil {
			s.log.Warn("自动扫描运行创建失败，跳过该引擎",
				"engine_id", resource.EngineID,
				"engine_name", resource.ResourceName,
				"error", err,
			)
			continue
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// ExecuteScanRun 执行扫描（供 Worker 调用）
func (s *ScanTaskService) ExecuteScanRun(ctx context.Context, executionID string) error {
	return s.executeRun(ctx, executionID)
}

func (s *ScanTaskService) executeRun(ctx context.Context, executionID string) error {
	exec, err := s.taskExecutionRepo.GetByExecutionID(ctx, executionID, 0)
	if err != nil {
		return err
	}

	execConfig := scanflow.ParseExecutionConfig(exec.ExecutionConfig)
	if execConfig.EngineID == 0 {
		return fmt.Errorf("执行配置缺少 engine_id: execution_id=%s", executionID)
	}

	defer func() {
		if s.dedupService != nil {
			taskKey := s.dedupService.GenerateTaskKey(uint(exec.TenantID), execConfig.EngineID, exec.TriggerType)
			if err := s.dedupService.ClearTask(context.Background(), taskKey); err != nil {
				s.log.Warn("清除任务标记失败", "execution_id", executionID, "error", err)
			}
			if err := s.dedupService.UpdateLastScanTime(context.Background(), execConfig.EngineID); err != nil {
				s.log.Warn("更新最后扫描时间失败", "execution_id", executionID, "error", err)
			}
		}
	}()

	if exec.Status != commonExecution.ExecutionStatusPending {
		s.log.Info("跳过非待执行任务", "execution_id", executionID, "status", exec.Status)
		return nil
	}

	start := time.Now()
	if err := s.taskExecutionRepo.UpdateFields(ctx, executionID, exec.TenantID, scantask.RunningExecutionFields(start, time.Now())); err != nil {
		return err
	}

	if execConfig.ScanDepth == "" {
		execConfig.ScanDepth = scanflow.ScanDepthDeep
	}

	reporter := scantask.NewExecProgressReporter(s, executionID, exec.TenantID)
	reporter.Message("任务开始执行")

	resp, scanErr := s.scanService.ScanEngineWithOptions(scanflow.Options{
		EngineID:     execConfig.EngineID,
		TenantID:     uint(exec.TenantID),
		CatalogPaths: execConfig.CatalogPaths,
		RefGroups:    execConfig.RefGroups,
		ItemID:       execConfig.ItemID,
		Token:        execConfig.Token,
		ScanDepth:    execConfig.ScanDepth,
		Force:        execConfig.Force,
		Source:       execConfig.Source,
		Reporter:     reporter,
	})
	completeTime := time.Now()
	durationMs := completeTime.Sub(start).Milliseconds()

	if scanErr != nil {
		_ = s.taskExecutionRepo.UpdateFields(ctx, executionID, exec.TenantID, scantask.FailedExecutionFields(scanErr, completeTime, durationMs, time.Now()))

		if exec.SourceTaskID != nil {
			s.backfillTaskStatus(uint(*exec.SourceTaskID), executionID, commonExecution.ExecutionStatusFailed, completeTime, exec.TenantID)
		}
		return scanErr
	}

	_ = s.taskExecutionRepo.UpdateFields(ctx, executionID, exec.TenantID, scantask.SuccessfulExecutionFields(resp, execConfig.StorageType, completeTime, durationMs, time.Now()))

	if exec.SourceTaskID != nil {
		s.backfillTaskStatus(uint(*exec.SourceTaskID), executionID, commonExecution.ExecutionStatusSuccess, completeTime, exec.TenantID)
	}

	return nil
}

// backfillTaskStatus 回写 ScanTask 的最近执行状态字段
func (s *ScanTaskService) backfillTaskStatus(taskID uint, executionID string, status string, completedAt time.Time, tenantID int) {
	taskUpdate := scantask.TaskStatusBackfillFields(executionID, status, completedAt, time.Now())
	if err := s.db.Model(&models.ScanTask{}).Where("id = ? AND tenant_id = ?", taskID, tenantID).Updates(taskUpdate).Error; err != nil {
		s.log.Warn("更新任务执行状态失败", "task_id", taskID, "error", err)
	}
}

func (s *ScanTaskService) updateExecutionProgress(executionID string, tenantID int, fields map[string]interface{}) {
	fields["updated_at"] = time.Now()
	if err := s.taskExecutionRepo.UpdateFields(context.Background(), executionID, tenantID, fields); err != nil {
		s.log.Warn("更新执行进度失败", "execution_id", executionID, "error", err)
	}
}

func (s *ScanTaskService) UpdateExecutionProgress(executionID string, tenantID int, fields map[string]interface{}) {
	s.updateExecutionProgress(executionID, tenantID, fields)
}

// GetExecution 获取执行详情
func (s *ScanTaskService) GetExecution(ctx context.Context, executionID string, tenantID int) (*commonExecution.TaskExecution, error) {
	return s.taskExecutionRepo.GetByExecutionID(ctx, executionID, tenantID)
}

func (s *ScanTaskService) WaitExecution(ctx context.Context, executionID string, tenantID int, timeout time.Duration) (*commonExecution.TaskExecution, error) {
	if timeout <= 0 {
		timeout = foregroundExecutionWait
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var last *commonExecution.TaskExecution
	for {
		select {
		case <-waitCtx.Done():
			if last != nil && last.IsCompleted() {
				return last, nil
			}
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
				return last, fmt.Errorf("%s: execution_id=%s", foregroundExecutionWaitErrMsg, executionID)
			}
			return last, waitCtx.Err()
		default:
		}

		exec, err := s.GetExecution(waitCtx, executionID, tenantID)
		if err != nil {
			return nil, err
		}
		last = exec
		if exec.IsCompleted() {
			return exec, nil
		}

		timer := time.NewTimer(foregroundExecutionPoll)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			if last != nil && last.IsCompleted() {
				return last, nil
			}
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
				return last, fmt.Errorf("%s: execution_id=%s", foregroundExecutionWaitErrMsg, executionID)
			}
			return last, waitCtx.Err()
		case <-timer.C:
		}
	}
}

// ListExecutions 列出 meta 模块的执行记录
func (s *ScanTaskService) ListExecutions(ctx context.Context, tenantID int, taskID *int, status, triggerType string, page, pageSize int) ([]*commonExecution.TaskExecution, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	filter := commonExecution.TaskExecutionFilter{
		TenantID:     tenantID,
		Module:       commonExecution.ModuleMeta,
		Status:       status,
		TriggerType:  triggerType,
		SourceTaskID: taskID,
		Page:         page,
		PageSize:     pageSize,
	}
	return s.taskExecutionRepo.List(ctx, filter)
}

// CancelExecution 取消执行
func (s *ScanTaskService) CancelExecution(ctx context.Context, executionID string, tenantID int) error {
	exec, err := s.taskExecutionRepo.GetByExecutionID(ctx, executionID, tenantID)
	if err != nil {
		return err
	}
	if exec.IsCompleted() {
		return fmt.Errorf("执行已完成，无法取消: status=%s", exec.Status)
	}
	return s.taskExecutionRepo.UpdateFields(ctx, executionID, tenantID, map[string]interface{}{
		"status":     commonExecution.ExecutionStatusCancelled,
		"updated_at": time.Now(),
	})
}
