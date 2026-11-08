package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	commonclient "github.com/addp/common/client"
	commonexecution "github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type FileExecutionRepository struct{ db *gorm.DB }

func NewFileExecutionRepository(db *gorm.DB) *FileExecutionRepository {
	return &FileExecutionRepository{db}
}

// CreatePending freezes the actual stored JSON in the same Owner transaction.
// The caller has already resolved DAG source coverage and verified User origin.
// No System/Meta/network operation takes place while this transaction is open.
func (r *FileExecutionRepository) CreatePending(ctx context.Context, item *commonexecution.TaskExecution, sources []commonexecution.DevelopFileSourceIntent, target *commonexecution.DevelopFileTargetIntent) (*commonclient.DevelopFileExecutionFacts, error) {
	if item == nil || item.Module != commonexecution.ModuleDevelop || item.Source != commonexecution.ModuleDevelop ||
		item.TaskType != commonexecution.TaskTypeWorkflow || item.TriggerType != commonexecution.TriggerTypeManual || item.MaxAttempts != 1 ||
		item.ExecutionBoundary != commonexecution.ExecutionBoundaryBounded || item.ParentExecutionID != nil || item.RetryOfExecutionID != nil ||
		item.ExecutionAuthorizationID != nil || item.AuthorizationExpiresAt != nil || item.ActorPrincipalID == nil || item.ActorTenantMembershipID == nil || item.IssuedAuthorizationVersion == nil ||
		item.TriggeredBy == nil || int64(*item.TriggeredBy) != *item.ActorPrincipalID {
		return nil, errors.New("invalid private file pending origin")
	}
	var result *commonclient.DevelopFileExecutionFacts
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := commonexecution.CreatePending(ctx, tx, item); err != nil {
			return err
		}
		raw, err := fileExecutionRawConfig(ctx, tx, item.ExecutionID, uint(item.TenantID))
		if err != nil {
			return err
		}
		var fields map[string]json.RawMessage
		var timeout int
		if json.Unmarshal(raw, &fields) != nil || json.Unmarshal(fields["timeout"], &timeout) != nil {
			return errors.New("file execution requires frozen integer timeout")
		}
		intent, err := commonexecution.NewDevelopFileIntent(raw, item.Attempt+1, timeout, sources, target)
		if err != nil {
			return err
		}
		result = &commonclient.DevelopFileExecutionFacts{SchemaVersion: commonclient.DevelopFileExecutionSchema,
			ExecutionID: item.ExecutionID, TenantID: strconv.Itoa(item.TenantID), ConfigJSON: bytes.Clone(raw), Intent: *intent,
			Actor: commonclient.DevelopFileExecutionActor{PrincipalID: strconv.FormatInt(*item.ActorPrincipalID, 10),
				TenantMembershipID: strconv.FormatInt(*item.ActorTenantMembershipID, 10), AuthorizationVersion: strconv.FormatInt(*item.IssuedAuthorizationVersion, 10)},
			AdmissionState: "pending"}
		if err := result.Validate(); err != nil {
			return err
		}
		encoded, err := json.Marshal(intent)
		if err != nil {
			return err
		}
		row := models.FileExecution{ExecutionID: item.ExecutionID, TenantID: uint(item.TenantID), ActorPrincipalID: *item.ActorPrincipalID,
			ActorMembershipID: *item.ActorTenantMembershipID, AuthorizationVersion: *item.IssuedAuthorizationVersion,
			ConfigJSON: raw, IntentJSON: encoded, AdmissionState: "pending", Version: 1}
		return tx.Create(&row).Error
	})
	if err != nil {
		return nil, err
	}
	return result.Clone(), nil
}

func fileExecutionRawConfig(ctx context.Context, db *gorm.DB, executionID string, tenant uint) (json.RawMessage, error) {
	var raw []byte
	err := db.WithContext(ctx).Model(&commonexecution.TaskExecution{}).Select("execution_config").
		Where("execution_id = ? AND tenant_id = ? AND module = ? AND task_type = ? AND source = ?",
			executionID, tenant, commonexecution.ModuleDevelop, commonexecution.TaskTypeWorkflow, commonexecution.ModuleDevelop).
		Row().Scan(&raw)
	return json.RawMessage(raw), err
}

