package repository

import (
	"context"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	"github.com/addp/graph/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *BuildRepository) BeginReview(ctx context.Context, graphID, itemID, tenantID, actor uint) (*models.ReviewItem, error) {
	var item models.ReviewItem
	if err := r.db.WithContext(ctx).Where("id = ? AND graph_id = ? AND tenant_id = ?", itemID, graphID, tenantID).First(&item).Error; err != nil {
		return nil, err
	}
	if item.Status != models.ReviewStatusPending || item.ActiveCommandID != "" {
		return nil, commonapi.ErrConflict
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := LockAdmission(tx, tenantID, false); err != nil {
			return err
		}
		if err := CheckGraphOpen(tx, graphID, tenantID); err != nil {
			return err
		}
		if err := tx.Where("id = ? AND graph_id = ? AND tenant_id = ?", itemID, graphID, tenantID).First(&item).Error; err != nil {
			return err
		}
		if _, err := lockTask(tx, item.TaskID, graphID, tenantID); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND graph_id = ? AND tenant_id = ?", itemID, graphID, tenantID).First(&item).Error; err != nil {
			return err
		}
		if item.Status != models.ReviewStatusPending || item.ActiveCommandID != "" {
			return commonapi.ErrConflict
		}
		item.ActiveCommandID = uuid.NewString()
		item.CommandActor = actor
		return tx.Model(&item).Updates(map[string]interface{}{"active_command_id": item.ActiveCommandID, "command_actor": actor}).Error
	})
	return &item, err
}

func (r *BuildRepository) EndReview(ctx context.Context, item *models.ReviewItem, status, elementID string, content []byte, callErr error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := LockAdmission(tx, item.TenantID, false); err != nil {
			return err
		}
		if _, err := lockTask(tx, item.TaskID, item.GraphID, item.TenantID); err != nil {
			return err
		}
		var current models.ReviewItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND graph_id = ? AND tenant_id = ?", item.ID, item.GraphID, item.TenantID).First(&current).Error; err != nil {
			return err
		}
		if current.ActiveCommandID != item.ActiveCommandID || current.CommandActor != item.CommandActor || current.Status != models.ReviewStatusPending {
			return commonapi.ErrConflict
		}
		var pending int64
		if err := tx.Model(&models.GraphOperation{}).Where("tenant_id = ? AND graph_id = ? AND command_id = ? AND state <> ?", item.TenantID, item.GraphID, item.ActiveCommandID, models.GraphOperationKnown).Count(&pending).Error; err != nil {
			return err
		}
		if pending > 0 {
			return ErrGraphAdmissionClosed
		}
		fields := map[string]interface{}{"active_command_id": "", "command_actor": 0}
		if callErr == nil {
			now, err := execution.DatabaseNow(tx)
			if err != nil {
				return err
			}
			fields["status"], fields["element_id"], fields["reviewed_by"], fields["reviewed_at"] = status, elementID, item.CommandActor, now
			if content != nil {
				fields["final_content"] = content
			}
		}
		return tx.Model(&current).Updates(fields).Error
	})
}

func (r *BuildRepository) BeginCommandOperation(ctx context.Context, op *models.GraphOperation) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := validateOperation(*op); err != nil {
			return err
		}
		if op.RunnerID == "" || op.LeaseTokenDigest != "" || op.ExecutionID != "" || op.CommandID == "" || op.ID == "" || op.TargetDigest == "" {
			return commonapi.ErrConflict
		}
		if err := LockAdmission(tx, op.TenantID, false); err != nil {
			return err
		}
		if err := CheckGraphOpen(tx, op.GraphID, op.TenantID); err != nil {
			return err
		}
		var kg models.KnowledgeGraph
		if err := tx.Where("id = ? AND tenant_id = ?", op.GraphID, op.TenantID).First(&kg).Error; err != nil {
			return err
		}
		if !operationMatchesGraph(*op, kg, 0) {
			return commonapi.ErrConflict
		}

		if op.ReviewItemID > 0 {
			var item models.ReviewItem
			if err := tx.Where("id = ? AND tenant_id = ? AND graph_id = ? AND active_command_id = ? AND status = ?", op.ReviewItemID, op.TenantID, op.GraphID, op.CommandID, models.ReviewStatusPending).First(&item).Error; err != nil {
				return err
			}
		}
		now, err := execution.DatabaseNow(tx)
		if err != nil {
			return err
		}
		op.CreatedAt = now
		op.State = models.GraphOperationInFlight
		return tx.Create(op).Error
	})
}

func (r *BuildRepository) SettleCommandOperation(ctx context.Context, op models.GraphOperation, digest string, known bool) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := LockAdmission(tx, op.TenantID, false); err != nil {
			return err
		}
		now, err := execution.DatabaseNow(tx)
		if err != nil {
			return err
		}
		state := models.GraphOperationUncertain
		if known && !validDigest(digest) {
			return commonapi.ErrConflict
		}
		if known && digest != "" {
			state = models.GraphOperationKnown
		}
		result := tx.Model(&models.GraphOperation{}).Where("id = ? AND tenant_id = ? AND graph_id = ? AND command_id = ? AND execution_id = '' AND state = ?", op.ID, op.TenantID, op.GraphID, op.CommandID, models.GraphOperationInFlight).Updates(map[string]interface{}{"state": state, "settled_at": now, "evidence_digest": digest})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return commonapi.ErrConflict
		}
		return nil
	})
}

// AppendLateEvidence deliberately cannot update operation state or execution
// results. A local return is evidence to retain, not permission to clear a hold.
func (r *BuildRepository) AppendLateEvidence(ctx context.Context, op models.GraphOperation, digest string) error {
	lease, ok := execution.LeaseFromContext(ctx)
	if !ok || lease.ExecutionID != op.ExecutionID || lease.TenantID != int(op.TenantID) || lease.Attempt != op.Attempt || lease.Owner != op.RunnerID || tokenDigest(lease.Token) != op.LeaseTokenDigest || !validDigest(digest) {
		return commonapi.ErrBadRequest
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var e execution.TaskExecution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("execution_id = ? AND tenant_id = ? AND module = ? AND task_type = ? AND attempt = ?", op.ExecutionID, op.TenantID, execution.ModuleGraph, execution.TaskTypeKGBuild, op.Attempt).First(&e).Error; err != nil {
			return err
		}
		if err := LockAdmission(tx, op.TenantID, false); err != nil {
			return err
		}
		result := tx.Model(&models.GraphOperation{}).Where("id = ? AND execution_id = ? AND tenant_id = ? AND graph_id = ? AND attempt = ? AND target_digest = ? AND runner_id = ? AND lease_token_digest = ? AND request_digest = ? AND state IN ? AND late_evidence_digest = ''", op.ID, op.ExecutionID, op.TenantID, op.GraphID, op.Attempt, op.TargetDigest, op.RunnerID, op.LeaseTokenDigest, op.RequestDigest, []string{models.GraphOperationInFlight, models.GraphOperationUncertain}).Update("late_evidence_digest", digest)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return commonapi.ErrConflict
		}
		return nil
	})
}
