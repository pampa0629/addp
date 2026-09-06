package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/security/internal/models"
	"gorm.io/gorm"
)

const legacyProjectionSchemaV1 = "addp.protection_projection/v1"

type legacyProjectionDecisionV1 struct {
	Effect             string                      `json:"effect"`
	Algorithm          string                      `json:"algorithm,omitempty"`
	Parameters         map[string]any              `json:"parameters,omitempty"`
	InvalidValueEffect string                      `json:"invalid_value_effect,omitempty"`
	ValidUntil         *time.Time                  `json:"valid_until,omitempty"`
	Fallback           *legacyProjectionDecisionV1 `json:"fallback,omitempty"`
}

type legacyProjectionRuleV1 struct {
	Action    string                     `json:"action"`
	Component dataprotection.Component   `json:"component"`
	Decision  legacyProjectionDecisionV1 `json:"decision"`
}

type legacyProjectionV1 struct {
	SchemaVersion      string                           `json:"schema_version"`
	ProjectionID       string                           `json:"projection_id"`
	Revision           string                           `json:"revision"`
	ConsumerOwner      string                           `json:"consumer_owner"`
	State              string                           `json:"state"`
	Target             dataprotection.ResourceReference `json:"target"`
	SourceSnapshotHash string                           `json:"source_snapshot_hash"`
	Rules              []legacyProjectionRuleV1         `json:"rules"`
	ValidFrom          time.Time                        `json:"valid_from"`
	ExpiresAt          time.Time                        `json:"expires_at"`
}

// migrateProtectionProjectionSchemaV2 is a one-way data migration. It rewrites
// every persisted v1 payload in place so existing feed cursors and sequence
// ordering remain valid. Legacy tenant-wide allow decisions are deliberately
// reduced to their protective fallback; subject grants can only be recreated
// through the v2 request and approval flow.
func migrateProtectionProjectionSchemaV2(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		payloadVersionPredicate := "json_extract(projection_payload, '$.schema_version') = ?"
		if tx.Dialector.Name() == "postgres" {
			payloadVersionPredicate = "projection_payload ->> 'schema_version' = ?"
		}
		var records []models.ProtectionProjectionRecord
		if err := tx.Where(payloadVersionPredicate, legacyProjectionSchemaV1).Find(&records).Error; err != nil {
			return fmt.Errorf("find legacy protection projection records: %w", err)
		}
		for _, record := range records {
			payload, err := migrateProtectionProjectionPayloadV2(record.ProjectionPayload)
			if err != nil {
				return fmt.Errorf("migrate protection projection record %s: %w", record.ID, err)
			}
			if err := tx.Model(&record).Update("projection_payload", payload).Error; err != nil {
				return fmt.Errorf("update protection projection record %s: %w", record.ID, err)
			}
		}

		var changes []models.ProtectionProjectionChange
		if err := tx.Where("projection_payload IS NOT NULL AND "+payloadVersionPredicate, legacyProjectionSchemaV1).Find(&changes).Error; err != nil {
			return fmt.Errorf("find legacy protection projection changes: %w", err)
		}
		for _, change := range changes {
			payload, err := migrateProtectionProjectionPayloadV2(*change.ProjectionPayload)
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

func migrateProtectionProjectionPayloadV2(payload string) (string, error) {
	var legacy legacyProjectionV1
	if err := json.Unmarshal([]byte(payload), &legacy); err != nil {
		return "", err
	}
	if legacy.SchemaVersion != legacyProjectionSchemaV1 {
		return "", errors.New("payload is not a v1 protection projection")
	}
	rules := make([]dataprotection.Rule, 0, len(legacy.Rules))
	for _, legacyRule := range legacy.Rules {
		decision := legacyRule.Decision
		if decision.Effect == dataprotection.EffectAllow {
			if decision.Fallback == nil {
				return "", errors.New("legacy allow decision has no protective fallback")
			}
			decision = *decision.Fallback
		}
		rules = append(rules, dataprotection.Rule{
			Action:    legacyRule.Action,
			Component: legacyRule.Component,
			Decision: dataprotection.Decision{
				Effect: decision.Effect, Algorithm: decision.Algorithm,
				Parameters: decision.Parameters, InvalidValueEffect: decision.InvalidValueEffect,
			},
		})
	}
	projection := dataprotection.Projection{
		SchemaVersion: dataprotection.ProjectionSchemaV2,
		ProjectionID:  legacy.ProjectionID, Revision: legacy.Revision,
		ConsumerOwner: legacy.ConsumerOwner, State: legacy.State,
		Target: legacy.Target, SourceSnapshotHash: legacy.SourceSnapshotHash,
		Rules: rules, ValidFrom: legacy.ValidFrom, ExpiresAt: legacy.ExpiresAt,
	}
	if err := projection.Seal(); err != nil {
		return "", err
	}
	if err := projection.Validate(time.Time{}); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(projection)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
