package repository

import (
	"context"
	"reflect"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PrepareExecutionInputs commits the complete finite manifest before any PUT.
func (r *Model3DGLBRepository) PrepareExecutionInputs(ctx context.Context, resultID uint, preparation FileInputPreparation) error {
	lease, ok := commonExecution.LeaseFromContext(ctx)
	if !ok || lease.TenantID <= 0 || preparation.ExecutionID != lease.ExecutionID || preparation.Attempt != lease.Attempt || preparation.Released || preparation.Watch || preparation.Cleaned || preparation.ValidateForTask(uint(lease.TenantID), commonExecution.TaskTypeModel3DGLBGeneration) != nil {
		return commonAPI.ErrConflict
	}
	return r.changeExecutionInputs(ctx, resultID, func(state *model3DGLBOutputState) error {
		if state.Pending == nil || state.Pending.ExecutionID != lease.ExecutionID {
			return commonAPI.ErrConflict
		}
		for _, existing := range state.InputPreparations {
			if existing.PreparationID == preparation.PreparationID {
				return commonAPI.ErrConflict
			}
		}
		preparation.Objects = append([]FileInputObject(nil), preparation.Objects...)
		state.InputPreparations = append(state.InputPreparations, preparation)
		return nil
	})
}

// FinishExecutionInputs acknowledges only the same live attempt's preparation.
// A stale worker cannot declare a crashed upload settled or erase its evidence.
func (r *Model3DGLBRepository) FinishExecutionInputs(ctx context.Context, resultID uint, preparation FileInputPreparation, writesSettled, cleaned bool) error {
	lease, ok := commonExecution.LeaseFromContext(ctx)
	if !ok || preparation.ExecutionID != lease.ExecutionID || preparation.Attempt != lease.Attempt || lease.TenantID <= 0 || preparation.ValidateForTask(uint(lease.TenantID), commonExecution.TaskTypeModel3DGLBGeneration) != nil {
		return commonAPI.ErrConflict
	}
	return r.changeExecutionInputs(ctx, resultID, func(state *model3DGLBOutputState) error {
		for i := range state.InputPreparations {
			current := &state.InputPreparations[i]
			if current.PreparationID != preparation.PreparationID {
				continue
			}
			if current.ExecutionID != lease.ExecutionID || current.Attempt != lease.Attempt || !reflect.DeepEqual(current.Objects, preparation.Objects) || current.Released {
				return commonAPI.ErrConflict
			}
			current.Released, current.Watch, current.Cleaned = true, !writesSettled, cleaned && writesSettled
			return nil
		}
		return commonAPI.ErrConflict
	})
}

func (r *Model3DGLBRepository) changeExecutionInputs(ctx context.Context, resultID uint, change func(*model3DGLBOutputState) error) error {
	lease, ok := commonExecution.LeaseFromContext(ctx)
	if !ok || lease.TenantID <= 0 {
		return commonAPI.ErrConflict
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGLBExecutionSource(ctx, tx, lease, 0); err != nil {
			return err
		}
		var item models.Model3DGLB
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", resultID, lease.TenantID).First(&item).Error; err != nil {
			return err
		}
		if !Model3DGLBUsesExecutionOutput(&item) {
			return commonAPI.ErrConflict
		}
		if err := commonExecution.UpdateWithLease(ctx, tx, lease, nil); err != nil {
			return err
		}
		if err := hydrateModel3DGLBOutputMetadata(ctx, tx, &item); err != nil {
			return err
		}
		state, err := readModel3DGLBOutputState(item.Metadata)
		if err != nil {
			return err
		}
		if err := change(&state); err != nil {
			return err
		}
		return tx.Model(&item).UpdateColumn("metadata", model3DGLBOutputMetadata(item.Metadata, state)).Error
	})
}
