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

// MigrateProjectionPayloadV2 performs the one-way persisted-data conversion
// from projection schema v1 to v2. It intentionally does not validate or seal
// the result because callers must migrate embedded algorithms before applying
// the current v2 contract and recalculating the checksum.
func MigrateProjectionPayloadV2(payload []byte) (Projection, bool, error) {
	var envelope struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return Projection{}, false, fmt.Errorf("decode protection projection schema: %w", err)
	}
	switch envelope.SchemaVersion {
	case ProjectionSchemaV2:
		var projection Projection
		if err := json.Unmarshal(payload, &projection); err != nil {
			return Projection{}, false, fmt.Errorf("decode v2 protection projection: %w", err)
		}
		return projection, false, nil
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
			SchemaVersion:      ProjectionSchemaV2,
			ProjectionID:       legacy.ProjectionID,
			Revision:           legacy.Revision,
			ConsumerOwner:      legacy.ConsumerOwner,
			State:              legacy.State,
			Target:             legacy.Target,
			SourceSnapshotHash: legacy.SourceSnapshotHash,
			Rules:              rules,
			ValidFrom:          legacy.ValidFrom,
			ExpiresAt:          legacy.ExpiresAt,
		}, true, nil
	default:
		if envelope.SchemaVersion == "" {
			return Projection{}, false, errors.New("protection projection schema_version is required")
		}
		return Projection{}, false, fmt.Errorf("unsupported protection projection schema %q", envelope.SchemaVersion)
	}
}
