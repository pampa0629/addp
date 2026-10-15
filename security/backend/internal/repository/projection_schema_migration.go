package repository

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/security/internal/models"
	"gorm.io/gorm"
)

// migrateProtectionProjectionSchemaV3 is a one-way data migration. It rewrites
// every persisted non-v3 payload in place so existing feed cursors and sequence
// ordering remain valid. The shared converter owns legacy protocol semantics;
// Security only orchestrates migration of its central persisted facts.
func migrateProtectionProjectionSchemaV3(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		payloadVersionPredicate := "COALESCE(json_extract(projection_payload, '$.schema_version'), '') <> ?"
		if tx.Dialector.Name() == "postgres" {
			payloadVersionPredicate = "COALESCE(projection_payload ->> 'schema_version', '') <> ?"
		}
		var records []models.ProtectionProjectionRecord
		if err := tx.Where(payloadVersionPredicate, dataprotection.ProjectionSchemaV3).Find(&records).Error; err != nil {
			return fmt.Errorf("find non-current protection projection records: %w", err)
		}
		for _, record := range records {
			payload, _, err := migratePersistedProjectionPayloadV3(record.ProjectionPayload)
			if err != nil {
				return fmt.Errorf("migrate protection projection record %s: %w", record.ID, err)
			}
			if err := tx.Model(&record).Update("projection_payload", payload).Error; err != nil {
				return fmt.Errorf("update protection projection record %s: %w", record.ID, err)
			}
		}

		var changes []models.ProtectionProjectionChange
		if err := tx.Where("projection_payload IS NOT NULL AND "+payloadVersionPredicate, dataprotection.ProjectionSchemaV3).Find(&changes).Error; err != nil {
			return fmt.Errorf("find non-current protection projection changes: %w", err)
		}
		for _, change := range changes {
			payload, _, err := migratePersistedProjectionPayloadV3(*change.ProjectionPayload)
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

func migratePersistedProjectionPayloadV3(payload string) (string, bool, error) {
	projection, schemaChanged, err := dataprotection.MigrateProjectionPayloadV3([]byte(payload))
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
