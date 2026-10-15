package dataprotection

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestProjectionSealAndValidate(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	projection := testProjection(now)
	if err := projection.Seal(); err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	if err := projection.Validate(now.Add(time.Hour)); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	projection.Rules[0].Decision.Parameters["prefix_runes"] = 4
	if err := projection.Validate(now.Add(time.Hour)); err == nil {
		t.Fatal("Validate() error = nil after checksum payload mutation")
	}
}

func TestProtectDocumentMasksStructuredStringsByActualRuneLength(t *testing.T) {
	rule := testProjection(time.Now().UTC()).Rules[0]
	document := map[string]any{"userInfo": map[string]any{"phone": "13661384499"}}
	if err := ProtectDocument(document, "preview", []Rule{rule}, SubjectReference{}); err != nil {
		t.Fatalf("ProtectDocument() error = %v", err)
	}
	userInfo := document["userInfo"].(map[string]any)
	if got := userInfo["phone"]; got != "136****4499" {
		t.Fatalf("masked phone = %#v, want 136****4499", got)
	}

	invalid := map[string]any{"userInfo": map[string]any{"phone": "123"}}
	if err := ProtectDocument(invalid, "preview", []Rule{rule}, SubjectReference{}); err != nil {
		t.Fatalf("ProtectDocument(invalid) error = %v", err)
	}
	if _, exists := invalid["userInfo"].(map[string]any)["phone"]; exists {
		t.Fatal("invalid phone was not suppressed")
	}

	email := map[string]any{"userInfo": map[string]any{"phone": "alice@example.com"}}
	if err := ProtectDocument(email, "preview", []Rule{rule}, SubjectReference{}); err != nil {
		t.Fatalf("ProtectDocument(email) error = %v", err)
	}
	if got := email["userInfo"].(map[string]any)["phone"]; got != "ali**********.com" {
		t.Fatalf("masked email = %#v, want ali**********.com", got)
	}

	unicodeValue := map[string]any{"userInfo": map[string]any{"phone": "中间字符测试甲乙"}}
	if err := ProtectDocument(unicodeValue, "preview", []Rule{rule}, SubjectReference{}); err != nil {
		t.Fatalf("ProtectDocument(unicode) error = %v", err)
	}
	if got := unicodeValue["userInfo"].(map[string]any)["phone"]; got != "中间字*测试甲乙" {
		t.Fatalf("masked unicode value = %#v, want 中间字*测试甲乙", got)
	}
}

func TestProjectionRejectsLegacyStructuredMaskAlgorithm(t *testing.T) {
	projection := testProjection(time.Now().UTC())
	projection.Rules[0].Decision.Algorithm = legacyAlgorithmKeepPrefixSuffixV1
	projection.Rules[0].Decision.Parameters = map[string]any{
		"prefix_runes": 3, "suffix_runes": 4, "replacement": "****",
		"exact_runes": 11, "character_class": "ascii_digit",
	}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := projection.Validate(time.Time{}); err == nil {
		t.Fatal("Validate() accepted the removed v1 structured mask runtime contract")
	}
}

func TestMigrateKeepPrefixSuffixAlgorithmV2ResealsProjection(t *testing.T) {
	projection := testProjection(time.Now().UTC())
	projection.Rules[0].Decision.Algorithm = legacyAlgorithmKeepPrefixSuffixV1
	projection.Rules[0].Decision.Parameters = map[string]any{
		"prefix_runes": 3, "suffix_runes": 4, "replacement": "****",
		"exact_runes": 11, "character_class": "ascii_digit",
	}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	legacyChecksum := projection.Checksum
	changed, err := MigrateKeepPrefixSuffixAlgorithmV2(&projection)
	if err != nil || !changed {
		t.Fatalf("MigrateKeepPrefixSuffixAlgorithmV2() changed=%v error=%v", changed, err)
	}
	decision := projection.Rules[0].Decision
	if decision.Algorithm != AlgorithmKeepPrefixSuffixV2 || decision.Parameters["mask_rune"] != "*" || len(decision.Parameters) != 3 {
		t.Fatalf("migrated decision = %#v", decision)
	}
	if projection.Checksum == legacyChecksum {
		t.Fatal("migration did not reseal the changed projection")
	}
	if err := projection.Validate(time.Time{}); err != nil {
		t.Fatalf("migrated projection validation: %v", err)
	}
}

func TestProtectDocumentTraversesArraysAndFailsClosed(t *testing.T) {
	rule := testProjection(time.Now().UTC()).Rules[0]
	rule.Component.Path = []PathSegment{{Name: "members", Container: "array"}, {Name: "phone", Container: "scalar"}}
	document := map[string]any{"members": []any{map[string]any{"phone": "13661384499"}, map[string]any{"phone": "13501206490"}}}
	if err := ProtectDocument(document, "preview", []Rule{rule}, SubjectReference{}); err != nil {
		t.Fatalf("ProtectDocument() error = %v", err)
	}
	items := document["members"].([]any)
	if got := items[1].(map[string]any)["phone"]; got != "135****6490" {
		t.Fatalf("masked array phone = %#v, want 135****6490", got)
	}

	rule.Decision.InvalidValueEffect = EffectDeny
	invalid := map[string]any{"members": "not-an-array"}
	if err := ProtectDocument(invalid, "preview", []Rule{rule}, SubjectReference{}); !errors.Is(err, ErrDenied) {
		t.Fatalf("ProtectDocument() error = %v, want ErrDenied", err)
	}
}

