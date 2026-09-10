package repository

import (
	"fmt"

	"github.com/addp/common/dataprotection"
	"github.com/addp/security/internal/models"
	"gorm.io/gorm"
)

const legacyKeepPrefixSuffixAlgorithmV1 = "addp.mask.keep_prefix_suffix/v1"

// migrateKeepPrefixSuffixAlgorithmV2 rewrites every Security-authored source
// of truth before the v1 runtime contract is removed: baseline definitions,
// current projections and historical change-feed payloads.
func migrateKeepPrefixSuffixAlgorithmV2(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.ProtectionBaseline{}).
			Where("algorithm = ?", legacyKeepPrefixSuffixAlgorithmV1).
			Update("algorithm", dataprotection.AlgorithmKeepPrefixSuffixV2).Error; err != nil {
			return fmt.Errorf("migrate protection baseline mask algorithm: %w", err)
		}

		legacyPayloadPattern := "%" + legacyKeepPrefixSuffixAlgorithmV1 + "%"
		var records []models.ProtectionProjectionRecord
		if err := tx.Where("CAST(projection_payload AS TEXT) LIKE ?", legacyPayloadPattern).Find(&records).Error; err != nil {
			return fmt.Errorf("read protection projection records for mask migration: %w", err)
		}
		for _, record := range records {
			payload, changed, err := migrateProjectionMaskPayload(record.ProjectionPayload)
			if err != nil {
				return fmt.Errorf("migrate protection projection record %s mask algorithm: %w", record.ID, err)
			}
			if changed {
				if err := tx.Model(&record).Update("projection_payload", payload).Error; err != nil {
					return fmt.Errorf("update protection projection record %s mask algorithm: %w", record.ID, err)
				}
			}
		}

		var changes []models.ProtectionProjectionChange
		if err := tx.Where("projection_payload IS NOT NULL AND CAST(projection_payload AS TEXT) LIKE ?", legacyPayloadPattern).Find(&changes).Error; err != nil {
			return fmt.Errorf("read protection projection changes for mask migration: %w", err)
		}
		for _, change := range changes {
			payload, changed, err := migrateProjectionMaskPayload(*change.ProjectionPayload)
			if err != nil {
				return fmt.Errorf("migrate protection projection change %s mask algorithm: %w", change.ChangeID, err)
			}
			if changed {
				if err := tx.Model(&change).Update("projection_payload", payload).Error; err != nil {
					return fmt.Errorf("update protection projection change %s mask algorithm: %w", change.ChangeID, err)
				}
			}
		}
		return nil
	})
}

func migrateProjectionMaskPayload(payload string) (string, bool, error) {
	return migratePersistedProjectionPayloadV2(payload)
}
