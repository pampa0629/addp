package execution

import (
	"context"
	"fmt"
	commonapi "github.com/addp/common/api"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// ContinuousAdmission checks owner-private runtime facts locked in the same
// outer transaction. It must be pure and may be rechecked before commit.
// Common never infers this admission from a missing bounded lease.
type ContinuousAdmission func(TaskExecution, time.Time) error

func StartContinuous(ctx context.Context, db *gorm.DB, executionID string, fields map[string]interface{}, admit ContinuousAdmission) (*TaskExecution, error) {
	return mutateContinuous(ctx, db, executionID, ExecutionStatusPending, ExecutionStatusRunning, fields, admit)
}

func UpdateContinuous(ctx context.Context, db *gorm.DB, executionID string, fields map[string]interface{}, admit ContinuousAdmission) error {
	_, err := mutateContinuous(ctx, db, executionID, ExecutionStatusRunning, "", fields, admit)
	return err
}

func CompleteContinuous(ctx context.Context, db *gorm.DB, executionID, expectedStatus, status string, fields map[string]interface{}, admit ContinuousAdmission) error {
	if expectedStatus != ExecutionStatusPending && expectedStatus != ExecutionStatusRunning {
		return fmt.Errorf("continuous terminal source state is invalid")
	}
	switch status {
	case ExecutionStatusSuccess, ExecutionStatusFailed, ExecutionStatusCancelled, ExecutionStatusTimeout:
	default:
		return fmt.Errorf("continuous terminal status is invalid")
	}
	if expectedStatus == ExecutionStatusPending && status != ExecutionStatusFailed && status != ExecutionStatusCancelled {
		return fmt.Errorf("pending continuous execution cannot report a runtime result")
	}
	_, err := mutateContinuous(ctx, db, executionID, expectedStatus, status, fields, admit)
	return err
}

func mutateContinuous(ctx context.Context, db *gorm.DB, executionID, expectedStatus, nextStatus string, input map[string]interface{}, admit ContinuousAdmission) (*TaskExecution, error) {
	if db == nil || executionID == "" || admit == nil {
		return nil, fmt.Errorf("continuous mutation requires explicit owner admission")
	}
	fields, err := copyExecutionFields(input)
	if err != nil {
		return nil, err
	}
	var item TaskExecution
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Session(&gorm.Session{}).Where("execution_id = ? AND execution_boundary = ? AND status = ?", executionID, ExecutionBoundaryContinuous, expectedStatus)
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(&item).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return fmt.Errorf("%w: continuous execution is no longer current", commonapi.ErrConflict)
			}
			return err
		}
		if item.TenantID <= 0 || item.Module == "" || item.TaskType == "" || item.SourceTaskID == nil || *item.SourceTaskID == "" || item.LeaseOwner != nil || item.LeaseToken != nil || item.LeaseExpiresAt != nil {
			return fmt.Errorf("continuous identity cannot contain bounded ownership")
		}
		if expectedStatus == ExecutionStatusPending && (item.Attempt != 0 || item.StartedAt != nil || item.CompletedAt != nil || item.ExecutionTimeMs != nil) {
			return fmt.Errorf("continuous pending identity has runtime facts")
		}
		if expectedStatus == ExecutionStatusRunning && (item.Attempt <= 0 || item.StartedAt == nil || item.CompletedAt != nil) {
			return fmt.Errorf("continuous running identity has invalid attempt facts")
		}
		now, err := DatabaseNow(tx)
		if err != nil {
			return err
		}
		if err := admit(cloneAdmissionExecution(item), now); err != nil {
			return err
		}
		if nextStatus == ExecutionStatusRunning {
			if err := preservePrivateMetadata(fields, item.Metadata); err != nil {
				return err
			}
			fields["status"], fields["attempt"], fields["started_at"], fields["updated_at"] = nextStatus, 1, now, now
		} else if nextStatus != "" {
			if err := applyTerminalFields(item, nextStatus, fields, now); err != nil {
				return err
			}
		} else {
			if err := preservePrivateMetadata(fields, item.Metadata); err != nil {
				return err
			}
			fields["updated_at"] = now
		}
		result := tx.Session(&gorm.Session{}).Model(&TaskExecution{}).Where("execution_id = ? AND tenant_id = ? AND execution_boundary = ? AND status = ? AND attempt = ?", item.ExecutionID, item.TenantID, ExecutionBoundaryContinuous, expectedStatus, item.Attempt).Updates(fields)
		if err := requireOwnedRow(result, item.ExecutionID, "continuous mutation"); err != nil {
			return err
		}
		checkedAt, err := DatabaseNow(tx)
		if err != nil {
			return err
		}
		if err := admit(cloneAdmissionExecution(item), checkedAt); err != nil {
			return err
		}
		return tx.Session(&gorm.Session{}).Where("execution_id = ? AND tenant_id = ?", item.ExecutionID, item.TenantID).First(&item).Error
	})
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// Admission receives detached facts; callbacks cannot rewrite the locked values
// used by Common for timing, identity or reserved metadata preservation.
func cloneAdmissionExecution(item TaskExecution) TaskExecution {
	item.SourceTaskID = cloneAdmissionPointer(item.SourceTaskID)
	item.SourceTaskName = cloneAdmissionPointer(item.SourceTaskName)
	item.ParentExecutionID = cloneAdmissionPointer(item.ParentExecutionID)
	item.CurrentStep = cloneAdmissionPointer(item.CurrentStep)
	item.RetryOfExecutionID = cloneAdmissionPointer(item.RetryOfExecutionID)
	item.LeaseOwner = cloneAdmissionPointer(item.LeaseOwner)
	item.LeaseToken = cloneAdmissionPointer(item.LeaseToken)
	item.LeaseExpiresAt = cloneAdmissionPointer(item.LeaseExpiresAt)
	item.TriggeredBy = cloneAdmissionPointer(item.TriggeredBy)
	item.ActorPrincipalID = cloneAdmissionPointer(item.ActorPrincipalID)
	item.ActorTenantMembershipID = cloneAdmissionPointer(item.ActorTenantMembershipID)
	item.IssuedAuthorizationVersion = cloneAdmissionPointer(item.IssuedAuthorizationVersion)
	item.ExecutionAuthorizationID = cloneAdmissionPointer(item.ExecutionAuthorizationID)
	item.AuthorizationExpiresAt = cloneAdmissionPointer(item.AuthorizationExpiresAt)
	item.ExecutionTimeMs = cloneAdmissionPointer(item.ExecutionTimeMs)
	item.RowsAffected = cloneAdmissionPointer(item.RowsAffected)
	item.RecordsRead = cloneAdmissionPointer(item.RecordsRead)
	item.RecordsWritten = cloneAdmissionPointer(item.RecordsWritten)
	item.BytesRead = cloneAdmissionPointer(item.BytesRead)
	item.BytesWritten = cloneAdmissionPointer(item.BytesWritten)
	item.StartedAt = cloneAdmissionPointer(item.StartedAt)
	item.CompletedAt = cloneAdmissionPointer(item.CompletedAt)
	item.ExecutionConfig = item.ExecutionConfig.Clone()
	item.ErrorDetails = item.ErrorDetails.Clone()
	item.Metadata = item.Metadata.Clone()
	return item
}

func cloneAdmissionPointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
