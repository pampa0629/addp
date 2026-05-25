package plugin

import "github.com/addp/common/datatype"

// ItemMetadataTableInfo returns table facts for a table-shaped item metadata.
func ItemMetadataTableInfo(metadata *ItemMetadata) *datatype.TableInfo {
	if metadata == nil {
		return nil
	}
	if metadata.Table != nil {
		return metadata.Table.Clone()
	}
	return nil
}

// ItemMetadataFields returns item fields, preferring table facts when present.
func ItemMetadataFields(metadata *ItemMetadata) []datatype.FieldInfo {
	info := ItemMetadataTableInfo(metadata)
	if info != nil {
		return append([]datatype.FieldInfo(nil), info.Fields...)
	}
	if metadata == nil || len(metadata.Fields) == 0 {
		return nil
	}
	return append([]datatype.FieldInfo(nil), metadata.Fields...)
}
