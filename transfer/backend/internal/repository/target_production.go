package repository

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	commonapi "github.com/addp/common/api"
	plugin "github.com/addp/common/engine/plugin"
	execution "github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *TaskRepository) BeginTargetProduction(ctx context.Context, lease execution.Lease, locator string) error {
	return r.changeTargetProduction(ctx, lease, locator, func(item execution.TaskExecution, now time.Time) (models.TargetProduction, error) {
		if _, exists := item.Metadata[models.TargetProductionMetadataKey]; exists {
			return models.TargetProduction{}, fmt.Errorf("%w: target production already started; do not replay target operations", commonapi.ErrConflict)
		}
		return models.TargetProduction{SchemaVersion: models.TargetProductionSchema, TenantID: item.TenantID, ExecutionID: item.ExecutionID, Attempt: item.Attempt, RetryOfExecutionID: item.RetryOfExecutionID, TargetLocator: locator, Phase: models.TargetProductionPending, StartedAt: now}, nil
	})
}

func (r *TaskRepository) RecordTargetPreparation(ctx context.Context, lease execution.Lease, locator string, preparation plugin.TableTargetPreparation) error {
	return r.changeTargetProduction(ctx, lease, locator, func(item execution.TaskExecution, now time.Time) (models.TargetProduction, error) {
		record, err := targetProductionForExecution(item, locator)
		if err != nil {
			return record, err
		}
		switch preparation.Disposition {
		case plugin.TableTargetCreated, plugin.TableTargetRecreated, plugin.TableTargetReused:
		default:
			return record, fmt.Errorf("unavailable target preparation cannot be recorded")
		}
		if record.Phase != models.TargetProductionPending {
			if record.Preparation != preparation.Disposition {
				return record, fmt.Errorf("%w: target preparation is immutable", commonapi.ErrConflict)
			}
			return record, nil
		}
		record.Phase, record.Preparation, record.PreparedAt = models.TargetProductionPrepared, preparation.Disposition, &now
		return record, nil
	})
}

func (r *TaskRepository) RecordTargetCommit(ctx context.Context, lease execution.Lease, locator string, sources *plugin.QueryReadSet, createdAndCommitted bool) error {
	if err := execution.ValidateProductionReadSet(sources); err != nil {
		return err
	}
	sources = sources.Clone()
	return r.changeTargetProduction(ctx, lease, locator, func(item execution.TaskExecution, now time.Time) (models.TargetProduction, error) {
		record, err := targetProductionForExecution(item, locator)
		if err != nil {
			return record, err
		}
		if record.Phase == models.TargetProductionPending {
			return record, fmt.Errorf("%w: target preparation is not recorded", commonapi.ErrConflict)
		}
		if createdAndCommitted && record.Preparation != plugin.TableTargetCreated {
			return record, fmt.Errorf("creation evidence cannot describe an existing target")
		}
		if record.Phase == models.TargetProductionPrepared {
			record.Phase, record.CommittedAt = models.TargetProductionCommitted, &now
			record.SourceReadSet = sources
			record.CreatedAndCommitted = createdAndCommitted
		} else if !reflect.DeepEqual(record.SourceReadSet, sources) || record.CreatedAndCommitted != createdAndCommitted {
			return record, fmt.Errorf("%w: committed production facts are immutable", commonapi.ErrConflict)
		}
		return record, nil
	})
}

func targetProductionForExecution(item execution.TaskExecution, locator string) (models.TargetProduction, error) {
	record, err := models.DecodeTargetProduction(item.Metadata[models.TargetProductionMetadataKey])
	if err != nil {
		return record, err
	}
	if record.TenantID != item.TenantID || record.ExecutionID != item.ExecutionID || record.Attempt != item.Attempt || record.TargetLocator != locator || !sameProductionRetry(record.RetryOfExecutionID, item.RetryOfExecutionID) {
		return record, fmt.Errorf("%w: target production identity changed", commonapi.ErrConflict)
	}
	return record, nil
}

func sameProductionRetry(left, right *string) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}

func (r *TaskRepository) changeTargetProduction(ctx context.Context, lease execution.Lease, locator string, change func(execution.TaskExecution, time.Time) (models.TargetProduction, error)) error {
	loc, err := resourcetree.ParseURI(locator)
	if err != nil || loc == nil || loc.EngineID == 0 || loc.Type != resourcetree.TypeTable || len(loc.Path) != 2 || loc.ItemID != nil || loc.NodeID != nil || loc.ToURI() != locator {
		return fmt.Errorf("exact canonical native table locator is required")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item execution.TaskExecution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("execution_id=? AND tenant_id=? AND module=? AND task_type=? AND execution_boundary=? AND status=? AND attempt=? AND lease_token=? AND lease_owner=?", lease.ExecutionID, lease.TenantID, execution.ModuleTransfer, execution.TaskTypeSync, execution.ExecutionBoundaryBounded, execution.ExecutionStatusRunning, lease.Attempt, lease.Token, lease.Owner).First(&item).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: target production execution is not owned", commonapi.ErrConflict)
			}
			return err
		}
		now := time.Now().UTC()
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
				return err
			}
		}
		if item.LeaseExpiresAt == nil || !item.LeaseExpiresAt.After(now) {
			return fmt.Errorf("%w: target production lease expired", commonapi.ErrConflict)
		}
		record, err := change(item, now)
		if err != nil {
			return err
		}
		encoded, err := models.EncodeTargetProduction(record)
		if err != nil {
			return err
		}
		if item.Metadata == nil {
			item.Metadata = commonmodels.JSONMap{}
		}
		item.Metadata[models.TargetProductionMetadataKey] = encoded
		guarded := tx
		if tx.Dialector.Name() == "postgres" {
			guarded = tx.Where("lease_expires_at > clock_timestamp()")
		}
		return execution.UpdateWithLease(ctx, guarded, lease, map[string]interface{}{"metadata": item.Metadata})
	})
}
