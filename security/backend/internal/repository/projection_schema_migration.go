package repository

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/security/internal/models"
	"gorm.io/gorm"
)

// migrateProtectionProjectionSchemaV5 is a one-way data migration. It rewrites
// every persisted non-v5 payload in place so existing feed cursors and sequence
// ordering remain valid. Ownership is verified against Enrollment before the
// new protocol is sealed; missing or inconsistent preimages abort migration.
func migrateProtectionProjectionSchemaV5(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		payloadVersionPredicate := "COALESCE(json_extract(projection_payload, '$.schema_version'), '') <> ?"
		if tx.Dialector.Name() == "postgres" {
			payloadVersionPredicate = "COALESCE(projection_payload ->> 'schema_version', '') <> ?"
		}
		var records []models.ProtectionProjectionRecord
		if err := tx.Where(payloadVersionPredicate, dataprotection.ProjectionSchemaV5).Find(&records).Error; err != nil {
			return fmt.Errorf("find non-current protection projection records: %w", err)
		}
		for _, record := range records {
			payload, _, err := migrateEnrollmentProjectionPayload(tx, record.TenantID, record.EnrollmentID, record.ProjectionPayload)
			if err != nil {
				return fmt.Errorf("migrate protection projection record %s: %w", record.ID, err)
			}
			if err := tx.Model(&record).Update("projection_payload", payload).Error; err != nil {
				return fmt.Errorf("update protection projection record %s: %w", record.ID, err)
			}
		}

		var changes []models.ProtectionProjectionChange
		if err := tx.Where("projection_payload IS NOT NULL AND "+payloadVersionPredicate, dataprotection.ProjectionSchemaV5).Find(&changes).Error; err != nil {
			return fmt.Errorf("find non-current protection projection changes: %w", err)
		}
		for _, change := range changes {
			payload, _, err := migrateEnrollmentProjectionPayload(tx, change.TenantID, change.EnrollmentID, *change.ProjectionPayload)
			if err != nil {
				return fmt.Errorf("migrate protection projection change %s: %w", change.ChangeID, err)
			}
			if err := tx.Model(&change).Update("projection_payload", payload).Error; err != nil {
				return fmt.Errorf("update protection projection change %s: %w", change.ChangeID, err)
			}
		}
		return nil
	})
}

func migrateEnrollmentProjectionPayload(tx *gorm.DB, tenantID int64, enrollmentID, payload string) (string, bool, error) {
	projection, _, err := dataprotection.DecodePersistedProjectionForMigration([]byte(payload))
	if err != nil {
		return "", false, err
	}
	var enrollment models.ProtectionEnrollment
	if err := tx.Where("id = ? AND tenant_id = ?", enrollmentID, tenantID).First(&enrollment).Error; err != nil {
		return "", false, fmt.Errorf("read protection ownership preimage: %w", err)
	}
	if projection.Target != enrollment.Target() {
		return "", false, fmt.Errorf("protection projection target differs from enrollment")
	}
	if err := dataprotection.VerifyTargetEngine(projection.Target, enrollment.TargetEngineID, enrollment.TargetFullName); err != nil {
		return "", false, err
	}
	projection.SchemaVersion = dataprotection.ProjectionSchemaV5
	projection.TargetEngineID = enrollment.TargetEngineID
	if _, err := dataprotection.MigrateKeepPrefixSuffixAlgorithmV2(&projection); err != nil {
		return "", false, err
	}
	if err := projection.Seal(); err != nil {
		return "", false, err
	}
	if err := projection.Validate(time.Time{}); err != nil {
		return "", false, err
	}
	encoded, err := json.Marshal(projection)
	return string(encoded), true, err
}

func migratePersistedProjectionAlgorithms(payload string) (string, bool, error) {
	projection, schemaChanged, err := dataprotection.DecodePersistedProjectionForMigration([]byte(payload))
	if err != nil {
		return "", false, err
	}
	algorithmChanged, err := dataprotection.MigrateKeepPrefixSuffixAlgorithmV2(&projection)
	if err != nil {
		return "", false, err
	}
	if schemaChanged && !algorithmChanged {
		if err := projection.Seal(); err != nil {
			return "", false, err
		}
	}
	if err := projection.Validate(time.Time{}); err != nil {
		return "", false, err
	}
	if !schemaChanged && !algorithmChanged {
		return payload, false, nil
	}
	encoded, err := json.Marshal(projection)
	if err != nil {
		return "", false, err
	}
	return string(encoded), true, nil
}
