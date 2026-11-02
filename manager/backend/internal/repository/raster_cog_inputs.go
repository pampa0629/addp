package repository

import (
	"context"
	"encoding/json"
	"reflect"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const rasterInputNamespace = "source_inputs"

func RasterCOGInputPreparations(metadata commonModels.JSONMap, tenantID uint) ([]FileInputPreparation, error) {
	raw, found := metadata[rasterInputNamespace]
	if !found {
		return nil, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, commonAPI.ErrConflict
	}
	var preparations []FileInputPreparation
	if json.Unmarshal(encoded, &preparations) != nil || preparations == nil || len(preparations) > 200 {
		return nil, commonAPI.ErrConflict
	}
	seen := map[string]bool{}
	for _, p := range preparations {
		if p.ValidateForTask(tenantID, commonExecution.TaskTypeRasterCOGGeneration) != nil || seen[p.PreparationID] {
			return nil, commonAPI.ErrConflict
		}
		seen[p.PreparationID] = true
	}
	return preparations, nil
}

// The COG result owns its finite input journal. The execution row remains the
// lease/Actor authority; no source credentials enter this metadata.
func (r *RasterCOGRepository) PrepareExecutionInputs(ctx context.Context, resultID uint, preparation FileInputPreparation) error {
	lease, ok := commonExecution.LeaseFromContext(ctx)
	if !ok || lease.TenantID <= 0 || preparation.ExecutionID != lease.ExecutionID || preparation.Attempt != lease.Attempt || preparation.Released || preparation.Watch || preparation.Cleaned || preparation.ValidateForTask(uint(lease.TenantID), commonExecution.TaskTypeRasterCOGGeneration) != nil {
		return commonAPI.ErrConflict
	}
	return r.changeExecutionInputs(ctx, resultID, func(current []FileInputPreparation) ([]FileInputPreparation, error) {
		for _, p := range current {
			if p.PreparationID == preparation.PreparationID || !p.Cleaned {
				return nil, commonAPI.ErrConflict
			}
		}
		if len(current) >= 200 {
			return nil, commonAPI.ErrConflict
		}
		preparation.Objects = append([]FileInputObject{}, preparation.Objects...)
		return append(current, preparation), nil
	})
}

func (r *RasterCOGRepository) FinishExecutionInputs(ctx context.Context, resultID uint, preparation FileInputPreparation, settled, cleaned bool) error {
	lease, ok := commonExecution.LeaseFromContext(ctx)
	if !ok || lease.TenantID <= 0 || preparation.ExecutionID != lease.ExecutionID || preparation.Attempt != lease.Attempt || preparation.ValidateForTask(uint(lease.TenantID), commonExecution.TaskTypeRasterCOGGeneration) != nil {
		return commonAPI.ErrConflict
	}
	return r.changeExecutionInputs(ctx, resultID, func(current []FileInputPreparation) ([]FileInputPreparation, error) {
		for index := range current {
			p := &current[index]
			if p.PreparationID != preparation.PreparationID {
				continue
			}
			if p.ExecutionID != lease.ExecutionID || p.Attempt != lease.Attempt || p.Released || !reflect.DeepEqual(p.Objects, preparation.Objects) {
				return nil, commonAPI.ErrConflict
			}
			p.Released, p.Watch, p.Cleaned = true, !settled, settled && cleaned
			return current, nil
		}
		return nil, commonAPI.ErrConflict
	})
}

func (r *RasterCOGRepository) changeExecutionInputs(ctx context.Context, resultID uint, change func([]FileInputPreparation) ([]FileInputPreparation, error)) error {
	lease, ok := commonExecution.LeaseFromContext(ctx)
	if !ok || lease.TenantID <= 0 || resultID == 0 {
		return commonAPI.ErrConflict
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := commonExecution.UpdateWithLease(ctx, tx, lease, nil); err != nil {
			return err
		}
		var execution commonExecution.TaskExecution
		if err := tx.Where("execution_id=? AND tenant_id=? AND module=? AND task_type=?", lease.ExecutionID, lease.TenantID, commonExecution.ModuleManager, commonExecution.TaskTypeRasterCOGGeneration).First(&execution).Error; err != nil {
			return err
		}
		var item models.RasterCOG
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND tenant_id=? AND last_execution_id=? AND status=?", resultID, lease.TenantID, lease.ExecutionID, models.RasterCOGStatusBuilding).First(&item).Error; err != nil {
			return err
		}
		preparations, err := RasterCOGInputPreparations(item.Metadata, item.TenantID)
		if err != nil {
			return err
		}
		preparations, err = change(preparations)
		if err != nil {
			return err
		}
		metadata := item.Metadata.Clone()
		if metadata == nil {
			metadata = commonModels.JSONMap{}
		}
		metadata[rasterInputNamespace] = preparations
		return tx.Model(&item).UpdateColumn("metadata", metadata).Error
	})
}

// Preserve the complete owned journal when generation replaces format facts.
func (r *RasterCOGRepository) MergeExecutionInputMetadata(ctx context.Context, resultID, tenantID uint, metadata commonModels.JSONMap) (commonModels.JSONMap, error) {
	var item models.RasterCOG
	if err := r.db.WithContext(ctx).Where("id=? AND tenant_id=?", resultID, tenantID).First(&item).Error; err != nil {
		return nil, err
	}
	preparations, err := RasterCOGInputPreparations(item.Metadata, tenantID)
	if err != nil {
		return nil, err
	}
	result := metadata.Clone()
	if result == nil {
		result = commonModels.JSONMap{}
	}
	if preparations != nil {
		result[rasterInputNamespace] = preparations
	}
	return result, nil
}
