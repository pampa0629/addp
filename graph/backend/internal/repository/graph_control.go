package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	"github.com/addp/graph/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrGraphVersionConflict = fmt.Errorf("%w: graph resource version changed", commonapi.ErrConflict)

var ErrGraphReconciliationIncomplete = fmt.Errorf("%w: graph reconciliation evidence is incomplete", commonapi.ErrConflict)
var ErrGraphPhysicalTargetUnverified = fmt.Errorf("%w: original physical graph target is not verifiable", commonapi.ErrConflict)

func graphQuiescent(tx *gorm.DB, kg *models.KnowledgeGraph) error {
	return CheckCleanupQuiescence(tx, kg, "")
}

// CheckCleanupQuiescence uses ordinary reads while the caller holds G. It never
// takes another execution lock; only this Cleanup C may be excluded.
func CheckCleanupQuiescence(tx *gorm.DB, kg *models.KnowledgeGraph, cleanupExecutionID string) error {
	var count int64
	tasks := tx.Model(&models.BuildTask{}).Select("CAST(id AS TEXT)").Where("tenant_id = ? AND graph_id = ?", kg.TenantID, kg.ID)
	if err := tx.Model(&execution.TaskExecution{}).Where("tenant_id = ? AND module = ? AND task_type = ? AND source_task_id IN (?) AND status IN ?", kg.TenantID, execution.ModuleGraph, execution.TaskTypeKGBuild, tasks, []string{execution.ExecutionStatusPending, execution.ExecutionStatusRunning}).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return ErrGraphReconciliationIncomplete
	}
	if kg.AdmissionExecutionID != "" && kg.AdmissionExecutionID != cleanupExecutionID {
		if err := tx.Model(&execution.TaskExecution{}).Where("tenant_id = ? AND execution_id = ? AND status IN ?", kg.TenantID, kg.AdmissionExecutionID, []string{execution.ExecutionStatusPending, execution.ExecutionStatusRunning}).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return ErrGraphReconciliationIncomplete
		}
	}
	if err := tx.Model(&models.ReviewItem{}).Where("tenant_id = ? AND graph_id = ? AND active_command_id <> ''", kg.TenantID, kg.ID).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return ErrGraphReconciliationIncomplete
	}
	if err := tx.Model(&models.GraphOperation{}).Where("tenant_id = ? AND graph_id = ? AND state <> ?", kg.TenantID, kg.ID, models.GraphOperationKnown).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return ErrGraphReconciliationIncomplete
	}
	return nil
}

func (r *KnowledgeGraphRepository) Reconcile(ctx context.Context, id, tenant, actor uint, version int64) (*models.KnowledgeGraph, error) {
	if actor == 0 {
		return nil, commonapi.ErrForbidden
	}
	var kg models.KnowledgeGraph
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := LockAdmission(tx, tenant, true); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", id, tenant).First(&kg).Error; err != nil {
			return err
		}
		if version <= 0 || kg.Version != version {
			return ErrGraphVersionConflict
		}
		if !kg.AdmissionClosed {
			return ErrGraphReconciliationIncomplete
		}
		if err := graphQuiescent(tx, &kg); err != nil {
			return err
		}
		var ops []models.GraphOperation
		if err := tx.Where("tenant_id = ? AND graph_id = ?", tenant, id).Order("id ASC").Find(&ops).Error; err != nil {
			return err
		}
		for _, op := range ops {
			if validateOperation(op) != nil || op.RunnerID == "" || op.State != models.GraphOperationKnown || !validDigest(op.EvidenceDigest) || op.SettledAt == nil {
				return ErrGraphReconciliationIncomplete
			}
		}
		evidence, err := json.Marshal(ops)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(evidence)
		proof := hex.EncodeToString(digest[:])
		// Only completely known effects are consumed in the initial contract.
		// Late local returns or a human confirmation cannot clear unknown calls.
		now, err := execution.DatabaseNow(tx)
		if err != nil {
			return err
		}
		if err := tx.Model(&kg).Updates(map[string]interface{}{"admission_closed": true, "admission_reconciled_at": now, "version": version + 1, "admission_reconciled_by": actor, "admission_reconciliation_digest": proof, "updated_at": now}).Error; err != nil {
			return err
		}
		kg.AdmissionClosed = true
		kg.AdmissionReconciledAt = &now
		kg.Version = version + 1
		kg.AdmissionReconciledBy = actor
		kg.AdmissionReconciliationDigest = proof
		return nil
	})
	return &kg, err
}

