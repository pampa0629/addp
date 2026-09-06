package dataprotection

import (
	"testing"
	"time"
)

func TestDocumentTextSampleAndProjectionUseSameSnapshot(t *testing.T) {
	now := time.Now().UTC()
	hash, err := DocumentTextSnapshotHash("联系人 13661384499", false)
	if err != nil {
		t.Fatal(err)
	}
	sample := DataItemSecuritySample{
		SchemaVersion: DataItemSecuritySampleSchemaV1, ItemFingerprint: "document-1", ItemType: "document",
		Text: "联系人 13661384499", SourceSnapshotHash: hash, ObservedAt: now,
	}
	if err := sample.Validate(); err != nil {
		t.Fatal(err)
	}
	decision := Decision{Effect: EffectMask, Algorithm: AlgorithmPhoneOccurrencesV1, InvalidValueEffect: EffectSuppress, Parameters: map[string]any{
		"prefix_runes": float64(3), "suffix_runes": float64(4), "replacement": "****", "exact_runes": float64(11), "character_class": "ascii_digit",
	}}
	projection := Projection{
		SchemaVersion: ProjectionSchemaV2, ProjectionID: "projection-1", Revision: "00000000000000000001",
		ConsumerOwner: "manager", State: ProjectionStateActive,
		Target:             ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "document-1"},
		SourceSnapshotHash: hash, Rules: []Rule{{Action: "search_index", Component: DocumentTextComponent(), Decision: decision}},
		ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateDocumentTextProjection(projection, "search_index", sample.Text, sample.Truncated, now); err != nil {
		t.Fatal(err)
	}
}

func TestMaskPhoneOccurrencesMasksOnlyExactASCIIDigitRuns(t *testing.T) {
	decision := Decision{Effect: EffectMask, Algorithm: AlgorithmPhoneOccurrencesV1, Parameters: map[string]any{
		"prefix_runes": float64(3), "suffix_runes": float64(4), "replacement": "****", "exact_runes": float64(11), "character_class": "ascii_digit",
	}}
	masked, err := MaskPhoneOccurrences("daydayup/13661384499/订单123456789012", decision)
	if err != nil {
		t.Fatal(err)
	}
	if masked != "daydayup/136****4499/订单123456789012" {
		t.Fatalf("masked text = %q", masked)
	}
}

func TestDocumentTextProjectionRejectsMismatchedTargetComponent(t *testing.T) {
	now := time.Now().UTC()
	text := "联系电话 13661384499"
	hash, err := DocumentTextSnapshotHash(text, false)
	if err != nil {
		t.Fatal(err)
	}
	decision := Decision{Effect: EffectMask, Algorithm: AlgorithmPhoneOccurrencesV1, InvalidValueEffect: EffectSuppress, Parameters: map[string]any{
		"prefix_runes": 3, "suffix_runes": 4, "replacement": "****", "exact_runes": 11, "character_class": "ascii_digit",
	}}
	projection := Projection{
		SchemaVersion: ProjectionSchemaV2, ProjectionID: "projection-1", Revision: "00000000000000000001",
		ConsumerOwner: "manager", State: ProjectionStateActive,
		Target:             ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "document-1", ComponentKey: "wrong.component"},
		SourceSnapshotHash: hash, Rules: []Rule{{Action: "search_index", Component: DocumentTextComponent(), Decision: decision}},
		ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateDocumentTextProjection(projection, "search_index", text, false, now); err == nil {
		t.Fatal("document projection accepted a mismatched target component")
	}
}
