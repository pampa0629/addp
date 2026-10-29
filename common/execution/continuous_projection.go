package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	commonModels "github.com/addp/common/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ContinuousScanProjection publishes only the Owner's existing scan facts. It
// is not an execution transition and cannot change identity or lifecycle time.
type ContinuousScanProjection struct {
	Status      string     `json:"status"`
	Attempt     uint64     `json:"attempt"`
	LeaseUntil  *time.Time `json:"lease_until,omitempty"`
	ExecutionID string     `json:"execution_id,omitempty"`
	Error       string     `json:"error,omitempty"`
}

type ContinuousSchemaChangeProjection struct {
	RequestID          uint                      `json:"request_id"`
	Status             string                    `json:"status"`
	Generation         uint64                    `json:"generation"`
	FromRevision       uint64                    `json:"from_revision"`
	ToRevision         uint64                    `json:"to_revision"`
	DetectedAt         time.Time                 `json:"detected_at"`
	Scope              string                    `json:"scope"`
	SourcePartition    string                    `json:"source_partition"`
	SourceOffset       int64                     `json:"source_offset"`
	MissingFields      []string                  `json:"missing_fields"`
	UnexpectedFields   []string                  `json:"unexpected_fields"`
	IncompatibleFields []string                  `json:"incompatible_fields"`
	MetadataScan       *ContinuousScanProjection `json:"metadata_scan,omitempty"`
	StoppedAt          *time.Time                `json:"stopped_at,omitempty"`
}

// PublishContinuousSchemaChange requires the Owner to hold its request/approval
// locks in the enclosing transaction. An approval origin is immutable except for
// the current scan claim; it never replaces the execution's initiating actor.
func PublishContinuousSchemaChange(ctx context.Context, db *gorm.DB, executionID string, tenantID int, taskID uint, projection ContinuousSchemaChangeProjection, origin *AppliedSchemaChangeScanOrigin, admit ContinuousAdmission) error {
	if projection.RequestID == 0 || projection.Generation == 0 || projection.ToRevision <= projection.FromRevision || projection.DetectedAt.IsZero() {
		return fmt.Errorf("schema change projection identity is incomplete")
	}
	switch projection.Status {
	case "pending":
		if projection.MetadataScan != nil || projection.StoppedAt != nil || origin != nil {
			return fmt.Errorf("pending schema change has approval or stop facts")
		}
	case "applied":
		if projection.MetadataScan == nil || projection.StoppedAt != nil {
			return fmt.Errorf("applied schema change has no scan projection")
		}
		if err := validateContinuousScanProjection(*projection.MetadataScan); err != nil {
			return err
		}
	case "stopped":
		if projection.StoppedAt == nil || projection.StoppedAt.IsZero() || projection.MetadataScan != nil || origin != nil {
			return fmt.Errorf("stopped schema change facts are invalid")
		}
	default:
		return fmt.Errorf("schema change projection status is invalid")
	}
	return publishContinuousProjection(ctx, db, executionID, tenantID, taskID, admit, func(metadata commonModels.JSONMap) error {
		if origin != nil {
			if origin.RequestID != projection.RequestID || origin.TaskID != taskID || origin.Generation != projection.Generation || origin.FromRevision != projection.FromRevision || origin.ToRevision != projection.ToRevision || origin.TargetLocator == "" || origin.AppliedAt.IsZero() || origin.TriggeredBy <= 0 || origin.ActorPrincipalID <= 0 || origin.ActorTenantMembershipID <= 0 || origin.IssuedAuthorizationVersion <= 0 || origin.Attempt != projection.MetadataScan.Attempt {
				return fmt.Errorf("schema change approval origin does not match projection")
			}
			if projection.MetadataScan.Status == "running" && (origin.ClaimToken == "" || origin.LeaseUntil == nil || projection.MetadataScan.LeaseUntil == nil || !origin.LeaseUntil.Equal(*projection.MetadataScan.LeaseUntil)) {
				return fmt.Errorf("schema change running claim is incomplete")
			}
			if projection.MetadataScan.Status != "running" && (origin.ClaimToken != "" || origin.LeaseUntil != nil) {
				return fmt.Errorf("schema change inactive scan retains a claim")
			}
		}
		if value, exists := metadata[SchemaChangeScanOriginKey]; exists {
			var stored AppliedSchemaChangeScanOrigin
			if err := decodeProjection(value, &stored); err != nil {
				return err
			}
			if origin == nil || !sameSchemaChangeApproval(stored, *origin) || origin.Attempt < stored.Attempt {
				return fmt.Errorf("schema change approval origin cannot be replaced or removed")
			}
		}
		continuous := continuousProjectionMap(metadata)
		continuous["schema_change"] = projection
		metadata["continuous"] = continuous
		if origin != nil {
			metadata[SchemaChangeScanOriginKey] = *origin
		}
		return nil
	})
}

