package repository

import (
	"context"
	"gorm.io/gorm"
)

// ContentInventoryGuard observes the managed outlet and its append-only delivery
// ledger in one database statement. It is not an external index revision. Ledger
// retention must replace this count with a monotonic revision before deleting rows.
type ContentInventoryGuard struct {
	Epoch      string
	EndpointID string
	Registered int64
}

func (r *ContentIndexDeliveryRepository) ObserveInventoryGuard(ctx context.Context, epoch string) (ContentInventoryGuard, error) {
	var row struct {
		ContentInventoryGuard
		Configured bool
		Isolated   bool
		Pending    int64
	}
	if r == nil || r.db == nil || epoch == "" {
		return row.ContentInventoryGuard, ErrContentIndexIsolated
	}
	result := r.db.WithContext(ctx).Raw(`SELECT o.epoch, o.endpoint_id, o.configured, o.isolated,
 (SELECT count(*) FROM manager.content_index_deliveries d WHERE d.index_name=o.index_name) AS registered,
 (SELECT count(*) FROM manager.content_index_deliveries d WHERE d.index_name=o.index_name AND d.status IN ('queued','submitting','submitted','unknown')) AS pending
 FROM manager.content_index_outlets o WHERE o.index_name=? AND o.epoch=? AND o.cleanup_hold_id=''`, r.index, epoch).Scan(&row)
	if result.Error != nil {
		return row.ContentInventoryGuard, result.Error
	}
	if result.RowsAffected != 1 || !row.Configured || row.Isolated || row.Pending != 0 || row.EndpointID == "" {
		return row.ContentInventoryGuard, ErrContentIndexIsolated
	}
	return row.ContentInventoryGuard, nil
}

// VerifyInventoryGuard serializes assessment commit with every managed index
// registration. External inventory itself was read without holding this lock.
func (r *ContentIndexDeliveryRepository) VerifyInventoryGuard(ctx context.Context, tx *gorm.DB, expected ContentInventoryGuard) error {
	if _, err := r.lock(tx); err != nil {
		return err
	}
	current, err := NewContentIndexDeliveryRepository(tx, r.index).ObserveInventoryGuard(ctx, expected.Epoch)
	if err != nil {
		return err
	}
	if current != expected {
		return ErrContentIndexIsolated
	}
	return nil
}
