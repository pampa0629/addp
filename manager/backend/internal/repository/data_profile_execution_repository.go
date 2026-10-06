package repository

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DataProfileExecutionRepository struct {
	db *gorm.DB
}

// SuppressConditionalScopesByItemFingerprints removes historical condition
// values from data-profile execution snapshots for projection-change targets.
func (r *DataProfileExecutionRepository) SuppressConditionalScopesByItemFingerprints(
	ctx context.Context,
	tx *gorm.DB,
	tenantID int64,
	itemFingerprints []string,
) error {
	if r == nil || tx == nil || tenantID <= 0 || len(itemFingerprints) == 0 {
		return errors.New("data profile execution cleanup requires transaction, tenant and item fingerprints")
	}
	query := tx.WithContext(ctx).Where(
		"tenant_id = ? AND module = ? AND task_type = ?",
		tenantID,
		commonExecution.ModuleManager,
		commonExecution.TaskTypeDataProfiling,
	)
	if tx.Dialector.Name() == "postgres" {
		query = query.Where("execution_config ->> 'item_fingerprint' IN ?", itemFingerprints)
	} else {
		query = query.Where("json_extract(execution_config, '$.item_fingerprint') IN ?", itemFingerprints)
	}
	var executions []commonExecution.TaskExecution
	if err := query.Find(&executions).Error; err != nil {
		return err
	}
	for index := range executions {
		config := executions[index].ExecutionConfig
		if config == nil {
			continue
		}
		scope, ok := config["data_scope"].(map[string]interface{})
		if !ok || strings.TrimSpace(fmt.Sprint(scope["kind"])) != "condition" {
			continue
		}
		config["data_scope"] = map[string]interface{}{
			"kind":              "condition",
			"values_suppressed": true,
		}
		if err := tx.WithContext(ctx).Model(&commonExecution.TaskExecution{}).
			Where("id = ?", executions[index].ID).
			Update("execution_config", config).Error; err != nil {
			return err
		}
	}
	return nil
}

func NewDataProfileExecutionRepository(db *gorm.DB) *DataProfileExecutionRepository {
	return &DataProfileExecutionRepository{db: db}
}

func (r *DataProfileExecutionRepository) CreateOrReuseActive(
	ctx context.Context,
	targetKey string,
	execution *commonExecution.TaskExecution,
) (*commonExecution.TaskExecution, bool, error) {
	if execution == nil {
		return nil, false, errors.New("execution is required")
	}
	var result *commonExecution.TaskExecution
	created := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", dataProfileLockID(execution.TenantID, targetKey)).Error; err != nil {
				return fmt.Errorf("lock data profile execution target: %w", err)
			}
		}
		active, err := findActiveDataProfileExecution(tx, execution.TenantID, targetKey)
		if err != nil {
			return err
		}
		if active != nil {
			result = active
			return nil
		}
		if err := tx.Create(execution).Error; err != nil {
			return err
		}
		result = execution
		created = true
		return nil
	})
	return result, created, err
}

func (r *DataProfileExecutionRepository) GetActive(
	ctx context.Context,
	tenantID int,
	targetKey string,
) (*commonExecution.TaskExecution, error) {
	return findActiveDataProfileExecution(r.db.WithContext(ctx), tenantID, targetKey)
}

func (r *DataProfileExecutionRepository) GetLatest(
	ctx context.Context,
	tenantID int,
	targetKey string,
) (*commonExecution.TaskExecution, error) {
	var execution commonExecution.TaskExecution
	query := r.db.WithContext(ctx).
		Where(
			"tenant_id = ? AND module = ? AND task_type = ?",
			tenantID,
			commonExecution.ModuleManager,
			commonExecution.TaskTypeDataProfiling,
		)
	if r.db.Dialector.Name() == "postgres" {
		query = query.Where("execution_config ->> 'target_key' = ?", targetKey)
	} else {
		query = query.Where("json_extract(execution_config, '$.target_key') = ?", targetKey)
	}
	err := query.Order("created_at DESC").First(&execution).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &execution, err
}

func (r *DataProfileExecutionRepository) GetByExecutionID(
	ctx context.Context,
	tenantID int,
	executionID string,
) (*commonExecution.TaskExecution, error) {
	var execution commonExecution.TaskExecution
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND execution_id = ? AND module = ? AND task_type = ?", tenantID, executionID, commonExecution.ModuleManager, commonExecution.TaskTypeDataProfiling).
		First(&execution).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &execution, err
}

// GetRawExecutionConfig never passes persisted numeric literals through JSONMap.
func (r *DataProfileExecutionRepository) GetRawExecutionConfig(ctx context.Context, tenantID int, executionID string) (json.RawMessage, error) {
	return rawDataProfileConfig(ctx, r.db, tenantID, executionID)
}

