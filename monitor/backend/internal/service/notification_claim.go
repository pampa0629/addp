package service

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// claimNotificationDelivery returns the stored delivery after taking its lease.
// Keep the row lock through the update and readback, and release it before sending.
func claimNotificationDelivery[T any](ctx context.Context, db *gorm.DB, now, expires time.Time, leaseFields map[string]any, channels []string) (*T, error) {
	var claimed *T
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var delivery T
		candidates := tx.Model(&delivery).Where(
			"((status = 'pending' AND next_attempt_at <= ?) OR (status = 'delivering' AND lease_expires_at <= ?))", now, now)
		if len(channels) > 0 {
			candidates = candidates.Where("channel IN ?", channels)
		}
		// An expired lease may have no next attempt. Preserve NULLS FIRST on both databases.
		candidate := candidates.Select("id").Order("next_attempt_at IS NOT NULL, next_attempt_at, id").
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Limit(1).Find(&delivery)
		if candidate.Error != nil || candidate.RowsAffected == 0 {
			return candidate.Error
		}
		leaseFields["status"] = "delivering"
		leaseFields["lease_expires_at"] = expires
		leaseFields["attempt_count"] = gorm.Expr("attempt_count + 1")
		if err := tx.Model(&delivery).UpdateColumns(leaseFields).Error; err != nil {
			return err
		}
		if err := tx.First(&delivery).Error; err != nil {
			return err
		}
		claimed = &delivery
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}
