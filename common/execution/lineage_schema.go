package execution

import (
	"fmt"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
)

func NewLineageSchemaSnapshot(fields []datatype.FieldInfo) (*LineageSchemaSnapshot, error) {
	hash, err := dataprotection.TableSchemaSnapshotHash(fields)
	if err != nil {
		return nil, err
	}
	copy := (&datatype.TableInfo{Fields: fields}).Clone()
	return &LineageSchemaSnapshot{Hash: hash, Fields: copy.Fields}, nil
}

func (s *LineageSchemaSnapshot) Validate() error {
	if s == nil {
		return fmt.Errorf("lineage schema snapshot is required")
	}
	hash, err := dataprotection.TableSchemaSnapshotHash(s.Fields)
	if err != nil {
		return err
	}
	if s.Hash != hash {
		return fmt.Errorf("lineage schema snapshot hash mismatch")
	}
	return nil
}

func (s *LineageSchemaSnapshot) HasField(name string) bool {
	if s == nil {
		return false
	}
	for _, field := range s.Fields {
		if field.Name == name && (len(field.Path) == 0 || (len(field.Path) == 1 && field.Path[0] == name)) {
			return true
		}
	}
	return false
}
