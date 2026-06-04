package scantask

import (
	"context"
	"fmt"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
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

func (r *ImmediateExecutionRecorder) Create(
	resource *commonModels.Engine,
	tenantID uint,
	catalogPaths []string,
	refGroups []models.ScanRefGroup,
	scanDepth string,
	force bool,
	source string,
	startTime time.Time,
) (string, error) {
	if resource == nil {
		return "", fmt.Errorf("resource is required to create immediate execution")
	}

	execID := uuid.New().String()
	exec := &commonExecution.TaskExecution{
		TenantID:        int(tenantID),
		ExecutionID:     execID,
		Module:          commonExecution.ModuleMeta,
		TaskType:        "scan",
		Status:          commonExecution.ExecutionStatusRunning,
		TriggerType:     commonExecution.TriggerTypeAPI,
		ExecutionConfig: ImmediateExecutionConfig(resource, catalogPaths, refGroups, scanDepth, force, source),
		StartedAt:       &startTime,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	if err := r.repo.Create(context.Background(), exec); err != nil {
		return "", fmt.Errorf("failed to create immediate execution record: %w", err)
	}
	return execID, nil
}

func ImmediateExecutionConfig(resource *commonModels.Engine, catalogPaths []string, refGroups []models.ScanRefGroup, scanDepth string, force bool, source string) commonModels.JSONMap {
	config := commonModels.JSONMap{
		"catalog_paths": catalogPaths,
		"ref_groups":    refGroups,
		"scan_depth":    scanDepth,
		"force":         force,
		"source":        source,
	}
	if resource != nil {
		config["engine_id"] = resource.ID
		config["storage_type"] = NormalizeStorageType(resource.EngineType)
	}
	return config
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
