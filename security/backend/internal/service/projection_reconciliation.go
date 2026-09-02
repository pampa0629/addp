package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/security/internal/models"
	"gorm.io/gorm"
)

// ReconcileStructuredOwnerProjections upgrades existing structured
// enrollments whose Manager field projection predates the Develop, Service,
// or Transfer field-level executor. It is idempotent because only enrolling
// owner projections are eligible.
func ReconcileStructuredOwnerProjections(ctx context.Context, db *gorm.DB, now time.Time) (int, error) {
	if db == nil {
		return 0, errors.New("security projection reconciliation requires database")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	reconciled := 0
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var managerRecords []models.ProtectionProjectionRecord
		if err := tx.Where("consumer_owner = ? AND state = ?", "manager", dataprotection.ProjectionStateActive).Order("tenant_id ASC, enrollment_id ASC").Find(&managerRecords).Error; err != nil {
			return err
		}
		for _, managerRecord := range managerRecords {
			var managerProjection dataprotection.Projection
			if err := json.Unmarshal([]byte(managerRecord.ProjectionPayload), &managerProjection); err != nil {
				return err
			}
			if !projectionHasAction(managerProjection, managerPreviewAction) {
				continue
			}
			pendingOwners := make([]string, 0, 3)
			for _, owner := range []string{"develop", "service", "transfer"} {
				var ownerRecord models.ProtectionProjectionRecord
				err := tx.Where("tenant_id = ? AND enrollment_id = ? AND consumer_owner = ?", managerRecord.TenantID, managerRecord.EnrollmentID, owner).First(&ownerRecord).Error
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				if err != nil {
					return err
				}
				if ownerRecord.State == dataprotection.ProjectionStateEnrolling {
					pendingOwners = append(pendingOwners, owner)
				}
			}
			if len(pendingOwners) == 0 {
				continue
			}
			var enrollment models.ProtectionEnrollment
			if err := tx.Where("tenant_id = ? AND id = ? AND state IN ?", managerRecord.TenantID, managerRecord.EnrollmentID, []string{models.EnrollmentStateEnrolling, models.EnrollmentStateActive}).First(&enrollment).Error; err != nil {
				return err
			}
			if strings.TrimSpace(enrollment.LatestSourceSnapshotHash) == "" || enrollment.LatestSourceSnapshotHash != managerProjection.SourceSnapshotHash {
				continue
			}
			if err := compileProtectionProjections(tx, enrollment, enrollment.LatestSourceSnapshotHash, now, pendingOwners); err != nil {
				return err
			}
			reconciled += len(pendingOwners)
		}
		return nil
	})
	return reconciled, err
}

func projectionHasAction(projection dataprotection.Projection, action string) bool {
	for _, rule := range projection.Rules {
		if rule.Action == action {
			return true
		}
	}
	return false
}
