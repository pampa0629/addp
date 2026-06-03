package scantask

import (
	"context"
	"fmt"
	commonExecution "github.com/addp/common/execution"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ImmediateExecutionRecorder struct {
	repo *commonExecution.TaskExecutionRepository
}

func NewImmediateExecutionRecorder(db *gorm.DB) *ImmediateExecutionRecorder {
	return &ImmediateExecutionRecorder{
		repo: commonExecution.NewTaskExecutionRepository(db),
	}
}

func (r *ImmediateExecutionRecorder) Create(resource *commonModels.Engine, tenantID uint, catalogPaths []string, scanDepth string, force bool, startTime time.Time) (string, error) {
	if resource == nil {
		return "", fmt.Errorf("resource is required to create immediate execution")
	}

	execID := uuid.New().String()
	engineIDInt := int(resource.ID)
	exec := &commonExecution.TaskExecution{
		TenantID:    int(tenantID),
		ExecutionID: execID,
		Module:      commonExecution.ModuleMeta,
		TaskType:    "scan",
		Status:      commonExecution.ExecutionStatusRunning,
		TriggerType: commonExecution.TriggerTypeAPI,
		ExecutionConfig: commonModels.JSONMap{
			"engine_id":     engineIDInt,
			"catalog_paths": catalogPaths,
			"scan_depth":    scanDepth,
			"force":         force,
		},
		StartedAt: &startTime,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := r.repo.Create(context.Background(), exec); err != nil {
		return "", fmt.Errorf("failed to create immediate execution record: %w", err)
	}
	return execID, nil
}

func (r *ImmediateExecutionRecorder) Fail(execID string, tenantID int, scanErr error, startTime time.Time) {
	completedAt := time.Now()
	durationMs := completedAt.Sub(startTime).Milliseconds()
	_ = r.repo.UpdateFields(context.Background(), execID, tenantID, map[string]interface{}{
		"status":            commonExecution.ExecutionStatusFailed,
		"error_details":     commonModels.JSONMap{"message": scanErr.Error()},
		"execution_time_ms": durationMs,
		"completed_at":      completedAt,
		"updated_at":        time.Now(),
	})
}

func (r *ImmediateExecutionRecorder) Complete(execID string, tenantID int, meta commonModels.JSONMap, startTime, completedAt time.Time) {
	durationMs := completedAt.Sub(startTime).Milliseconds()
	_ = r.repo.UpdateFields(context.Background(), execID, tenantID, map[string]interface{}{
		"status":            commonExecution.ExecutionStatusSuccess,
		"metadata":          meta,
		"execution_time_ms": durationMs,
		"progress":          100,
		"completed_at":      completedAt,
		"updated_at":        time.Now(),
	})
}