func privateFileFacts(row *models.FileExecution) (*commonclient.DevelopFileExecutionFacts, error) {
	value := &commonclient.DevelopFileExecutionFacts{SchemaVersion: commonclient.DevelopFileExecutionSchema,
		ExecutionID: row.ExecutionID, TenantID: strconv.FormatUint(uint64(row.TenantID), 10), ConfigJSON: bytes.Clone(row.ConfigJSON),
		AdmissionState: row.AdmissionState, Actor: commonclient.DevelopFileExecutionActor{
			PrincipalID: strconv.FormatInt(row.ActorPrincipalID, 10), TenantMembershipID: strconv.FormatInt(row.ActorMembershipID, 10),
			AuthorizationVersion: strconv.FormatInt(row.AuthorizationVersion, 10)}}
	if err := json.Unmarshal(row.IntentJSON, &value.Intent); err != nil {
		return nil, err
	}
	if len(row.IssuedAuthorizationJSON) != 0 {
		var issued commonclient.IssuedDevelopFileAuthorization
		if err := json.Unmarshal(row.IssuedAuthorizationJSON, &issued); err != nil {
			return nil, err
		}
		value.AuthorizationID = issued.Authorization.ID
		value.BoundScope, value.ScopeDigest = issued.Scope.Clone(), issued.ScopeDigest
	}
	return value, value.Validate()
}

func (r *FileExecutionRepository) Read(ctx context.Context, tenant uint, executionID string) (*commonclient.DevelopFileExecutionFacts, error) {
	var row models.FileExecution
	if err := r.db.WithContext(ctx).Where("execution_id = ? AND tenant_id = ?", executionID, tenant).First(&row).Error; err != nil {
		return nil, err
	}
	return privateFileFacts(&row)
}

// RejectUnissued closes only a known rejected issuance. The caller must not
// use an HTTP timeout, lost reply or dependency outage as a rejection proof.
func (r *FileExecutionRepository) RejectUnissued(ctx context.Context, tenant uint, executionID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row models.FileExecution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("execution_id = ? AND tenant_id = ?", executionID, tenant).First(&row).Error; err != nil {
			return err
		}
		if row.AdmissionState != "pending" || len(row.IssuedAuthorizationJSON) != 0 {
			return commonapi.ErrConflict
		}
		var item commonexecution.TaskExecution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"execution_id = ? AND tenant_id = ? AND module = ? AND source = ? AND task_type = ? AND status = ? AND attempt = 0 AND execution_authorization_id IS NULL AND authorization_expires_at IS NULL AND lease_token IS NULL AND started_at IS NULL",
			executionID, tenant, commonexecution.ModuleDevelop, commonexecution.ModuleDevelop, commonexecution.TaskTypeWorkflow, commonexecution.ExecutionStatusPending).First(&item).Error; err != nil {
			return err
		}
		if err := commonexecution.RejectUnstarted(ctx, tx, commonexecution.PendingIdentity{ExecutionID: executionID,
			TenantID: int(tenant), Module: commonexecution.ModuleDevelop, TaskType: commonexecution.TaskTypeWorkflow, Source: commonexecution.ModuleDevelop},
			map[string]interface{}{"error_details": commonmodels.JSONMap{"code": "develop.workflow.file_authorization_rejected"}}); err != nil {
			return err
		}
		if err := updateDevTaskExecutionSummary(tx, &item, commonexecution.ExecutionStatusFailed, time.Now()); err != nil {
			return err
		}
		return tx.Model(&row).Updates(map[string]any{"admission_state": "rejected", "version": row.Version + 1}).Error
	})
}

func (r *FileExecutionRepository) BindAuthorization(ctx context.Context, tenant uint, executionID string, issued commonclient.IssuedDevelopFileAuthorization) error {
	issued = *issued.Clone()
	if err := issued.Validate(); err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row models.FileExecution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("execution_id = ? AND tenant_id = ?", executionID, tenant).First(&row).Error; err != nil {
			return err
		}
		facts, err := privateFileFacts(&row)
		if err != nil {
			return err
		}
		if row.AdmissionState != "pending" || len(row.IssuedAuthorizationJSON) != 0 || issued.Authorization.ExecutionID != executionID || issued.Authorization.TenantID != facts.TenantID ||
			issued.Authorization.ActorPrincipalID != facts.Actor.PrincipalID || issued.Authorization.TenantMembershipID != facts.Actor.TenantMembershipID ||
			issued.Authorization.IssuedAuthorizationVersion != facts.Actor.AuthorizationVersion || issued.Scope.ValidateAgainstIntent(facts.Intent) != nil {
			return commonapi.ErrConflict
		}
		encoded, err := json.Marshal(issued)
		if err != nil {
			return err
		}
		authorizationID, _ := strconv.ParseInt(issued.Authorization.ID, 10, 64)
		changed := tx.Model(&commonexecution.TaskExecution{}).Where(
			"execution_id = ? AND tenant_id = ? AND module = ? AND source = ? AND task_type = ? AND status = ? AND attempt = 0 AND execution_authorization_id IS NULL AND lease_token IS NULL AND started_at IS NULL",
			executionID, tenant, commonexecution.ModuleDevelop, commonexecution.ModuleDevelop, commonexecution.TaskTypeWorkflow, commonexecution.ExecutionStatusPending).
			Where("actor_principal_id = ? AND actor_tenant_membership_id = ? AND issued_authorization_version = ?", row.ActorPrincipalID, row.ActorMembershipID, row.AuthorizationVersion).
			Updates(map[string]any{"execution_authorization_id": authorizationID, "authorization_expires_at": issued.Authorization.ExpiresAt})
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return commonapi.ErrConflict
		}
		raw, err := fileExecutionRawConfig(ctx, tx, executionID, tenant)
		if err != nil {
			return err
		}
		current, err := commonexecution.NewDevelopFileIntent(raw, facts.Intent.ExpectedAttempt, facts.Intent.TimeoutSeconds, facts.Intent.Sources, facts.Intent.Target)
		if err != nil || !reflect.DeepEqual(current, &facts.Intent) {
			return commonapi.ErrConflict
		}
		return tx.Model(&row).Updates(map[string]any{"issued_authorization_json": encoded, "version": row.Version + 1}).Error
	})
}

