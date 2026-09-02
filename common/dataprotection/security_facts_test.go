package dataprotection

import (
	"testing"
	"time"

	"github.com/addp/common/datatype"
)

func TestDataItemSecurityFactsValidate(t *testing.T) {
	fields := []datatype.FieldInfo{{Name: "phone", Path: []string{"phone"}, Type: datatype.FieldTypeString}}
	hash, err := TableSchemaSnapshotHash(fields)
	if err != nil {
		t.Fatal(err)
	}
	facts := DataItemSecurityFacts{
		SchemaVersion: DataItemSecurityFactsSchemaV1, ItemFingerprint: "fingerprint-1",
		ItemType: "table", Fields: fields, SourceSnapshotHash: hash, ObservedAt: time.Now().UTC(),
	}
	if err := facts.Validate(); err != nil {
		t.Fatal(err)
	}
	facts.SourceSnapshotHash = "sha256:stale"
	if err := facts.Validate(); err == nil {
		t.Fatal("stale snapshot hash must fail validation")
	}
}

func TestFieldlessDataItemSecurityFactsRequireEmptyStructureSnapshot(t *testing.T) {
	facts := DataItemSecurityFacts{
		SchemaVersion: DataItemSecurityFactsSchemaV1, ItemFingerprint: "document-1",
		ItemType: "file", Fields: nil, SourceSnapshotHash: "", ObservedAt: time.Now().UTC(),
	}
	if err := facts.Validate(); err != nil {
		t.Fatal(err)
	}
	facts.SourceSnapshotHash = "sha256:not-a-table"
	if err := facts.Validate(); err == nil {
		t.Fatal("fieldless facts accepted a fabricated structure snapshot")
	}
}