// Reopen refuses targets until the native Provider can prove the original
// physical identity. No caller-supplied identity or operator confirmation is proof.
func (r *KnowledgeGraphRepository) Reopen(ctx context.Context, id, tenant uint, version int64) (*models.KnowledgeGraph, error) {
	var kg models.KnowledgeGraph
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := LockAdmission(tx, tenant, true); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", id, tenant).First(&kg).Error; err != nil {
			return err
		}
		if version <= 0 || version != kg.Version {
			return ErrGraphVersionConflict
		}
		if !kg.AdmissionClosed || kg.AdmissionReconciledAt == nil {
			return ErrGraphReconciliationIncomplete
		}
		if err := graphQuiescent(tx, &kg); err != nil {
			return err
		}
		return ErrGraphPhysicalTargetUnverified
	})
	return &kg, err
}

func (r *KnowledgeGraphRepository) FillAdmissionView(ctx context.Context, kg *models.KnowledgeGraph) error {
	var count int64
	if err := r.db.WithContext(ctx).Model(&models.GraphOperation{}).Where("tenant_id = ? AND graph_id = ? AND state <> ?", kg.TenantID, kg.ID, models.GraphOperationKnown).Count(&count).Error; err != nil {
		return err
	}
	kg.Admission = &models.GraphAdmissionView{Closed: kg.AdmissionClosed, HasUnresolvedOperations: count != 0}
	return nil
}

// UpdateDefinition cannot change lifecycle, admission, or the physical target.
func (r *KnowledgeGraphRepository) UpdateDefinition(ctx context.Context, id, tenant uint, req *models.UpdateKnowledgeGraphRequest) (*models.KnowledgeGraph, error) {
	var kg models.KnowledgeGraph
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := LockAdmission(tx, tenant, false); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", id, tenant).First(&kg).Error; err != nil {
			return err
		}
		if req.Version <= 0 || req.Version != kg.Version {
			return ErrGraphVersionConflict
		}
		if err := CheckGraphOpen(tx, id, tenant); err != nil {
			return err
		}
		fields := map[string]interface{}{"version": kg.Version + 1}
		if req.Name != "" {
			fields["name"] = req.Name
		}
		if req.Description != nil {
			fields["description"] = *req.Description
		}
		if req.IsPublic != nil {
			fields["is_public"] = *req.IsPublic
		}
		if err := tx.Model(&kg).Updates(fields).Error; err != nil {
			return err
		}
		return tx.Preload("Ontology").First(&kg, kg.ID).Error
	})
	if err == nil {
		err = r.FillAdmissionView(ctx, &kg)
	}
	return &kg, err
}

func (r *KnowledgeGraphRepository) DeleteDefinition(ctx context.Context, id, tenant uint, version int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := LockAdmission(tx, tenant, true); err != nil {
			return err
		}
		var kg models.KnowledgeGraph
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", id, tenant).First(&kg).Error; err != nil {
			return err
		}
		if version <= 0 || version != kg.Version {
			return ErrGraphVersionConflict
		}
		if err := CheckGraphOpen(tx, id, tenant); err != nil {
			return err
		}
		if err := graphQuiescent(tx, &kg); err != nil {
			return err
		}
		for _, model := range []interface{}{&models.ReviewItem{}, &models.BuildMaterial{}, &models.BuildTask{}} {
			if err := tx.Where("tenant_id = ? AND graph_id = ?", tenant, id).Delete(model).Error; err != nil {
				return err
			}
		}
		// Operation and execution controls remain audit evidence, never deletion proof.
		return tx.Delete(&kg).Error
	})
}
