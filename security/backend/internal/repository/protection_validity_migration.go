package repository

import (
	"fmt"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/security/internal/models"
	"gorm.io/gorm"
)

// The nullable column is the single persisted validity fact. Existing resources
// keep their earliest installed deadline; migration never renews their rules.
func migrateProtectionValidity(db *gorm.DB, existed bool) error {
	if existed {
		return nil
	}
	if db.Dialector.Name() == "sqlite" {
		for _, column := range []string{"protection_expires_at DATETIME", "validity_updated_by INTEGER", "validity_updated_at DATETIME"} {
			if err := db.Exec("ALTER TABLE security.protection_enrollments ADD COLUMN " + column).Error; err != nil {
				return err
			}
		}
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var rows []models.ProtectionEnrollment
		if err := tx.Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			var records []models.ProtectionProjectionRecord
			if err := tx.Where("tenant_id = ? AND enrollment_id = ?", row.TenantID, row.ID).Find(&records).Error; err != nil {
				return err
			}
			if len(records) == 0 {
				return fmt.Errorf("existing enrollment %s has no protection validity evidence", row.ID)
			}
			var earliest *time.Time
			for _, record := range records {
				projection, _, err := dataprotection.MigrateProjectionPayloadV3([]byte(record.ProjectionPayload))
				if err != nil {
					return err
				}
				if projection.ExpiresAt == nil {
					return fmt.Errorf("existing enrollment %s lacks bounded legacy validity", row.ID)
				}
				if earliest == nil || projection.ExpiresAt.Before(*earliest) {
					earliest = projection.ExpiresAt
				}
			}
			if err := tx.Model(&row).Update("protection_expires_at", earliest).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func protectionValidityColumnExists(db *gorm.DB) (bool, error) {
	if db.Dialector.Name() != "sqlite" {
		return db.Migrator().HasColumn(&models.ProtectionEnrollment{}, "protection_expires_at"), nil
	}
	var columns []struct{ Name string }
	if err := db.Raw("PRAGMA security.table_info(protection_enrollments)").Scan(&columns).Error; err != nil {
		return false, err
	}
	for _, column := range columns {
		if column.Name == "protection_expires_at" {
			return true, nil
		}
	}
	return false, nil
}
