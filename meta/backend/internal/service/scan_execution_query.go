package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	commonExecution "github.com/addp/common/execution"
)

const (
	foregroundExecutionWait       = 55 * time.Second
	foregroundExecutionPoll       = 300 * time.Millisecond
	foregroundExecutionWaitErrMsg = "execution wait timed out"
)

// GetExecution 获取执行详情
func (s *ScanExecutionService) GetExecution(ctx context.Context, executionID string, tenantID int) (*commonExecution.TaskExecution, error) {
	return s.taskExecutionRepo.GetByExecutionID(ctx, executionID, tenantID)
}

func (s *ScanExecutionService) WaitExecution(ctx context.Context, executionID string, tenantID int, timeout time.Duration) (*commonExecution.TaskExecution, error) {
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
func (s *ScanExecutionService) ListExecutions(ctx context.Context, tenantID int, taskID *int, status, triggerType string, page, pageSize int) ([]*commonExecution.TaskExecution, int64, error) {
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
func (s *ScanExecutionService) CancelExecution(ctx context.Context, executionID string, tenantID int) error {
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
