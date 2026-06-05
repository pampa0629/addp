package scantask

import (
	"fmt"
	"strings"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	metaModels "github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/google/uuid"
)

func NewManualExecution(tenantID, userID uint, engineID uint, itemID uint, storageType string, catalogPaths []string, refGroups []metaModels.ScanRefGroup, scanDepth string, force bool, source string, token string, now time.Time) *commonExecution.TaskExecution {
	userIDInt := int(userID)
	source = strings.TrimSpace(source)
	if source == "" {
		source = commonExecution.ModuleMeta
	}
	return &commonExecution.TaskExecution{
		TenantID:    int(tenantID),
		ExecutionID: uuid.New().String(),
		Module:      commonExecution.ModuleMeta,
		TaskType:    "scan",
		Source:      source,
		Status:      commonExecution.ExecutionStatusPending,
		TriggerType: commonExecution.TriggerTypeManual,
		TriggeredBy: &userIDInt,
		ExecutionConfig: scanflow.ManualExecutionConfig(
			engineID,
			itemID,
			storageType,
			catalogPaths,
			refGroups,
			scanDepth,
			force,
			source,
			token,
		),
		StartedAt: &now,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func NewTaskManualExecution(task *metaModels.ScanTask, userID uint, storageType string, now time.Time) *commonExecution.TaskExecution {
	userIDInt := int(userID)
	taskIDInt := int(task.ID)
	return &commonExecution.TaskExecution{
		TenantID:        int(task.TenantID),
		ExecutionID:     uuid.New().String(),
		Module:          commonExecution.ModuleMeta,
		TaskType:        "scan",
		Source:          commonExecution.ModuleMeta,
		SourceTaskID:    &taskIDInt,
		SourceTaskName:  &task.Name,
		Status:          commonExecution.ExecutionStatusPending,
		TriggerType:     commonExecution.TriggerTypeManual,
		TriggeredBy:     &userIDInt,
		ExecutionConfig: scanflow.TaskExecutionConfig(task.EngineID, storageType, task.Scope, task.Parameters, scanflow.ScanDepthDeep, "meta"),
		StartedAt:       &now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func NewScheduledExecution(task *metaModels.ScanTask, storageType string, targets scanflow.TargetSet, plannedRunAt time.Time, now time.Time) *commonExecution.TaskExecution {
	taskIDInt := int(task.ID)
	return &commonExecution.TaskExecution{
		TenantID:        int(task.TenantID),
		ExecutionID:     uuid.New().String(),
		Module:          commonExecution.ModuleMeta,
		TaskType:        "scan",
		Source:          commonExecution.ModuleMeta,
		SourceTaskID:    &taskIDInt,
		SourceTaskName:  &task.Name,
		Status:          commonExecution.ExecutionStatusPending,
		TriggerType:     metaModels.TriggerTypeScheduled,
		ExecutionConfig: scanflow.TargetExecutionConfig(task.EngineID, storageType, targets.CatalogPaths, targets.RefGroups, JSONMapString(task.Parameters, "scan_depth", scanflow.ScanDepthDeep), JSONMapBool(task.Parameters, "force", false), "meta", plannedRunAt.Format(time.RFC3339Nano)),
		StartedAt:       &now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func RunningExecutionFields(startedAt time.Time, now time.Time) map[string]interface{} {
	return map[string]interface{}{
		"status":       commonExecution.ExecutionStatusRunning,
		"started_at":   startedAt,
		"current_step": "任务开始执行",
		"updated_at":   now,
	}
}

func FailedExecutionFields(scanErr error, completedAt time.Time, durationMs int64, now time.Time) map[string]interface{} {
	return map[string]interface{}{
		"status":            commonExecution.ExecutionStatusFailed,
		"completed_at":      completedAt,
		"execution_time_ms": durationMs,
		"current_step":      fmt.Sprintf("执行失败: %v", scanErr),
		"error_details": commonModels.JSONMap{
			"message": scanErr.Error(),
		},
		"updated_at": now,
	}
}

func SuccessfulExecutionFields(resp *metaModels.ScanResponse, storageType string, completedAt time.Time, durationMs int64, now time.Time) map[string]interface{} {
	metadata := commonModels.JSONMap{
		"storage_type": storageType,
	}
	if resp != nil {
		metadata["catalog_nodes_scanned"] = resp.CatalogNodesScanned
		metadata["items_scanned"] = resp.ItemsScanned
		metadata["fields_scanned"] = resp.FieldsScanned
		if resp.Extraction != nil {
			metadata["extraction"] = commonModels.JSONMap{
				"documents":    resp.Extraction.Documents,
				"extracted":    resp.Extraction.Extracted,
				"unsupported":  resp.Extraction.Unsupported,
				"failed":       resp.Extraction.Failed,
				"indexed":      resp.Extraction.Indexed,
				"index_failed": resp.Extraction.IndexFailed,
			}
		}
	}
	return map[string]interface{}{
		"status":            commonExecution.ExecutionStatusSuccess,
		"completed_at":      completedAt,
		"execution_time_ms": durationMs,
		"progress":          100,
		"current_step":      "执行完成",
		"metadata":          metadata,
		"updated_at":        now,
	}
}

func TaskStatusBackfillFields(executionID string, status string, completedAt time.Time, now time.Time) map[string]interface{} {
	return map[string]interface{}{
		"last_run_at":           completedAt,
		"last_execution_id":     executionID,
		"last_execution_status": status,
		"updated_at":            now,
	}
}

func ScheduledTaskTriggerFields(lastRunAt time.Time, nextRunAt *time.Time, now time.Time) map[string]interface{} {
	fields := map[string]interface{}{
		"last_run_at": lastRunAt,
		"updated_at":  now,
	}
	if nextRunAt != nil {
		fields["next_run_at"] = *nextRunAt
	}
	return fields
}
