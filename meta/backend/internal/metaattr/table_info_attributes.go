package metaattr

import "github.com/addp/common/datatype"

func TableInfoAttributes(info *datatype.TableInfo) map[string]interface{} {
	if info == nil {
		return nil
	}
	attrs := map[string]interface{}{}
	if info.Name != "" {
		attrs["name"] = info.Name
	}
	if info.Kind != "" {
		attrs["kind"] = info.Kind
	}
	if info.Comment != "" {
		attrs["comment"] = info.Comment
	}
	if info.RowCount != nil {
		attrs["row_count"] = *info.RowCount
	}
	if info.SizeBytes != nil {
		attrs["size_bytes"] = *info.SizeBytes
	}
	if info.CreatedAt != nil {
		attrs["created_at"] = info.CreatedAt
	}
	if info.UpdatedAt != nil {
		attrs["updated_at"] = info.UpdatedAt
	}
	if len(info.Fields) > 0 {
		attrs["fields"] = FieldAttributes(info.Fields)
	}
	if len(info.PrimaryKey) > 0 {
		attrs["primary_key"] = append([]string(nil), info.PrimaryKey...)
	}
	if len(info.Native) > 0 {
		attrs["native"] = cloneInterfaceMap(info.Native)
	}
	if len(attrs) == 0 {
		return nil
	}
	return attrs
}
