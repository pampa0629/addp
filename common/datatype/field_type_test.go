package datatype

import "testing"

func TestFieldTypeHelpers(t *testing.T) {
	if got := ParseFieldType(" geometry "); got != FieldTypeGeometry {
		t.Fatalf("ParseFieldType() = %q, want %q", got, FieldTypeGeometry)
	}
	if !IsNumericFieldType(FieldTypeDecimal) || IsNumericFieldType(FieldTypeString) {
		t.Fatalf("numeric field type helper returned unexpected result")
	}
	if !IsTemporalFieldType(FieldTypeTimestamp) || IsTemporalFieldType(FieldTypeUUID) {
		t.Fatalf("temporal field type helper returned unexpected result")
	}
	if !IsSpatialFieldType(FieldTypeGeometry) || IsSpatialFieldType(FieldTypeJSON) {
		t.Fatalf("spatial field type helper returned unexpected result")
	}
	if !IsSemiStructuredFieldType(FieldTypeArray) || IsSemiStructuredFieldType(FieldTypeBytes) {
		t.Fatalf("semi-structured field type helper returned unexpected result")
	}
}
