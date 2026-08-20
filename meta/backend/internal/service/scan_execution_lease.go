package service

import (
	"context"
	"fmt"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scantask"
	"gorm.io/gorm"
)

func (s *ScanExecutionService) ClaimNextBoundedExecution(ctx context.Context, workerID string, now time.Time, leaseDuration time.Duration) (*commonExecution.TaskExecution, *commonExecution.Lease, error) {
	var execution *commonExecution.TaskExecution
	var lease *commonExecution.Lease
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		execution, lease, err = commonExecution.ClaimNext(ctx, tx, commonExecution.ClaimOptions{
			Module: commonExecution.ModuleMeta, TaskType: commonExecution.TaskTypeScan,
			WorkerID: workerID, Now: now, LeaseDuration: leaseDuration,
		})
		if err != nil || execution == nil || execution.SourceTaskID == nil {
			return err
		}
		taskID, err := commonExecution.ParseSourceTaskIDUint(execution.SourceTaskID)
		if err != nil {
			return err
		}
		result := tx.Model(&models.ScanTask{}).
			Where("id = ? AND tenant_id = ? AND last_execution_id = ? AND last_execution_status = ?", taskID, execution.TenantID, execution.ExecutionID, commonExecution.ExecutionStatusPending).
			Updates(map[string]interface{}{"last_run_at": now, "last_execution_status": commonExecution.ExecutionStatusRunning, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("meta task %d summary no longer matches execution %s", taskID, execution.ExecutionID)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return execution, lease, nil
}

func (s *ScanExecutionService) FailExpiredBoundedExecutions(ctx context.Context, now time.Time, limit int) (int, error) {
	type expiredLock struct {
		executionID string
		tenantID    int
		config      scanflow.ExecutionConfig
	}
	locks := make([]expiredLock, 0)
	count := 0
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		items, err := commonExecution.FindExpiredForUpdate(ctx, tx, commonExecution.ExpiredOptions{
			Module: commonExecution.ModuleMeta, TaskType: commonExecution.TaskTypeScan, Now: now, Limit: limit,
		})
		if err != nil {
			return err
		}
		for i := range items {
			item := items[i]
			lease, err := commonExecution.LeaseFromExecution(item)
			if err != nil {
				return err
			}
			fields := map[string]interface{}{
				"current_step": "执行租约已过期，需要显式重试",
				"error_details": commonModels.JSONMap{
					"code":    "meta.execution.lease_expired_recovery_required",
					"message": "meta scan execution lease expired; inspect partial metadata before retrying",
				},
			}
			if item.StartedAt != nil {
				fields["execution_time_ms"] = now.Sub(*item.StartedAt).Milliseconds()
			}
			if err := commonExecution.FailExpired(ctx, tx, lease, now, fields); err != nil {
				return err
			}
			if err := updateScanTaskSummary(tx, &item, commonExecution.ExecutionStatusFailed, now); err != nil {
				return err
			}
			locks = append(locks, expiredLock{executionID: item.ExecutionID, tenantID: item.TenantID, config: scanflow.ParseExecutionConfig(item.ExecutionConfig)})
			count++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	for _, item := range locks {
		if s.dedupService == nil || item.config.EngineID == 0 {
			continue
		}
		lockKey := s.dedupService.GenerateExecutionLockKey(uint(item.tenantID), item.config.EngineID, item.config.ItemID, item.config.CatalogPaths, item.config.RefGroups)
		s.releaseExecutionLock(ctx, true, lockKey, item.executionID, "释放过期扫描执行范围锁失败", "execution_id", item.executionID)
	}
	return count, nil
}

func (s *ScanExecutionService) completeBoundedExecution(ctx context.Context, execution *commonExecution.TaskExecution, status string, completedAt time.Time, fields map[string]interface{}) error {
	if execution == nil {
		return fmt.Errorf("scan execution is required")
	}
	lease, ok := s.boundedLease(ctx, execution.ExecutionID)
	if !ok {
		return fmt.Errorf("scan execution %s has no active bounded lease", execution.ExecutionID)
	}
	delete(fields, "status")
	delete(fields, "completed_at")
	delete(fields, "updated_at")
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := commonExecution.CompleteWithLease(ctx, tx, lease, status, completedAt, fields); err != nil {
			return err
		}
		return updateScanTaskSummary(tx, execution, status, completedAt)
	})
}

func updateScanTaskSummary(tx *gorm.DB, execution *commonExecution.TaskExecution, status string, completedAt time.Time) error {
	if execution == nil || execution.SourceTaskID == nil {
		return nil
	}
	taskID, err := commonExecution.ParseSourceTaskIDUint(execution.SourceTaskID)
	if err != nil {
		return err
	}
	result := tx.Model(&models.ScanTask{}).
		Where("id = ? AND tenant_id = ? AND last_execution_id = ? AND last_execution_status = ?", taskID, execution.TenantID, execution.ExecutionID, commonExecution.ExecutionStatusRunning).
		Updates(scantask.TaskStatusBackfillFields(execution.ExecutionID, status, completedAt, completedAt))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("meta task %d summary no longer matches execution %s", taskID, execution.ExecutionID)
	}
	return nil
}
