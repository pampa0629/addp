package scantask

import (
	"context"
	"fmt"
	"time"

	commonModels "github.com/addp/common/models"
	commonRepo "github.com/addp/common/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ImmediateExecutionRecorder struct {
	repo *commonRepo.TaskExecutionRepository
}

func NewImmediateExecutionRecorder(db *gorm.DB) *ImmediateExecutionRecorder {
	return &ImmediateExecutionRecorder{
		repo: commonRepo.NewTaskExecutionRepository(db),
	}
}

func (r *ImmediateExecutionRecorder) Create(resource *commonModels.Engine, tenantID uint, namespaces, objectPaths []string, startTime time.Time) (string, error) {
	if resource == nil {
		return "", fmt.Errorf("resource is required to create immediate execution")
	}

	execID := uuid.New().String()
	engineIDInt := int(resource.ID)
	exec := &commonModels.TaskExecution{
		TenantID:    int(tenantID),
		ExecutionID: execID,
		Module:      commonModels.ModuleMeta,
		TaskType:    "scan",
		Status:      commonModels.ExecutionStatusRunning,
		TriggerType: commonModels.TriggerTypeAPI,
		ExecutionConfig: commonModels.JSONMap{
			"engine_id":    engineIDInt,
			"namespaces":   namespaces,
			"object_paths": objectPaths,
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
		"status":            commonModels.ExecutionStatusFailed,
		"error_details":     commonModels.JSONMap{"message": scanErr.Error()},
		"execution_time_ms": durationMs,
		"completed_at":      completedAt,
		"updated_at":        time.Now(),
	})
}

func (r *ImmediateExecutionRecorder) Complete(execID string, tenantID int, meta commonModels.JSONMap, startTime, completedAt time.Time) {
	durationMs := completedAt.Sub(startTime).Milliseconds()
	_ = r.repo.UpdateFields(context.Background(), execID, tenantID, map[string]interface{}{
		"status":            commonModels.ExecutionStatusSuccess,
		"metadata":          meta,
		"execution_time_ms": durationMs,
		"progress":          100,
		"completed_at":      completedAt,
		"updated_at":        time.Now(),
	})
}
