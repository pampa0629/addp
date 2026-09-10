package dataprotection

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestMigrateProjectionPayloadV2ConvertsLegacyAllowToProtectiveFallback(t *testing.T) {
	now := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	legacy := map[string]any{
		"schema_version": legacyProjectionSchemaV1,
		"projection_id":  "11111111-1111-1111-1111-111111111111",
		"revision":       "00000000000000000003",
		"consumer_owner": "manager",
		"state":          ProjectionStateActive,
		"target": map[string]any{
			"owner_module": "meta", "resource_type": "data_item", "resource_identity": "sha256:target",
		},
		"source_snapshot_hash": "sha256:snapshot",
		"rules": []any{map[string]any{
			"action": "preview",
			"component": map[string]any{
				"key": "phone", "path": []any{map[string]any{"name": "phone", "container": "scalar"}},
				"value_type": "string", "schema_fingerprint": "sha256:schema",
			},
			"decision": map[string]any{
				"effect": EffectAllow, "valid_until": now.Add(time.Hour),
				"fallback": map[string]any{
					"effect": EffectMask, "algorithm": legacyAlgorithmKeepPrefixSuffixV1,
					"parameters": map[string]any{
						"prefix_runes": 3, "suffix_runes": 4, "replacement": "****",
						"exact_runes": 11, "character_class": "ascii_digit",
					},
					"invalid_value_effect": EffectSuppress,
				},
			},
		}},
		"valid_from": now.Add(-time.Hour), "expires_at": now.Add(24 * time.Hour),
		"checksum": "sha256:legacy",
	}
	payload, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}

	projection, changed, err := MigrateProjectionPayloadV2(payload)
	if err != nil || !changed {
		t.Fatalf("MigrateProjectionPayloadV2() changed=%v error=%v", changed, err)
	}
	if projection.SchemaVersion != ProjectionSchemaV2 || projection.Checksum != "" {
		t.Fatalf("migrated projection schema=%q checksum=%q", projection.SchemaVersion, projection.Checksum)
	}
	decision := projection.Rules[0].Decision
	if decision.Effect != EffectMask || decision.Algorithm != legacyAlgorithmKeepPrefixSuffixV1 || len(projection.Rules[0].Authorizations) != 0 {
		t.Fatalf("migrated legacy decision = %#v", projection.Rules[0])
	}
	algorithmChanged, err := MigrateKeepPrefixSuffixAlgorithmV2(&projection)
	if err != nil || !algorithmChanged {
		t.Fatalf("MigrateKeepPrefixSuffixAlgorithmV2() changed=%v error=%v", algorithmChanged, err)
	}
	if err := projection.Validate(time.Time{}); err != nil {
		t.Fatalf("Validate() migrated projection: %v", err)
	}
}

func TestMigrateProjectionPayloadV2LeavesCurrentProjectionUnvalidated(t *testing.T) {
	projection := testProjection(time.Now().UTC())
	projection.Rules[0].Decision.Algorithm = legacyAlgorithmKeepPrefixSuffixV1
	projection.Rules[0].Decision.Parameters = map[string]any{
		"prefix_runes": 3, "suffix_runes": 4, "replacement": "****",
		"exact_runes": 11, "character_class": "ascii_digit",
	}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}

	decoded, changed, err := MigrateProjectionPayloadV2(payload)
	if err != nil || changed {
		t.Fatalf("MigrateProjectionPayloadV2() changed=%v error=%v", changed, err)
	}
	if decoded.Rules[0].Decision.Algorithm != legacyAlgorithmKeepPrefixSuffixV1 {
		t.Fatalf("decoded algorithm = %q", decoded.Rules[0].Decision.Algorithm)
	}
}

func TestMigrateProjectionPayloadV2RejectsUnsafeOrUnknownLegacyPayload(t *testing.T) {
	for name, testCase := range map[string]struct {
		payload string
		message string
	}{
		"allow without fallback": {
			payload: `{"schema_version":"addp.protection_projection/v1","rules":[{"decision":{"effect":"allow"}}]}`,
			message: "has no protective fallback",
		},
		"allow fallback": {
			payload: `{"schema_version":"addp.protection_projection/v1","rules":[{"decision":{"effect":"allow","fallback":{"effect":"allow"}}}]}`,
			message: "fallback is not protective",
		},
		"missing schema": {payload: `{}`, message: "schema_version is required"},
		"unknown schema": {payload: `{"schema_version":"addp.protection_projection/v9"}`, message: "unsupported protection projection schema"},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := MigrateProjectionPayloadV2([]byte(testCase.payload))
			if err == nil || !strings.Contains(err.Error(), testCase.message) {
				t.Fatalf("MigrateProjectionPayloadV2() error=%v, want %q", err, testCase.message)
			}
		})
	}
}
