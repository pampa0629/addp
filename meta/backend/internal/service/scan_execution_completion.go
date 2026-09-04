package service

import (
	"context"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scantask"
)

func (s *ScanExecutionService) completeExecutionWithFailure(ctx context.Context, execution *commonExecution.TaskExecution, resp *models.ScanResponse, storageType string, scanErr error, completedAt time.Time, durationMs int64) error {
	return s.completeBoundedExecution(ctx, execution, commonExecution.ExecutionStatusFailed, completedAt, scantask.FailedExecutionFields(resp, storageType, scanErr, completedAt, durationMs, time.Now()))
}

func (s *ScanExecutionService) completeExecutionWithSuccess(ctx context.Context, execution *commonExecution.TaskExecution, resp *models.ScanResponse, storageType string, completedAt time.Time, durationMs int64) error {
	return s.completeBoundedExecution(ctx, execution, commonExecution.ExecutionStatusSuccess, completedAt, scantask.SuccessfulExecutionFields(resp, storageType, completedAt, durationMs, time.Now()))
}
