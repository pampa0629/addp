package repository

import (
	"context"
	"encoding/json"
	"strconv"

	commonapi "github.com/addp/common/api"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonexecution "github.com/addp/common/execution"
	"github.com/addp/develop/backend/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PrivateFileRuntimeRecord struct {
	RuntimeExecutionID string
	Manifest           plugin.WorkflowPrivateFileManifest
	OwnerInstanceID    string
	InputDirectory     string
}

// Both stages lock E before O and finish with the same canonical lease fence.
func (r *FileExecutionRepository) withPrivateFileLease(ctx context.Context, lease commonexecution.Lease, write func(*gorm.DB, *models.FileExecution, commonclient.IssuedDevelopFileAuthorization) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, err := commonexecution.LockActiveLease(ctx, tx, lease)
		if err != nil {
			return err
		}
		if item.Module != commonexecution.ModuleDevelop || item.TaskType != commonexecution.TaskTypeWorkflow || item.Source != commonexecution.ModuleDevelop {
			return commonapi.ErrConflict
		}
		var row models.FileExecution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("execution_id = ? AND tenant_id = ?", lease.ExecutionID, lease.TenantID).First(&row).Error; err != nil {
			return err
		}
		if row.AdmissionState != "ready" {
			return commonapi.ErrConflict
		}
		var issued commonclient.IssuedDevelopFileAuthorization
		if err := json.Unmarshal(row.IssuedAuthorizationJSON, &issued); err != nil {
			return err
		}
		if item.ExecutionAuthorizationID == nil || strconv.FormatInt(*item.ExecutionAuthorizationID, 10) != issued.Authorization.ID || lease.Attempt != issued.Scope.ExpectedAttempt {
			return commonapi.ErrConflict
		}
		if err := write(tx, &row, issued); err != nil {
			return err
		}
		return commonexecution.UpdateWithLease(ctx, tx, lease, map[string]interface{}{})
	})
}

// Ownership is registered before source bytes enter private staging. A missing
// final Runtime manifest cannot erase this earlier cleanup reference.
func (r *FileExecutionRepository) RecordPrivateInputDirectory(ctx context.Context, lease commonexecution.Lease, instanceID, directory string) error {
	return r.withPrivateFileLease(ctx, lease, func(tx *gorm.DB, row *models.FileExecution, _ commonclient.IssuedDevelopFileAuthorization) error {
		if instanceID == "" || directory == "" || row.InputDirectory != "" || row.InputOwnerInstanceID != "" || row.RuntimeExecutionID != nil {
			return commonapi.ErrConflict
		}
		return tx.Model(row).Updates(map[string]interface{}{"input_directory": directory, "input_owner_instance_id": instanceID, "version": row.Version + 1}).Error
	})
}

// RecordPrivateRuntime receives the validated manifest from the Owner transport
// constructor. This transaction checks current ownership/state, not source IAM.
// It stores no connection, token or business publication. Directory/instance
// references are Owner-private and do not themselves authorize deletion.
func (r *FileExecutionRepository) RecordPrivateRuntime(ctx context.Context, lease commonexecution.Lease, record PrivateFileRuntimeRecord) error {
	return r.withPrivateFileLease(ctx, lease, func(tx *gorm.DB, row *models.FileExecution, issued commonclient.IssuedDevelopFileAuthorization) error {
		m := record.Manifest
		if row.RuntimeExecutionID != nil || record.RuntimeExecutionID == "" || row.InputDirectory == "" || row.InputOwnerInstanceID == "" || record.InputDirectory != row.InputDirectory || record.OwnerInstanceID != row.InputOwnerInstanceID {
			return commonapi.ErrConflict
		}
		if m.ExecutionID != lease.ExecutionID || m.Attempt != lease.Attempt || int(m.TenantID) != lease.TenantID || strconv.FormatInt(m.AuthorizationID, 10) != issued.Authorization.ID || m.ScopeDigest != issued.ScopeDigest {
			return commonapi.ErrConflict
		}
		raw, err := json.Marshal(m)
		if err != nil {
			return err
		}
		return tx.Model(row).Updates(map[string]interface{}{"runtime_execution_id": record.RuntimeExecutionID, "runtime_manifest_json": raw, "version": row.Version + 1}).Error
	})
}

type PrivateInputDirectoryRecord struct {
	ExecutionID     string
	TenantID        uint
	Attempt         int
	AuthorizationID int64
	ScopeDigest     string
	OwnerInstanceID string
	InputDirectory  string
}

func (r *FileExecutionRepository) ReadPrivateInputDirectory(ctx context.Context, tenant uint, executionID string) (*PrivateInputDirectoryRecord, error) {
	var row models.FileExecution
	if err := r.db.WithContext(ctx).Where("execution_id = ? AND tenant_id = ?", executionID, tenant).First(&row).Error; err != nil {
		return nil, err
	}
	if row.InputDirectory == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var issued commonclient.IssuedDevelopFileAuthorization
	if err := json.Unmarshal(row.IssuedAuthorizationJSON, &issued); err != nil {
		return nil, err
	}
	id, _ := strconv.ParseInt(issued.Authorization.ID, 10, 64)
	return &PrivateInputDirectoryRecord{ExecutionID: row.ExecutionID, TenantID: row.TenantID, Attempt: issued.Scope.ExpectedAttempt, AuthorizationID: id, ScopeDigest: issued.ScopeDigest, OwnerInstanceID: row.InputOwnerInstanceID, InputDirectory: row.InputDirectory}, nil
}

// Recovery can read only this original record; it is not fresh source access,
// a new Runtime identity or permission to replay Create/Upload/Execute.
func (r *FileExecutionRepository) ReadPrivateRuntime(ctx context.Context, tenant uint, executionID string) (*PrivateFileRuntimeRecord, error) {
	var row models.FileExecution
	if err := r.db.WithContext(ctx).Where("execution_id = ? AND tenant_id = ?", executionID, tenant).First(&row).Error; err != nil {
		return nil, err
	}
	if row.RuntimeExecutionID == nil {
		return nil, gorm.ErrRecordNotFound
	}
	var manifest plugin.WorkflowPrivateFileManifest
	if err := json.Unmarshal(row.RuntimeManifestJSON, &manifest); err != nil {
		return nil, err
	}
	return &PrivateFileRuntimeRecord{RuntimeExecutionID: *row.RuntimeExecutionID, Manifest: manifest, OwnerInstanceID: row.InputOwnerInstanceID, InputDirectory: row.InputDirectory}, nil
}
