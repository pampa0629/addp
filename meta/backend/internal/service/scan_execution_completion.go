package service

import (
	"context"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scantask"
)

func (s *ScanExecutionService) completeExecutionWithFailure(ctx context.Context, executionID string, tenantID int, sourceTaskID *int, scanErr error, completedAt time.Time, durationMs int64) {
	_ = s.taskExecutionRepo.UpdateFields(ctx, executionID, tenantID, scantask.FailedExecutionFields(scanErr, completedAt, durationMs, time.Now()))
	if sourceTaskID != nil {
		s.backfillTaskStatus(uint(*sourceTaskID), executionID, commonExecution.ExecutionStatusFailed, completedAt, tenantID)
	}
}

func (s *ScanExecutionService) completeExecutionWithSuccess(ctx context.Context, executionID string, tenantID int, sourceTaskID *int, resp *models.ScanResponse, storageType string, completedAt time.Time, durationMs int64) {
	_ = s.taskExecutionRepo.UpdateFields(ctx, executionID, tenantID, scantask.SuccessfulExecutionFields(resp, storageType, completedAt, durationMs, time.Now()))
	if sourceTaskID != nil {
		s.backfillTaskStatus(uint(*sourceTaskID), executionID, commonExecution.ExecutionStatusSuccess, completedAt, tenantID)
	}
}
