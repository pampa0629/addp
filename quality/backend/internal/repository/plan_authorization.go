package repository

import (
	"context"
	"fmt"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/quality/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

func (r *PlanRepository) AttachPendingAuthorization(ctx context.Context, tenantID int64, executionID string, fields map[string]interface{}) error {
	id, idOK := fields["execution_authorization_id"].(*int64)
	principal, principalOK := fields["actor_principal_id"].(*int64)
	membership, membershipOK := fields["actor_tenant_membership_id"].(*int64)
	version, versionOK := fields["issued_authorization_version"].(*int64)
	expiresAt, expiryOK := fields["authorization_expires_at"].(time.Time)
	if len(fields) != 5 || !idOK || id == nil || !principalOK || principal == nil || !membershipOK || membership == nil || !versionOK || version == nil || !expiryOK {
		return fmt.Errorf("quality execution authorization fields are invalid")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item commonExecution.TaskExecution
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND execution_id = ? AND module = ? AND task_type = ?", tenantID, executionID, commonExecution.ModuleQuality, commonExecution.TaskTypeQualityPlan).First(&item).Error
		if err != nil {
			return err
		}

		return commonExecution.AttachPendingAuthorization(ctx, tx, commonExecution.PendingIdentity{ExecutionID: item.ExecutionID, TenantID: item.TenantID, Module: item.Module, TaskType: item.TaskType, Source: item.Source}, commonExecution.PendingAuthorization{ID: *id, ExpiresAt: expiresAt, ActorPrincipalID: *principal, ActorTenantMembershipID: *membership, IssuedAuthorizationVersion: *version})
	})
}

func (r *PlanRepository) FailPendingExecution(ctx context.Context, taskID, tenantID int64, executionID, errorCode string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item commonExecution.TaskExecution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND execution_id = ? AND module = ? AND task_type = ? AND status = ?", tenantID, executionID, commonExecution.ModuleQuality, commonExecution.TaskTypeQualityPlan, commonExecution.ExecutionStatusPending).First(&item).Error; err != nil {
			return err
		}
		now, err := commonExecution.DatabaseNow(tx)
		if err != nil {
			return err
		}
		if err := tx.Model(&models.QualityPlan{}).Where("id = ? AND tenant_id = ? AND last_execution_id = ? AND last_execution_status = ?", taskID, tenantID, executionID, commonExecution.ExecutionStatusPending).Updates(map[string]interface{}{"last_run_at": now, "last_execution_status": commonExecution.ExecutionStatusFailed}).Error; err != nil {
			return err
		}
		return commonExecution.FailPending(ctx, tx, commonExecution.PendingIdentity{ExecutionID: item.ExecutionID, TenantID: item.TenantID, Module: item.Module, TaskType: item.TaskType, Source: item.Source}, map[string]interface{}{"error_details": commonModels.JSONMap{"code": errorCode, "message": "quality execution authorization could not be prepared"}})
	})
}
