package dataprotection

import (
	"errors"
	"strings"
	"time"

	"github.com/addp/common/datatype"
)

const DataItemSecurityFactsSchemaV1 = "addp.data_item_security_facts/v1"

// DataItemSecurityFacts is the exact, value-free Meta contract consumed by
// Security for one explicitly enrolled DataItem.
type DataItemSecurityFacts struct {
	SchemaVersion      string               `json:"schema_version"`
	ItemFingerprint    string               `json:"item_fingerprint"`
	ItemType           string               `json:"item_type"`
	Fields             []datatype.FieldInfo `json:"fields"`
	SourceSnapshotHash string               `json:"source_snapshot_hash"`
	ObservedAt         time.Time            `json:"observed_at"`
}

func (f DataItemSecurityFacts) Validate() error {
	if f.SchemaVersion != DataItemSecurityFactsSchemaV1 || strings.TrimSpace(f.ItemFingerprint) == "" || strings.TrimSpace(f.ItemType) == "" || f.ObservedAt.IsZero() {
		return errors.New("invalid DataItem security facts identity")
	}
	if len(f.Fields) == 0 {
		if f.SourceSnapshotHash != "" {
			return errors.New("fieldless DataItem security facts must not declare a structure snapshot")
		}
		return nil
	}
	hash, err := TableSchemaSnapshotHash(f.Fields)
	if err != nil {
		return err
	}
	if f.SourceSnapshotHash != hash {
		return errors.New("DataItem security facts snapshot hash mismatch")
	}
	return nil
}
