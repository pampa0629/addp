package service

import (
	"context"
	"time"

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scantask"
)

func (s *ScanExecutionService) completeExecutionWithFailure(ctx context.Context, execution *commonExecution.TaskExecution, resp *models.ScanResponse, storageType string, scanErr error, completedAt time.Time, durationMs int64) error {
	return s.completeBoundedExecution(ctx, execution, commonExecution.ExecutionStatusFailed, completedAt, scantask.FailedExecutionFields(resp, storageType, scanErr, completedAt, durationMs, time.Now()))
}

func (s *ScanExecutionService) completeExecutionWithSuccess(ctx context.Context, execution *commonExecution.TaskExecution, resp *models.ScanResponse, storageType string, completedAt time.Time, durationMs int64) error {
	fields := scantask.SuccessfulExecutionFields(resp, storageType, completedAt, durationMs, time.Now())
	if execution.Source == commonClient.MetaScanSourceManagerMosaicProducedTarget {
		output, err := s.captureMosaicOutput(ctx, execution)
		if err != nil {
			return s.completeExecutionWithFailure(ctx, execution, resp, storageType, err, completedAt, durationMs)
		}
		fields["metadata"].(models.JSONMap)["mosaic_output"] = output
	}
	return s.completeBoundedExecution(ctx, execution, commonExecution.ExecutionStatusSuccess, completedAt, fields)
}