func rawDataProfileConfig(ctx context.Context, db *gorm.DB, tenantID int, executionID string) (json.RawMessage, error) {
	var raw []byte
	err := db.WithContext(ctx).Model(&commonExecution.TaskExecution{}).
		Select("execution_config").Where("tenant_id = ? AND execution_id = ? AND module = ? AND task_type = ?", tenantID, executionID, commonExecution.ModuleManager, commonExecution.TaskTypeDataProfiling).
		Row().Scan(&raw)
	return json.RawMessage(raw), err
}

// BindSourceAuthorization only binds a validated System issuance response.
// Holding the execution row lock closes the check/write/claim race. No System
// private tables are read and this binding is not a source-access decision.
func (r *DataProfileExecutionRepository) BindSourceAuthorization(ctx context.Context, expected *commonExecution.TaskExecution, scope commonExecution.ManagerProfileReadScope, issued *commonClient.IssuedManagerProfileAuthorization) error {
	if expected == nil || expected.TenantID <= 0 || issued == nil || issued.ExecutionID != expected.ExecutionID || !issued.Matches(uint(expected.TenantID), scope) {
		return commonAPI.ErrConflict
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current commonExecution.TaskExecution
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND execution_id = ? AND module = ? AND task_type = ?", expected.TenantID, expected.ExecutionID, commonExecution.ModuleManager, commonExecution.TaskTypeDataProfiling).First(&current).Error
		if err != nil {
			return err
		}
		if current.Source != commonExecution.ModuleManager || current.TriggerType != commonExecution.TriggerTypeManual || current.ExecutionBoundary != commonExecution.ExecutionBoundaryBounded || current.SourceTaskID != nil || current.ParentExecutionID != nil ||
			current.Status != commonExecution.ExecutionStatusPending || current.Attempt != 0 || current.ExecutionAuthorizationID != nil || current.AuthorizationExpiresAt != nil || current.LeaseToken != nil || current.LeaseOwner != nil || current.LeaseExpiresAt != nil ||
			!samePositiveProfileFact(current.ActorPrincipalID, expected.ActorPrincipalID) || !samePositiveProfileFact(current.ActorTenantMembershipID, expected.ActorTenantMembershipID) || !samePositiveProfileFact(current.IssuedAuthorizationVersion, expected.IssuedAuthorizationVersion) {
			return commonAPI.ErrConflict
		}
		raw, err := rawDataProfileConfig(ctx, tx, current.TenantID, current.ExecutionID)
		if err != nil {
			return err
		}
		var header struct {
			Version string              `json:"config_version"`
			ReadSet plugin.QueryReadSet `json:"read_set"`
		}
		if json.Unmarshal(raw, &header) != nil || header.Version != "data-profile-config/v6" {
			return commonAPI.ErrConflict
		}
		actual, err := commonExecution.NewManagerProfileReadScope(raw, &header.ReadSet)
		if err != nil || !reflect.DeepEqual(actual, &scope) || !issued.Matches(uint(current.TenantID), *actual) {
			return commonAPI.ErrConflict
		}
		authorizationID, err := strconv.ParseInt(issued.ID, 10, 64)
		if err != nil {
			return commonAPI.ErrConflict
		}
		result := tx.Model(&commonExecution.TaskExecution{}).Where("id = ? AND status = ? AND attempt = 0 AND execution_authorization_id IS NULL AND authorization_expires_at IS NULL", current.ID, commonExecution.ExecutionStatusPending).
			Updates(map[string]interface{}{"execution_authorization_id": authorizationID, "authorization_expires_at": issued.ExpiresAt, "updated_at": time.Now().UTC()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return commonAPI.ErrConflict
		}
		return nil
	})
}

func samePositiveProfileFact(left, right *int64) bool {
	return left != nil && right != nil && *left > 0 && *left == *right
}

// FailUnbound closes only this producer's still-unclaimed, unbound execution.
func (r *DataProfileExecutionRepository) FailUnbound(ctx context.Context, expected *commonExecution.TaskExecution) error {
	if expected == nil || expected.TenantID <= 0 || expected.ActorPrincipalID == nil || *expected.ActorPrincipalID <= 0 || expected.ActorTenantMembershipID == nil || *expected.ActorTenantMembershipID <= 0 || expected.IssuedAuthorizationVersion == nil || *expected.IssuedAuthorizationVersion <= 0 {
		return commonAPI.ErrConflict
	}
	now := time.Now().UTC()
	result := r.db.WithContext(ctx).Model(&commonExecution.TaskExecution{}).
		Where("source = ? AND trigger_type = ? AND execution_boundary = ? AND source_task_id IS NULL AND parent_execution_id IS NULL AND lease_token IS NULL AND lease_owner IS NULL AND lease_expires_at IS NULL", commonExecution.ModuleManager, commonExecution.TriggerTypeManual, commonExecution.ExecutionBoundaryBounded).
		Where("tenant_id = ? AND execution_id = ? AND module = ? AND task_type = ? AND status = ? AND attempt = 0 AND execution_authorization_id IS NULL AND authorization_expires_at IS NULL AND actor_principal_id = ? AND actor_tenant_membership_id = ? AND issued_authorization_version = ?", expected.TenantID, expected.ExecutionID, commonExecution.ModuleManager, commonExecution.TaskTypeDataProfiling, commonExecution.ExecutionStatusPending, *expected.ActorPrincipalID, *expected.ActorTenantMembershipID, *expected.IssuedAuthorizationVersion).
		Updates(map[string]interface{}{"status": commonExecution.ExecutionStatusFailed, "completed_at": now, "updated_at": now, "error_details": commonModels.JSONMap{"code": "source_authorization_required", "message": "data profiling execution source authorization unavailable"}})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return commonAPI.ErrConflict
	}
	return nil
}