func (r *FileExecutionRepository) ApplyAdmission(ctx context.Context, tenant uint, executionID string, observation commonclient.DevelopFileAdmissionObservation) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row models.FileExecution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("execution_id = ? AND tenant_id = ?", executionID, tenant).First(&row).Error; err != nil {
			return err
		}
		facts, err := privateFileFacts(&row)
		if err != nil {
			return err
		}
		if facts.AuthorizationID == "" || observation.AuthorizationID != facts.AuthorizationID || observation.ExecutionID != executionID ||
			observation.ExpectedAttempt != facts.Intent.ExpectedAttempt || observation.ScopeDigest != facts.ScopeDigest || (row.AdmissionState != "pending" && row.AdmissionState != "ready") ||
			!observation.ExpiresAt.After(time.Now()) || observation.ObservedAt.After(time.Now()) {
			return commonapi.ErrConflict
		}
		var item commonexecution.TaskExecution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"execution_id = ? AND tenant_id = ? AND module = ? AND source = ? AND task_type = ? AND status = ? AND attempt = 0 AND lease_token IS NULL AND started_at IS NULL",
			executionID, tenant, commonexecution.ModuleDevelop, commonexecution.ModuleDevelop, commonexecution.TaskTypeWorkflow, commonexecution.ExecutionStatusPending).First(&item).Error; err != nil {
			return err
		}
		if item.ExecutionAuthorizationID == nil || strconv.FormatInt(*item.ExecutionAuthorizationID, 10) != facts.AuthorizationID ||
			item.AuthorizationExpiresAt == nil || !item.AuthorizationExpiresAt.After(time.Now()) || observation.ExpiresAt.After(*item.AuthorizationExpiresAt) {
			return commonapi.ErrConflict
		}
		raw, err := fileExecutionRawConfig(ctx, tx, executionID, tenant)
		if err != nil {
			return err
		}
		current, err := commonexecution.NewDevelopFileIntent(raw, facts.Intent.ExpectedAttempt, facts.Intent.TimeoutSeconds, facts.Intent.Sources, facts.Intent.Target)
		if err != nil || !reflect.DeepEqual(current, &facts.Intent) {
			return commonapi.ErrConflict
		}
		state := observation.State
		if state == "awaiting_approval" {
			return nil
		}
		if state != "ready" && state != "rejected" {
			return commonapi.ErrConflict
		}
		encoded, err := json.Marshal(observation)
		if err != nil {
			return err
		}
		if (facts.Intent.Target == nil && observation.ApprovalID != "") || (facts.Intent.Target != nil && state == "ready" && observation.ApprovalID == "") {
			return commonapi.ErrConflict
		}
		if state == "rejected" {
			if err := commonexecution.RejectUnstarted(ctx, tx, commonexecution.PendingIdentity{ExecutionID: executionID,
				TenantID: int(tenant), Module: commonexecution.ModuleDevelop, TaskType: commonexecution.TaskTypeWorkflow, Source: commonexecution.ModuleDevelop},
				map[string]interface{}{"error_details": commonmodels.JSONMap{"code": "develop.workflow.file_admission_rejected"}}); err != nil {
				return err
			}
			if err := updateDevTaskExecutionSummary(tx, &item, commonexecution.ExecutionStatusFailed, time.Now()); err != nil {
				return err
			}
		}
		return tx.Model(&row).Updates(map[string]any{"admission_state": state, "admission_json": encoded,
			"admission_expires_at": observation.ExpiresAt, "version": row.Version + 1}).Error
	})
}
