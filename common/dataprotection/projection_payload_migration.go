package dataprotection

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
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
	Component Component                  `json:"component"`
	Decision  legacyProjectionDecisionV1 `json:"decision"`
}

type legacyProjectionV1 struct {
	SchemaVersion      string                   `json:"schema_version"`
	ProjectionID       string                   `json:"projection_id"`
	Revision           string                   `json:"revision"`
	ConsumerOwner      string                   `json:"consumer_owner"`
	State              string                   `json:"state"`
	Target             ResourceReference        `json:"target"`
	SourceSnapshotHash string                   `json:"source_snapshot_hash"`
	Rules              []legacyProjectionRuleV1 `json:"rules"`
	ValidFrom          time.Time                `json:"valid_from"`
	ExpiresAt          time.Time                `json:"expires_at"`
}

// DecodePersistedProjectionForMigration decodes historical persisted payloads.
// It is never a runtime decoder: Security must bind trusted enrollment ownership,
// upgrade algorithms, seal and validate the current protocol before publishing.
func DecodePersistedProjectionForMigration(payload []byte) (Projection, bool, error) {
	var envelope struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return Projection{}, false, fmt.Errorf("decode protection projection schema: %w", err)
	}
	switch envelope.SchemaVersion {
	case ProjectionSchemaV5, "addp.protection_projection/v4", "addp.protection_projection/v3":
		var projection Projection
		if err := json.Unmarshal(payload, &projection); err != nil {
			return Projection{}, false, fmt.Errorf("decode v3 protection projection: %w", err)
		}
		if envelope.SchemaVersion != ProjectionSchemaV5 {
			for _, rule := range projection.Rules {
				if rule.RoleOptions != nil {
					return Projection{}, false, errors.New("legacy projection cannot carry role options")
				}
			}
			expected, err := projection.CalculateChecksum()
			if err != nil || projection.Checksum != expected {
				return Projection{}, false, errors.New("legacy protection projection checksum mismatch")
			}
		}
		return projection, false, nil
	case "addp.protection_projection/v2":
		var projection Projection
		if err := json.Unmarshal(payload, &projection); err != nil {
			return Projection{}, false, err
		}
		if projection.ExpiresAt == nil || projection.ExpiresAt.IsZero() {
			return Projection{}, false, errors.New("legacy projection deadline is required")
		}
		// Verify legacy integrity before converting the schema and resealing.
		expected, err := projection.CalculateChecksum()
		if err != nil || projection.Checksum != expected {
			return Projection{}, false, errors.New("legacy protection projection checksum mismatch")
		}
		projection.SchemaVersion = "addp.protection_projection/v3"
		return projection, true, nil
	case legacyProjectionSchemaV1:
		var legacy legacyProjectionV1
		if err := json.Unmarshal(payload, &legacy); err != nil {
			return Projection{}, false, fmt.Errorf("decode v1 protection projection: %w", err)
		}
		rules := make([]Rule, 0, len(legacy.Rules))
		for index, legacyRule := range legacy.Rules {
			decision := legacyRule.Decision
			if decision.Effect == EffectAllow {
				if decision.Fallback == nil {
					return Projection{}, false, fmt.Errorf("legacy protection rule %d allow decision has no protective fallback", index)
				}
				decision = *decision.Fallback
				if decision.Effect == EffectAllow {
					return Projection{}, false, fmt.Errorf("legacy protection rule %d allow fallback is not protective", index)
				}
			}
			rules = append(rules, Rule{
				Action:    legacyRule.Action,
				Component: legacyRule.Component,
				Decision: Decision{
					Effect:             decision.Effect,
					Algorithm:          decision.Algorithm,
					Parameters:         decision.Parameters,
					InvalidValueEffect: decision.InvalidValueEffect,
				},
			})
		}
		return Projection{
			SchemaVersion:      "addp.protection_projection/v3",
			ProjectionID:       legacy.ProjectionID,
			Revision:           legacy.Revision,
			ConsumerOwner:      legacy.ConsumerOwner,
			State:              legacy.State,
			Target:             legacy.Target,
			SourceSnapshotHash: legacy.SourceSnapshotHash,
			Rules:              rules,
			ValidFrom:          legacy.ValidFrom,
			ExpiresAt:          ProjectionDeadline(legacy.ExpiresAt),
		}, true, nil
	default:
		if envelope.SchemaVersion == "" {
			return Projection{}, false, errors.New("protection projection schema_version is required")
		}
		return Projection{}, false, fmt.Errorf("unsupported protection projection schema %q", envelope.SchemaVersion)
	}
}
