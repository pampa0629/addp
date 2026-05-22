package format

import "github.com/addp/common/datatype"

// TableDescribeResultFromSchema converts the format operation schema into the
// common datatype describe result used by metadata attributes.
func TableDescribeResultFromSchema(info *TableInfo) *datatype.TableDescribeResult {
	if info == nil {
		return nil
	}
	result := &datatype.TableDescribeResult{
		Table:        DatatypeTableInfo(info),
		Spatial:      info.SpatialInfo.Clone(),
		ContentIndex: info.ContentIndex.Clone(),
		FormatInfo:   cloneInterfaceMap(info.FormatInfo),
	}
	return result
}

// TableSchemaFromDescribeResult converts a datatype describe result back to
// the format operation schema needed by readers and writers.
func TableSchemaFromDescribeResult(result *datatype.TableDescribeResult) *TableInfo {
	if result == nil {
		return nil
	}
	info := FormatTableInfo(result.Table)
	info.SpatialInfo = result.Spatial.Clone()
	info.FormatInfo = cloneInterfaceMap(result.FormatInfo)
	info.ContentIndex = result.ContentIndex.Clone()
	return info
}

func DatatypeTableInfo(info *TableInfo) *datatype.TableInfo {
	if info == nil {
		return nil
	}
	fields := make([]datatype.FieldInfo, 0, len(info.Fields))
	for i, field := range info.Fields {
		fields = append(fields, datatype.FieldInfo{
			Name:            field.Name,
			Type:            field.Type,
			Nullable:        field.Nullable,
			PrimaryKey:      field.IsPrimaryKey,
			Comment:         field.Comment,
			Size:            field.Size,
			Precision:       field.Size,
			Scale:           field.Precision,
			OrdinalPosition: i + 1,
		})
	}
	return &datatype.TableInfo{
		Name:       info.Name,
		RowCount:   info.RowCount,
		SizeBytes:  info.SizeBytes,
		CreatedAt:  info.CreatedAt,
		UpdatedAt:  info.UpdatedAt,
		Fields:     fields,
		PrimaryKey: append([]string(nil), info.PrimaryKey...),
	}
}

func FormatTableInfo(info *datatype.TableInfo) *TableInfo {
	if info == nil {
		return &TableInfo{}
	}
	fields := make([]FieldInfo, 0, len(info.Fields))
	for _, field := range info.Fields {
		fields = append(fields, FieldInfo{
			Name:         field.Name,
			Type:         field.Type,
			Nullable:     field.Nullable,
			IsPrimaryKey: field.PrimaryKey,
			Comment:      field.Comment,
			Size:         field.Size,
			Precision:    field.Scale,
		})
	}
	return &TableInfo{
		Name:       info.Name,
		RowCount:   info.RowCount,
		SizeBytes:  info.SizeBytes,
		CreatedAt:  info.CreatedAt,
		UpdatedAt:  info.UpdatedAt,
		Fields:     fields,
		PrimaryKey: append([]string(nil), info.PrimaryKey...),
	}
}

func cloneInterfaceMap(values map[string]interface{}) map[string]interface{} {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]interface{}, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
