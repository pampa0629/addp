package repository

import (
	"context"
	"fmt"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"strings"
)

// BeginCleanupIndex is called inside Run.Effect: C -> outlet -> private facts.
// It never calls a provider or refreshes a previous operation's authority.
func (r *ContentIndexDeliveryRepository) BeginCleanupIndex(ctx context.Context, tx *gorm.DB, lease execution.Lease, guard ContentInventoryGuard, digest string, engineIDs []uint) (*models.CleanupIndexOperation, error) {
	item, err := execution.LockActiveLease(ctx, tx, lease)
	if err != nil {
		return nil, err
	}
	input, err := execution.CleanupRequestFromExecution(*item)
	if err != nil || item.Module != events.ModuleManager || input.Action != events.CleanupActionExecute || !events.CleanupDigestValid(digest) || len(engineIDs) == 0 {
		return nil, ErrContentIndexDeliveryConflict
	}
	state, err := r.lock(tx)
	if err != nil {
		return nil, err
	}
	if state.Epoch != guard.Epoch || state.EndpointID != guard.EndpointID || !state.Configured || state.CleanupHoldID != "" {
		return nil, ErrContentIndexIsolated
	}
	observed, err := NewContentIndexDeliveryRepository(tx, r.index).ObserveInventoryGuard(ctx, guard.Epoch)
	if err != nil || observed != guard {
		return nil, ErrContentIndexIsolated
	}
	operation := models.CleanupIndexOperation{ID: uuid.NewString(), IndexName: r.index, TenantID: int64(item.TenantID), ExecutionID: item.ExecutionID, Attempt: item.Attempt, RequestDigest: input.Digest, Epoch: state.Epoch, EndpointID: state.EndpointID, InventoryDigest: digest, State: "prepared"}
	if err := tx.Create(&operation).Error; err != nil {
		return nil, err
	}
	seen := map[uint]bool{}
	for _, engineID := range engineIDs {
		if engineID == 0 || seen[engineID] || input.Context.EngineID != 0 && input.Context.EngineID != engineID {
			return nil, ErrContentIndexDeliveryConflict
		}
		seen[engineID] = true
		target := models.CleanupIndexTarget{IndexName: r.index, TenantID: operation.TenantID, EngineID: engineID, OperationID: operation.ID}
		if err := tx.Create(&target).Error; err != nil {
			return nil, err
		}
	}
	if err := tx.Model(state).Updates(map[string]any{"cleanup_hold_id": operation.ID, "isolated": true}).Error; err != nil {
		return nil, err
	}
	return &operation, nil
}

// RequireNoCleanupHold protects index initialization as well as reopening.
func (r *ContentIndexDeliveryRepository) RequireNoCleanupHold(ctx context.Context, epoch string) error {
	state, err := r.State(ctx, epoch)
	if err != nil {
		return err
	}
	if state.CleanupHoldID != "" {
		return ErrContentIndexIsolated
	}
	return nil
}

// QueueCleanupDelete authorizes exactly one pre-registered C-bound deletion.
// Generic maintenance workers must never dispatch this delivery.
func (r *ContentIndexDeliveryRepository) QueueCleanupDelete(ctx context.Context, tx *gorm.DB, lease execution.Lease, operationID string, engineID uint) (*models.ContentIndexDelivery, error) {
	item, err := execution.LockActiveLease(ctx, tx, lease)
	if err != nil {
		return nil, err
	}
	state, err := r.lock(tx)
	if err != nil {
		return nil, err
	}
	var op models.CleanupIndexOperation
	if err := tx.Where("id=? AND index_name=? AND tenant_id=? AND execution_id=? AND attempt=? AND state=?", operationID, r.index, item.TenantID, item.ExecutionID, item.Attempt, "prepared").First(&op).Error; err != nil {
		return nil, ErrContentIndexDeliveryConflict
	}
	if state.CleanupHoldID != op.ID || state.Epoch != op.Epoch || state.EndpointID != op.EndpointID {
		return nil, ErrContentIndexIsolated
	}
	var target models.CleanupIndexTarget
	if err := tx.Where("index_name=? AND tenant_id=? AND engine_id=? AND operation_id=?", r.index, item.TenantID, engineID, op.ID).First(&target).Error; err != nil {
		return nil, ErrContentIndexDeliveryConflict
	}
	var pending int64
	if err := tx.Model(&models.ContentIndexDelivery{}).Where("index_name=? AND status IN ?", r.index, unfinishedIndexDeliveries).Count(&pending).Error; err != nil {
		return nil, err
	}
	if pending != 0 {
		return nil, ErrContentIndexIsolated
	}
	filter := fmtCleanupIndexFilter(item.TenantID, engineID)
	var existing int64
	if err := tx.Model(&models.ContentIndexDelivery{}).Where("cleanup_operation_id=? AND filter=?", op.ID, filter).Count(&existing).Error; err != nil {
		return nil, err
	}
	if existing != 0 {
		return nil, ErrContentIndexDeliveryConflict
	}
	id := uuid.NewString()
	delivery := models.ContentIndexDelivery{ID: id, IndexName: r.index, TenantID: int64(item.TenantID), CleanupOperationID: op.ID, EngineIDs: []uint{engineID}, Kind: IndexDeliveryDelete, Filter: filter, EndpointID: op.EndpointID, TaskCorrelation: id, Status: IndexDeliverySubmitting}
	if err := tx.Create(&delivery).Error; err != nil {
		return nil, err
	}
	return &delivery, nil
}

func (r *ContentIndexDeliveryRepository) CompleteCleanupIndex(ctx context.Context, tx *gorm.DB, lease execution.Lease, operationID string) error {
	item, err := execution.LockActiveLease(ctx, tx, lease)
	if err != nil {
		return err
	}
	state, err := r.lock(tx)
	if err != nil {
		return err
	}
	var op models.CleanupIndexOperation
	if err := tx.Where("id=? AND index_name=? AND tenant_id=? AND execution_id=? AND attempt=? AND state=?", operationID, r.index, item.TenantID, item.ExecutionID, item.Attempt, "prepared").First(&op).Error; err != nil {
		return ErrContentIndexDeliveryConflict
	}
	if state.CleanupHoldID != op.ID || state.Epoch != op.Epoch || state.EndpointID != op.EndpointID {
		return ErrContentIndexIsolated
	}
	var targets []models.CleanupIndexTarget
	if err := tx.Where("operation_id=?", op.ID).Find(&targets).Error; err != nil {
		return err
	}
	if len(targets) == 0 {
		return ErrContentIndexDeliveryConflict
	}
	for _, target := range targets {
		var count int64
		if err := tx.Model(&models.ContentIndexDelivery{}).Where("cleanup_operation_id=? AND filter=? AND status=? AND endpoint_id=?", op.ID, fmtCleanupIndexFilter(item.TenantID, target.EngineID), IndexDeliverySucceeded, op.EndpointID).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return ErrContentIndexIsolated
		}
	}
	now, err := execution.DatabaseNow(tx)
	if err != nil {
		return err
	}
	if err := tx.Model(&op).Updates(map[string]any{"state": "completed", "settled_at": now}).Error; err != nil {
		return err
	}
	// Target guards deliberately remain: old callers cannot register fresh writes.
	return tx.Model(state).Updates(map[string]any{"cleanup_hold_id": "", "isolated": true}).Error
}

func fmtCleanupIndexFilter(tenant int, engine uint) string {
	return strings.Join([]string{fmt.Sprintf("tenant_id = %d", tenant), fmt.Sprintf("engine_id = %d", engine)}, " AND ")
}
