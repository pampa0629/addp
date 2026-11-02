package repository

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strconv"
	"time"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type fileSourceAuthorizationRepository struct {
	db       *gorm.DB
	taskType string
}

// FileTaskSnapshot is an owner configuration snapshot, never a source
// authorization. Config is read directly from SQL to preserve numeric literals.
type FileTaskSnapshot struct {
	TaskID   uint
	TenantID uint
	Version  uint
	Config   json.RawMessage
}

// BindSourceAuthorization publishes a validated System reference atomically
// under the pending row lock. The original human provenance is never rewritten.
func (r fileSourceAuthorizationRepository) BindSourceAuthorization(ctx context.Context, expected *commonExecution.TaskExecution, scope commonExecution.ManagerFileReadScope, issued *commonClient.IssuedManagerFileAuthorization) error {
	if scope.Purpose() != r.taskType || expected == nil || expected.TenantID <= 0 || expected.SourceTaskID == nil || *expected.SourceTaskID != scope.OwnerTaskID || issued == nil || issued.ExecutionID != expected.ExecutionID || !issued.Matches(uint(expected.TenantID), scope) ||
		commonExecution.CopyActorOrigin(&commonExecution.TaskExecution{TenantID: expected.TenantID}, expected) != nil {
		return commonAPI.ErrConflict
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current commonExecution.TaskExecution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND execution_id = ? AND module = ? AND task_type = ?", expected.TenantID, expected.ExecutionID, commonExecution.ModuleManager, r.taskType).First(&current).Error; err != nil {
			return err
		}
		if current.Source != commonExecution.ModuleManager || current.TriggerType != commonExecution.TriggerTypeManual || current.ParentExecutionID != nil || current.ExecutionBoundary != commonExecution.ExecutionBoundaryBounded || current.SourceTaskID == nil || *current.SourceTaskID != scope.OwnerTaskID ||
			current.Status != commonExecution.ExecutionStatusPending || current.Attempt != 0 || current.ExecutionAuthorizationID != nil || current.AuthorizationExpiresAt != nil || current.LeaseToken != nil || current.LeaseOwner != nil || current.LeaseExpiresAt != nil || current.TriggeredBy == nil || *current.TriggeredBy != *expected.TriggeredBy ||
			!samePositiveProfileFact(current.ActorPrincipalID, expected.ActorPrincipalID) || !samePositiveProfileFact(current.ActorTenantMembershipID, expected.ActorTenantMembershipID) || !samePositiveProfileFact(current.IssuedAuthorizationVersion, expected.IssuedAuthorizationVersion) {
			return commonAPI.ErrConflict
		}
		raw, err := rawFileExecutionConfig(ctx, tx, current.TenantID, current.ExecutionID, r.taskType)
		if err != nil {
			return err
		}
		actual, err := commonExecution.NewManagerFileReadScope(raw, scope.OwnerTaskID, scope.OwnerTaskVersion, scope.EngineVersion, &scope.FileComposition)
		if err != nil || !reflect.DeepEqual(actual, &scope) || !issued.Matches(uint(current.TenantID), *actual) {
			return commonAPI.ErrConflict
		}
		id, err := strconv.ParseInt(issued.ID, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != issued.ID {
			return commonAPI.ErrConflict
		}
		return commonExecution.AttachPendingAuthorization(ctx, tx, commonExecution.PendingIdentity{ExecutionID: current.ExecutionID, TenantID: current.TenantID, Module: current.Module, TaskType: current.TaskType, Source: current.Source}, commonExecution.PendingAuthorization{ID: id, ExpiresAt: issued.ExpiresAt, ActorPrincipalID: *current.ActorPrincipalID, ActorTenantMembershipID: *current.ActorTenantMembershipID, IssuedAuthorizationVersion: *current.IssuedAuthorizationVersion})
	})
}

func (r fileSourceAuthorizationRepository) TaskConfigSnapshot(ctx context.Context, taskID, tenantID uint) (*FileTaskSnapshot, error) {
	return fileTaskConfigSnapshot(ctx, r.db, taskID, tenantID, r.taskType)
}

func fileTaskConfigSnapshot(ctx context.Context, db *gorm.DB, taskID, tenantID uint, taskType string) (*FileTaskSnapshot, error) {
	if taskID == 0 || tenantID == 0 {
		return nil, commonAPI.ErrNotFound
	}
	item := &FileTaskSnapshot{TaskID: taskID, TenantID: tenantID}
	var raw []byte
	err := db.WithContext(ctx).Model(&models.TaskDefinition{}).Select("version, config").
		Where("id = ? AND tenant_id = ? AND task_type = ?", taskID, tenantID, taskType).
		Row().Scan(&item.Version, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, commonAPI.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if item.Version == 0 {
		return nil, commonAPI.ErrConflict
	}
	if _, err := canonicalFileTaskConfig(raw); err != nil {
		return nil, err
	}
	item.Config = append(json.RawMessage(nil), raw...)
	return item, nil
}

// The caller must already hold the task row lock in the pending-creation
// transaction. Rechecking both fields rejects raw config edits without a bump.
func verifyFileTaskSnapshot(ctx context.Context, tx *gorm.DB, expected *FileTaskSnapshot, taskType string) error {
	if expected == nil || tx == nil {
		return commonAPI.ErrConflict
	}
	current, err := fileTaskConfigSnapshot(ctx, tx, expected.TaskID, expected.TenantID, taskType)
	if err != nil {
		return err
	}
	if current.Version != expected.Version {
		return commonAPI.ErrConflict
	}
	left, err := canonicalFileTaskConfig(expected.Config)
	if err != nil {
		return err
	}
	right, err := canonicalFileTaskConfig(current.Config)
	if err != nil {
		return err
	}
	if !bytes.Equal(left, right) {
		return commonAPI.ErrConflict
	}
	return nil
}

func canonicalFileTaskConfig(raw json.RawMessage) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var config map[string]any
	if decoder.Decode(&config) != nil || config == nil {
		return nil, commonAPI.ErrConflict
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, commonAPI.ErrConflict
	}
	canonical, err := json.Marshal(config)
	if err != nil {
		return nil, commonAPI.ErrConflict
	}
	return canonical, nil
}