func (r *DataProfileExecutionRepository) Start(
	ctx context.Context,
	tenantID int,
	executionID string,
	startedAt time.Time,
) error {
	lease, ok := commonExecution.LeaseFromContext(ctx)
	if !ok || lease.ExecutionID != executionID || lease.TenantID != tenantID {
		return errors.New("data profile execution requires its claimed lease")
	}
	return commonExecution.UpdateWithLease(ctx, r.db, lease, map[string]interface{}{"updated_at": startedAt.UTC()})
}

func (r *DataProfileExecutionRepository) Complete(
	ctx context.Context,
	tenantID int,
	executionID string,
	startedAt time.Time,
	rowsRead int64,
	metadata map[string]interface{},
) error {
	completedAt := time.Now().UTC()
	return r.updateTerminal(ctx, tenantID, executionID, commonExecution.ExecutionStatusSuccess, map[string]interface{}{
		"completed_at":      completedAt,
		"updated_at":        completedAt,
		"execution_time_ms": completedAt.Sub(startedAt).Milliseconds(),
		"records_read":      rowsRead,
		"metadata":          commonModels.JSONMap(metadata),
		"progress":          100,
	})
}

func (r *DataProfileExecutionRepository) Fail(
	ctx context.Context,
	tenantID int,
	executionID string,
	startedAt time.Time,
	errorCode string,
	errorMessage string,
) error {
	return r.failWithStatus(ctx, tenantID, executionID, startedAt, commonExecution.ExecutionStatusFailed, errorCode, errorMessage)
}

func (r *DataProfileExecutionRepository) Timeout(
	ctx context.Context,
	tenantID int,
	executionID string,
	startedAt time.Time,
	errorCode string,
	errorMessage string,
) error {
	return r.failWithStatus(ctx, tenantID, executionID, startedAt, commonExecution.ExecutionStatusTimeout, errorCode, errorMessage)
}

func (r *DataProfileExecutionRepository) failWithStatus(
	ctx context.Context,
	tenantID int,
	executionID string,
	startedAt time.Time,
	status string,
	errorCode string,
	errorMessage string,
) error {
	completedAt := time.Now().UTC()
	return r.updateTerminal(ctx, tenantID, executionID, status, map[string]interface{}{
		"completed_at":      completedAt,
		"updated_at":        completedAt,
		"execution_time_ms": completedAt.Sub(startedAt).Milliseconds(),
		"error_details": commonModels.JSONMap{
			"code":    errorCode,
			"message": errorMessage,
		},
	})
}

func (r *DataProfileExecutionRepository) updateTerminal(
	ctx context.Context,
	tenantID int,
	executionID string,
	status string,
	fields map[string]interface{},
) error {
	lease, ok := commonExecution.LeaseFromContext(ctx)
	if !ok || lease.ExecutionID != executionID || lease.TenantID != tenantID {
		return errors.New("data profile execution requires its claimed lease")
	}
	completedAt, _ := fields["completed_at"].(time.Time)
	ownedFields := make(map[string]interface{}, len(fields))
	for key, value := range fields {
		if key != "status" && key != "completed_at" && key != "updated_at" {
			ownedFields[key] = value
		}
	}
	return commonExecution.CompleteWithLease(ctx, r.db, lease, status, completedAt, ownedFields)
}

func findActiveDataProfileExecution(db *gorm.DB, tenantID int, targetKey string) (*commonExecution.TaskExecution, error) {
	var execution commonExecution.TaskExecution
	query := db.
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(
			"tenant_id = ? AND module = ? AND task_type = ? AND status IN ?",
			tenantID,
			commonExecution.ModuleManager,
			commonExecution.TaskTypeDataProfiling,
			[]string{commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning},
		)
	if db.Dialector.Name() == "postgres" {
		query = query.Where("execution_config ->> 'target_key' = ?", targetKey)
	} else {
		query = query.Where("json_extract(execution_config, '$.target_key') = ?", targetKey)
	}
	err := query.Order("created_at DESC").First(&execution).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &execution, err
}

func dataProfileLockID(tenantID int, targetKey string) int64 {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", tenantID, targetKey)))
	return int64(binary.BigEndian.Uint64(hash[:8]))
}
