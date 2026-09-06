package dataprotection

import (
	"errors"
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

func TestProtectDocumentMasksNestedPhoneAndSuppressesInvalidValue(t *testing.T) {
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

	nonDigit := map[string]any{"userInfo": map[string]any{"phone": "136ABCD4499"}}
	if err := ProtectDocument(nonDigit, "preview", []Rule{rule}, SubjectReference{}); err != nil {
		t.Fatalf("ProtectDocument(non-digit) error = %v", err)
	}
	if _, exists := nonDigit["userInfo"].(map[string]any)["phone"]; exists {
		t.Fatal("non-ASCII-digit phone was not suppressed")
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
		SchemaVersion: ProjectionSchemaV2,
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
				Algorithm:          AlgorithmKeepPrefixSuffixV1,
				Parameters:         map[string]any{"prefix_runes": 3, "suffix_runes": 4, "replacement": "****", "exact_runes": 11, "character_class": "ascii_digit"},
				InvalidValueEffect: EffectSuppress,
			},
		}},
		ValidFrom: now,
		ExpiresAt: now.Add(24 * time.Hour),
	}
}