// RawExecutionConfig retains exact numeric literals for the complete scope digest.
func (r fileSourceAuthorizationRepository) RawExecutionConfig(ctx context.Context, tenantID int, executionID string) (json.RawMessage, error) {
	return rawFileExecutionConfig(ctx, r.db, tenantID, executionID, r.taskType)
}

func rawFileExecutionConfig(ctx context.Context, db *gorm.DB, tenantID int, executionID string, taskType string) (json.RawMessage, error) {
	var raw []byte
	err := db.WithContext(ctx).Model(&commonExecution.TaskExecution{}).Select("execution_config").
		Where("tenant_id = ? AND execution_id = ? AND module = ? AND task_type = ?", tenantID, executionID, commonExecution.ModuleManager, taskType).Row().Scan(&raw)
	return json.RawMessage(raw), err
}

func (r fileSourceAuthorizationRepository) GetExecution(ctx context.Context, tenantID int, executionID string) (*commonExecution.TaskExecution, error) {
	var item commonExecution.TaskExecution
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND execution_id = ? AND module = ? AND task_type = ?", tenantID, executionID, commonExecution.ModuleManager, r.taskType).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &item, err
}

// FailUnboundExecution cannot close a claimed or already bound execution. The
// execution and the owning task summary converge in one transaction.
func (r fileSourceAuthorizationRepository) FailUnboundExecution(ctx context.Context, expected *commonExecution.TaskExecution) error {
	if expected == nil || expected.TenantID <= 0 || expected.SourceTaskID == nil ||
		commonExecution.CopyActorOrigin(&commonExecution.TaskExecution{TenantID: expected.TenantID}, expected) != nil {
		return commonAPI.ErrConflict
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current commonExecution.TaskExecution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND execution_id = ? AND module = ? AND task_type = ?", expected.TenantID, expected.ExecutionID, commonExecution.ModuleManager, r.taskType).First(&current).Error; err != nil {
			return err
		}
		if current.Source != commonExecution.ModuleManager || current.TriggerType != commonExecution.TriggerTypeManual || current.ParentExecutionID != nil || current.ExecutionBoundary != commonExecution.ExecutionBoundaryBounded ||
			current.SourceTaskID == nil || *current.SourceTaskID != *expected.SourceTaskID || current.TriggeredBy == nil || *current.TriggeredBy != *expected.TriggeredBy ||
			current.Status != commonExecution.ExecutionStatusPending || current.Attempt != 0 || current.ExecutionAuthorizationID != nil || current.AuthorizationExpiresAt != nil || current.LeaseToken != nil || current.LeaseOwner != nil || current.LeaseExpiresAt != nil ||
			!samePositiveProfileFact(current.ActorPrincipalID, expected.ActorPrincipalID) || !samePositiveProfileFact(current.ActorTenantMembershipID, expected.ActorTenantMembershipID) || !samePositiveProfileFact(current.IssuedAuthorizationVersion, expected.IssuedAuthorizationVersion) {
			return commonAPI.ErrConflict
		}
		return failPendingFileExecution(tx, &current, time.Now().UTC())
	})
}

func failPendingFileExecution(tx *gorm.DB, item *commonExecution.TaskExecution, now time.Time) error {
	if item == nil || (item.TaskType != commonExecution.TaskTypeModel3DGLBGeneration && item.TaskType != commonExecution.TaskTypeRasterCOGGeneration) || item.Module != commonExecution.ModuleManager {
		return commonAPI.ErrConflict
	}
	if err := commonExecution.FailPending(tx.Statement.Context, tx.Where("attempt = 0"), commonExecution.PendingIdentity{ExecutionID: item.ExecutionID, TenantID: item.TenantID, Module: item.Module, TaskType: item.TaskType, Source: item.Source}, map[string]interface{}{"error_details": commonModels.JSONMap{"code": "source_authorization_required", "message": "exact file source authorization preparation unavailable"}}); err != nil {
		return err
	}
	if item.SourceTaskID == nil {
		return nil
	}
	taskID, err := commonExecution.ParseSourceTaskIDUint(item.SourceTaskID)
	if err != nil {
		return nil
	} // Retain the invalid historical reference; never invent an owner.
	return tx.Model(&models.TaskDefinition{}).
		Where("id = ? AND tenant_id = ? AND task_type = ? AND last_execution_id = ? AND last_execution_status = ?", taskID, item.TenantID, item.TaskType, item.ExecutionID, commonExecution.ExecutionStatusPending).
		Updates(map[string]interface{}{"last_execution_status": commonExecution.ExecutionStatusFailed, "updated_at": now}).Error
}
