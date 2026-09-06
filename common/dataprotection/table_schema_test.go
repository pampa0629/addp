package dataprotection

import (
	"testing"
	"time"

	"github.com/addp/common/datatype"
)

func TestTableSchemaSnapshotHashIsStableAcrossDisplayFactsAndFieldOrder(t *testing.T) {
	left := outdoorPersonFields()
	right := []datatype.FieldInfo{
		{Name: "userInfo.phone", Path: []string{"userInfo", "phone"}, Type: datatype.FieldTypeString, Nullable: true, Comment: "changed", NativeType: "varchar", OrdinalPosition: 99},
		{Name: "_id", Type: datatype.FieldTypeString},
		{Name: "userInfo", Path: []string{"userInfo"}, Type: datatype.FieldTypeJSON, Nullable: true, NativeType: "document"},
	}
	leftHash, err := TableSchemaSnapshotHash(left)
	if err != nil {
		t.Fatal(err)
	}
	rightHash, err := TableSchemaSnapshotHash(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftHash != rightHash {
		t.Fatalf("schema hashes differ: %q / %q", leftHash, rightHash)
	}
}

func TestValidateTableProjectionAcceptsOutdoorPhoneAndRejectsDrift(t *testing.T) {
	fields := outdoorPersonFields()
	component := Component{
		Key: "userInfo.phone",
		Path: []PathSegment{
			{Name: "userInfo", Container: "object"},
			{Name: "phone", Container: "scalar"},
		},
		ValueType: "string",
	}
	componentFingerprint, err := ComponentSchemaFingerprint(fields, component)
	if err != nil {
		t.Fatal(err)
	}
	component.SchemaFingerprint = componentFingerprint
	snapshotHash, err := TableSchemaSnapshotHash(fields)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	projection := Projection{
		SchemaVersion: ProjectionSchemaV2, ProjectionID: "projection-1", Revision: "00000000000000000001",
		ConsumerOwner: "manager", State: ProjectionStateActive,
		Target:             ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "sha256:item", ComponentKey: "userInfo.phone"},
		SourceSnapshotHash: snapshotHash,
		Rules: []Rule{{
			Action: "preview", Component: component,
			Decision: Decision{Effect: EffectMask, Algorithm: AlgorithmKeepPrefixSuffixV1, Parameters: map[string]any{"prefix_runes": 3, "suffix_runes": 4, "replacement": "****", "exact_runes": 11, "character_class": "ascii_digit"}, InvalidValueEffect: EffectSuppress},
		}},
		ValidFrom: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
	}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	rules, err := ValidateTableProjection(projection, "preview", fields, now)
	if err != nil || len(rules) != 1 {
		t.Fatalf("ValidateTableProjection() rules=%#v error=%v", rules, err)
	}

	drifted := outdoorPersonFields()
	drifted[2].Type = datatype.FieldTypeBigInt
	if _, err := ValidateTableProjection(projection, "preview", drifted, now); err == nil {
		t.Fatal("ValidateTableProjection() accepted schema drift")
	}
}

func TestComponentSchemaFingerprintRejectsContainerMismatch(t *testing.T) {
	component := Component{
		Key: "userInfo.phone", ValueType: "string",
		Path: []PathSegment{{Name: "userInfo", Container: "array"}, {Name: "phone", Container: "scalar"}},
	}
	if _, err := ComponentSchemaFingerprint(outdoorPersonFields(), component); err == nil {
		t.Fatal("ComponentSchemaFingerprint() accepted an object as an array")
	}
}

func outdoorPersonFields() []datatype.FieldInfo {
	return []datatype.FieldInfo{
		{Name: "_id", Type: datatype.FieldTypeString},
		{Name: "userInfo", Path: []string{"userInfo"}, Type: datatype.FieldTypeJSON, Nullable: true},
		{Name: "userInfo.phone", Path: []string{"userInfo", "phone"}, Type: datatype.FieldTypeString, Nullable: true},
	}
}
