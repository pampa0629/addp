package service

import (
	"context"
	"errors"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/security/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ReconcileGraphOwnerProjections installs the new consumer once, preserving
// existing owners, deadlines, released resources and monotonic feed positions.
func ReconcileGraphOwnerProjections(ctx context.Context, db *gorm.DB, now time.Time) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []models.ProtectionEnrollment
		if err := tx.Where("state <> ?", models.EnrollmentStateReleased).Order("tenant_id ASC, id ASC").Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			enrollment, err := lockProtectionEnrollment(tx, row.TenantID, row.ID)
			if err != nil {
				return err
			}
			if enrollment.State == models.EnrollmentStateReleased {
				continue
			}
			var record models.ProtectionProjectionRecord
			err = tx.Where("tenant_id = ? AND enrollment_id = ? AND consumer_owner = ?", row.TenantID, row.ID, graphProtectionOwner).First(&record).Error
			if err == nil {
				continue
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err := installEnrollmentOwnerProjection(tx, enrollment, graphProtectionOwner, now); err != nil {
				return err
			}
			if enrollment.State == models.EnrollmentStateReleasing {
				if err := tx.Where("tenant_id = ? AND enrollment_id = ? AND consumer_owner = ?", row.TenantID, row.ID, graphProtectionOwner).First(&record).Error; err != nil {
					return err
				}
				revision, err := nextRevision(record.Revision)
				if err != nil {
					return err
				}
				change := models.ProtectionProjectionChange{ChangeID: uuid.NewString(), TenantID: row.TenantID, EnrollmentID: row.ID, ConsumerOwner: graphProtectionOwner, Operation: dataprotection.ChangeOperationRelease, ProjectionID: record.ID, Revision: revision, TargetOwner: row.TargetOwner, TargetType: row.TargetType, TargetIdentity: row.TargetIdentity, CreatedAt: now}
				if err := tx.Create(&change).Error; err != nil {
					return err
				}
				if err := tx.Model(&record).Updates(map[string]any{"revision": revision, "release_sequence": change.Sequence, "updated_at": now}).Error; err != nil {
					return err
				}
			} else if enrollment.LatestSourceSnapshotHash != "" {
				if err := compileProtectionProjections(tx, enrollment, enrollment.LatestSourceSnapshotHash, now, []string{graphProtectionOwner}); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
