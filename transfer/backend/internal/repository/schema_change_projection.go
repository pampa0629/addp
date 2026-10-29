package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	schemaChangeProjectionPending = "pending"
	schemaChangeProjectionApplied = "applied"
	schemaChangeProjectionStopped = "stopped"
)

func UpdateSchemaChangeExecutionProjectionTx(tx *gorm.DB, request *models.SchemaChangeRequest) error {
	if request == nil {
		return nil
	}
	status := schemaChangeProjectionPending
	if request.Status == models.SchemaChangeRequestApplied {
		status = schemaChangeProjectionApplied
	}
	return updateSchemaChangeExecutionProjectionTx(tx, request, status, request.UpdatedAt)
}

func StopPendingSchemaChangeExecutionProjectionTx(tx *gorm.DB, taskID, tenantID uint, now time.Time) error {
	request, err := pendingSchemaChangeRequestTx(tx, taskID, tenantID)
	if err != nil {
		return err
	}
	if request == nil {
		return nil
	}
	return updateSchemaChangeExecutionProjectionTx(tx, request, schemaChangeProjectionStopped, now)
}

func updateSchemaChangeExecutionProjectionTx(tx *gorm.DB, request *models.SchemaChangeRequest, status string, now time.Time) error {
	var execution commonExecution.TaskExecution
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("execution_id = ? AND tenant_id = ?", request.ExecutionID, request.TenantID).
		First(&execution).Error; err != nil {
		return err
	}
	metadata := schemaChangeProjectionMetadata(execution.Metadata, request, status, now)
	if value, exists := metadata[commonExecution.SchemaChangeScanOriginKey]; exists {
		var origin commonExecution.AppliedSchemaChangeScanOrigin
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(data, &origin); err != nil {
			return err
		}
		if origin.RequestID != request.ID {
			return ErrSchemaChangeRequestConflict
		}
		origin.ClaimToken = request.MetadataScanClaimToken
		origin.Attempt = request.MetadataScanAttempt
		origin.LeaseUntil = request.MetadataScanLeaseUntil
		metadata[commonExecution.SchemaChangeScanOriginKey] = origin
	}
	return publishSchemaChangeProjection(tx, request, metadata, nil)
}

func pendingSchemaChangeRequestTx(tx *gorm.DB, taskID, tenantID uint) (*models.SchemaChangeRequest, error) {
	var request models.SchemaChangeRequest
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("task_id = ? AND tenant_id = ? AND status = ?", taskID, tenantID, models.SchemaChangeRequestPending).
		Order("detected_at DESC, id DESC").First(&request).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &request, nil
}

func schemaChangeProjectionMetadata(metadata commonModels.JSONMap, request *models.SchemaChangeRequest, status string, now time.Time) commonModels.JSONMap {
	if metadata == nil {
		metadata = commonModels.JSONMap{}
	}
	continuous, _ := metadata["continuous"].(map[string]interface{})
	if continuous == nil {
		continuous = map[string]interface{}{}
	}
	projection := map[string]interface{}{
		"request_id": request.ID, "status": status,
		"generation": request.Generation, "from_revision": request.FromRevision, "to_revision": request.ToRevision,
		"detected_at": request.DetectedAt, "scope": request.Scope,
		"source_partition": request.SourcePartition, "source_offset": request.SourceOffset,
		"missing_fields": request.Diff["missing_fields"], "unexpected_fields": request.Diff["unexpected_fields"],
		"incompatible_fields": request.Diff["incompatible_fields"],
	}
	if status == schemaChangeProjectionApplied {
		metadataScan := map[string]interface{}{
			"status": request.MetadataScanStatus, "attempt": request.MetadataScanAttempt,
		}
		if request.MetadataScanLeaseUntil != nil {
			metadataScan["lease_until"] = *request.MetadataScanLeaseUntil
		}
		if request.MetadataScanExecutionID != "" {
			metadataScan["execution_id"] = request.MetadataScanExecutionID
		}
		projection["metadata_scan"] = metadataScan
	}
	if status == schemaChangeProjectionStopped {
		projection["stopped_at"] = now
	}
	continuous["schema_change"] = projection
	metadata["continuous"] = continuous
	return metadata
}

// PublishAppliedSchemaChangeScanOriginTx is called only after target DDL and the
// applied request/projection have been saved in this approval transaction.
func PublishAppliedSchemaChangeScanOriginTx(tx *gorm.DB, request *models.SchemaChangeRequest, origin commonExecution.AppliedSchemaChangeScanOrigin) error {
	if request == nil || request.Status != models.SchemaChangeRequestApplied {
		return ErrSchemaChangeRequestConflict
	}
	metadata := schemaChangeProjectionMetadata(nil, request, schemaChangeProjectionApplied, request.UpdatedAt)
	metadata[commonExecution.SchemaChangeScanOriginKey] = origin
	return publishSchemaChangeProjection(tx, request, metadata, &origin)
}

func publishSchemaChangeProjection(tx *gorm.DB, request *models.SchemaChangeRequest, metadata commonModels.JSONMap, origin *commonExecution.AppliedSchemaChangeScanOrigin) error {
	continuous, _ := metadata["continuous"].(map[string]interface{})
	var projection commonExecution.ContinuousSchemaChangeProjection
	data, err := json.Marshal(continuous["schema_change"])
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &projection); err != nil {
		return err
	}
	if value, ok := metadata[commonExecution.SchemaChangeScanOriginKey]; ok && origin == nil {
		var stored commonExecution.AppliedSchemaChangeScanOrigin
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &stored); err != nil {
			return err
		}
		origin = &stored
	}
	return commonExecution.PublishContinuousSchemaChange(tx.Statement.Context, tx, request.ExecutionID, int(request.TenantID), request.TaskID, projection, origin, func(item commonExecution.TaskExecution, _ time.Time) error {
		if request.ID == 0 || item.ExecutionID != request.ExecutionID || item.TenantID != int(request.TenantID) || item.SourceTaskID == nil || *item.SourceTaskID != fmt.Sprint(request.TaskID) || projection.RequestID != request.ID {
			return ErrSchemaChangeRequestConflict
		}
		if projection.Status == schemaChangeProjectionApplied && request.Status != models.SchemaChangeRequestApplied {
			return ErrSchemaChangeRequestConflict
		}
		if origin != nil && (request.AppliedAt == nil || !origin.AppliedAt.Equal(*request.AppliedAt) || origin.ClaimToken != request.MetadataScanClaimToken || origin.Attempt != request.MetadataScanAttempt) {
			return ErrSchemaChangeRequestConflict
		}
		return nil
	})
}
