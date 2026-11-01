package service

import (
	"context"
	"fmt"

	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// RetireCollectionApproval runs once inside the Backend schema revision transaction.
// It retains definitions/history and never submits work. Engine facts belong to System.
func RetireCollectionApproval(ctx context.Context, tx *gorm.DB, readEngine func(context.Context, uint, uint) (*commonmodels.Engine, error)) error {
	if readEngine == nil {
		return fmt.Errorf("System engine facts reader is required")
	}
	var tasks []models.ScanTask
	if err := tx.WithContext(ctx).Order("tenant_id, engine_id, id").Find(&tasks).Error; err != nil {
		return err
	}
	type engineKey struct{ tenant, engine uint }
	engines := map[engineKey]*commonmodels.Engine{}
	for _, task := range tasks {
		key := engineKey{task.TenantID, task.EngineID}
		engine, ok := engines[key]
		if !ok {
			var err error
			engine, err = readEngine(ctx, key.tenant, key.engine)
			if err != nil {
				return fmt.Errorf("resolve scan task %d engine: %w", task.ID, err)
			}
			if engine == nil || engine.ID != key.engine || engine.TenantID == nil || *engine.TenantID != key.tenant {
				return fmt.Errorf("scan task %d engine identity mismatch", task.ID)
			}
			engines[key] = engine
		}
		if engine.EngineType != "postgresql" {
			continue
		}
		// Disable all old PG schedules, including unsupported deeper configurations.
		if err := tx.Model(&models.ScanTask{}).Where("id=? AND tenant_id=?", task.ID, task.TenantID).Updates(map[string]any{"enabled": false, "next_run_at": nil}).Error; err != nil {
			return err
		}
	}
	// Old admitted PG basic runs must stop at their next enumeration even if their
	// task definition has since changed or been deleted.
	var runs []execution.TaskExecution
	if err := tx.Where("module=? AND task_type=? AND execution_boundary=? AND status IN ? AND source_task_id IS NOT NULL", execution.ModuleMeta, execution.TaskTypeScan, execution.ExecutionBoundaryBounded, []string{execution.ExecutionStatusPending, execution.ExecutionStatusRunning}).Find(&runs).Error; err != nil {
		return err
	}
	owner := &ScanExecutionService{db: tx}
	for _, run := range runs {
		if run.ExecutionConfig["engine_type"] != "postgresql" || run.ExecutionConfig["scan_depth"] != "basic" {
			continue
		}
		if _, err := owner.cancelPGCollection(ctx, uint(run.TenantID), run.ExecutionID, commonmodels.JSONMap{"reason": "collection_approval_retired"}, func(context.Context, uint) error { return nil }); err != nil {
			return err
		}
	}
	return nil
}