// PublishContinuousMetadataScan preserves terminal execution facts while the
// Owner finishes a scan submission already claimed under its task lock.
func PublishContinuousMetadataScan(ctx context.Context, db *gorm.DB, executionID string, tenantID int, taskID uint, scan ContinuousScanProjection, targetLocator string, admit ContinuousAdmission) error {
	if err := validateContinuousScanProjection(scan); err != nil {
		return err
	}
	return publishContinuousProjection(ctx, db, executionID, tenantID, taskID, admit, func(metadata commonModels.JSONMap) error {
		continuous := continuousProjectionMap(metadata)
		continuous["metadata_scan"] = scan
		metadata["continuous"] = continuous
		if targetLocator != "" {
			metadata["outputs"] = commonModels.JSONMap{"execution_id": executionID, "target_locator": targetLocator, "continuous_prepared": true}
		}
		return nil
	})
}

func publishContinuousProjection(ctx context.Context, db *gorm.DB, executionID string, tenantID int, taskID uint, admit ContinuousAdmission, mutate func(commonModels.JSONMap) error) error {
	if db == nil || executionID == "" || tenantID <= 0 || taskID == 0 || admit == nil {
		return fmt.Errorf("continuous projection requires explicit owner admission")
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item TaskExecution
		query := tx.Session(&gorm.Session{}).Where("execution_id = ? AND tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ? AND execution_boundary = ?", executionID, tenantID, ModuleTransfer, TaskTypeSync, strconv.FormatUint(uint64(taskID), 10), ExecutionBoundaryContinuous)
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(&item).Error; err != nil {
			return err
		}
		now, err := DatabaseNow(tx)
		if err != nil {
			return err
		}
		if err := admit(cloneAdmissionExecution(item), now); err != nil {
			return err
		}
		metadata := item.Metadata.Clone()
		if metadata == nil {
			metadata = commonModels.JSONMap{}
		}
		if err := mutate(metadata); err != nil {
			return err
		}
		result := tx.Session(&gorm.Session{}).Model(&TaskExecution{}).Where("id = ? AND tenant_id = ?", item.ID, item.TenantID).Updates(map[string]interface{}{"metadata": metadata, "updated_at": now})
		if err := requireOwnedRow(result, item.ExecutionID, "continuous projection"); err != nil {
			return err
		}
		checkedAt, err := DatabaseNow(tx)
		if err != nil {
			return err
		}
		return admit(cloneAdmissionExecution(item), checkedAt)
	})
}
func continuousProjectionMap(metadata commonModels.JSONMap) map[string]interface{} {
	continuous, _ := metadata["continuous"].(map[string]interface{})
	if continuous == nil {
		continuous = map[string]interface{}{}
	}
	return continuous
}
func decodeProjection(value interface{}, target interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}
func validateContinuousScanProjection(scan ContinuousScanProjection) error {
	switch scan.Status {
	case "pending", "running", "success", "failed":
	default:
		return fmt.Errorf("continuous scan projection status is invalid")
	}
	if scan.Status == "running" && scan.Attempt == 0 {
		return fmt.Errorf("continuous scan running attempt is missing")
	}
	return nil
}
func sameSchemaChangeApproval(a, b AppliedSchemaChangeScanOrigin) bool {
	return a.RequestID == b.RequestID && a.TaskID == b.TaskID && a.Generation == b.Generation && a.FromRevision == b.FromRevision && a.ToRevision == b.ToRevision && a.TargetLocator == b.TargetLocator && a.AppliedAt.Equal(b.AppliedAt) && a.TriggeredBy == b.TriggeredBy && a.ActorPrincipalID == b.ActorPrincipalID && a.ActorTenantMembershipID == b.ActorTenantMembershipID && a.IssuedAuthorizationVersion == b.IssuedAuthorizationVersion
}