func TestSubjectScopedAuthorizationAllowsOnlyMatchingUserUntilDeadline(t *testing.T) {
	now := time.Now().UTC()
	rule := testProjection(now).Rules[0]
	rule.Authorizations = []TemporaryAuthorization{{Subject: SubjectReference{Type: "user", ID: "41"}, Effect: EffectAllow, ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour)}}

	plaintext := map[string]any{"userInfo": map[string]any{"phone": "13661384499"}}
	if err := ProtectDocument(plaintext, "preview", []Rule{rule}, SubjectReference{Type: "user", ID: "41"}); err != nil {
		t.Fatalf("ProtectDocument(active exemption) error = %v", err)
	}
	if got := plaintext["userInfo"].(map[string]any)["phone"]; got != "13661384499" {
		t.Fatalf("active exemption phone = %#v", got)
	}

	protected := map[string]any{"userInfo": map[string]any{"phone": "13661384499"}}
	if err := ProtectDocument(protected, "preview", []Rule{rule}, SubjectReference{Type: "user", ID: "42"}); err != nil {
		t.Fatalf("ProtectDocument(other subject) error = %v", err)
	}
	if got := protected["userInfo"].(map[string]any)["phone"]; got != "136****4499" {
		t.Fatalf("other subject phone = %#v, want default mask", got)
	}
}

func TestProjectionRejectsAllowDefaultAndInvalidAuthorization(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	projection := testProjection(now)
	projection.Rules[0].Decision = Decision{Effect: EffectAllow}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := projection.Validate(now); err == nil {
		t.Fatal("Validate() accepted unbounded allow")
	}

	projection = testProjection(now)
	projection.Rules[0].Authorizations = []TemporaryAuthorization{{Subject: SubjectReference{Type: "role", ID: "41"}, Effect: EffectAllow, ValidFrom: now, ValidUntil: now.Add(time.Hour)}}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := projection.Validate(now); err == nil {
		t.Fatal("Validate() accepted non-user authorization")
	}
}

func testProjection(now time.Time) Projection {
	return Projection{
		SchemaVersion: ProjectionSchemaV3,
		ProjectionID:  "projection-1",
		Revision:      "00000000000000000001",
		ConsumerOwner: "manager",
		State:         ProjectionStateActive,
		Target: ResourceReference{
			OwnerModule:      "meta",
			ResourceType:     "data_item",
			ResourceIdentity: "fingerprint-1",
		},
		SourceSnapshotHash: "sha256:snapshot",
		Rules: []Rule{{
			Action: "preview",
			Component: Component{
				Key:               "userInfo.phone",
				Path:              []PathSegment{{Name: "userInfo", Container: "object"}, {Name: "phone", Container: "scalar"}},
				ValueType:         "string",
				SchemaFingerprint: "sha256:schema",
			},
			Decision: Decision{
				Effect:             EffectMask,
				Algorithm:          AlgorithmKeepPrefixSuffixV2,
				Parameters:         map[string]any{"prefix_runes": 3, "suffix_runes": 4, "mask_rune": "*"},
				InvalidValueEffect: EffectSuppress,
			},
		}},
		ValidFrom: now,
		ExpiresAt: ProjectionDeadline(now.Add(24 * time.Hour)),
	}
}

func TestPermanentProjectionRequiresExplicitNullAndKeepsTemporaryAuthorizationBounded(t *testing.T) {
	now := time.Now().UTC()
	projection := testProjection(now)
	projection.ExpiresAt = nil
	projection.Rules[0].Authorizations = []TemporaryAuthorization{{Subject: SubjectReference{Type: "user", ID: "41"}, Effect: EffectAllow, ValidFrom: now, ValidUntil: now.Add(time.Hour)}}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := projection.Validate(now.AddDate(10, 0, 0)); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"expires_at":null`) {
		t.Fatal("permanent projection must encode an explicit null")
	}
	var decoded Projection
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := decoded.Validate(now); err != nil {
		t.Fatal(err)
	}
	if decision := projection.Rules[0].EffectiveDecision(SubjectReference{Type: "user", ID: "41"}, now.Add(2*time.Hour)); decision.Effect != EffectMask {
		t.Fatal("permanent rules must not extend temporary plaintext authorization")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "expires_at")
	missing, _ := json.Marshal(fields)
	if json.Unmarshal(missing, &decoded) == nil {
		t.Fatal("missing deadline silently became permanent")
	}
	projection.SchemaVersion = "addp.protection_projection/v2"
	_ = projection.Seal()
	if projection.Validate(now) == nil {
		t.Fatal("runtime accepted the old protocol")
	}
	projection.SchemaVersion = ProjectionSchemaV3
	projection.ExpiresAt = ProjectionDeadline(time.Time{})
	_ = projection.Seal()
	if projection.Validate(now) == nil {
		t.Fatal("zero deadline treated as permanent")
	}
}

func TestMigrateV2ProjectionPreservesDeadlineAndRejectsCorruptPayload(t *testing.T) {
	projection := testProjection(time.Now().UTC())
	projection.SchemaVersion = "addp.protection_projection/v2"
	_ = projection.Seal()
	payload, _ := json.Marshal(projection)
	migrated, changed, err := MigrateProjectionPayloadV3(payload)
	if err != nil || !changed || migrated.ExpiresAt == nil || !migrated.ExpiresAt.Equal(*projection.ExpiresAt) || migrated.Revision != projection.Revision {
		t.Fatalf("migration changed validity: %#v %v", migrated, err)
	}
	_ = migrated.Seal()
	if err := migrated.Validate(time.Now()); err != nil {
		t.Fatal(err)
	}
	projection.Checksum = "sha256:corrupt"
	payload, _ = json.Marshal(projection)
	if _, _, err := MigrateProjectionPayloadV3(payload); err == nil {
		t.Fatal("corrupt persisted projection accepted")
	}
}
